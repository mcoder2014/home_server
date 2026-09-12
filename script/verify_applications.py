#!/usr/bin/env python3
"""Black-box validation for CQ Home Server application credentials."""

from __future__ import annotations

import argparse
import base64
import concurrent.futures
import http.client
import io
import json
import os
import ssl
import stat
import time
import urllib.parse
import zipfile
from pathlib import Path


class ValidationFailure(AssertionError):
    pass


class Response:
    def __init__(self, status: int, headers: list[tuple[str, str]], body: bytes):
        self.status = status
        self.headers = headers
        self.body = body

    def values(self, name: str) -> list[str]:
        return [value for key, value in self.headers if key.lower() == name.lower()]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValidationFailure(message)


def read_private_json(path: Path) -> dict:
    metadata = path.stat()
    require(stat.S_ISREG(metadata.st_mode), "auth input is not a regular file")
    require(stat.S_IMODE(metadata.st_mode) & 0o077 == 0, "auth input permissions exceed 0600")
    return json.loads(path.read_text(encoding="utf-8"))


class Client:
    def __init__(self, origin: str, ca_file: Path):
        parsed = urllib.parse.urlsplit(origin)
        require(parsed.scheme == "https" and parsed.hostname and not parsed.path.rstrip("/"), "invalid HTTPS origin")
        self.origin = origin.rstrip("/")
        self.host = parsed.hostname
        self.port = parsed.port or 443
        self.context = ssl.create_default_context(cafile=str(ca_file))

    def request(self, method: str, path: str, headers=None, body: bytes | None = None) -> Response:
        connection = http.client.HTTPSConnection(self.host, self.port, timeout=20, context=self.context)
        try:
            connection.request(method, path, body=body, headers=headers or {})
            raw = connection.getresponse()
            return Response(raw.status, raw.getheaders(), raw.read())
        finally:
            connection.close()

    def direct_http(self, port: int, headers: dict[str, str], body: bytes) -> Response:
        connection = http.client.HTTPConnection(
            "127.0.0.1", port, timeout=10, source_address=("127.0.0.2", 0)
        )
        try:
            connection.request("POST", "/api/auth/token", body=body, headers=headers)
            raw = connection.getresponse()
            return Response(raw.status, raw.getheaders(), raw.read())
        finally:
            connection.close()


