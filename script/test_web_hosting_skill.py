from __future__ import annotations

import base64
import json
import os
import runpy
import shutil
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock
import urllib.parse
import zipfile
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
MANAGE = REPOSITORY_ROOT / "skills" / "home-server-web-share" / "scripts" / "manage.py"
ACCESS_KEY = "ak_cq_" + "A" * 22
SECRET_KEY = "sk_cq_" + "S" * 43
ACCESS_TOKEN = "at_cq_" + "T" * 43


@dataclass
class ResponseSpec:
    status: int
    body: object
    headers: dict[str, str] | None = None
    delay: float = 0


class LocalHTTPSServer(ThreadingHTTPServer):
    daemon_threads = True

    def handle_error(self, request, client_address):
        # A client-side read timeout deliberately closes TLS before the delayed
        # fixture responds. That disconnect is the expected behavior under test.
        error = sys.exc_info()[1]
        if isinstance(error, (BrokenPipeError, ConnectionResetError, ssl.SSLError)):
            return
        self.handler_errors.append(error)

    def __init__(self, certificate: Path, key: Path, responder):
        super().__init__(("localhost", 0), RecordingHandler)
        self.calls = []
        self.handler_errors = []
        self.responder = responder
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(certificate, key)
        self.socket = context.wrap_socket(self.socket, server_side=True)
        self.thread = threading.Thread(target=self.serve_forever, daemon=True)

    @property
    def origin(self):
        return f"https://localhost:{self.server_address[1]}"

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, exc_type, exc, traceback):
        self.shutdown()
        self.server_close()
        self.thread.join(timeout=2)
        if exc_type is None and self.handler_errors:
            raise AssertionError(f"HTTPS fixture handler failed: {self.handler_errors!r}")


class RecordingHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    # 处理本地 HTTPS 夹具的 GET 请求，交由统一记录器保存调用并生成测试响应。
    def do_GET(self):
        self._handle()

    # 处理本地 HTTPS 夹具的 POST 请求，让测试核对 Token 交换和业务写入的原始请求。
    def do_POST(self):
        self._handle()

    # 处理本地 HTTPS 夹具的 PATCH 请求，复用请求记录与场景响应逻辑。
    def do_PATCH(self):
        self._handle()

    # 记录本地 HTTPS 测试请求，再按场景生成状态、头部和延迟响应；忽略客户端超时断连造成的回写错误。
    def _handle(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else b""
        request = {
            "method": self.command,
            "path": self.path,
            "headers": {name.lower(): value for name, value in self.headers.items()},
            "body": body,
        }
        self.server.calls.append(request)
        spec = self.server.responder(request)
        if spec.delay:
            time.sleep(spec.delay)
        payload = spec.body if isinstance(spec.body, bytes) else json.dumps(spec.body).encode("utf-8")
        self.send_response(spec.status)
        headers = dict(spec.headers or {})
        headers.setdefault("Content-Type", "application/json")
        headers.setdefault("Content-Length", str(len(payload)))
        for name, value in headers.items():
            self.send_header(name, value)
        self.end_headers()
        try:
            self.wfile.write(payload)
        except (BrokenPipeError, ConnectionResetError, ssl.SSLError):
            pass

    def log_message(self, format, *args):
        pass


def success(data, status=200):
    return ResponseSpec(status, {"code": 0, "message": "success", "data": data})


def token_response():
    return ResponseSpec(200, {
        "access_token": ACCESS_TOKEN,
        "token_type": "Bearer",
        "expires_in": 900,
        "scope": "web-projects:read web-projects:write",
    })


class WebHostingSkillEndToEndTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.certificate_directory = tempfile.TemporaryDirectory()
        cls.certificate_root = Path(cls.certificate_directory.name)
        cls.ca_file = cls.certificate_root / "ca.pem"
        cls.server_certificate = cls.certificate_root / "server.pem"
        cls.server_key = cls.certificate_root / "server.key"
        cls._generate_certificates()

    @classmethod
    def tearDownClass(cls):
        cls.certificate_directory.cleanup()

    @classmethod
    # 在临时目录生成短期测试 CA 和 localhost/127.0.0.1 服务证书，供真实 HTTPS 夹具使用，不读取部署证书。
    def _generate_certificates(cls):
        ca_key = cls.certificate_root / "ca.key"
        request = cls.certificate_root / "server.csr"
        extension = cls.certificate_root / "server.ext"
        extension.write_text(
            "subjectAltName=DNS:localhost,IP:127.0.0.1\n"
            "extendedKeyUsage=serverAuth\n",
            encoding="utf-8",
        )
        commands = [
            [
                "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                "-keyout", str(ca_key), "-out", str(cls.ca_file), "-days", "1",
                "-subj", "/CN=home-server-test-ca",
                "-addext", "basicConstraints=critical,CA:TRUE",
                "-addext", "keyUsage=critical,keyCertSign,cRLSign",
            ],
            [
                "openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes",
                "-keyout", str(cls.server_key), "-out", str(request),
                "-subj", "/CN=localhost",
            ],
            [
                "openssl", "x509", "-req", "-in", str(request),
                "-CA", str(cls.ca_file), "-CAkey", str(ca_key), "-CAcreateserial",
                "-out", str(cls.server_certificate), "-days", "1",
                "-extfile", str(extension),
            ],
        ]
        for command in commands:
            subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def setUp(self):
        self.fixture_directory = tempfile.TemporaryDirectory()
        self.fixture_root = Path(self.fixture_directory.name)
        self.other_cwd = self.fixture_root / "unrelated-cwd"
        self.other_cwd.mkdir()

    def tearDown(self):
        self.fixture_directory.cleanup()

    # 为单个用例生成私有配置和可选合成凭证，复制测试 CA，并把相对路径限定在当前临时夹具目录。
    def write_config(
        self,
        internal_url,
        external_url=None,
        *,
        endpoint="auto",
        timeout=1,
        connect_timeout=0.2,
        with_credentials=True,
    ):
        shutil.copy2(self.ca_file, self.fixture_root / "ca.pem")
        payload = {
            "internal_url": internal_url,
            "external_url": external_url or internal_url,
            "ca_file": "ca.pem",
            "endpoint": endpoint,
            "connect_timeout": connect_timeout,
            "timeout": timeout,
        }
        if with_credentials:
            credentials = self.fixture_root / "credentials.json"
            credentials.write_text(json.dumps({
                "access_key": ACCESS_KEY,
                "secret_key": SECRET_KEY,
            }), encoding="utf-8")
            credentials.chmod(0o600)
            payload["credentials_file"] = "credentials.json"
        config = self.fixture_root / "config.json"
        config.write_text(json.dumps(payload), encoding="utf-8")
        config.chmod(0o600)
        return config

    def run_cli(self, config, *arguments, timeout=6, extra_environment=None):
        return self.run_script(
            MANAGE,
            config,
            *arguments,
            timeout=timeout,
            extra_environment=extra_environment,
        )

    def run_script(self, script, config, *arguments, timeout=6, extra_environment=None):
        environment = os.environ.copy()
        environment.pop("CQ_HOME_SERVER_ACCESS_KEY", None)
        environment.pop("CQ_HOME_SERVER_SECRET_KEY", None)
        environment.pop("CQ_HOME_SERVER_BASE_URL", None)
        environment["PYTHONDONTWRITEBYTECODE"] = "1"
        environment.update(extra_environment or {})
        return subprocess.run(
            [sys.executable, str(script), "--config", str(config), *arguments],
            cwd=self.other_cwd,
            env=environment,
            text=True,
            capture_output=True,
            timeout=timeout,
        )

    def assert_success(self, completed, endpoint, origin, expected_result):
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stderr, "")
        payload = json.loads(completed.stdout)
        self.assertEqual(payload, {
            "endpoint": endpoint,
            "origin": origin,
            "result": expected_result,
        })

    def assert_json_error(self, completed):
        self.assertNotEqual(completed.returncode, 0, completed.stdout)
        self.assertEqual(completed.stdout, "")
        payload = json.loads(completed.stderr)
        self.assertIsInstance(payload, dict)
        self.assertTrue(payload)
        return payload

    # 从无关工作目录运行 CLI 并访问本地 HTTPS 夹具，逐项核对命令路由、认证、multipart、幂等键、修订号和可见范围载荷。
    def test_commands_use_real_https_from_an_unrelated_working_directory(self):
        def responder(request):
            parsed = urllib.parse.urlsplit(request["path"])
            if request["method"] == "POST" and parsed.path == "/api/auth/token":
                self.assertEqual(request["body"], b"grant_type=client_credentials")
                scheme, encoded = request["headers"]["authorization"].split(" ", 1)
                self.assertEqual(scheme, "Basic")
                self.assertEqual(base64.b64decode(encoded), f"{ACCESS_KEY}:{SECRET_KEY}".encode())
                return token_response()
            self.assertEqual(request["headers"].get("authorization"), f"Bearer {ACCESS_TOKEN}")
            result = {"operation": f"{request['method']} {parsed.path}"}
            if request["method"] == "GET" and parsed.path == "/api/web-share/12":
                result["container_mode"] = "enhanced"
            return success(result, 201 if parsed.path.endswith("/releases") and request["method"] == "POST" else 200)

        with LocalHTTPSServer(self.server_certificate, self.server_key, responder) as server:
            config = self.write_config(server.origin, endpoint="internal")
            upload = self.fixture_root / "site.zip"
            with zipfile.ZipFile(upload, "w") as archive:
                archive.writestr(
                    "public/index.html",
                    '<!doctype html><html data-hs-page-id="fixture-page"><head><meta charset="utf-8">'
                    '<title>Fixture</title></head><body><main data-hs-comment-root>'
                    '<section data-hs-comment-id="fixture-section" data-hs-comment-kind="text">'
                    'offline-site-marker</section></main></body></html>',
                )
            cases = [
                (("--endpoint", "internal", "list", "--cursor", "11", "--limit", "7", "--status", "enabled"), "GET /api/web-share"),
                (("--endpoint", "internal", "show", "12"), "GET /api/web-share/12"),
                (("--endpoint", "internal", "users"), "GET /api/web-share/eligible-users"),
                (("--endpoint", "internal", "releases", "12", "--cursor", "5", "--limit", "6"), "GET /api/web-share/12/releases"),
                (("--endpoint", "internal", "create", "--name", "Fixture", "--slug", "fixture-site", "--description", "fixture description", "--request-id", "create-1"), "POST /api/web-share"),
                (("--endpoint", "internal", "upload", "12", "--file", str(upload), "--entry-file", "public/index.html", "--request-id", "upload-1"), "POST /api/web-share/12/releases"),
                (("--endpoint", "internal", "publish", "12", "--release", "31", "--revision", "2"), "POST /api/web-share/12/publish"),
                (("--endpoint", "internal", "visibility", "12", "--mode", "members", "--member", "201", "--member", "202", "--revision", "3"), "PATCH /api/web-share/12"),
                (("--endpoint", "internal", "disable", "12", "--revision", "4"), "POST /api/web-share/12/disable"),
                (("--endpoint", "internal", "visibility", "12", "--mode", "owner", "--revision", "5"), "PATCH /api/web-share/12"),
                (("--endpoint", "internal", "visibility", "12", "--mode", "authenticated", "--revision", "6"), "PATCH /api/web-share/12"),
                (("--endpoint", "internal", "visibility", "12", "--mode", "public", "--revision", "7"), "PATCH /api/web-share/12"),
                (("--endpoint", "internal", "visibility", "12", "--mode", "members", "--clear-members", "--revision", "8"), "PATCH /api/web-share/12"),
            ]
            for arguments, operation in cases:
                with self.subTest(command=arguments[-1] if arguments else ""):
                    completed = self.run_cli(config, *arguments)
                    expected = {"operation": operation}
                    if operation == "GET /api/web-share/12":
                        expected["container_mode"] = "enhanced"
                    if operation != "POST /api/web-share/12/releases":
                        self.assert_success(completed, "internal", server.origin, expected)
                        continue
                    self.assertEqual(completed.returncode, 0, completed.stderr)
                    self.assertEqual(completed.stderr, "")
                    payload = json.loads(completed.stdout)
                    self.assertEqual(payload["endpoint"], "internal")
                    self.assertEqual(payload["origin"], server.origin)
                    self.assertEqual(payload["result"], expected)
                    self.assertEqual(payload["html_check"]["status"], "passed")
                    self.assertEqual(payload["html_check"]["mode"], "enhanced")
                    self.assertEqual(payload["html_check"]["errors"], 0)

        business = [call for call in server.calls if call["path"] != "/api/auth/token"]
        self.assertEqual(len([call for call in server.calls if call["path"] == "/api/auth/token"]), len(cases))
        self.assertEqual(urllib.parse.parse_qs(urllib.parse.urlsplit(business[0]["path"]).query), {
            "cursor": ["11"], "limit": ["7"], "status": ["enabled"],
        })
        self.assertEqual(urllib.parse.parse_qs(urllib.parse.urlsplit(business[3]["path"]).query), {
            "cursor": ["5"], "limit": ["6"],
        })
        self.assertEqual(json.loads(business[4]["body"]), {
            "name": "Fixture",
            "description": "fixture description",
            "slug": "fixture-site",
            "access_mode": "owner",
            "member_user_ids": [],
            "client_request_id": "create-1",
        })
        self.assertEqual(business[5]["method"], "GET")
        self.assertEqual(business[5]["path"], "/api/web-share/12")
        self.assertEqual(business[6]["headers"].get("idempotency-key"), "upload-1")
        self.assertIn(b'name="entry_file"\r\n\r\npublic/index.html', business[6]["body"])
        self.assertIn(b'name="file"; filename="site.zip"', business[6]["body"])
        self.assertIn(b"offline-site-marker", business[6]["body"])
        self.assertEqual(business[7]["headers"].get("if-match"), "2")
        self.assertEqual(json.loads(business[7]["body"]), {"release_id": "31"})
        self.assertEqual(business[8]["headers"].get("if-match"), "3")
        self.assertEqual(json.loads(business[8]["body"]), {
            "access_mode": "members",
            "member_user_ids": ["201", "202"],
        })
        self.assertEqual(business[9]["headers"].get("if-match"), "4")
        self.assertEqual(business[9]["body"], b"")
        for index, mode, revision in (
            (10, "owner", "5"),
            (11, "authenticated", "6"),
            (12, "public", "7"),
            (13, "members", "8"),
        ):
            with self.subTest(visibility=mode):
                self.assertEqual(business[index]["method"], "PATCH")
                self.assertEqual(business[index]["path"], "/api/web-share/12")
                self.assertEqual(business[index]["headers"].get("if-match"), revision)
                self.assertEqual(json.loads(business[index]["body"]), {
                    "access_mode": mode,
                    "member_user_ids": [],
                })

    def test_doctor_auto_falls_back_to_external_without_credentials(self):
        def responder(request):
            self.assertEqual(request["method"], "GET")
            self.assertEqual(request["path"], "/ping")
            self.assertNotIn("authorization", request["headers"])
            return ResponseSpec(200, {"message": "pong"})

        with LocalHTTPSServer(self.server_certificate, self.server_key, responder) as external:
            unused_port = self._unused_port()
            config = self.write_config(
                f"https://localhost:{unused_port}",
                external.origin,
                with_credentials=False,
            )
            completed = self.run_cli(config, "doctor")
            self.assert_success(completed, "external", external.origin, {"message": "pong"})
            self.assertEqual(len(external.calls), 1)

    def test_permission_denial_stops_before_trying_another_origin(self):
        module = runpy.run_path(str(MANAGE), run_name="skill_test")
        denied = module["api"].ClientError("HTTPS 请求失败")
        denied.__cause__ = PermissionError("sandbox denied network access")
        config = {"endpoint": "auto", "internal_url": "https://home.internal.example.com",
                  "external_url": "https://home.example.com", "connect_timeout": 1}
        with mock.patch.object(module["api"], "HTTPTransport") as transport:
            transport.return_value.request.side_effect = denied
            with self.assertRaises(module["EndpointUnavailable"]) as raised:
                module["select_endpoint"](config)
            self.assertEqual(transport.call_count, 1)
            self.assertEqual(raised.exception.probes, [{"endpoint": "internal", "reason": "permission_denied"}])

    def test_redirect_is_rejected_and_never_followed(self):
        with LocalHTTPSServer(
            self.server_certificate,
            self.server_key,
            lambda request: ResponseSpec(302, b"", {"Location": "https://localhost:1/collect"}),
        ) as server:
            config = self.write_config(server.origin, endpoint="internal", with_credentials=False)
            completed = self.run_cli(config, "--endpoint", "internal", "doctor")
            self.assert_json_error(completed)
            self.assertEqual([(call["method"], call["path"]) for call in server.calls], [("GET", "/ping")])

    # 模拟自动选中内网后的业务 401，验证不切外网、不重试，并确保错误输出剔除回显的认证材料。
    def test_401_after_auto_selection_does_not_switch_origin_or_retry(self):
        def internal_responder(request):
            if request["path"] == "/ping":
                return ResponseSpec(200, {"message": "pong"})
            if request["path"] == "/api/auth/token":
                return token_response()
            return ResponseSpec(401, {
                "code": 40101,
                "message": f"denied {ACCESS_KEY} {SECRET_KEY} Bearer {ACCESS_TOKEN}",
            })

        with LocalHTTPSServer(self.server_certificate, self.server_key, internal_responder) as internal, LocalHTTPSServer(
            self.server_certificate,
            self.server_key,
            lambda request: success({"unexpected": True}),
        ) as external:
            config = self.write_config(internal.origin, external.origin)
            completed = self.run_cli(config, "list")
            self.assert_json_error(completed)
            rendered = completed.stderr
            self.assertNotIn(ACCESS_KEY, rendered)
            self.assertNotIn(SECRET_KEY, rendered)
            self.assertNotIn(ACCESS_TOKEN, rendered)
            self.assertEqual(
                [(call["method"], call["path"]) for call in internal.calls],
                [("GET", "/ping"), ("POST", "/api/auth/token"), ("GET", "/api/web-share?limit=20")],
            )
            self.assertEqual(external.calls, [])

    def test_untrusted_tls_certificate_is_rejected(self):
        with LocalHTTPSServer(
            self.server_certificate,
            self.server_key,
            lambda request: ResponseSpec(200, {"message": "pong"}),
        ) as server:
            config = self.fixture_root / "config.json"
            config.write_text(json.dumps({
                "internal_url": server.origin,
                "external_url": server.origin,
                "endpoint": "internal",
                "connect_timeout": 0.2,
                "timeout": 1,
            }), encoding="utf-8")
            config.chmod(0o600)
            completed = self.run_cli(config, "--endpoint", "internal", "doctor")
            self.assert_json_error(completed)
            self.assertEqual(server.calls, [])

    def test_symlinked_skill_can_run_from_an_unrelated_working_directory(self):
        def responder(request):
            self.assertEqual((request["method"], request["path"]), ("GET", "/ping"))
            return ResponseSpec(200, {"message": "pong"})

        with LocalHTTPSServer(self.server_certificate, self.server_key, responder) as server:
            config = self.write_config(server.origin, endpoint="internal", with_credentials=False)
            linked_skill = self.fixture_root / "linked-skill"
            linked_skill.symlink_to(MANAGE.parents[1], target_is_directory=True)
            linked_manage = linked_skill / "scripts" / "manage.py"
            completed = self.run_script(
                linked_manage,
                config,
                "--endpoint", "internal", "doctor",
            )
            self.assert_success(completed, "internal", server.origin, {"message": "pong"})

    # 分别模拟发布冲突和写请求超时，确认 CLI 均只发送一次业务写入，避免结果未知时自动重放。
    def test_write_conflict_and_timeout_are_each_sent_once(self):
        conflict_business_calls = []

        def conflict_responder(request):
            if request["path"] == "/api/auth/token":
                return token_response()
            conflict_business_calls.append(request)
            return ResponseSpec(409, {"code": 40901, "message": "stale revision"})

        with LocalHTTPSServer(self.server_certificate, self.server_key, conflict_responder) as server:
            config = self.write_config(server.origin, endpoint="internal")
            completed = self.run_cli(
                config,
                "--endpoint", "internal",
                "publish", "12", "--release", "31", "--revision", "2",
            )
            self.assert_json_error(completed)
            self.assertEqual(len(conflict_business_calls), 1)

        timeout_business_calls = []

        def timeout_responder(request):
            if request["path"] == "/api/auth/token":
                return token_response()
            timeout_business_calls.append(request)
            return ResponseSpec(200, {"code": 0, "message": "success", "data": {}}, delay=0.5)

        with LocalHTTPSServer(self.server_certificate, self.server_key, timeout_responder) as server:
            config = self.write_config(server.origin, endpoint="internal", timeout=0.1)
            completed = self.run_cli(
                config,
                "--endpoint", "internal",
                "disable", "12", "--revision", "2",
            )
            self.assert_json_error(completed)
            self.assertEqual(len(timeout_business_calls), 1)

    @unittest.skipUnless(os.name == "posix", "Unix permission checks")
    def test_config_and_credentials_files_must_be_private(self):
        config = self.write_config("https://localhost:1", endpoint="internal")
        config.chmod(0o644)
        completed = self.run_cli(config, "--endpoint", "internal", "list")
        self.assert_json_error(completed)

        config.chmod(0o600)
        credentials = self.fixture_root / "credentials.json"
        credentials.chmod(0o644)
        completed = self.run_cli(config, "--endpoint", "internal", "list")
        self.assert_json_error(completed)
        self.assertNotIn(ACCESS_KEY, completed.stderr)
        self.assertNotIn(SECRET_KEY, completed.stderr)

    # 以未监听地址执行 dry-run，验证离线操作摘要不会泄露上传正文或环境中的合成凭证。
    def test_dry_run_is_offline_and_does_not_render_file_contents_or_secrets(self):
        unused_port = self._unused_port()
        config = self.write_config(
            f"https://localhost:{unused_port}",
            endpoint="internal",
            with_credentials=False,
        )
        upload = self.fixture_root / "site.html"
        marker = "private-upload-body-marker"
        upload.write_text(f"<html>{marker}</html>", encoding="utf-8")
        completed = self.run_cli(
            config,
            "--endpoint", "internal",
            "--dry-run",
            "upload", "12", "--file", str(upload), "--request-id", "upload-dry-run",
            extra_environment={
                "CQ_HOME_SERVER_ACCESS_KEY": ACCESS_KEY,
                "CQ_HOME_SERVER_SECRET_KEY": SECRET_KEY,
            },
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        payload = json.loads(completed.stdout)
        self.assertEqual(payload["endpoint"], "internal")
        self.assertEqual(payload["origin"], f"https://localhost:{unused_port}")
        self.assertIsInstance(payload["result"], dict)
        self.assertTrue(payload["result"].get("dry_run"))
        rendered = completed.stdout + completed.stderr
        self.assertNotIn(marker, rendered)
        self.assertNotIn(ACCESS_KEY, rendered)
        self.assertNotIn(SECRET_KEY, rendered)

    def test_comment_create_dry_run_validates_files_without_rendering_body(self):
        config = self.write_config("https://localhost:1", endpoint="internal", with_credentials=False)
        body = self.fixture_root / "comment.txt"
        marker = "private-comment-body-marker"
        body.write_text(marker, encoding="utf-8")
        anchor = self.fixture_root / "anchor.json"
        anchor.write_text(json.dumps({
            "kind": "text",
            "target_id": "intro-section",
            "exact": "需要核对的原文",
            "page_id": "guide-page",
        }), encoding="utf-8")

        completed = self.run_cli(
            config,
            "--endpoint", "internal",
            "--dry-run",
            "comment-create", "12",
            "--page", "index.html",
            "--anchor-file", str(anchor),
            "--body-file", str(body),
            "--release", "31",
            "--request-id", "comment-create-1",
        )

        self.assertEqual(completed.returncode, 0, completed.stderr)
        result = json.loads(completed.stdout)["result"]
        self.assertEqual(result["method"], "POST")
        self.assertEqual(result["path"], "/api/web-share/12/comment-threads")
        self.assertEqual(result["request_id"], "comment-create-1")
        self.assertEqual(result["anchor_kind"], "text")
        self.assertEqual(result["body_characters"], len(marker))
        self.assertNotIn(marker, completed.stdout)

        anchor.write_text(json.dumps({"kind": "module", "target_id": "bad_ID"}), encoding="utf-8")
        rejected = self.run_cli(
            config,
            "--endpoint", "internal",
            "--dry-run",
            "comment-create", "12",
            "--page", "index.html",
            "--anchor-file", str(anchor),
            "--body-file", str(body),
            "--release", "31",
            "--request-id", "comment-create-2",
        )
        self.assert_json_error(rejected)

    def test_comment_reads_all_pages_and_write_is_read_back_by_request_id(self):
        thread = {
            "id": "90", "project_id": "12", "release_id": "31", "page_key": "path:index.html",
            "page_path": "index.html", "anchor": {"kind": "page"}, "status": "open", "revision": 2,
        }

        def responder(request):
            parsed = urllib.parse.urlsplit(request["path"])
            query = urllib.parse.parse_qs(parsed.query)
            if parsed.path == "/api/auth/token":
                return token_response()
            self.assertEqual(request["headers"].get("authorization"), f"Bearer {ACCESS_TOKEN}")
            if request["method"] == "GET" and parsed.path == "/api/web-share/12/comment-threads":
                if query.get("cursor") == ["90"]:
                    return success({"items": [dict(thread, id="80")], "has_more": False, "next_cursor": ""})
                return success({"items": [thread], "has_more": True, "next_cursor": "90"})
            if request["method"] == "GET" and parsed.path == "/api/web-share/12/comment-threads/90":
                return success(thread)
            if request["method"] == "GET" and parsed.path == "/api/web-share/12/comment-threads/90/events":
                if query.get("request_id") == ["reply-1"]:
                    self.assertNotIn("after_seq", query)
                    return success({
                        "items": [{"sequence": 2, "kind": "reply", "body": "reply body", "request_id": "reply-1"}],
                        "has_more": False, "next_cursor": "", "next_seq": "",
                    })
                if query.get("after_seq") == ["1"]:
                    return success({
                        "items": [{"sequence": 2, "kind": "reply", "body": "reply body"}],
                        "has_more": False, "next_cursor": "", "next_seq": "",
                    })
                self.assertNotIn("after_seq", query)
                return success({
                    "items": [{"sequence": 1, "kind": "comment", "body": "first"}],
                    "has_more": True, "next_cursor": "1", "next_seq": "1",
                })
            if request["method"] == "POST" and parsed.path == "/api/web-share/12/comment-threads/90/replies":
                self.assertEqual(json.loads(request["body"]), {"request_id": "reply-1", "release_id": "31", "body": "reply body"})
                return success(thread)
            return ResponseSpec(404, {"code": 404, "message": "unexpected request"})

        with LocalHTTPSServer(self.server_certificate, self.server_key, responder) as server:
            config = self.write_config(server.origin, endpoint="internal")
            listed = self.run_cli(config, "--endpoint", "internal", "comments", "12", "--limit", "1")
            self.assertEqual(listed.returncode, 0, listed.stderr)
            listed_result = json.loads(listed.stdout)["result"]
            self.assertEqual([item["id"] for item in listed_result["items"]], ["90", "80"])
            self.assertEqual(listed_result["pages"], 2)

            shown = self.run_cli(config, "--endpoint", "internal", "comment-show", "12", "90", "--limit", "1")
            self.assertEqual(shown.returncode, 0, shown.stderr)
            shown_result = json.loads(shown.stdout)["result"]
            self.assertEqual([item["sequence"] for item in shown_result["events"]], [1, 2])
            self.assertEqual(shown_result["pages"], 2)

            body = self.fixture_root / "reply.txt"
            body.write_text("reply body", encoding="utf-8")
            replied = self.run_cli(
                config, "--endpoint", "internal", "comment-reply", "12", "90",
                "--body-file", str(body), "--release", "31", "--request-id", "reply-1",
            )
            self.assertEqual(replied.returncode, 0, replied.stderr)
            reply_result = json.loads(replied.stdout)["result"]
            self.assertEqual(reply_result["thread"]["id"], "90")
            self.assertEqual(reply_result["event"]["request_id"], "reply-1")

    def test_comment_state_and_reanchor_dry_run_build_exact_requests(self):
        config = self.write_config("https://localhost:1", endpoint="internal", with_credentials=False)
        anchor = self.fixture_root / "anchor.json"
        anchor.write_text(json.dumps({"kind": "module", "target_id": "route-map", "page_id": "guide-page"}), encoding="utf-8")
        cases = [
            (("comment-resolve", "12", "90", "--release", "31", "--revision", "2", "--request-id", "resolve-1"), "/resolve", "2"),
            (("comment-reopen", "12", "90", "--release", "31", "--revision", "3", "--request-id", "reopen-1"), "/reopen", "3"),
            ((
                "comment-reanchor", "12", "90", "--page", "guide/index.html", "--anchor-file", str(anchor),
                "--release", "32", "--revision", "4", "--request-id", "reanchor-1",
            ), "/reanchor", "4"),
        ]
        for arguments, suffix, revision in cases:
            with self.subTest(command=arguments[0]):
                completed = self.run_cli(config, "--endpoint", "internal", "--dry-run", *arguments)
                self.assertEqual(completed.returncode, 0, completed.stderr)
                result = json.loads(completed.stdout)["result"]
                self.assertEqual(result["method"], "POST")
                self.assertTrue(result["path"].endswith(suffix))
                self.assertEqual(result["revision"], revision)
                self.assertIn(result["release_id"], {"31", "32"})
                self.assertNotIn("body", result)
        reanchor = json.loads(completed.stdout)["result"]
        self.assertEqual(reanchor["anchor_kind"], "module")
        self.assertEqual(reanchor["page"], "guide/index.html")

    # 验证成员列表与显式清空参数必须互斥且只用于 members 可见范围，合法清空计划输出空成员集合。
    def test_member_arguments_require_an_explicit_unambiguous_choice(self):
        config = self.write_config("https://localhost:1", endpoint="internal", with_credentials=False)
        invalid_arguments = [
            ("--dry-run", "visibility", "12", "--mode", "members", "--revision", "2"),
            ("--dry-run", "visibility", "12", "--mode", "owner", "--member", "201", "--revision", "2"),
            ("--dry-run", "visibility", "12", "--mode", "public", "--clear-members", "--revision", "2"),
        ]
        for arguments in invalid_arguments:
            with self.subTest(arguments=arguments):
                completed = self.run_cli(config, "--endpoint", "internal", *arguments)
                self.assert_json_error(completed)

        completed = self.run_cli(
            config,
            "--endpoint", "internal",
            "--dry-run",
            "visibility", "12", "--mode", "members", "--clear-members", "--revision", "2",
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        payload = json.loads(completed.stdout)
        self.assertEqual(payload["result"].get("member_user_ids"), [])

    def test_argument_errors_do_not_echo_unsupported_secret_values(self):
        config = self.write_config("https://localhost:1", endpoint="internal", with_credentials=False)
        completed = self.run_cli(
            config,
            "--endpoint", "internal",
            "list", "--secret-key", SECRET_KEY,
        )
        self.assert_json_error(completed)
        self.assertNotIn(SECRET_KEY, completed.stderr)

        ordinary_value = "ordinary-value-that-must-not-be-echoed"
        completed = self.run_cli(
            config,
            "--endpoint", "internal",
            "list", "--secret-key", ordinary_value,
        )
        self.assert_json_error(completed)
        self.assertNotIn(ordinary_value, completed.stderr)

    @staticmethod
    def _unused_port():
        import socket

        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            listener.bind(("localhost", 0))
            return listener.getsockname()[1]


if __name__ == "__main__":
    unittest.main()
