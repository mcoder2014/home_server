#!/usr/bin/env python3
"""Call one CQ Home Server endpoint with a short-lived application token."""

from __future__ import annotations

import argparse
import base64
import http.client
import json
import os
import re
import secrets
import ssl
import stat
import sys
import urllib.parse
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Mapping, Optional


ACCESS_KEY_ENV = "CQ_HOME_SERVER_ACCESS_KEY"
SECRET_KEY_ENV = "CQ_HOME_SERVER_SECRET_KEY"
BASE_URL_ENV = "CQ_HOME_SERVER_BASE_URL"
ALLOWED_METHODS = ("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD")
MAX_UPLOAD_BYTES = 50 * 1024 * 1024
CONTROL_CHARACTER = re.compile(r"[\x00-\x1f\x7f]")
AUTHORIZATION_VALUE = re.compile(r"(?i)\b(?:basic|bearer)\s+[A-Za-z0-9._~+/=-]+")
CQ_CREDENTIAL_VALUE = re.compile(r"\b(?:ak|sk|at)_cq_[A-Za-z0-9_-]+")
ACCESS_KEY_FORMAT = re.compile(r"^ak_cq_[A-Za-z0-9_-]{22}$")
SECRET_KEY_FORMAT = re.compile(r"^sk_cq_[A-Za-z0-9_-]{43}$")
ACCESS_TOKEN_FORMAT = re.compile(r"^at_cq_[A-Za-z0-9_-]{43}$")


class ClientError(Exception):
    """A user-facing error whose message is safe to print."""


@dataclass
class Credentials:
    access_key: str
    secret_key: str


@dataclass
class Response:
    status: int
    headers: dict[str, str]
    body: bytes


def validate_base_url(value: str) -> str:
    if not isinstance(value, str) or CONTROL_CHARACTER.search(value):
        raise ClientError("Base URL 必须是有效的 HTTPS origin")
    try:
        parsed = urllib.parse.urlsplit(value)
        port = parsed.port
    except ValueError as error:
        raise ClientError("Base URL 必须是有效的 HTTPS origin") from error
    if parsed.scheme.lower() != "https" or not parsed.hostname:
        raise ClientError("Base URL 必须使用 HTTPS")
    if parsed.username is not None or parsed.password is not None:
        raise ClientError("Base URL 不能包含用户名或密码")
    if parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
        raise ClientError("Base URL 只能包含 origin，不能包含路径、查询或 fragment")

    host = parsed.hostname.lower()
    rendered_host = f"[{host}]" if ":" in host else host
    rendered_port = f":{port}" if port is not None and port != 443 else ""
    return f"https://{rendered_host}{rendered_port}"


def validate_request_path(value: str) -> str:
    if not isinstance(value, str) or not value.startswith("/") or value.startswith("//"):
        raise ClientError("--path 必须是以 / 开头的同源路径")
    if "\\" in value or CONTROL_CHARACTER.search(value):
        raise ClientError("--path 包含非法字符")
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme or parsed.netloc or parsed.fragment:
        raise ClientError("--path 只能使用同源路径，且不能包含 fragment")
    sensitive_names = {"access_key", "secret_key", "access_token", "client_id", "client_secret", "token"}
    if any(name.lower() in sensitive_names for name, _ in urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)):
        raise ClientError("认证信息不能放在 URL 查询参数中")
    return value


def _validate_credentials(access_key: object, secret_key: object) -> Credentials:
    if not isinstance(access_key, str) or not ACCESS_KEY_FORMAT.fullmatch(access_key):
        raise ClientError("Access Key 格式无效")
    if not isinstance(secret_key, str) or not SECRET_KEY_FORMAT.fullmatch(secret_key):
        raise ClientError("Secret Key 格式无效")
    return Credentials(access_key, secret_key)


def _read_private_file(path: Path) -> str:
    flags = os.O_RDONLY
    if hasattr(os, "O_CLOEXEC"):
        flags |= os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise ClientError(f"无法读取凭证文件：{path}") from error
    try:
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise ClientError("凭证文件必须是普通文件，不能是目录或符号链接")
        if os.name == "posix":
            if hasattr(os, "getuid") and metadata.st_uid != os.getuid():
                raise ClientError("凭证文件必须属于当前用户")
            if stat.S_IMODE(metadata.st_mode) & 0o077:
                raise ClientError(f"凭证文件权限过宽，请执行 chmod 600 {path}")
        with os.fdopen(descriptor, "r", encoding="utf-8") as file:
            descriptor = -1
            return file.read()
    except UnicodeDecodeError as error:
        raise ClientError("凭证文件必须使用 UTF-8 编码") from error
    finally:
        if descriptor >= 0:
            os.close(descriptor)


