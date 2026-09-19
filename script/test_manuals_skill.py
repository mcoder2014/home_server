from __future__ import annotations

import json
import os
import shutil
import ssl
import subprocess
import sys
import tempfile
import threading
import unittest
import urllib.parse
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Optional


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
MANUALS = REPOSITORY_ROOT / "skills" / "home-server-web-share" / "scripts" / "manuals.py"
ACCESS_KEY = "ak_cq_" + "A" * 22
SECRET_KEY = "sk_cq_" + "S" * 43
ACCESS_TOKEN = "at_cq_" + "T" * 43


@dataclass
class ResponseSpec:
    status: int
    body: object
    headers: Optional[dict[str, str]] = None


class LocalHTTPSServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, certificate, key, responder):
        super().__init__(("localhost", 0), RecordingHandler)
        self.calls = []
        self.errors = []
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
        if exc_type is None and self.errors:
            raise AssertionError(f"fixture handler failed: {self.errors!r}")

    def handle_error(self, request, client_address):
        error = sys.exc_info()[1]
        if isinstance(error, (BrokenPipeError, ConnectionResetError, ssl.SSLError)):
            return
        self.errors.append(error)


class RecordingHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self._handle()

    def do_POST(self):
        self._handle()

    def do_PATCH(self):
        self._handle()

    def do_DELETE(self):
        self._handle()

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
        "scope": "manuals:read manuals:write",
    })


class ManualFixture:
    def __init__(self, *, fail_request_id=None):
        self.fail_request_id = fail_request_id
        self.failed_request_ids = set()
        self.appended_request_ids = []
        self.deleted = False
        self.manual = {
            "id": "123",
            "name": "夹具说明书",
            "description": "",
            "categories": [],
            "access_mode": "owner",
            "status": "draft",
            "revision": 1,
            "cover_item_id": None,
            "cover_url": "",
            "cover": None,
            "item_count": 0,
            "can_edit": True,
            "create_time": "2026-09-19T00:00:00Z",
            "update_time": "2026-09-19T00:00:00Z",
            "items": [],
        }
        self.next_item_id = 500

    def responder(self, request):
        parsed = urllib.parse.urlsplit(request["path"])
        if parsed.path == "/api/auth/token":
            return token_response()
        if request["headers"].get("authorization") != f"Bearer {ACCESS_TOKEN}":
            return ResponseSpec(401, {"code": 40101, "message": "missing token"})
        if parsed.path == "/api/manuals" and request["method"] == "GET":
            return success({"items": [] if self.deleted else [self._summary()], "has_more": False, "next_cursor": ""})
        if parsed.path == "/api/manuals" and request["method"] == "POST":
            payload = json.loads(request["body"])
            self.deleted = False
            self.manual.update({
                "name": payload["name"],
                "description": payload["description"],
                "categories": payload["categories"],
                "access_mode": payload["access_mode"],
                "status": "draft",
                "revision": 1,
                "items": [],
                "item_count": 0,
            })
            return success(self._view(), 201)
        if parsed.path == "/api/manuals/123" and request["method"] == "GET":
            if self.deleted:
                return ResponseSpec(404, {"code": 40401, "message": "not found"})
            return success(self._view())
        if parsed.path == "/api/manuals/123" and request["method"] == "PATCH":
            payload = json.loads(request["body"])
            if payload["revision"] != self.manual["revision"]:
                return ResponseSpec(409, {"code": 40901, "message": "stale revision"})
            for key in ("name", "description", "categories", "access_mode", "status", "cover_item_id"):
                if key in payload:
                    self.manual[key] = payload[key]
            if "item_ids" in payload:
                by_id = {item["id"]: item for item in self.manual["items"]}
                self.manual["items"] = [by_id[item_id] for item_id in payload["item_ids"]]
                for position, item in enumerate(self.manual["items"], 1):
                    item["position"] = position
            self.manual["revision"] += 1
            return success(self._view())
        if parsed.path == "/api/manuals/123/items" and request["method"] == "POST":
            payload = json.loads(request["body"])
            return self._append(payload["kind"], payload.get("title", ""), payload["client_request_id"], payload)
        if parsed.path == "/api/manuals/123/files" and request["method"] == "POST":
            request_id = self._multipart_value(request["body"], "client_request_id")
            title = self._multipart_value(request["body"], "title")
            return self._append("image", title, request_id, {})
        if parsed.path.startswith("/api/manuals/123/items/") and request["method"] == "DELETE":
            item_id = parsed.path.rsplit("/", 1)[-1]
            payload = json.loads(request["body"])
            if payload["revision"] != self.manual["revision"]:
                return ResponseSpec(409, {"code": 40901, "message": "stale revision"})
            self.manual["items"] = [item for item in self.manual["items"] if item["id"] != item_id]
            self.manual["item_count"] = len(self.manual["items"])
            self.manual["revision"] += 1
            return success({"revision": self.manual["revision"], "cover_item_id": ""})
        if parsed.path == "/api/manuals/123" and request["method"] == "DELETE":
            payload = json.loads(request["body"])
            if payload["revision"] != self.manual["revision"]:
                return ResponseSpec(409, {"code": 40901, "message": "stale revision"})
            self.manual["revision"] += 1
            self.deleted = True
            return success({"id": "123", "revision": self.manual["revision"]})
        return ResponseSpec(404, {"code": 40401, "message": "fixture route not found"})

    def _append(self, kind, title, request_id, payload):
        if request_id == self.fail_request_id and request_id not in self.failed_request_ids:
            self.failed_request_ids.add(request_id)
            return ResponseSpec(500, {"code": 50001, "message": f"failed {ACCESS_TOKEN}"})
        self.appended_request_ids.append(request_id)
        self.next_item_id += 1
        item = {
            "id": str(self.next_item_id),
            "kind": kind,
            "title": title,
            "text": payload.get("text", ""),
            "url": payload.get("url", ""),
            "original_name": "fixture.jpg" if kind == "image" else "",
            "content_type": "image/jpeg" if kind == "image" else "",
            "size_bytes": 12,
            "position": len(self.manual["items"]) + 1,
            "content_url": f"/api/manuals/123/items/{self.next_item_id}/content",
            "thumbnail_url": "",
            "preview_status": "none",
        }
        self.manual["items"].append(item)
        self.manual["item_count"] = len(self.manual["items"])
        self.manual["revision"] += 1
        return success({"item": item, "revision": self.manual["revision"]}, 201)

    def _view(self):
        return json.loads(json.dumps(self.manual))

    def _summary(self):
        value = self._view()
        value.pop("items", None)
        return value

    @staticmethod
    def _multipart_value(body, name):
        marker = f'name="{name}"\r\n\r\n'.encode("utf-8")
        start = body.index(marker) + len(marker)
        return body[start:body.index(b"\r\n", start)].decode("utf-8")


