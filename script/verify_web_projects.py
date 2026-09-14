#!/usr/bin/env python3
"""Black-box acceptance checks for the isolated web-projects deployment.

The script intentionally uses only Python's standard library. Authentication
tokens are read from a local 0600 JSON file and are never included in output.
"""

from __future__ import annotations

import argparse
import hashlib
import http.client
import io
import json
import os
import secrets
import ssl
import stat
import sys
import time
import urllib.parse
import zipfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable


@dataclass
class Response:
    status: int
    headers: dict[str, list[str]]
    body: bytes

    def header(self, name: str) -> str:
        values = self.headers.get(name.lower(), [])
        return values[-1] if values else ""


class HttpClient:
    def __init__(self, base_url: str, timeout: float) -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.hostname:
            raise ValueError("base URL must be an absolute HTTP(S) origin")
        if parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
            raise ValueError("base URL must not contain a path, query, or fragment")
        self.scheme = parsed.scheme
        self.host = parsed.hostname
        self.port = parsed.port or (443 if parsed.scheme == "https" else 80)
        self.origin = f"{parsed.scheme}://{parsed.netloc}"
        self.timeout = timeout

    # 向指定受测 origin 发送一次相对路径请求并保留重复响应头；本验收客户端的 HTTPS 分支关闭证书校验，连接结束后关闭句柄。
    def request(
        self,
        method: str,
        path: str,
        *,
        headers: dict[str, str] | None = None,
        body: bytes | None = None,
    ) -> Response:
        if not path.startswith("/") or path.startswith("//"):
            raise ValueError("request path must be an origin-relative path")
        request_headers = {"Accept": "application/json"}
        request_headers.update(headers or {})
        if self.scheme == "https":
            context = ssl.create_default_context()
            context.check_hostname = False
            context.verify_mode = ssl.CERT_NONE
            conn: http.client.HTTPConnection = http.client.HTTPSConnection(
                self.host, self.port, timeout=self.timeout, context=context
            )
        else:
            conn = http.client.HTTPConnection(self.host, self.port, timeout=self.timeout)
        try:
            conn.request(method, path, body=body, headers=request_headers)
            raw = conn.getresponse()
            response_headers: dict[str, list[str]] = {}
            for key, value in raw.getheaders():
                response_headers.setdefault(key.lower(), []).append(value)
            return Response(raw.status, response_headers, raw.read())
        finally:
            conn.close()