def load_credentials(path: Optional[Path], environment: Mapping[str, str]) -> Credentials:
    if path is None:
        access_key = environment.get(ACCESS_KEY_ENV)
        secret_key = environment.get(SECRET_KEY_ENV)
        if not access_key or not secret_key:
            raise ClientError(
                f"请使用 --credentials-file，或同时设置 {ACCESS_KEY_ENV} 和 {SECRET_KEY_ENV}"
            )
        return _validate_credentials(access_key, secret_key)

    try:
        payload = json.loads(_read_private_file(path))
    except json.JSONDecodeError as error:
        raise ClientError("凭证文件不是有效的 JSON") from error
    if not isinstance(payload, dict):
        raise ClientError("凭证文件必须是 JSON object")
    return _validate_credentials(payload.get("access_key"), payload.get("secret_key"))


def sanitize_message(message: object, sensitive_values=()) -> str:
    value = str(message)
    value = AUTHORIZATION_VALUE.sub("[REDACTED]", value)
    value = CQ_CREDENTIAL_VALUE.sub("[REDACTED]", value)
    for sensitive in sensitive_values:
        if sensitive:
            value = value.replace(sensitive, "[REDACTED]")
    return value[:500]


def redact_output(value: object, sensitive_values=()) -> object:
    sensitive_keys = {
        "access_key",
        "secret_key",
        "access_token",
        "refresh_token",
        "client_secret",
        "authorization",
        "token",
    }
    if isinstance(value, dict):
        return {
            key: "[REDACTED]" if str(key).lower() in sensitive_keys else redact_output(item, sensitive_values)
            for key, item in value.items()
        }
    if isinstance(value, list):
        return [redact_output(item, sensitive_values) for item in value]
    if isinstance(value, tuple):
        return tuple(redact_output(item, sensitive_values) for item in value)
    if isinstance(value, str):
        rendered = AUTHORIZATION_VALUE.sub("[REDACTED]", value)
        rendered = CQ_CREDENTIAL_VALUE.sub("[REDACTED]", rendered)
        for sensitive in sensitive_values:
            if sensitive:
                rendered = rendered.replace(sensitive, "[REDACTED]")
        return rendered
    return value


class HTTPTransport:
    """A single-origin HTTPS transport that deliberately has no redirect logic."""

    def __init__(
        self,
        base_url: str,
        *,
        timeout: float = 15,
        ca_file: Optional[Path] = None,
        connection_factory: Optional[Callable[[], http.client.HTTPSConnection]] = None,
    ) -> None:
        self.origin = validate_base_url(base_url)
        parsed = urllib.parse.urlsplit(self.origin)
        self.host = parsed.hostname or ""
        self.port = parsed.port or 443
        self.timeout = timeout
        self._connection_factory = connection_factory
        if connection_factory is None:
            try:
                self._context = ssl.create_default_context(cafile=str(ca_file) if ca_file else None)
            except (OSError, ssl.SSLError) as error:
                raise ClientError("无法加载 CA 文件") from error
        else:
            self._context = None

    def _connection(self):
        if self._connection_factory is not None:
            return self._connection_factory()
        return http.client.HTTPSConnection(
            self.host,
            self.port,
            timeout=self.timeout,
            context=self._context,
        )

    def request(self, method: str, path: str, *, headers=None, body: Optional[bytes] = None) -> Response:
        method = method.upper()
        if method not in ALLOWED_METHODS:
            raise ClientError(f"不支持 HTTP 方法 {method}")
        path = validate_request_path(path)
        request_headers = {"Accept": "application/json"}
        request_headers.update(headers or {})
        connection = self._connection()
        try:
            connection.request(method, path, body=body, headers=request_headers)
            raw = connection.getresponse()
            response_headers = {name.lower(): value for name, value in raw.getheaders()}
            return Response(raw.status, response_headers, raw.read())
        except (OSError, http.client.HTTPException) as error:
            raise ClientError("HTTPS 请求失败") from error
        finally:
            connection.close()