class ManualsSkillTest(unittest.TestCase):
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
    def _generate_certificates(cls):
        ca_key = cls.certificate_root / "ca.key"
        request = cls.certificate_root / "server.csr"
        extension = cls.certificate_root / "server.ext"
        extension.write_text("subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n", encoding="utf-8")
        commands = [
            ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(ca_key), "-out", str(cls.ca_file), "-days", "1", "-subj", "/CN=manuals-test-ca", "-addext", "basicConstraints=critical,CA:TRUE", "-addext", "keyUsage=critical,keyCertSign,cRLSign"],
            ["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-keyout", str(cls.server_key), "-out", str(request), "-subj", "/CN=localhost"],
            ["openssl", "x509", "-req", "-in", str(request), "-CA", str(cls.ca_file), "-CAkey", str(ca_key), "-CAcreateserial", "-out", str(cls.server_certificate), "-days", "1", "-extfile", str(extension)],
        ]
        for command in commands:
            subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.cwd = self.root / "unrelated-cwd"
        self.cwd.mkdir()

    def tearDown(self):
        self.directory.cleanup()

    def write_config(self, origin, *, credentials=True):
        shutil.copy2(self.ca_file, self.root / "ca.pem")
        payload = {"internal_url": origin, "external_url": origin, "endpoint": "internal", "ca_file": "ca.pem", "timeout": 2, "connect_timeout": 0.2}
        if credentials:
            credential_file = self.root / "credentials.json"
            credential_file.write_text(json.dumps({"access_key": ACCESS_KEY, "secret_key": SECRET_KEY}), encoding="utf-8")
            credential_file.chmod(0o600)
            payload["credentials_file"] = "credentials.json"
        config = self.root / "config.json"
        config.write_text(json.dumps(payload), encoding="utf-8")
        config.chmod(0o600)
        return config

    def run_cli(self, config, *arguments):
        environment = os.environ.copy()
        environment.pop("CQ_HOME_SERVER_ACCESS_KEY", None)
        environment.pop("CQ_HOME_SERVER_SECRET_KEY", None)
        environment["PYTHONDONTWRITEBYTECODE"] = "1"
        return subprocess.run([sys.executable, str(MANUALS), "--config", str(config), *arguments], cwd=self.cwd,
                              env=environment, text=True, capture_output=True, timeout=10)

    def assert_success(self, completed):
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stderr, "")
        return json.loads(completed.stdout)

    # Exercise the real TLS/token/client path and the draft -> mixed append ->
    # activation flow, including exact multi-category normalization.
    def test_create_mixed_batch_then_update_and_delete(self):
        fixture = ManualFixture()
        with LocalHTTPSServer(self.server_certificate, self.server_key, fixture.responder) as server:
            config = self.write_config(server.origin)
            image = self.root / "front.jpg"
            image.write_bytes(b"synthetic jpeg")
            text = self.root / "cleaning.txt"
            text.write_text("第一行\n第二行", encoding="utf-8")
            created = self.run_cli(
                config, "--endpoint", "internal", "create", "--name", " 咖啡机 ", "--category", " 厨房 ",
                "--category", "厨房", "--category", "kitchen", "--file", str(image), "--text-file", str(text),
                "--url", "https://example.com/manual?q=1", "--request-id", "create-1",
            )
            output = self.assert_success(created)
            self.assertEqual(output["result"]["manual"]["status"], "active")
            self.assertEqual(output["result"]["manual"]["categories"], ["kitchen", "厨房"])
            self.assertEqual([entry["request_id"] for entry in output["result"]["successful_items"]], [
                "create-1:file:1", "create-1:text:1", "create-1:url:1",
            ])

            revision = output["result"]["manual"]["revision"]
            updated = self.run_cli(config, "--endpoint", "internal", "update", "123", "--revision", str(revision),
                                   "--clear-categories", "--auto-cover")
            update_output = self.assert_success(updated)
            self.assertEqual(update_output["result"]["categories"], [])
            self.assertIsNone(update_output["result"]["cover_item_id"])

            item_id = update_output["result"]["items"][-1]["id"]
            item_revision = update_output["result"]["revision"]
            removed = self.run_cli(config, "--endpoint", "internal", "delete-item", "123", item_id, "--revision", str(item_revision))
            remove_output = self.assert_success(removed)
            self.assertNotIn(item_id, [item["id"] for item in remove_output["result"]["manual"]["items"]])

            delete_revision = remove_output["result"]["manual"]["revision"]
            deleted = self.run_cli(config, "--endpoint", "internal", "delete", "123", "--revision", str(delete_revision))
            delete_output = self.assert_success(deleted)
            self.assertTrue(delete_output["result"]["verified_deleted"])

        business = [call for call in server.calls if call["path"] != "/api/auth/token"]
        create_request = next(call for call in business if call["method"] == "POST" and call["path"] == "/api/manuals")
        self.assertEqual(json.loads(create_request["body"])["categories"], ["kitchen", "厨房"])
        self.assertTrue(any(call["path"] == "/api/manuals/123/files" and b'name="file"; filename="front.jpg"' in call["body"] for call in business))
        update_request = next(call for call in business if call["method"] == "PATCH" and b'"categories":[]' in call["body"])
        self.assertIsNone(json.loads(update_request["body"])["cover_item_id"])
        for call in business:
            self.assertEqual(call["headers"].get("authorization"), f"Bearer {ACCESS_TOKEN}")

    def test_list_preserves_omitted_and_explicit_empty_category(self):
        fixture = ManualFixture()
        with LocalHTTPSServer(self.server_certificate, self.server_key, fixture.responder) as server:
            config = self.write_config(server.origin)
            self.assert_success(self.run_cli(config, "--endpoint", "internal", "list", "--limit", "7"))
            self.assert_success(self.run_cli(config, "--endpoint", "internal", "list", "--category", "", "--limit", "7"))
            self.assert_success(self.run_cli(config, "--endpoint", "internal", "list", "--category", "厨房", "--limit", "7"))
        paths = [call["path"] for call in server.calls if call["method"] == "GET"]
        self.assertEqual(urllib.parse.parse_qs(urllib.parse.urlsplit(paths[0]).query, keep_blank_values=True), {"limit": ["7"]})
        self.assertEqual(urllib.parse.parse_qs(urllib.parse.urlsplit(paths[1]).query, keep_blank_values=True), {"limit": ["7"], "category": [""]})
        self.assertEqual(urllib.parse.parse_qs(urllib.parse.urlsplit(paths[2]).query, keep_blank_values=True), {"limit": ["7"], "category": ["厨房"]})

    def test_partial_batch_reports_success_and_stable_pending_request_ids(self):
        fixture = ManualFixture(fail_request_id="batch-1:file:2")
        with LocalHTTPSServer(self.server_certificate, self.server_key, fixture.responder) as server:
            config = self.write_config(server.origin)
            first = self.root / "one.jpg"
            second = self.root / "two.jpg"
            first.write_bytes(b"one")
            second.write_bytes(b"two")
            completed = self.run_cli(config, "--endpoint", "internal", "upload", "123", "--file", str(first),
                                     "--file", str(second), "--request-id", "batch-1")
        self.assertNotEqual(completed.returncode, 0)
        self.assertEqual(completed.stdout, "")
        self.assertNotIn(ACCESS_TOKEN, completed.stderr)
        payload = json.loads(completed.stderr)
        self.assertEqual(payload["error"], "partial_failure")
        self.assertEqual(payload["manual_id"], "123")
        self.assertEqual(payload["successful_items"][0]["request_id"], "batch-1:file:1")
        self.assertEqual([item["request_id"] for item in payload["pending_items"]], ["batch-1:file:2"])
        self.assertEqual(payload["endpoint"], "internal")
        business = [call for call in server.calls if call["path"] != "/api/auth/token"]
        self.assertEqual([(call["method"], call["path"]) for call in business], [
            ("POST", "/api/manuals/123/files"),
            ("POST", "/api/manuals/123/files"),
            ("GET", "/api/manuals/123"),
        ])

    def test_partial_batch_retries_multiple_pending_items_separately_without_duplicates(self):
        fixture = ManualFixture(fail_request_id="recover-1:file:2")
        with LocalHTTPSServer(self.server_certificate, self.server_key, fixture.responder) as server:
            config = self.write_config(server.origin)
            files = []
            for name in ("one", "two", "three"):
                path = self.root / f"{name}.jpg"
                path.write_bytes(name.encode("ascii"))
                files.append(path)

            failed = self.run_cli(
                config, "--endpoint", "internal", "upload", "123",
                "--file", str(files[0]), "--file", str(files[1]), "--file", str(files[2]),
                "--title", "第 1 页", "--title", "第 2 页", "--title", "第 3 页",
                "--request-id", "recover-1",
            )
            self.assertNotEqual(failed.returncode, 0)
            failure = json.loads(failed.stderr)
            self.assertEqual([item["request_id"] for item in failure["pending_items"]], [
                "recover-1:file:2", "recover-1:file:3",
            ])

            recovered = []
            for index, pending in enumerate(failure["pending_items"], 1):
                completed = self.run_cli(
                    config, "--endpoint", "internal", "upload", "123",
                    "--file", pending["file"], "--title", f"第 {index + 1} 页",
                    "--request-id", pending["request_id"],
                )
                recovered.append(self.assert_success(completed))

            final = recovered[-1]["result"]["manual"]
            self.assertEqual([result["result"]["successful_items"][0]["request_id"] for result in recovered], [
                "recover-1:file:2", "recover-1:file:3",
            ])
            self.assertEqual(fixture.appended_request_ids, [
                "recover-1:file:1", "recover-1:file:2", "recover-1:file:3",
            ])
            self.assertEqual([item["title"] for item in final["items"]], ["第 1 页", "第 2 页", "第 3 页"])
            self.assertEqual(final["item_count"], 3)
            self.assertEqual(len({item["id"] for item in final["items"]}), 3)

        file_calls = [call for call in server.calls if call["method"] == "POST" and call["path"] == "/api/manuals/123/files"]
        self.assertEqual([fixture._multipart_value(call["body"], "client_request_id") for call in file_calls], [
            "recover-1:file:1", "recover-1:file:2", "recover-1:file:2", "recover-1:file:3",
        ])

    def test_dry_run_is_offline_and_validates_formats_without_exposing_content(self):
        config = self.write_config("https://home.invalid.example", credentials=False)
        text = self.root / "private.txt"
        secret_body = "正文不应进入输出"
        text.write_text(secret_body, encoding="utf-8")
        completed = self.run_cli(
            config, "--dry-run", "create", "--name", "说明书", "--category", " A ", "--category", "A",
            "--category", "a", "--text-file", str(text), "--url", "https://example.com/manual?private=value",
            "--request-id", "dry-1",
        )
        output = self.assert_success(completed)
        self.assertEqual(output["result"]["categories"], ["A", "a"])
        self.assertEqual(output["result"]["items"][0]["request_id"], "dry-1:text:1")
        self.assertNotIn(secret_body, completed.stdout)
        self.assertNotIn("private=value", completed.stdout)

        heic = self.root / "photo.heic"
        heic.write_bytes(b"fixture")
        rejected = self.run_cli(config, "--dry-run", "upload", "123", "--file", str(heic), "--request-id", "heic-1")
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("HEIC/HEIF", json.loads(rejected.stderr)["message"])

        invalid = self.run_cli(config, "--dry-run", "add-url", "123", "--url", "https://user:secret@example.com/manual",
                               "--request-id", "url-1")
        self.assertNotEqual(invalid.returncode, 0)
        self.assertNotIn("secret", invalid.stderr)

        update = self.run_cli(config, "--dry-run", "update", "123", "--revision", "7", "--auto-cover")
        update_output = self.assert_success(update)
        self.assertEqual(update_output["result"]["revision"], 7)
        self.assertIsNone(update_output["result"]["cover_item_id"])


if __name__ == "__main__":
    unittest.main()
