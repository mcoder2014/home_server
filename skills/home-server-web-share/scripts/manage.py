#!/usr/bin/env python3
"""Manage static web shares with private configuration and the repository API client."""

from __future__ import annotations

import argparse
import json
import math
import os
import posixpath
from pathlib import Path
import re
import socket
import ssl
import stat
import sys
from urllib.parse import parse_qsl, urlencode

# Resolve the actual checkout when the skill is installed through a symlink.
sys.dont_write_bytecode = True
REPOSITORY = Path(__file__).resolve().parents[3]
if not (REPOSITORY / "script" / "home_server_api.py").is_file():
    print(json.dumps({"error": "missing_client", "message": "请保留完整 home_server 仓库，不能只复制 skill 目录"}, ensure_ascii=False), file=sys.stderr)
    sys.exit(2)
sys.path.insert(0, str(REPOSITORY / "script"))
sys.path.insert(0, str(Path(__file__).resolve().parent))
import home_server_api as api
import html_check


API_PATH = "/api/web-share"
MODES = ("owner", "members", "authenticated", "public")
ENDPOINTS = ("auto", "internal", "external")
CONFIG_ENV = "CQ_HOME_SERVER_SKILL_CONFIG"
SLUG = re.compile(r"[a-z0-9][a-z0-9-]{1,254}[a-z0-9]\Z")
STABLE_COMMENT_ID = re.compile(r"[a-z][a-z0-9]*(?:-[a-z0-9]+)*\Z")
REQUEST_ID = re.compile(r"[A-Za-z0-9._:-]{1,128}\Z")


def read_regular_file(path, maximum_bytes, label):
    path = path.expanduser().absolute()
    try:
        info = path.lstat()
    except OSError as error:
        raise api.ClientError(f"{label}文件不可读") from error
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode) or info.st_size > maximum_bytes:
        raise api.ClientError(f"{label}文件必须是大小受限的普通文件，不能使用符号链接")
    try:
        data = path.read_bytes()
    except OSError as error:
        raise api.ClientError(f"{label}文件不可读") from error
    if len(data) > maximum_bytes:
        raise api.ClientError(f"{label}文件过大")
    return data


def comment_page(value):
    if not isinstance(value, str) or not value or len(value.encode("utf-8")) > 2048 or "\\" in value or any(char in value for char in "\x00?#"):
        raise argparse.ArgumentTypeError("--page 必须是项目内规范相对路径")
    if value.startswith("/") or posixpath.normpath(value) != value or value.startswith("../"):
        raise argparse.ArgumentTypeError("--page 必须是项目内规范相对路径")
    return value


def read_comment_body(path):
    try:
        text = read_regular_file(path, 16000, "评论正文").decode("utf-8")
    except UnicodeDecodeError as error:
        raise api.ClientError("评论正文必须是 UTF-8") from error
    if not text.strip() or len(text) > 4000:
        raise api.ClientError("评论正文必须为 1 到 4000 个 Unicode 字符")
    return text