def _response_json(response: Response, sensitive_values) -> object:
    try:
        return json.loads(response.body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ClientError(f"HTTP {response.status} 返回了无效 JSON") from error


def _response_error(response: Response, sensitive_values) -> ClientError:
    message = "request failed"
    code = ""
    try:
        payload = json.loads(response.body.decode("utf-8"))
        if isinstance(payload, dict):
            code = payload.get("error") or payload.get("code") or ""
            message = payload.get("error_description") or payload.get("message") or payload.get("msg") or message
    except (UnicodeDecodeError, json.JSONDecodeError):
        pass
    rendered_code = sanitize_message(code, sensitive_values)
    rendered_message = sanitize_message(message, sensitive_values)
    prefix = f"HTTP {response.status}"
    if rendered_code:
        prefix += f" ({rendered_code})"
    return ClientError(f"{prefix}: {rendered_message}")


class HomeServerAPI:
    def __init__(self, transport, credentials: Credentials) -> None:
        self.transport = transport
        self.credentials = credentials
        self.access_token = ""

    def exchange_token(self) -> str:
        raw = f"{self.credentials.access_key}:{self.credentials.secret_key}".encode("utf-8")
        authorization = "Basic " + base64.b64encode(raw).decode("ascii")
        response = self.transport.request(
            "POST",
            "/api/auth/token",
            headers={
                "Authorization": authorization,
                "Content-Type": "application/x-www-form-urlencoded",
                "Cache-Control": "no-store",
            },
            body=b"grant_type=client_credentials",
        )
        sensitive = (self.credentials.access_key, self.credentials.secret_key)
        if not 200 <= response.status < 300:
            raise _response_error(response, sensitive)
        payload = _response_json(response, sensitive)
        if not isinstance(payload, dict):
            raise ClientError("token endpoint 返回格式无效")
        token = payload.get("access_token")
        token_type = payload.get("token_type")
        expires_in = payload.get("expires_in")
        if (
            not isinstance(token, str)
            or not ACCESS_TOKEN_FORMAT.fullmatch(token)
            or not isinstance(token_type, str)
            or token_type.lower() != "bearer"
            or not isinstance(expires_in, int)
            or expires_in <= 0
        ):
            raise ClientError("token endpoint 返回格式无效")
        self.access_token = token
        return token

    def call(self, method: str, path: str, *, body: Optional[bytes] = None, headers=None) -> Response:
        path = validate_request_path(path)
        token = self.access_token or self.exchange_token()
        request_headers = dict(headers or {})
        request_headers["Authorization"] = f"Bearer {token}"
        response = self.transport.request(method, path, headers=request_headers, body=body)
        if not 200 <= response.status < 300:
            raise _response_error(
                response,
                (self.credentials.access_key, self.credentials.secret_key, token),
            )
        return response


def build_multipart(upload_file: Path, entry_file: Optional[str], *, boundary: Optional[str] = None):
    suffix = upload_file.suffix.lower()
    if suffix not in {".html", ".htm", ".zip"}:
        raise ClientError("--upload-file 仅支持 HTML 或 ZIP")
    try:
        metadata = upload_file.stat()
        if not stat.S_ISREG(metadata.st_mode):
            raise ClientError("--upload-file 必须是普通文件")
        if metadata.st_size > MAX_UPLOAD_BYTES:
            raise ClientError("上传文件不能超过 50 MiB")
        content = upload_file.read_bytes()
    except OSError as error:
        raise ClientError(f"无法读取上传文件：{upload_file}") from error
    if entry_file is not None:
        if CONTROL_CHARACTER.search(entry_file) or len(entry_file.encode("utf-8")) > 2048:
            raise ClientError("--entry-file 格式无效或超过 2048 字节")

    selected_boundary = boundary or "----cq-home-server-" + secrets.token_hex(12)
    filename = upload_file.name.replace('"', "_")
    content_type = "application/zip" if suffix == ".zip" else "text/html"
    parts = []
    if entry_file:
        parts.append(
            f'--{selected_boundary}\r\nContent-Disposition: form-data; name="entry_file"\r\n\r\n{entry_file}\r\n'.encode(
                "utf-8"
            )
        )
    parts.extend([
        (
            f'--{selected_boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\n'
            f"Content-Type: {content_type}\r\n\r\n"
        ).encode("utf-8"),
        content,
        f"\r\n--{selected_boundary}--\r\n".encode("ascii"),
    ])
    return b"".join(parts), f"multipart/form-data; boundary={selected_boundary}"


def read_json_body(path: Path) -> bytes:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ClientError(f"无法读取有效的 JSON 文件：{path}") from error
    return json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def business_result(response: Response, sensitive_values) -> object:
    content_type = response.headers.get("content-type", "").lower()
    if "json" in content_type or response.body.lstrip().startswith((b"{", b"[")):
        payload = _response_json(response, sensitive_values)
        if isinstance(payload, dict) and "code" in payload:
            if payload.get("code") != 0:
                raise _response_error(response, sensitive_values)
            return payload.get("data")
        return payload
    if content_type.startswith("text/"):
        try:
            return response.body.decode("utf-8")
        except UnicodeDecodeError as error:
            raise ClientError("文本响应不是有效 UTF-8") from error
    raise ClientError("响应是二进制内容，请使用 --output 保存")


def write_output(path: Path, body: bytes) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
    except OSError as error:
        raise ClientError(f"无法写入输出文件：{path}") from error


class SafeArgumentParser(argparse.ArgumentParser):
    """Reject unsupported credential arguments without echoing their values."""

    def error(self, message):
        safe_message = str(redact_output(message))
        super().error(safe_message)


def parse_args(argv=None):
    parser = SafeArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default=os.environ.get(BASE_URL_ENV), help=f"HTTPS origin，也可用 {BASE_URL_ENV}")
    parser.add_argument("--credentials-file", type=Path, help="管理页下载的 0600 凭证 JSON")
    parser.add_argument("--path", required=True, help="以 / 开头的同源请求路径")
    parser.add_argument("--method", choices=ALLOWED_METHODS, default="GET")
    body = parser.add_mutually_exclusive_group()
    body.add_argument("--json-file", type=Path, help="请求 JSON 文件")
    body.add_argument("--upload-file", type=Path, help="HTML 或 ZIP 上传文件")
    parser.add_argument("--entry-file", help="ZIP 入口文件，与 --upload-file 一起使用")
    parser.add_argument("--if-match", help="写请求的 revision")
    parser.add_argument("--output", type=Path, help="保存二进制响应")
    parser.add_argument("--ca-file", type=Path, help="自签名服务的 CA 证书；仍会校验主机名")
    parser.add_argument("--timeout", type=float, default=15)
    args = parser.parse_args(argv)
    if not args.base_url:
        parser.error(f"必须提供 --base-url 或 {BASE_URL_ENV}")
    if args.entry_file and not args.upload_file:
        parser.error("--entry-file 只能与 --upload-file 一起使用")
    if args.timeout <= 0:
        parser.error("--timeout 必须大于 0")
    return args


