#!/usr/bin/env python3
"""Manage static web shares with private configuration and the repository API client."""

from __future__ import annotations

import argparse
import json
import math
import os
from pathlib import Path
import re
import socket
import ssl
import sys
from urllib.parse import urlencode

# Resolve the actual checkout when the skill is installed through a symlink.
sys.dont_write_bytecode = True
REPOSITORY = Path(__file__).resolve().parents[3]
if not (REPOSITORY / "script" / "home_server_api.py").is_file():
    print(json.dumps({"error": "missing_client", "message": "请保留完整 home_server 仓库，不能只复制 skill 目录"}, ensure_ascii=False), file=sys.stderr)
    sys.exit(2)
sys.path.insert(0, str(REPOSITORY / "script"))
import home_server_api as api


API_PATH = "/api/web-share"
MODES = ("owner", "members", "authenticated", "public")
ENDPOINTS = ("auto", "internal", "external")
CONFIG_ENV = "CQ_HOME_SERVER_SKILL_CONFIG"
SLUG = re.compile(r"[a-z0-9][a-z0-9-]{1,254}[a-z0-9]\Z")


class Parser(argparse.ArgumentParser):
    def error(self, message):
        # argparse may include arbitrary rejected values, including pasted SKs.
        raise api.ClientError("命令参数无效；请用 --help 查看参数，不支持命令行传入 AK/SK")


class EndpointUnavailable(api.ClientError):
    def __init__(self, probes):
        super().__init__("入口探测失败；检查 DNS、路由、TLS 和工具权限。沙箱限制需通过平台正式授权机制处理")
        self.probes = probes


def positive_id(value):
    if not re.fullmatch(r"[1-9][0-9]{0,18}", value) or int(value) > 2**63 - 1:
        raise argparse.ArgumentTypeError("需要正整数 ID 或 revision")
    return value


# 定义网页管理子命令及所需 ID、修订号、请求幂等键和可见范围参数，支持仅输出离线计划。
def parse_args(argv=None):
    parser = Parser(description=__doc__)
    parser.add_argument("--config", type=Path, default=Path(os.environ.get(CONFIG_ENV, "~/.config/cq-home-server/web-share.json")))
    parser.add_argument("--endpoint", choices=ENDPOINTS)
    parser.add_argument("--dry-run", action="store_true", help="离线校验并输出操作摘要，不读取凭证或发送请求")
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("doctor", "users", "list", "show", "releases", "create", "upload", "publish", "disable", "visibility"):
        command = commands.add_parser(name)
        if name in {"show", "releases", "upload", "publish", "disable", "visibility"}:
            command.add_argument("id", type=positive_id)
        if name in {"publish", "disable", "visibility"}:
            command.add_argument("--revision", type=positive_id, required=True)
        if name in {"list", "releases"}:
            command.add_argument("--cursor", type=positive_id)
            command.add_argument("--limit", type=int, default=20)
        if name == "list":
            command.add_argument("--status", choices=("draft", "enabled", "disabled", "deleted"))
        if name in {"create", "visibility"}:
            command.add_argument("--member", type=positive_id, action="append", default=[])
            command.add_argument("--clear-members", action="store_true")
        if name == "create":
            command.add_argument("--name", required=True)
            command.add_argument("--slug", required=True)
            command.add_argument("--description", default="")
            command.add_argument("--visibility", choices=MODES, default="owner")
        if name in {"create", "upload"}:
            command.add_argument("--request-id", required=True, help="本次操作的唯一标识；人工核对后重试时保留原值")
        if name == "upload":
            command.add_argument("--file", type=Path, required=True)
            command.add_argument("--entry-file")
        if name == "publish":
            command.add_argument("--release", type=positive_id, required=True)
        if name == "visibility":
            command.add_argument("--mode", choices=MODES, required=True)
    return parser.parse_args(argv)