class CheckFailure(AssertionError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckFailure(message)


class WebProjectsAPI:
    def __init__(self, client: HttpClient, users: dict[str, dict[str, str]]) -> None:
        self.client = client
        self.users = users
        self.projects: dict[str, dict[str, Any]] = {}

    def token(self, role: str, *, cleanup: bool = False) -> str:
        user = self.users.get(role, {})
        key = "cleanup_token" if cleanup and user.get("cleanup_token") else "token"
        token = user.get(key, "")
        require(bool(token), f"missing authentication material for role {role}")
        return token

    def request(
        self,
        method: str,
        path: str,
        *,
        role: str | None = "owner",
        payload: Any | None = None,
        headers: dict[str, str] | None = None,
        body: bytes | None = None,
    ) -> Response:
        request_headers = dict(headers or {})
        if role:
            request_headers["passport"] = self.token(role)
        if payload is not None:
            body = json.dumps(payload, separators=(",", ":")).encode()
            request_headers["Content-Type"] = "application/json"
        return self.client.request(method, path, headers=request_headers, body=body)

    @staticmethod
    def data(response: Response, expected_status: int | set[int]) -> Any:
        statuses = {expected_status} if isinstance(expected_status, int) else expected_status
        require(response.status in statuses, f"unexpected HTTP status {response.status}")
        try:
            envelope = json.loads(response.body)
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise CheckFailure("response is not a JSON envelope") from exc
        if response.status < 300:
            require(envelope.get("code") == 0, "success envelope has non-zero code")
            require(envelope.get("message") == "success", "success envelope message changed")
            return envelope.get("data")
        require(envelope.get("code") not in {None, 0}, "failure envelope has zero or missing code")
        return envelope.get("data")

    def create_project(
        self, access_mode: str, *, members: list[str] | None = None, label: str = "project"
    ) -> dict[str, Any]:
        nonce = secrets.token_hex(5)
        payload = {
            "name": f"acceptance-{label}-{nonce}",
            "description": "isolated HTTP acceptance fixture",
            "slug": f"acceptance-{label}-{nonce}",
            "access_mode": access_mode,
            "member_user_ids": members or [],
            "client_request_id": f"acceptance-{nonce}",
        }
        project = self.data(self.request("POST", "/api/web-share", payload=payload), 201)
        require(isinstance(project.get("id"), str), "project id is not a JSON string")
        require(project.get("revision") == 1, "new project revision is not 1")
        self.projects[project["id"]] = project
        return project

    def patch_project(self, project: dict[str, Any], payload: dict[str, Any]) -> dict[str, Any]:
        response = self.request(
            "PATCH",
            f"/api/web-share/{project['id']}",
            payload=payload,
            headers={"If-Match": str(project["revision"])},
        )
        updated = self.data(response, 200)
        self.projects[updated["id"]] = updated
        return updated

    # 把验收文件及入口名封装成带随机幂等键的 multipart 上传，按调用方指定的成功或失败状态解析响应。
    def upload(
        self,
        project: dict[str, Any],
        filename: str,
        content: bytes,
        *,
        entry_file: str = "index.html",
        expected_status: int | set[int] = 201,
    ) -> Any:
        boundary = "----home-server-acceptance-" + secrets.token_hex(12)
        parts = [
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"entry_file\"\r\n\r\n{entry_file}\r\n".encode(),
            (
                f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; "
                f"filename=\"{filename}\"\r\nContent-Type: application/octet-stream\r\n\r\n"
            ).encode(),
            content,
            f"\r\n--{boundary}--\r\n".encode(),
        ]
        response = self.request(
            "POST",
            f"/api/web-share/{project['id']}/releases",
            headers={
                "Content-Type": f"multipart/form-data; boundary={boundary}",
                "Idempotency-Key": "acceptance-" + secrets.token_hex(8),
            },
            body=b"".join(parts),
        )
        return self.data(response, expected_status)

    def publish(self, project: dict[str, Any], release_id: str) -> dict[str, Any]:
        response = self.request(
            "POST",
            f"/api/web-share/{project['id']}/publish",
            payload={"release_id": release_id},
            headers={"If-Match": f'"{project["revision"]}"'},
        )
        updated = self.data(response, 200)
        self.projects[updated["id"]] = updated
        return updated

    def create_and_publish(
        self,
        access_mode: str,
        *,
        members: list[str] | None = None,
        label: str,
        marker: str | None = None,
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        project = self.create_project(access_mode, members=members, label=label)
        value = marker or ("marker-" + secrets.token_hex(6))
        archive = make_site_zip(value)
        release = self.upload(project, "site.zip", archive)
        require(release.get("status") == "ready", "uploaded release is not ready")
        project = self.publish(project, release["id"])
        return project, release

    def browser_cookie(self, role: str) -> tuple[str, str]:
        response = self.request(
            "POST",
            "/api/web-share/browser-login",
            role=role,
            headers={"Origin": self.client.origin},
        )
        self.data(response, 200)
        set_cookie = response.header("set-cookie")
        require(set_cookie.startswith("__Host-cq_session="), "browser session cookie name changed")
        cookie = set_cookie.split(";", 1)[0]
        return cookie, set_cookie

    def content(
        self,
        path: str,
        *,
        cookie: str | None = None,
        method: str = "GET",
        headers: dict[str, str] | None = None,
    ) -> Response:
        request_headers = dict(headers or {})
        request_headers["Accept"] = "text/html" if path.endswith("/") or path.endswith(".html") else "*/*"
        if cookie:
            request_headers["Cookie"] = cookie
        return self.client.request(method, path, headers=request_headers)

    def cleanup(self) -> list[str]:
        failures: list[str] = []
        for project_id, project in reversed(list(self.projects.items())):
            if project.get("status") == "deleted":
                continue
            try:
                headers = {
                    "passport": self.token("owner", cleanup=True),
                    "If-Match": str(project["revision"]),
                }
                response = self.client.request(
                    "DELETE", f"/api/web-share/{project_id}", headers=headers
                )
                if response.status not in {200, 404}:
                    failures.append(f"project cleanup returned HTTP {response.status}")
            except Exception:
                failures.append("project cleanup request failed")
        return failures


def make_site_zip(marker: str) -> bytes:
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        archive.writestr(
            "index.html",
            f'<!doctype html><meta charset="utf-8"><script src="assets/app.js"></script>{marker}',
        )
        archive.writestr("assets/app.js", f"window.ACCEPTANCE_MARKER={json.dumps(marker)};")
        archive.writestr("assets/data.txt", "0123456789abcdef")
        archive.writestr("assets/version.1.txt", "encoded single dot remains valid")
    return output.getvalue()


def make_traversal_zip() -> bytes:
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w") as archive:
        archive.writestr("index.html", "safe")
        archive.writestr("../escape.txt", "must not escape")
    return output.getvalue()


class Runner:
    def __init__(self, mode: str, client: HttpClient, api: WebProjectsAPI | None = None) -> None:
        self.mode = mode
        self.client = client
        self.api = api
        self.results: list[dict[str, Any]] = []
        self.started_at = int(time.time())

    def check(self, name: str, function: Callable[[], str | None]) -> None:
        started = time.monotonic()
        try:
            detail = function() or "verified"
            result = "pass"
        except Exception as exc:
            detail = str(exc) if isinstance(exc, CheckFailure) else type(exc).__name__
            result = "fail"
        self.results.append(
            {
                "name": name,
                "result": result,
                "detail": detail,
                "duration_ms": round((time.monotonic() - started) * 1000, 2),
            }
        )

    def baseline(self) -> None:
        def probe() -> str:
            ping = self.client.request("GET", "/ping")
            route = self.client.request("GET", "/api/web-share")
            require(ping.status == 200, f"health endpoint returned HTTP {ping.status}")
            require(route.status == 404, f"new route unexpectedly returned HTTP {route.status}")
            return "health=200, web-projects=404 (expected pre-implementation RED)"

        self.check("pre-implementation route baseline", probe)

    def full(self) -> None:
        require(self.api is not None, "full mode requires authentication material")
        self.check("management auth and project envelope", self.management_auth)
        self.check("four access modes and member revocation", self.access_modes)
        self.check("dynamic slug, stale revision, delete and restore", self.lifecycle)
        self.check("release publish, rollback, HEAD, Range and 304", self.release_rollback)
        self.check("request and ZIP traversal rejection", self.traversal)
        self.check("browser cookie attributes and logout invalidation", self.cookie_logout)

    def management_auth(self) -> str:
        assert self.api is not None
        project = self.api.create_project("owner", label="management")
        unauth = self.api.request("GET", f"/api/web-share/{project['id']}", role=None)
        require(unauth.status == 401, f"unauthenticated detail returned HTTP {unauth.status}")
        member = self.api.request("GET", f"/api/web-share/{project['id']}", role="member")
        require(member.status in {403, 404}, f"non-owner detail returned HTTP {member.status}")
        detail = self.api.data(
            self.api.request("GET", f"/api/web-share/{project['id']}"), 200
        )
        require(detail["id"] == project["id"], "detail returned a different project")
        return "unauthenticated=401, non-owner hidden, owner envelope valid"

    # 创建四类可见范围的页面并用不同测试 Cookie 访问，校验匿名导航、资源鉴权及移除成员后的即时拒绝。
    def access_modes(self) -> str:
        assert self.api is not None
        owner_cookie, _ = self.api.browser_cookie("owner")
        member_cookie, _ = self.api.browser_cookie("member")
        authenticated_cookie, _ = self.api.browser_cookie("authenticated")
        member_id = self.api.users["member"]["id"]

        public, _ = self.api.create_and_publish("public", label="public")
        require(self.api.content(public["url"]).status == 200, "public project is not public")

        owner, _ = self.api.create_and_publish("owner", label="owner")
        require(self.api.content(owner["url"], cookie=owner_cookie).status == 200, "owner cannot read")
        require(self.api.content(owner["url"], cookie=member_cookie).status == 404, "non-owner can read owner project")

        authenticated, _ = self.api.create_and_publish("authenticated", label="authenticated")
        require(self.api.content(authenticated["url"], cookie=authenticated_cookie).status == 200, "authenticated user cannot read")
        redirect = self.api.content(authenticated["url"])
        require(redirect.status == 302, f"document navigation returned HTTP {redirect.status}")
        require(redirect.header("location").startswith("/web-share/open?target="), "document redirect target changed")
        asset = self.api.content(authenticated["url"] + "assets/app.js")
        require(asset.status == 401, f"unauthenticated asset returned HTTP {asset.status}")

        members, _ = self.api.create_and_publish("members", members=[member_id], label="members")
        require(self.api.content(members["url"], cookie=member_cookie).status == 200, "member cannot read")
        require(self.api.content(members["url"], cookie=authenticated_cookie).status == 404, "non-member can read")
        members = self.api.patch_project(members, {"member_user_ids": []})
        require(self.api.content(members["url"], cookie=member_cookie).status == 404, "revoked member still reads")
        return "owner/members/authenticated/public enforced; revoked member denied immediately"

    # 验收 slug 修改、旧修订号冲突以及删除/恢复流程，确认旧地址失效且恢复项目仍处于下线状态。
    def lifecycle(self) -> str:
        assert self.api is not None
        project, _ = self.api.create_and_publish("public", label="lifecycle")
        old_url = project["url"]
        old_revision = project["revision"]
        new_slug = "acceptance-renamed-" + secrets.token_hex(5)
        project = self.api.patch_project(project, {"slug": new_slug})
        require(self.api.content(old_url).status == 404, "old slug remains readable")
        require(self.api.content(project["url"]).status == 200, "new slug is not readable")
        stale = self.api.request(
            "PATCH",
            f"/api/web-share/{project['id']}",
            payload={"description": "stale update must fail"},
            headers={"If-Match": str(old_revision)},
        )
        self.api.data(stale, 409)
        deleted = self.api.data(
            self.api.request(
                "DELETE",
                f"/api/web-share/{project['id']}",
                headers={"If-Match": str(project["revision"])},
            ),
            200,
        )
        self.api.projects[deleted["id"]] = deleted
        require(self.api.content(project["url"]).status == 404, "deleted project remains readable")
        restored = self.api.data(
            self.api.request(
                "POST",
                f"/api/web-share/{project['id']}/restore",
                headers={"If-Match": str(deleted["revision"])},
            ),
            200,
        )
        self.api.projects[restored["id"]] = restored
        require(restored["status"] == "disabled", "restored project is not disabled")
        require(self.api.content(restored["url"]).status == 404, "restored disabled project is readable")
        return "slug switch atomic; stale If-Match=409; delete hidden; restore disabled"

    # 上传并切换两个可识别版本，再回退首版；同时核对静态资源的安全响应头、HEAD、Range 与 ETag 条件请求。
    def release_rollback(self) -> str:
        assert self.api is not None
        first_marker = "release-one-" + secrets.token_hex(4)
        second_marker = "release-two-" + secrets.token_hex(4)
        project = self.api.create_project("public", label="release")
        first = self.api.upload(project, "site-one.zip", make_site_zip(first_marker))
        project = self.api.publish(project, first["id"])
        require(first_marker.encode() in self.api.content(project["url"]).body, "first release not served")
        second = self.api.upload(project, "site-two.zip", make_site_zip(second_marker))
        project = self.api.publish(project, second["id"])
        require(second_marker.encode() in self.api.content(project["url"]).body, "second release not served")
        project = self.api.publish(project, first["id"])
        require(first_marker.encode() in self.api.content(project["url"]).body, "rollback release not served")

        asset_path = project["url"] + "assets/data.txt"
        normal = self.api.content(asset_path)
        require(normal.status == 200 and normal.body == b"0123456789abcdef", "asset body changed")
        require(normal.header("x-content-type-options").lower() == "nosniff", "nosniff header missing")
        require("no-store" in normal.header("cache-control").lower(), "no-store header missing")
        head = self.api.content(asset_path, method="HEAD")
        require(head.status == 200 and not head.body, "HEAD semantics changed")
        partial = self.api.content(asset_path, headers={"Range": "bytes=0-3"})
        require(partial.status == 206 and partial.body == b"0123", "Range semantics changed")
        etag = normal.header("etag")
        require(bool(etag), "ETag missing")
        unchanged = self.api.content(asset_path, headers={"If-None-Match": etag})
        require(unchanged.status == 304 and not unchanged.body, "If-None-Match semantics changed")
        return "second release served; rollback restored first; HEAD/Range/304 verified"

    # 对照合法资源路径，验证 ZIP 和多种编码 URL 穿越均被拒绝，且拒绝响应不泄露配置正文。
    def traversal(self) -> str:
        assert self.api is not None
        project, _ = self.api.create_and_publish("public", label="traversal")
        normal_asset = project["url"] + "assets/version.1.txt"
        require(self.api.content(normal_asset).status == 200, "normal asset control failed")
        encoded_dot = self.api.content(project["url"] + "assets/version%2e1.txt")
        require(encoded_dot.status == 200, "single encoded dot was rejected")
        query_only = self.api.content(normal_asset + "?next=%2e%2e%2foutside")
        require(query_only.status == 200, "encoded query value was rejected")
        bad_upload = self.api.upload(
            project,
            "traversal.zip",
            make_traversal_zip(),
            expected_status={400, 415, 422},
        )
        require(bad_upload is None or isinstance(bad_upload, (dict, list)), "failure envelope malformed")
        slug = project["url"].split("/")[2]
        paths = [
            project["url"] + "%2e%2e/%2e%2e/config.yaml",
            project["url"] + "..%2f..%2fconfig.yaml",
            project["url"] + "%5c..%5cconfig.yaml",
            project["url"] + "../config.yaml",
            f"/%70/{slug}/%2e%2e/config.yaml",
            f"/p%2f{slug}/%2e%2e/config.yaml",
        ]
        for path in paths:
            response = self.api.content(path)
            require(response.status in {400, 404}, f"traversal returned HTTP {response.status}")
            require(b"master_db" not in response.body, "traversal response exposed configuration")
        return "ZIP/URL traversal rejected; normal, encoded-dot and query controls served"

    def cookie_logout(self) -> str:
        assert self.api is not None
        project, _ = self.api.create_and_publish("owner", label="cookie")
        cookie, raw = self.api.browser_cookie("owner")
        lower = raw.lower()
        for attribute in ("secure", "httponly", "samesite=lax", "path=/"):
            require(attribute in lower, f"cookie attribute {attribute} missing")
        require("domain=" not in lower, "host cookie unexpectedly has Domain")
        asset_path = project["url"] + "assets/app.js"
        require(self.api.content(asset_path, cookie=cookie).status == 200, "browser cookie cannot read")
        logout = self.api.request("POST", "/passport/logout", role="owner")
        require(logout.status == 200, f"logout returned HTTP {logout.status}")
        denied = self.api.content(asset_path, cookie=cookie)
        require(denied.status == 401, f"logged-out cookie returned HTTP {denied.status}")
        return "__Host cookie attributes valid; logout invalidated content access"

    def report(self) -> dict[str, Any]:
        passed = sum(item["result"] == "pass" for item in self.results)
        return {
            "schema_version": 1,
            "mode": self.mode,
            "origin": self.client.origin,
            "started_at_unix": self.started_at,
            "finished_at_unix": int(time.time()),
            "summary": {"total": len(self.results), "passed": passed, "failed": len(self.results) - passed},
            "results": self.results,
        }


def load_auth(path: Path) -> dict[str, dict[str, str]]:
    mode = stat.S_IMODE(path.stat().st_mode)
    if mode & 0o077:
        raise ValueError("auth file must not be readable or writable by group/others")
    raw = json.loads(path.read_text())
    users = raw.get("users")
    if not isinstance(users, dict):
        raise ValueError("auth file must contain a users object")
    for role in ("owner", "member", "authenticated"):
        user = users.get(role)
        if not isinstance(user, dict) or not isinstance(user.get("id"), str) or not user.get("token"):
            raise ValueError(f"auth file is missing {role}.id or {role}.token")
    return users


def write_report(path: Path, report: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as output:
        json.dump(report, output, ensure_ascii=False, indent=2)
        output.write("\n")
    os.replace(temporary, path)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True, help="isolated server origin")
    parser.add_argument("--mode", choices=("baseline", "full"), default="full")
    parser.add_argument("--auth-file", type=Path, help="0600 JSON file with test user ids and tokens")
    parser.add_argument("--report-json", type=Path, required=True)
    parser.add_argument("--timeout", type=float, default=10.0)
    return parser.parse_args()


# 按基线或完整模式运行网页验收，完整模式结束时尝试清理夹具，写报告并输出摘要及报告 SHA256。
def main() -> int:
    args = parse_args()
    client = HttpClient(args.base_url, args.timeout)
    api: WebProjectsAPI | None = None
    if args.mode == "full":
        if args.auth_file is None:
            raise SystemExit("--auth-file is required in full mode")
        api = WebProjectsAPI(client, load_auth(args.auth_file))
    runner = Runner(args.mode, client, api)
    if args.mode == "baseline":
        runner.baseline()
    else:
        try:
            runner.full()
        finally:
            cleanup_failures = api.cleanup() if api else []
            if cleanup_failures:
                runner.results.append(
                    {"name": "fixture cleanup", "result": "fail", "detail": cleanup_failures[0], "duration_ms": 0}
                )
    report = runner.report()
    write_report(args.report_json, report)
    print(
        json.dumps(
            {
                "mode": report["mode"],
                "origin": report["origin"],
                "summary": report["summary"],
                "report_sha256": hashlib.sha256(args.report_json.read_bytes()).hexdigest(),
            },
            separators=(",", ":"),
        )
    )
    return 0 if report["summary"]["failed"] == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