def main(argv=None) -> int:
    try:
        args = parse_args(argv)
        credentials = load_credentials(args.credentials_file, os.environ)
        transport = HTTPTransport(args.base_url, timeout=args.timeout, ca_file=args.ca_file)
        api = HomeServerAPI(transport, credentials)
        headers = {}
        body = None
        if args.json_file:
            body = read_json_body(args.json_file)
            headers["Content-Type"] = "application/json"
        elif args.upload_file:
            body, headers["Content-Type"] = build_multipart(args.upload_file, args.entry_file)
        if args.if_match:
            if CONTROL_CHARACTER.search(args.if_match):
                raise ClientError("--if-match 包含非法字符")
            headers["If-Match"] = args.if_match

        response = api.call(args.method, args.path, body=body, headers=headers)
        sensitive = (credentials.access_key, credentials.secret_key, api.access_token)
        if args.output:
            content_type = response.headers.get("content-type", "").lower()
            if "json" in content_type or response.body.lstrip().startswith((b"{", b"[")):
                result = redact_output(business_result(response, sensitive), sensitive)
                output_body = json.dumps(result, ensure_ascii=False, indent=2).encode("utf-8") + b"\n"
            else:
                output_body = response.body
            write_output(args.output, output_body)
            print(json.dumps({"output": str(args.output), "bytes": len(output_body)}, ensure_ascii=False))
        else:
            result = redact_output(business_result(response, sensitive), sensitive)
            if isinstance(result, str):
                print(result)
            else:
                print(json.dumps(result, ensure_ascii=False, indent=2))
        return 0
    except ClientError as error:
        print(f"错误：{error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