class Validator:
    def __init__(self, client: Client, auth: dict, credential_out: Path, backend_port: int, backend_log: Path):
        self.client = client
        self.users = auth["users"]
        self.credential_out = credential_out
        self.backend_port = backend_port
        self.backend_log = backend_log
        self.results: list[dict[str, str]] = []
        self.secrets: set[str] = {user["token"] for user in self.users.values()}
        self.primary: dict | None = None
        self.primary_secret = ""
        self.primary_project: dict | None = None
        self.created_application_ids: list[str] = []

    def check(self, name: str, function) -> None:
        start = time.monotonic()
        try:
            detail = function() or "verified"
            result = "pass"
        except Exception as error:
            detail = str(error) if isinstance(error, ValidationFailure) else type(error).__name__
            result = "fail"
        self.results.append({"name": name, "result": result, "detail": detail, "duration_ms": str(round((time.monotonic() - start) * 1000, 2))})

    def request(self, method: str, path: str, *, role: str | None = None, bearer: str | None = None,
                cookie: str | None = None, payload=None, body: bytes | None = None, headers=None) -> Response:
        request_headers = dict(headers or {})
        if role:
            request_headers["passport"] = self.users[role]["token"]
        if bearer:
            request_headers["Authorization"] = "Bearer " + bearer
        if cookie:
            request_headers["Cookie"] = cookie
        if payload is not None:
            body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
            request_headers["Content-Type"] = "application/json"
        return self.client.request(method, path, request_headers, body)

    @staticmethod
    def envelope(response: Response, status: int | set[int]):
        statuses = {status} if isinstance(status, int) else status
        require(response.status in statuses, f"unexpected HTTP {response.status}")
        try:
            payload = json.loads(response.body)
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise ValidationFailure("response is not JSON") from error
        if response.status < 300:
            require(payload.get("code") == 0, "success envelope code changed")
            return payload.get("data")
        require(payload.get("code") not in (None, 0), "failure envelope has no error code")
        return payload

    def create_app(self, role: str, name: str, scopes: list[str]) -> tuple[dict, str]:
        data = self.envelope(self.request("POST", "/api/applications", role=role, payload={
            "name": name, "description": "cq-app-auth-validation", "scopes": scopes, "expires_in_days": 30,
        }), 201)
        application, secret = data["application"], data["secret_key"]
        require(secret.startswith("sk_cq_") and len(secret) == 49, "invalid one-time secret format")
        self.secrets.update((application["access_key"], secret))
        return application, secret

    def issue(self, application: dict, secret: str, *, form: bool = False) -> tuple[str, int, list[str]]:
        fields = {"grant_type": "client_credentials"}
        headers = {"Content-Type": "application/x-www-form-urlencoded"}
        if form:
            fields.update(client_id=application["access_key"], client_secret=secret)
        else:
            raw = f"{application['access_key']}:{secret}".encode()
            encoded = base64.b64encode(raw).decode()
            self.secrets.add(encoded)
            headers["Authorization"] = "Basic " + encoded
        response = self.client.request("POST", "/api/auth/token", headers, urllib.parse.urlencode(fields).encode())
        require(response.status == 200, f"token exchange returned HTTP {response.status}")
        require("no-store" in ",".join(response.values("Cache-Control")).lower(), "token response is cacheable")
        require("no-cache" in ",".join(response.values("Pragma")).lower(), "token response lacks pragma")
        data = json.loads(response.body)
        token = data.get("access_token", "")
        require(token.startswith("at_cq_") and len(token) == 49, "invalid access token format")
        require(data.get("token_type") == "Bearer" and isinstance(data.get("expires_in"), int), "invalid token metadata")
        self.secrets.add(token)
        return token, data["expires_in"], data.get("scope", "").split()

    def write_credentials(self, application: dict, secret: str, access_token: str = "") -> None:
        flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        descriptor = os.open(self.credential_out, flags, 0o600)
        try:
            os.fchmod(descriptor, 0o600)
            payload = {"access_key": application["access_key"], "secret_key": secret}
            if access_token:
                payload["access_token"] = access_token
            os.write(descriptor, json.dumps(payload, separators=(",", ":")).encode())
        finally:
            os.close(descriptor)

    @staticmethod
    def no_sensitive_fields(value) -> bool:
        forbidden = {"secret_key", "secret_digest", "token_digest", "access_token", "owner_user_id"}
        if isinstance(value, dict):
            return not (forbidden & {str(key).lower() for key in value}) and all(Validator.no_sensitive_fields(item) for item in value.values())
        if isinstance(value, list):
            return all(Validator.no_sensitive_fields(item) for item in value)
        return True

    def setup_and_secrecy(self) -> str:
        primary, secret = self.create_app("owner", "qa-primary-20260912", ["web-projects:write", "library:write", "webdav:write"])
        require(primary["scopes"] == ["web-projects:read", "web-projects:write", "library:read", "library:write", "webdav:read", "webdav:write"], "write scopes did not inherit read")
        listing = self.envelope(self.request("GET", "/api/applications", role="owner"), 200)
        detail = self.envelope(self.request("GET", f"/api/applications/{primary['id']}", role="owner"), 200)
        require(self.no_sensitive_fields(listing) and self.no_sensitive_fields(detail), "list/get exposed a sensitive field")
        require(self.envelope(self.request("GET", f"/api/applications/{primary['id']}", role="member"), 404), "cross-owner get unexpectedly succeeded")
        self.primary, self.primary_secret = primary, secret
        self.write_credentials(primary, secret)
        return "primary created; list/get fields and owner isolation verified"

    def token_protocol_and_https(self) -> str:
        assert self.primary
        token, _, _ = self.issue(self.primary, self.primary_secret)
        form_token, _, _ = self.issue(self.primary, self.primary_secret, form=True)
        self.secrets.add(form_token)
        raw = f"{self.primary['access_key']}:{self.primary_secret}".encode()
        basic = "Basic " + base64.b64encode(raw).decode()
        mixed = self.client.request("POST", "/api/auth/token", {"Authorization": basic, "Content-Type": "application/x-www-form-urlencoded"}, urllib.parse.urlencode({"grant_type":"client_credentials","client_id":self.primary["access_key"],"client_secret":self.primary_secret}).encode())
        require(mixed.status == 400, f"mixed token credentials returned HTTP {mixed.status}")
        query = self.client.request("POST", "/api/auth/token?client_secret=" + urllib.parse.quote(self.primary_secret), {"Authorization": basic, "Content-Type": "application/x-www-form-urlencoded"}, b"grant_type=client_credentials")
        require(query.status == 400, f"query credential path returned HTTP {query.status}")
        spoof = self.client.direct_http(self.backend_port, {"Authorization": basic, "Content-Type": "application/x-www-form-urlencoded", "X-Forwarded-Proto": "https"}, b"grant_type=client_credentials")
        require(spoof.status == 403, f"untrusted XFP spoof returned HTTP {spoof.status}")
        return "Basic/form accepted; mixed/query rejected; untrusted XFP rejected"

    def scopes_resources_and_owner(self) -> str:
        assert self.primary
        primary_token, _, _ = self.issue(self.primary, self.primary_secret)
        read_app, read_secret = self.create_app("owner", "qa-read-20260912", ["web-projects:read", "library:read", "webdav:read"])
        read_token, _, _ = self.issue(read_app, read_secret, form=True)
        denied = self.request("POST", "/api/web-projects", bearer=read_token, payload={})
        require(denied.status == 403, f"read token write returned HTTP {denied.status}")
        name, slug = "界" * 256, "q" * 256
        project = self.envelope(self.request("POST", "/api/web-projects", bearer=primary_token, payload={
            "name": name, "description": "application-owned fixture", "slug": slug,
            "access_mode": "owner", "member_user_ids": [], "client_request_id": "qa-app-auth-project",
        }), 201)
        require(len(project["name"]) == 256 and len(project["slug"]) == 256, "256-char project boundary changed")
        bad_enum = self.request("POST", "/api/web-projects", bearer=primary_token, payload={
            "name":"bad enum", "description":"", "slug":"qa-bad-enum", "access_mode":"unknown", "member_user_ids":[],
        })
        require(bad_enum.status == 400, f"unknown HTTP enum returned HTTP {bad_enum.status}")
        member_app, member_secret = self.create_app("member", "qa-owner-b-20260912", ["web-projects:read"])
        member_token, _, _ = self.issue(member_app, member_secret)
        require(self.request("GET", f"/api/web-projects/{project['id']}", bearer=member_token).status == 404, "cross-owner project was visible")
        self.envelope(self.request("GET", "/library/book/total?offset=0&limit=10", bearer=read_token), 200)
        require(self.request("POST", "/library/address/add", bearer=read_token, payload={"address":"denied","short_name":"denied"}).status == 403, "library read token wrote data")
        self.envelope(self.request("POST", "/library/address/add", bearer=primary_token, payload={"address":"qa isolated","short_name":"qa-app"}), 200)
        require(self.request("GET", "/library/book/total?offset=0&limit=10", role="owner").status == 200, "legacy user library read failed")
        propfind = self.request("PROPFIND", "/webdav_dev/", bearer=read_token, headers={"Depth":"0"})
        require(propfind.status == 207, f"WebDAV read returned HTTP {propfind.status}")
        require(self.request("PROPFIND", "/webdav_dev/", role="owner", bearer=read_token, headers={"Depth":"0"}).status == 400, "WebDAV accepted mixed passport/Bearer")
        basic_user = base64.b64encode(f"{self.users['owner']['user_name']}:{self.users['owner']['password']}".encode()).decode()
        self.secrets.add(basic_user)
        require(self.request("PROPFIND", "/webdav_dev/", role="owner", headers={"Authorization":"Basic " + basic_user,"Depth":"0"}).status == 400, "WebDAV accepted mixed passport/Basic")
        require(self.request("PUT", "/webdav_dev/qa-read-denied.txt", bearer=read_token, body=b"denied").status == 403, "WebDAV read token wrote data")
        put = self.request("PUT", "/webdav_dev/qa-app-auth-20260912.txt", bearer=primary_token, body=b"isolated application fixture")
        require(put.status in {201, 204}, f"WebDAV write returned HTTP {put.status}")
        get = self.request("GET", "/webdav_dev/qa-app-auth-20260912.txt", bearer=primary_token)
        require(get.status == 200 and get.body == b"isolated application fixture", "WebDAV write/read mismatch")
        self.request("GET", "/webdav_dev/qa-missing-app-auth.txt", bearer=primary_token)
        self.primary_project = project
        return "scope inheritance, project owner isolation, library and WebDAV verified"

    @staticmethod
    def site_zip() -> bytes:
        output = io.BytesIO()
        with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
            archive.writestr("index.html", "<!doctype html><title>app-auth-validation</title>qa-app-auth-marker")
            archive.writestr("assets/app.js", "window.QA_APP_AUTH=true")
        return output.getvalue()

    def project_upload_cookie_and_identity(self) -> str:
        assert self.primary and self.primary_project
        token, _, _ = self.issue(self.primary, self.primary_secret)
        boundary = "----cq-app-auth-validation"
        body = (
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"entry_file\"\r\n\r\nindex.html\r\n"
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"site.zip\"\r\nContent-Type: application/zip\r\n\r\n"
        ).encode() + self.site_zip() + f"\r\n--{boundary}--\r\n".encode()
        release = self.envelope(self.request("POST", f"/api/web-projects/{self.primary_project['id']}/releases", bearer=token, body=body, headers={"Content-Type":f"multipart/form-data; boundary={boundary}","Idempotency-Key":"qa-app-auth-upload"}), 201)
        project = self.envelope(self.request("POST", f"/api/web-projects/{self.primary_project['id']}/publish", bearer=token, payload={"release_id":release["id"]}, headers={"If-Match":str(self.primary_project["revision"])}), 200)
        login = self.request("POST", "/api/auth/browser-login", role="owner", headers={"Origin":self.client.origin})
        self.envelope(login, 200)
        cookies = login.values("Set-Cookie")
        canonical = next((value.split(";",1)[0] for value in cookies if value.startswith("__Host-cq_session=")), "")
        require(canonical and "Secure" in "".join(cookies) and "HttpOnly" in "".join(cookies) and "SameSite=Lax" in "".join(cookies), "canonical cookie attributes changed")
        require(any(value.startswith("__Host-web_projects_session=") and "Max-Age=0" in value for value in cookies), "legacy cookie was not cleared on login")
        path = "/p/" + self.primary_project["slug"] + "/"
        require(self.request("GET", path, cookie=canonical).status == 200, "canonical cookie content access failed")
        legacy = "__Host-web_projects_session=" + self.users["owner"]["token"]
        require(self.request("GET", path, cookie=legacy).status == 200, "legacy cookie compatibility failed")
        switched = self.request("POST", "/api/auth/browser-login", role="member", headers={"Origin":self.client.origin})
        switched_cookie = next((value.split(";",1)[0] for value in switched.values("Set-Cookie") if value.startswith("__Host-cq_session=")), "")
        require(switched_cookie and self.request("GET", path, cookie=switched_cookie).status == 404, "A to B cookie switch retained A access")
        require(self.request("GET", "/api/applications", cookie=canonical).status == 401, "cookie managed credentials")
        require(self.request("GET", "/api/applications", bearer=token).status == 403, "application managed credentials")
        require(self.request("GET", "/api/web-projects", role="owner", bearer=token).status == 400, "mixed user/application credential was accepted")
        require(self.request("POST", "/api/auth/browser-login", bearer=token, headers={"Origin":self.client.origin}).status == 403, "application created browser session")
        require(self.request("POST", "/passport/logout", bearer=token).status == 403, "application logged out a user")
        deleted = self.envelope(self.request("DELETE", f"/api/web-projects/{project['id']}", bearer=token, headers={"If-Match":str(project["revision"])}), 200)
        restored = self.envelope(self.request("POST", f"/api/web-projects/{project['id']}/restore", bearer=token, headers={"If-Match":str(deleted["revision"])}), 200)
        self.primary_project = restored
        return "upload/publish, canonical+legacy cookie, switch, user-only and archive/restore verified"

    def lifecycle_and_expiry(self) -> str:
        app, secret = self.create_app("owner", "qa-lifecycle-20260912", ["web-projects:write"])
        old_token, _, _ = self.issue(app, secret)
        disabled = self.envelope(self.request("PATCH", f"/api/applications/{app['id']}", role="owner", headers={"If-Match":str(app["revision"])}, payload={"status":"disabled"}), 200)
        require(self.request("GET", "/api/web-projects", bearer=old_token).status == 401, "disabled token remained valid")
        enabled = self.envelope(self.request("PATCH", f"/api/applications/{app['id']}", role="owner", headers={"If-Match":str(disabled["revision"])}, payload={"status":"enabled"}), 200)
        require(self.request("GET", "/api/web-projects", bearer=old_token).status == 401, "enable revived old token")
        before_rotate = []
        def issue_old():
            try:
                value, _, _ = self.issue(enabled, secret)
                return value
            except ValidationFailure:
                return ""
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            issues = [pool.submit(issue_old) for _ in range(7)]
            rotate = pool.submit(lambda: self.envelope(self.request("POST", f"/api/applications/{app['id']}/rotate", role="owner", headers={"If-Match":str(enabled["revision"])}), 200))
            rotated = rotate.result()
            before_rotate = [future.result() for future in issues if future.result()]
        new_secret = rotated["secret_key"]
        self.secrets.add(new_secret)
        for issued in before_rotate:
            require(self.request("GET", "/api/web-projects", bearer=issued).status == 401, "issuance/rotate race left a token valid")
        try:
            self.issue(rotated["application"], secret)
            raise ValidationFailure("old secret issued after rotation")
        except ValidationFailure as error:
            if str(error) == "old secret issued after rotation":
                raise
        current_token, _, _ = self.issue(rotated["application"], new_secret)
        scoped = self.envelope(self.request("PATCH", f"/api/applications/{app['id']}", role="owner", headers={"If-Match":str(rotated["application"]["revision"])}, payload={"scopes":["web-projects:read"]}), 200)
        require(self.request("GET", "/api/web-projects", bearer=current_token).status == 401, "scope change left old token valid")
        scoped_token, _, _ = self.issue(scoped, new_secret)
        require(self.request("POST", "/api/web-projects", bearer=scoped_token, payload={}).status == 403, "read-only token wrote after scope change")
        revoked = self.envelope(self.request("DELETE", f"/api/applications/{app['id']}", role="owner", headers={"If-Match":str(scoped["revision"])}), 200)
        require(self.request("GET", "/api/web-projects", bearer=scoped_token).status == 401, "revoked token remained valid")
        require(self.request("PATCH", f"/api/applications/{app['id']}", role="owner", headers={"If-Match":str(revoked["revision"])}, payload={"status":"enabled"}).status == 409, "revoked app was restored")
        expiry_app, expiry_secret = self.create_app("owner", "qa-expiry-20260912", ["web-projects:read"])
        expiry_token, ttl, _ = self.issue(expiry_app, expiry_secret)
        time.sleep(ttl + 1)
        require(self.request("GET", "/api/web-projects", bearer=expiry_token).status == 401, "expired token remained valid")
        return "disable/enable/rotate/scope/revoke races and expiry verified"

    def log_redaction(self) -> str:
        time.sleep(0.3)
        content = self.backend_log.read_text(encoding="utf-8", errors="replace")
        leaked = [value for value in self.secrets if value and value in content]
        require(not leaked, "backend log contains raw credential material")
        return "backend log contains none of the generated AK/SK/token values"

    def run(self) -> int:
        self.check("application create/list/get secrecy and owner isolation", self.setup_and_secrecy)
        self.check("OAuth token protocol and HTTPS boundary", self.token_protocol_and_https)
        self.check("scope inheritance and protected resources", self.scopes_resources_and_owner)
        self.check("project upload, browser cookies and identity separation", self.project_upload_cookie_and_identity)
        self.check("credential lifecycle, races and expiry", self.lifecycle_and_expiry)
        self.check("credential log redaction", self.log_redaction)
        return 0 if all(item["result"] == "pass" for item in self.results) else 1

    def run_quota(self, role: str, attempts: int, expected_success: int) -> int:
        def verify() -> str:
            def create(index: int):
                response = self.request("POST", "/api/applications", role=role, payload={
                    "name": f"qa-quota-{role}-{index}-{time.time_ns()}",
                    "description": "isolated quota fixture",
                    "scopes": ["web-projects:read"],
                    "expires_in_days": 30,
                })
                if response.status == 201:
                    data = self.envelope(response, 201)
                    self.secrets.add(data["secret_key"])
                    return response.status, data["application"]["id"]
                return response.status, ""

            with concurrent.futures.ThreadPoolExecutor(max_workers=min(attempts, 24)) as pool:
                outcomes = list(pool.map(create, range(attempts)))
            successes = [application_id for status, application_id in outcomes if status == 201]
            failures = [status for status, _ in outcomes if status != 201]
            require(len(successes) == expected_success, f"created {len(successes)}, expected {expected_success}")
            require(all(status == 429 for status in failures), f"quota failures were {sorted(set(failures))}")
            self.created_application_ids = successes
            return f"success={len(successes)} rate_limited={len(failures)}"

        self.check("concurrent application quota", verify)
        return 0 if self.results[0]["result"] == "pass" else 1

    def run_revision(self, role: str) -> int:
        def verify() -> str:
            application, secret = self.create_app(role, "qa-revision-race", ["web-projects:read"])
            self.secrets.add(secret)

            def update(name: str) -> int:
                return self.request(
                    "PATCH",
                    f"/api/applications/{application['id']}",
                    role=role,
                    headers={"If-Match": str(application["revision"])},
                    payload={"name": name},
                ).status

            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                statuses = sorted(pool.map(update, ("qa-revision-a", "qa-revision-b")))
            require(statuses == [200, 409], f"concurrent revisions returned {statuses}")
            self.created_application_ids = [application["id"]]
            return "one update committed and one stale revision was rejected"

        self.check("concurrent application revision", verify)
        return 0 if self.results[0]["result"] == "pass" else 1

    def run_terminal(self, role: str) -> int:
        def verify() -> str:
            application, secret = self.create_app(role, "qa-terminal-lifecycle", ["web-projects:read"])
            token, _, _ = self.issue(application, secret)
            revoked = self.envelope(self.request(
                "DELETE", f"/api/applications/{application['id']}", role=role,
                headers={"If-Match": str(application["revision"])},
            ), 200)
            require(self.request("GET", "/api/web-projects", bearer=token).status == 401, "revoked token remained valid")
            try:
                self.issue(revoked, secret)
                raise ValidationFailure("revoked secret issued a token")
            except ValidationFailure as error:
                if str(error) == "revoked secret issued a token":
                    raise
            require(self.request(
                "PATCH", f"/api/applications/{application['id']}", role=role,
                headers={"If-Match": str(revoked["revision"])}, payload={"status": "enabled"},
            ).status == 409, "revoked application was restored")
            self.created_application_ids = [application["id"]]
            return "revoked token and secret rejected; revoked status remained terminal"

        self.check("terminal application revoke", verify)
        return 0 if self.results[0]["result"] == "pass" else 1

    def run_prepare_expiry(self, role: str) -> int:
        def verify() -> str:
            application, secret = self.create_app(role, "qa-credential-expiry", ["web-projects:read"])
            token, _, _ = self.issue(application, secret)
            self.write_credentials(application, secret, token)
            self.created_application_ids = [application["id"]]
            return "credential and pre-expiry token stored in protected file"

        self.check("prepare credential expiry", verify)
        return 0 if self.results[0]["result"] == "pass" else 1

    def run_legacy_user_web(self, role: str) -> int:
        def verify() -> str:
            suffix = str(time.time_ns())
            project = self.envelope(self.request("POST", "/api/web-projects", role=role, payload={
                "name": "qa-legacy-user-" + suffix,
                "description": "isolated legacy user fixture",
                "slug": "qa-legacy-user-" + suffix,
                "access_mode": "owner",
                "member_user_ids": [],
                "client_request_id": "qa-legacy-user-" + suffix,
            }), 201)
            require(self.request("GET", f"/api/web-projects/{project['id']}", role="viewer").status == 404, "legacy user owner isolation failed")
            boundary = "----cq-legacy-user"
            upload_body = (
                f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"index.html\"\r\n"
                "Content-Type: text/html\r\n\r\n<!doctype html><title>legacy-user</title>"
                f"\r\n--{boundary}--\r\n"
            ).encode()
            release = self.envelope(self.request(
                "POST", f"/api/web-projects/{project['id']}/releases", role=role, body=upload_body,
                headers={"Content-Type": f"multipart/form-data; boundary={boundary}", "Idempotency-Key": suffix},
            ), 201)
            published = self.envelope(self.request(
                "POST", f"/api/web-projects/{project['id']}/publish", role=role,
                headers={"If-Match": str(project["revision"])}, payload={"release_id": release["id"]},
            ), 200)
            deleted = self.envelope(self.request(
                "DELETE", f"/api/web-projects/{project['id']}", role=role,
                headers={"If-Match": str(published["revision"])},
            ), 200)
            restored = self.envelope(self.request(
                "POST", f"/api/web-projects/{project['id']}/restore", role=role,
                headers={"If-Match": str(deleted["revision"])},
            ), 200)
            require(restored["id"] == project["id"], "legacy user restore changed project identity")
            self.primary_project = restored
            return "user header CRUD, upload, publish, archive, restore and owner 404 verified"

        self.check("legacy user web-project lifecycle", verify)
        return 0 if self.results[0]["result"] == "pass" else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--ca-file", required=True, type=Path)
    parser.add_argument("--auth-file", required=True, type=Path)
    parser.add_argument("--credential-out", required=True, type=Path)
    parser.add_argument("--backend-port", required=True, type=int)
    parser.add_argument("--backend-log", required=True, type=Path)
    parser.add_argument("--results", required=True, type=Path)
    parser.add_argument("--mode", choices=("full", "quota", "revision", "terminal", "prepare-expiry", "legacy-user-web"), default="full")
    parser.add_argument("--quota-role", choices=("owner", "member", "viewer"), default="viewer")
    parser.add_argument("--attempts", type=int, default=0)
    parser.add_argument("--expected-success", type=int, default=0)
    args = parser.parse_args()
    validator = Validator(Client(args.base_url, args.ca_file), read_private_json(args.auth_file), args.credential_out, args.backend_port, args.backend_log)
    if args.mode == "quota":
        require(args.attempts > 0, "quota mode requires --attempts")
        exit_code = validator.run_quota(args.quota_role, args.attempts, args.expected_success)
    elif args.mode == "revision":
        exit_code = validator.run_revision(args.quota_role)
    elif args.mode == "terminal":
        exit_code = validator.run_terminal(args.quota_role)
    elif args.mode == "prepare-expiry":
        exit_code = validator.run_prepare_expiry(args.quota_role)
    elif args.mode == "legacy-user-web":
        exit_code = validator.run_legacy_user_web(args.quota_role)
    else:
        exit_code = validator.run()
    args.results.write_text(json.dumps({"checks":validator.results,"primary_project_id":(validator.primary_project or {}).get("id","").strip(),"created_application_ids":validator.created_application_ids}, indent=2) + "\n", encoding="utf-8")
    os.chmod(args.results, 0o600)
    print(json.dumps({"passed":sum(item["result"] == "pass" for item in validator.results),"failed":sum(item["result"] == "fail" for item in validator.results)}))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