def read_anchor(path):
    try:
        value = json.loads(read_regular_file(path, 32768, "锚点").decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise api.ClientError("锚点文件必须是有效 UTF-8 JSON") from error
    allowed = {"kind", "target_id", "exact", "prefix", "suffix", "label", "page_id"}
    if not isinstance(value, dict) or set(value) - allowed or not all(isinstance(item, str) for item in value.values()):
        raise api.ClientError("锚点字段无效")
    kind = value.get("kind")
    target_id, page_id = value.get("target_id", ""), value.get("page_id", "")
    if kind not in {"text", "image", "module", "page"}:
        raise api.ClientError("锚点 kind 无效")
    if target_id and (len(target_id) > 96 or not STABLE_COMMENT_ID.fullmatch(target_id)):
        raise api.ClientError("锚点 target_id 无效")
    if page_id and (len(page_id) > 96 or not STABLE_COMMENT_ID.fullmatch(page_id)):
        raise api.ClientError("锚点 page_id 无效")
    if kind == "text" and not value.get("exact", "").strip():
        raise api.ClientError("文字锚点必须包含 exact")
    if kind in {"image", "module"} and not target_id:
        raise api.ClientError("图片或组件锚点必须包含稳定 target_id")
    limits = {"exact": 4096, "prefix": 128, "suffix": 128, "label": 256}
    if any(len(value.get(name, "")) > limit for name, limit in limits.items()):
        raise api.ClientError("锚点文本超过长度限制")
    return value


class Parser(argparse.ArgumentParser):
    def error(self, message):
        # argparse may include arbitrary rejected values, including pasted SKs.
        raise api.ClientError("命令参数无效；请用 --help 查看参数，不支持命令行传入 AK/SK")


class EndpointUnavailable(api.ClientError):
    def __init__(self, probes):
        super().__init__("入口探测失败；检查 DNS、路由、TLS 和工具权限。沙箱限制需通过平台正式授权机制处理")
        self.probes = probes


class HTMLIncompatible(api.ClientError):
    def __init__(self, report):
        super().__init__("上传前检查发现阻断项；修正产物或核对项目容器模式后再上传")
        self.report = report


def positive_id(value):
    if not re.fullmatch(r"[1-9][0-9]{0,18}", value) or int(value) > 2**63 - 1:
        raise argparse.ArgumentTypeError("需要正整数 ID 或 revision")
    return value


def nonnegative_id(value):
    if not re.fullmatch(r"[0-9]{1,19}", value) or int(value) > 2**63 - 1:
        raise argparse.ArgumentTypeError("需要非负整数 sequence")
    return value


def request_id(value):
    if not REQUEST_ID.fullmatch(value):
        raise argparse.ArgumentTypeError("--request-id 应为 1 到 128 位字母、数字或 . _ : -")
    return value


# 定义网页管理子命令及所需 ID、修订号、请求幂等键和可见范围参数，支持仅输出离线计划。
def parse_args(argv=None):
    parser = Parser(description=__doc__)
    parser.add_argument("--config", type=Path, default=Path(os.environ.get(CONFIG_ENV, "~/.config/cq-home-server/web-share.json")))
    parser.add_argument("--endpoint", choices=ENDPOINTS)
    parser.add_argument("--dry-run", action="store_true", help="离线校验并输出操作摘要，不读取凭证或发送请求")
    commands = parser.add_subparsers(dest="command", required=True)
    for name in (
        "doctor", "users", "list", "show", "releases", "create", "upload", "publish", "disable", "visibility",
        "check-html", "comments", "comment-show", "comment-create", "comment-reply", "comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor",
    ):
        command = commands.add_parser(name)
        if name in {
            "show", "releases", "upload", "publish", "disable", "visibility", "comments", "comment-show", "comment-create",
            "comment-reply", "comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor",
        }:
            command.add_argument("id", type=positive_id)
        if name in {"comment-show", "comment-reply", "comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor"}:
            command.add_argument("thread_id", type=positive_id)
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
        if name in {"upload", "check-html"}:
            command.add_argument("--file", type=Path, required=True)
            command.add_argument("--entry-file")
        if name == "check-html":
            command.add_argument("--mode", choices=("enhanced", "raw"), default="enhanced")
        if name == "publish":
            command.add_argument("--release", type=positive_id, required=True)
        if name == "visibility":
            command.add_argument("--mode", choices=MODES, required=True)
        if name == "comments":
            command.add_argument("--cursor", type=positive_id)
            command.add_argument("--limit", type=int, default=100)
            command.add_argument("--status", choices=("all", "open", "resolved", "deleted"), default="all")
            command.add_argument("--request-id", type=request_id)
        if name == "comment-show":
            command.add_argument("--after-seq", type=nonnegative_id, default="0")
            command.add_argument("--limit", type=int, default=100)
        if name in {"comment-create", "comment-reanchor"}:
            command.add_argument("--page", type=comment_page, required=True)
            command.add_argument("--anchor-file", type=Path)
            command.add_argument("--release", type=positive_id, required=True)
        if name in {"comment-reply", "comment-resolve", "comment-delete", "comment-reopen"}:
            command.add_argument("--release", type=positive_id)
        if name in {"comment-create", "comment-reply"}:
            command.add_argument("--body-file", type=Path, required=True)
        if name in {"comment-create", "comment-reply", "comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor"}:
            command.add_argument("--request-id", type=request_id, required=True)
        if name in {"comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor"}:
            command.add_argument("--revision", type=positive_id, required=True)
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


# 校验 CLI 动作并构造方法、路径、版本头和正文；上传只准备本地文件，不隐式发送请求或发布网页。
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
    if getattr(args, "request_id", None) and not REQUEST_ID.fullmatch(args.request_id):
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
        body, headers["Content-Type"], content = api.build_multipart(args.file.expanduser(), args.entry_file, include_content=True)
        args.html_findings = html_check.inspect_upload(content, args.file.suffix, args.entry_file)
        args.html_report = html_check.report(args.html_findings, "unknown")
        if args.html_report["errors"]:
            raise HTMLIncompatible(args.html_report)
        headers["Idempotency-Key"] = args.request_id
        summary.update(file=str(args.file.expanduser().absolute()), entry_file=args.entry_file, request_id=args.request_id, request_bytes=len(body))
    if args.command == "comments":
        if not 1 <= args.limit <= 100:
            raise api.ClientError("--limit 必须在 1 到 100 之间")
        query = {"limit": args.limit, "status": args.status}
        if args.cursor:
            query["cursor"] = args.cursor
        if args.request_id:
            query["request_id"] = args.request_id
        path += "/comment-threads?" + urlencode(query)
        summary.update(method="GET", path=path)
    if args.command == "comment-show":
        if not 1 <= args.limit <= 100:
            raise api.ClientError("--limit 必须在 1 到 100 之间")
        path += "/comment-threads/" + args.thread_id
        summary.update(method="GET", path=path, after_seq=args.after_seq, limit=args.limit)
    if args.command in {"comment-create", "comment-reply", "comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor"}:
        method = "POST"
        path += "/comment-threads"
        if args.command != "comment-create":
            suffix = {
                "comment-reply": "replies",
                "comment-resolve": "resolve",
                "comment-delete": "delete",
                "comment-reopen": "reopen",
                "comment-reanchor": "reanchor",
            }[args.command]
            path += "/" + args.thread_id + "/" + suffix
        payload = {"request_id": args.request_id}
        summary = {"method": method, "path": path, "request_id": args.request_id}
        if args.command in {"comment-create", "comment-reanchor"}:
            anchor = read_anchor(args.anchor_file) if args.anchor_file else {"kind": "page"}
            page_key = "id:" + anchor["page_id"] if anchor.get("page_id") else "path:" + args.page
            payload.update(release_id=args.release, page_key=page_key, page_path=args.page, anchor=anchor)
            summary.update(release_id=args.release, page=args.page, anchor_kind=anchor["kind"])
        elif getattr(args, "release", None):
            payload["release_id"] = args.release
            summary["release_id"] = args.release
        if args.command in {"comment-create", "comment-reply"}:
            comment_body = read_comment_body(args.body_file)
            payload["body"] = comment_body
            summary["body_characters"] = len(comment_body)
        if args.command in {"comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor"}:
            headers["If-Match"] = args.revision
            summary["revision"] = args.revision
        headers["Content-Type"] = "application/json"
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
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


def replace_query_parameter(path, name, value):
    base, separator, query = path.partition("?")
    values = [(key, item) for key, item in parse_qsl(query, keep_blank_values=True) if key != name]
    values.append((name, str(value)))
    return base + (separator or "?") + urlencode(values)


def call_business(api_client, method, path, headers, body, sensitive):
    response = api_client.call(method, path, body=body, headers=headers)
    return api.business_result(response, sensitive)


def fetch_all_pages(api_client, path, cursor_parameter, next_field, sensitive):
    items = []
    cursors = set()
    pages = 0
    while True:
        data = call_business(api_client, "GET", path, {}, None, sensitive)
        if not isinstance(data, dict) or not isinstance(data.get("items"), list) or not isinstance(data.get("has_more"), bool):
            raise api.ClientError("评论分页响应格式无效")
        items.extend(data["items"])
        pages += 1
        if not data["has_more"]:
            result = dict(data)
            result.update(items=items, has_more=False, next_cursor="", pages=pages)
            if next_field == "next_seq":
                result["next_seq"] = ""
            return result
        cursor = data.get(next_field) or data.get("next_cursor")
        if not isinstance(cursor, str) or not re.fullmatch(r"[1-9][0-9]{0,18}", cursor) or cursor in cursors:
            raise api.ClientError("评论分页游标无效或未推进")
        cursors.add(cursor)
        path = replace_query_parameter(path, cursor_parameter, cursor)


def execute_operation(api_client, args, method, path, headers, body, sensitive):
    if args.command == "comments":
        return fetch_all_pages(api_client, path, "cursor", "next_cursor", sensitive)
    if args.command == "comment-show":
        thread = call_business(api_client, "GET", path, {}, None, sensitive)
        if not isinstance(thread, dict) or not isinstance(thread.get("id"), str):
            raise api.ClientError("评论主题响应格式无效")
        query_values = {"limit": args.limit}
        if args.after_seq != "0":
            query_values["after_seq"] = args.after_seq
        query = urlencode(query_values)
        events = fetch_all_pages(api_client, path + "/events?" + query, "after_seq", "next_seq", sensitive)
        return {"thread": thread, "events": events["items"], "pages": events["pages"]}
    if args.command in {"comment-create", "comment-reply", "comment-resolve", "comment-delete", "comment-reopen", "comment-reanchor"}:
        written = call_business(api_client, method, path, headers, body, sensitive)
        if not isinstance(written, dict) or not isinstance(written.get("id"), str):
            raise api.ClientError("评论写入响应格式无效")
        thread_path = API_PATH + "/" + args.id + "/comment-threads/" + written["id"]
        thread = call_business(api_client, "GET", thread_path, {}, None, sensitive)
        event_query = urlencode({"limit": 100, "request_id": args.request_id})
        events = fetch_all_pages(api_client, thread_path + "/events?" + event_query, "after_seq", "next_seq", sensitive)
        expected_kind = args.command.removeprefix("comment-").replace("create", "comment")
        if len(events["items"]) != 1 or not isinstance(events["items"][0], dict) or events["items"][0].get("request_id") != args.request_id or events["items"][0].get("kind") != expected_kind:
            raise api.ClientError("评论写入已返回，但按 request_id 读回事件失败；请停止重试并人工核对")
        expected_status = {"comment-delete": "deleted", "comment-resolve": "resolved", "comment-reopen": "open"}.get(args.command)
        if not isinstance(thread, dict) or thread.get("id") != written["id"] or expected_status and thread.get("status") != expected_status:
            raise api.ClientError("评论事件已写入，但当前主题状态不一致；可能有并发更新，请停止重试并核对最新历史")
        return {"thread": thread, "event": events["items"][0]}
    return call_business(api_client, method, path, headers, body, sensitive)


# 执行离线计划或选定入口上的单次认证调用，统一脱敏 JSON 输出；写入结果未知时提示人工核对，不自动重试。
def main(argv=None):
    api_client = None
    sensitive = ()
    mutation_attempted = False
    try:
        args = parse_args(argv)
        if args.command == "check-html":
            if api.CONTROL_CHARACTER.search(args.file.name):
                raise api.ClientError("上传文件名不能包含控制字符")
            _, _, content = api.build_multipart(args.file.expanduser(), args.entry_file, include_content=True)
            checked = html_check.report(html_check.inspect_upload(content, args.file.suffix, args.entry_file), args.mode)
            print(json.dumps({"result": checked, "mutation_attempted": False}, ensure_ascii=False, indent=2))
            return 1 if checked["errors"] else 0
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
                if args.command == "upload":
                    project = call_business(api_client, "GET", API_PATH + "/" + args.id, {}, None, sensitive)
                    if not isinstance(project, dict) or project.get("container_mode") not in {"raw", "enhanced"}:
                        raise api.ClientError("无法核对项目容器模式；上传尚未发送，请确认服务端已升级")
                    args.html_report = html_check.report(args.html_findings, project["container_mode"])
                    if args.html_report["errors"]:
                        raise HTMLIncompatible(args.html_report)
                mutation_attempted = method != "GET"
                data = execute_operation(api_client, args, method, path, headers, body, sensitive)
            result = {"endpoint": selected, "origin": origin, "result": data}
            if isinstance(data, dict) and isinstance(data.get("url"), str) and re.fullmatch(r"/p/[a-z0-9][a-z0-9-]{1,254}[a-z0-9]/", data["url"]):
                result["urls"] = {name: config[name + "_url"] + data["url"] for name in ("internal", "external") if config.get(name + "_url")}
        if hasattr(args, "html_report"):
            result["html_check"] = args.html_report
        print(json.dumps(api.redact_output(result, sensitive), ensure_ascii=False, indent=2))
        return 0
    except HTMLIncompatible as error:
        print(json.dumps(api.redact_output({"error": "html_incompatible", "message": str(error), "html_check": error.report,
                                           "mutation_attempted": False}, sensitive), ensure_ascii=False, indent=2), file=sys.stderr)
        return 1
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