# 读取私有配置并拒绝未知字段，规范内外网 HTTPS origin、超时与 endpoint，按配置文件目录解析相对凭证/CA 路径。
def load_config(path, endpoint_override):
    path = path.expanduser().absolute()
    try:
        config = json.loads(api._read_private_file(path))
    except json.JSONDecodeError as error:
        raise api.ClientError("私有配置不是有效 JSON") from error
    fields = {"internal_url", "external_url", "endpoint", "credentials_file", "ca_file", "connect_timeout", "timeout"}
    if not isinstance(config, dict) or set(config) - fields:
        raise api.ClientError("配置字段无效；请参照 config.example.json，AK/SK 放在独立凭证文件")
    for key in ("internal_url", "external_url"):
        if key in config:
            config[key] = api.validate_base_url(config[key])
    if not config.get("internal_url") and not config.get("external_url"):
        raise api.ClientError("至少配置一个 internal_url 或 external_url")
    selected = endpoint_override or config.get("endpoint", "auto")
    if selected not in ENDPOINTS or (selected != "auto" and not config.get(selected + "_url")):
        raise api.ClientError("所选 endpoint 无效或缺少对应 URL")
    config["endpoint"] = selected
    for key, default in (("connect_timeout", 3), ("timeout", 30)):
        value = config.get(key, default)
        if type(value) not in (int, float) or not math.isfinite(value) or value <= 0:
            raise api.ClientError("timeout 和 connect_timeout 必须是有限的正数")
        config[key] = value
    for key in ("credentials_file", "ca_file"):
        if key in config:
            value = config[key]
            if not isinstance(value, str) or not value or api.CONTROL_CHARACTER.search(value):
                raise api.ClientError("配置文件路径无效")
            file_path = Path(value).expanduser()
            config[key] = file_path if file_path.is_absolute() else path.parent / file_path
    return config


def build_operation(args):
    """Validate one requested action before authentication; never publish implicitly."""
    method, path, payload, headers, body = "GET", API_PATH, {}, {}, None
    if args.command == "doctor":
        path = "/ping"
    elif args.command == "users":
        path += "/eligible-users"
    elif hasattr(args, "id"):
        path += "/" + args.id
    if args.command in {"list", "releases"}:
        if not 1 <= args.limit <= 100:
            raise api.ClientError("--limit 必须在 1 到 100 之间")
        query = {"limit": args.limit}
        if args.cursor:
            query["cursor"] = args.cursor
        if getattr(args, "status", None):
            query["status"] = args.status
        if args.command == "releases":
            path += "/releases"
        path += "?" + urlencode(query)
    if hasattr(args, "request_id") and not re.fullmatch(r"[A-Za-z0-9._:-]{1,128}", args.request_id):
        raise api.ClientError("--request-id 应为 1 到 128 位字母、数字或 . _ : -")
    if args.command in {"create", "visibility"}:
        mode = args.visibility if args.command == "create" else args.mode
        if mode == "members":
            if bool(args.member) == args.clear_members:
                raise api.ClientError("members 需要完整 --member 列表，或显式 --clear-members；不能同时使用")
        elif args.member or args.clear_members:
            raise api.ClientError("只有 members 可携带成员参数")
        payload = {"access_mode": mode, "member_user_ids": sorted(set(args.member), key=int)}
        method = "POST" if args.command == "create" else "PATCH"
    if args.command == "create":
        if not args.name.strip() or len(args.name.strip()) > 256 or len(args.description.encode("utf-8")) > 4000 or not SLUG.fullmatch(args.slug):
            raise api.ClientError("name、description 或 slug 无效；slug 为 3 到 256 位小写字母、数字和非首尾连字符")
        payload.update(name=args.name.strip(), description=args.description, slug=args.slug, client_request_id=args.request_id)
    if args.command in {"upload", "publish", "disable"}:
        method = "POST"
        path += "/releases" if args.command == "upload" else "/" + args.command
    if args.command == "publish":
        payload = {"release_id": args.release}
    if hasattr(args, "revision"):
        headers["If-Match"] = args.revision
    if payload:
        headers["Content-Type"] = "application/json"
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    summary = dict(payload, method=method, path=path)
    if hasattr(args, "revision"):
        summary["revision"] = args.revision
    if args.command == "upload":
        if api.CONTROL_CHARACTER.search(args.file.name):
            raise api.ClientError("上传文件名不能包含控制字符")
        body, headers["Content-Type"] = api.build_multipart(args.file.expanduser(), args.entry_file)
        headers["Idempotency-Key"] = args.request_id
        summary.update(file=str(args.file.expanduser().absolute()), entry_file=args.entry_file, request_id=args.request_id, request_bytes=len(body))
    return method, path, headers, body, summary


def probe_reason(error):
    while error is not None:
        if isinstance(error, ssl.SSLCertVerificationError):
            return "tls_verification_failed"
        if isinstance(error, PermissionError):
            return "permission_denied"
        if isinstance(error, socket.gaierror):
            return "dns_failed"
        if isinstance(error, (TimeoutError, socket.timeout)):
            return "timeout"
        error = error.__cause__
    return "connection_failed"


# 仅通过未认证的 /ping 探测选择入口；自动模式按内网、外网顺序尝试，权限拒绝立即停止，不重放业务请求。
def select_endpoint(config, *, probe=False):
    selected = config["endpoint"]
    if selected != "auto" and not probe:
        return selected, config[selected + "_url"]
    candidates = ("internal", "external") if selected == "auto" else (selected,)
    failures = []
    for name in candidates:
        origin = config.get(name + "_url")
        if not origin:
            continue
        # Only unauthenticated probes may fall back. Never reuse this selection
        # loop for token exchanges or business requests, including failed writes.
        transport = api.HTTPTransport(origin, timeout=config["connect_timeout"], ca_file=config.get("ca_file"))
        try:
            response = transport.request("GET", "/ping")
            if response.status != 200:
                failures.append({"endpoint": name, "reason": "http_status", "status": response.status})
                continue
            if json.loads(response.body) != {"message": "pong"}:
                raise ValueError("unexpected ping")
            return name, origin
        except api.ClientError as error:
            reason = probe_reason(error)
            failures.append({"endpoint": name, "reason": reason})
            if reason == "permission_denied":
                raise EndpointUnavailable(failures) from error
        except (ValueError, UnicodeError):
            failures.append({"endpoint": name, "reason": "unexpected_ping"})
    raise EndpointUnavailable(failures)


# 执行离线计划或选定入口上的单次认证调用，统一脱敏 JSON 输出；写入结果未知时提示人工核对，不自动重试。
def main(argv=None):
    api_client = None
    sensitive = ()
    mutation_attempted = False
    try:
        args = parse_args(argv)
        config = load_config(args.config, args.endpoint)
        method, path, headers, body, summary = build_operation(args)
        if args.dry_run:
            selected = config["endpoint"]
            result = {"endpoint": selected, "origin": config.get(selected + "_url"), "result": dict(summary, dry_run=True)}
        else:
            credentials = None
            if args.command != "doctor":
                credentials = api.load_credentials(config.get("credentials_file"), os.environ)
                sensitive = (credentials.access_key, credentials.secret_key)
            selected, origin = select_endpoint(config, probe=args.command == "doctor")
            if args.command == "doctor":
                data = {"message": "pong"}
            else:
                transport = api.HTTPTransport(origin, timeout=config["timeout"], ca_file=config.get("ca_file"))
                api_client = api.HomeServerAPI(transport, credentials)
                api_client.exchange_token()
                sensitive += (api_client.access_token,)
                mutation_attempted = method != "GET"
                response = api_client.call(method, path, body=body, headers=headers)
                data = api.business_result(response, sensitive)
            result = {"endpoint": selected, "origin": origin, "result": data}
            if isinstance(data, dict) and isinstance(data.get("url"), str) and re.fullmatch(r"/p/[a-z0-9][a-z0-9-]{1,254}[a-z0-9]/", data["url"]):
                result["urls"] = {name: config[name + "_url"] + data["url"] for name in ("internal", "external") if config.get(name + "_url")}
        print(json.dumps(api.redact_output(result, sensitive), ensure_ascii=False, indent=2))
        return 0
    except EndpointUnavailable as error:
        print(json.dumps({"error": "endpoint_unavailable", "message": str(error), "probes": error.probes}, ensure_ascii=False), file=sys.stderr)
        return 3
    except (api.ClientError, OSError, ValueError) as error:
        message = api.sanitize_message(error, sensitive) if isinstance(error, api.ClientError) else "本地文件、配置或 TLS 初始化失败；检查文件路径、权限与 CA"
        result = {"error": "request_failed", "message": message, "mutation_attempted": mutation_attempted}
        if mutation_attempted:
            result["next_action"] = "未自动重试。先读取项目和版本核对结果；409 需重新核对并发修改，超时或 5xx 的写入结果可能待确认"
        print(json.dumps(result, ensure_ascii=False), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
