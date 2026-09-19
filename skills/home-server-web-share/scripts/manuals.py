#!/usr/bin/env python3
"""Manage equipment manuals through the CQ Home Server manuals API."""

from __future__ import annotations

import argparse
import json
import os
import re
import secrets
import stat
import sys
import urllib.parse
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Optional


sys.dont_write_bytecode = True
REPOSITORY = Path(__file__).resolve().parents[3]
if not (REPOSITORY / "script" / "home_server_api.py").is_file():
    print(json.dumps({"error": "missing_client", "message": "请保留完整 home_server 仓库，不能只复制 skill 目录"}, ensure_ascii=False), file=sys.stderr)
    sys.exit(2)
sys.path.insert(0, str(REPOSITORY / "script"))
import home_server_api as api

# Reuse the web-share skill's private configuration and endpoint-selection
# rules. Both CLIs intentionally use the same credentials and HTTPS client.
sys.path.insert(0, str(Path(__file__).resolve().parent))
import manage


API_PATH = "/api/manuals"
CONFIG_ENV = "CQ_HOME_SERVER_SKILL_CONFIG"
ACCESS_MODES = ("owner", "authenticated", "public")
MAX_FILE_BYTES = 50 * 1024 * 1024
MAX_TEXT_RUNES = 100_000
MAX_CATEGORIES = 20
MAX_CATEGORY_RUNES = 100
MAX_NAME_RUNES = 120
MAX_DESCRIPTION_RUNES = 2_000
MAX_TITLE_RUNES = 200
MAX_URL_BYTES = 2_048
MAX_ITEMS = 100
REQUEST_ID = re.compile(r"[A-Za-z0-9._:-]{1,128}\Z")
SUPPORTED_UPLOADS = {
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".png": "image/png",
    ".gif": "image/gif",
    ".webp": "image/webp",
    ".pdf": "application/pdf",
    ".txt": "text/plain; charset=utf-8",
}


class PartialFailure(api.ClientError):
    """A mutation stopped after zero or more server-side writes."""

    def __init__(self, message, context):
        super().__init__(message)
        self.context = context


@dataclass
class AppendPlan:
    kind: str
    request_id: str
    title: str = ""
    file: Optional[Path] = None
    text: str = ""
    url: str = ""

    def summary(self):
        result = {"kind": self.kind, "request_id": self.request_id}
        if self.title:
            result["title"] = self.title
        if self.file is not None:
            result["file"] = str(self.file)
            try:
                result["bytes"] = self.file.stat().st_size
            except OSError:
                pass
        elif self.kind == "text":
            result["characters"] = len(self.text)
        elif self.kind == "url":
            result["url_host"] = urllib.parse.urlsplit(self.url).hostname or ""
        return result


Parser = manage.Parser


def manual_id(value):
    if not re.fullmatch(r"[1-9][0-9]{0,18}", value) or int(value) > 2**63 - 1:
        raise argparse.ArgumentTypeError("ID 必须是 1 到 9223372036854775807 的十进制字符串")
    return value


def revision(value):
    if not re.fullmatch(r"[1-9][0-9]{0,18}", value) or int(value) > 2**63 - 1:
        raise argparse.ArgumentTypeError("revision 必须是正整数")
    return int(value)


def request_id(value):
    if not REQUEST_ID.fullmatch(value):
        raise argparse.ArgumentTypeError("--request-id 应为 1 到 128 位字母、数字或 . _ : -")
    return value


def add_batch_options(command, *, create=False):
    if create:
        command.add_argument("--file", type=Path, action="append", default=[], help="上传图片、PDF 或 TXT；可重复")
        command.add_argument("--file-title", action="append", default=[], help="按 --file 顺序提供标题；可少于文件数")
        command.add_argument("--text-file", type=Path, action="append", default=[], help="作为文字资料追加的 UTF-8 文件；可重复")
        command.add_argument("--text-title", action="append", default=[], help="按 --text-file 顺序提供标题")
        command.add_argument("--url", action="append", default=[], help="追加 HTTP/HTTPS 链接；可重复")
        command.add_argument("--url-title", action="append", default=[], help="按 --url 顺序提供标题")
        return
    if command.prog.endswith(" upload"):
        command.add_argument("--file", type=Path, action="append", required=True, help="上传图片、PDF 或 TXT；可重复")
    elif command.prog.endswith(" add-text"):
        command.add_argument("--file", type=Path, action="append", required=True, help="UTF-8 文本文件；可重复")
    else:
        command.add_argument("--url", action="append", required=True, help="HTTP/HTTPS URL；可重复")
    command.add_argument("--title", action="append", default=[], help="按输入顺序提供标题；可少于输入数")


# Keep IDs as strings while parsing revisions as JSON integers. Repeated
# category/source flags preserve an omitted value as distinct from an explicit
# empty update.
def parse_args(argv=None):
    parser = Parser(description=__doc__)
    parser.add_argument("--config", type=Path, default=Path(os.environ.get(CONFIG_ENV, "~/.config/cq-home-server/web-share.json")))
    parser.add_argument("--endpoint", choices=manage.ENDPOINTS)
    parser.add_argument("--dry-run", action="store_true", help="离线校验并输出计划，不读取凭证或发送请求")
    commands = parser.add_subparsers(dest="command", required=True)

    listing = commands.add_parser("list")
    listing.add_argument("--query", "-q")
    listing.add_argument("--category", help="省略表示全部，显式空字符串表示未分类")
    listing.add_argument("--mine", action="store_true")
    listing.add_argument("--cursor", type=manual_id)
    listing.add_argument("--limit", type=int, default=20)

    show = commands.add_parser("show")
    show.add_argument("id", type=manual_id)

    create = commands.add_parser("create")
    create.add_argument("--name", required=True)
    create.add_argument("--description", default="")
    create.add_argument("--category", action="append", default=[])
    create.add_argument("--access-mode", choices=ACCESS_MODES, default="owner")
    create.add_argument("--request-id", type=request_id)
    add_batch_options(create, create=True)

    update = commands.add_parser("update")
    update.add_argument("id", type=manual_id)
    update.add_argument("--revision", type=revision, required=True)
    update.add_argument("--name")
    update.add_argument("--description")
    categories = update.add_mutually_exclusive_group()
    categories.add_argument("--category", action="append", default=None)
    categories.add_argument("--clear-categories", action="store_true")
    update.add_argument("--access-mode", choices=ACCESS_MODES)
    update.add_argument("--status", choices=("active",))
    cover = update.add_mutually_exclusive_group()
    cover.add_argument("--cover-item-id", type=manual_id)
    cover.add_argument("--auto-cover", action="store_true")
    update.add_argument("--item-id", type=manual_id, action="append", default=None, help="完整资料顺序；每项重复一次")

    for name in ("upload", "add-text", "add-url"):
        command = commands.add_parser(name)
        command.add_argument("id", type=manual_id)
        command.add_argument("--request-id", type=request_id)
        add_batch_options(command)

    delete = commands.add_parser("delete")
    delete.add_argument("id", type=manual_id)
    delete.add_argument("--revision", type=revision, required=True)

    delete_item = commands.add_parser("delete-item")
    delete_item.add_argument("id", type=manual_id)
    delete_item.add_argument("item_id", type=manual_id)
    delete_item.add_argument("--revision", type=revision, required=True)
    return parser.parse_args(argv)


def validate_text(value, maximum, label, *, required=False):
    if not isinstance(value, str) or "\x00" in value:
        raise api.ClientError(f"{label}包含非法字符")
    if required and not value.strip():
        raise api.ClientError(f"{label}不能为空")
    if len(value) > maximum:
        raise api.ClientError(f"{label}不能超过 {maximum} 个 Unicode 字符")
    return value


def normalize_categories(values):
    result, seen = [], set()
    for raw in values:
        category = raw.strip()
        validate_text(category, MAX_CATEGORY_RUNES, "分类", required=True)
        if category not in seen:
            seen.add(category)
            result.append(category)
    if len(result) > MAX_CATEGORIES:
        raise api.ClientError(f"分类不能超过 {MAX_CATEGORIES} 个")
    return sorted(result)


def normalize_titles(values, count, label):
    if len(values) > count:
        raise api.ClientError(f"{label}数量不能多于对应资料数量")
    titles = []
    for value in values:
        titles.append(validate_text(value.strip(), MAX_TITLE_RUNES, label))
    return titles + [""] * (count - len(titles))


def stable_request_id(base, kind, index, count):
    if count == 1:
        return base
    suffix = f":{kind}:{index}"
    return base[:128 - len(suffix)] + suffix


def operation_request_id(value):
    generated = value or "manual:" + uuid.uuid4().hex
    if not REQUEST_ID.fullmatch(generated):
        raise api.ClientError("request ID 格式无效")
    return generated


# Read and validate the same descriptor without following symlinks. Uploads
# remain bounded even if the file grows while it is being read.
def read_regular_file(path, maximum_bytes, label):
    absolute = path.expanduser().absolute()
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
    descriptor = -1
    try:
        metadata = absolute.lstat()
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
            raise api.ClientError(f"{label}必须是普通文件，不能使用目录或符号链接")
        descriptor = os.open(absolute, flags)
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise api.ClientError(f"{label}必须是普通文件，不能使用目录或符号链接")
        if metadata.st_size > maximum_bytes:
            raise api.ClientError(f"{label}超过大小限制")
        with os.fdopen(descriptor, "rb") as source:
            descriptor = -1
            content = source.read(maximum_bytes + 1)
    except api.ClientError:
        raise
    except OSError as error:
        raise api.ClientError(f"无法读取{label}：{absolute}") from error
    finally:
        if descriptor >= 0:
            os.close(descriptor)
    if len(content) > maximum_bytes:
        raise api.ClientError(f"{label}超过大小限制")
    return absolute, content


def validate_upload(path, *, include_content=False):
    suffix = path.suffix.lower()
    if suffix in {".heic", ".heif"}:
        raise api.ClientError("不支持 HEIC/HEIF；请先转换为 JPEG 或 PNG")
    if suffix not in SUPPORTED_UPLOADS:
        raise api.ClientError("说明书文件仅支持 JPG、PNG、GIF、WebP、PDF 和 TXT")
    absolute, content = read_regular_file(path, MAX_FILE_BYTES, "说明书文件")
    if api.CONTROL_CHARACTER.search(absolute.name):
        raise api.ClientError("上传文件名不能包含控制字符")
    if suffix == ".txt":
        try:
            decoded = content.decode("utf-8")
        except UnicodeDecodeError as error:
            raise api.ClientError("TXT 说明书必须使用 UTF-8 编码") from error
        if not decoded or len(decoded) > MAX_TEXT_RUNES:
            raise api.ClientError(f"TXT 说明书必须包含 1 到 {MAX_TEXT_RUNES} 个 Unicode 字符")
    return absolute, content if include_content else None


def read_text_file(path):
    absolute, content = read_regular_file(path, MAX_TEXT_RUNES * 4, "文本文件")
    try:
        text = content.decode("utf-8")
    except UnicodeDecodeError as error:
        raise api.ClientError("文本文件必须使用 UTF-8 编码") from error
    if not text or len(text) > MAX_TEXT_RUNES:
        raise api.ClientError(f"文本资料必须包含 1 到 {MAX_TEXT_RUNES} 个 Unicode 字符")
    return absolute, text


def validate_url(value):
    if not isinstance(value, str) or value.strip() != value or len(value.encode("utf-8")) > MAX_URL_BYTES or api.CONTROL_CHARACTER.search(value):
        raise api.ClientError("URL 格式无效或超过 2048 字节")
    try:
        parsed = urllib.parse.urlsplit(value)
        _ = parsed.port
    except ValueError as error:
        raise api.ClientError("URL 格式无效") from error
    if parsed.scheme.lower() not in {"http", "https"} or not parsed.hostname or parsed.username is not None or parsed.password is not None:
        raise api.ClientError("URL 必须使用 HTTP/HTTPS，且不能包含用户名或密码")
    return value


def build_append_plans(kind, sources, titles, base_request_id):
    normalized_titles = normalize_titles(titles, len(sources), "资料标题")
    plans = []
    for index, (source, title) in enumerate(zip(sources, normalized_titles), 1):
        item_request_id = stable_request_id(base_request_id, kind, index, len(sources))
        if kind == "file":
            absolute, _ = validate_upload(source)
            plans.append(AppendPlan(kind="file", request_id=item_request_id, title=title, file=absolute))
        elif kind == "text":
            absolute, text = read_text_file(source)
            plans.append(AppendPlan(kind="text", request_id=item_request_id, title=title or absolute.stem, file=absolute, text=text))
        else:
            plans.append(AppendPlan(kind="url", request_id=item_request_id, title=title, url=validate_url(source)))
    return plans


def build_create_items(args, base_request_id):
    item_count = len(args.file) + len(args.text_file) + len(args.url)
    if item_count > MAX_ITEMS:
        raise api.ClientError(f"一份说明书最多包含 {MAX_ITEMS} 条资料")
    plans = []
    file_titles = normalize_titles(args.file_title, len(args.file), "文件标题")
    text_titles = normalize_titles(args.text_title, len(args.text_file), "文本标题")
    url_titles = normalize_titles(args.url_title, len(args.url), "URL 标题")
    for index, (source, title) in enumerate(zip(args.file, file_titles), 1):
        absolute, _ = validate_upload(source)
        plans.append(AppendPlan("file", stable_request_id(base_request_id, "file", index, max(2, len(args.file))), title, file=absolute))
    for index, (source, title) in enumerate(zip(args.text_file, text_titles), 1):
        absolute, text = read_text_file(source)
        plans.append(AppendPlan("text", stable_request_id(base_request_id, "text", index, max(2, len(args.text_file))), title or absolute.stem, file=absolute, text=text))
    for index, (source, title) in enumerate(zip(args.url, url_titles), 1):
        plans.append(AppendPlan("url", stable_request_id(base_request_id, "url", index, max(2, len(args.url))), title, url=validate_url(source)))
    return plans


def build_manual_multipart(plan, *, boundary=None):
    path, content = validate_upload(plan.file, include_content=True)
    selected = boundary or "----cq-manual-" + secrets.token_hex(12)
    filename = path.name.replace("\\", "_").replace('"', "_")
    parts = []
    for name, value in (("title", plan.title), ("client_request_id", plan.request_id)):
        parts.append(f'--{selected}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode("utf-8"))
    parts.extend([
        (
            f'--{selected}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\n'
            f"Content-Type: {SUPPORTED_UPLOADS[path.suffix.lower()]}\r\n\r\n"
        ).encode("utf-8"),
        content,
        f"\r\n--{selected}--\r\n".encode("ascii"),
    ])
    return b"".join(parts), f"multipart/form-data; boundary={selected}"


def json_body(payload):
    return json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def read_path(args):
    if args.command == "show":
        return API_PATH + "/" + args.id
    query = {"limit": args.limit}
    if args.query is not None:
        validate_text(args.query, MAX_NAME_RUNES, "查询关键词")
        query["q"] = args.query
    if args.category is not None:
        if args.category:
            validate_text(args.category, MAX_CATEGORY_RUNES, "分类", required=True)
        query["category"] = args.category
    if args.mine:
        query["mine"] = "true"
    if args.cursor:
        query["cursor"] = args.cursor
    if not 1 <= args.limit <= 100:
        raise api.ClientError("--limit 必须在 1 到 100 之间")
    return API_PATH + "?" + urllib.parse.urlencode(query)


def update_payload(args):
    payload = {"revision": args.revision}
    if args.name is not None:
        payload["name"] = validate_text(args.name.strip(), MAX_NAME_RUNES, "名称", required=True)
    if args.description is not None:
        payload["description"] = validate_text(args.description, MAX_DESCRIPTION_RUNES, "描述")
    if args.category is not None:
        payload["categories"] = normalize_categories(args.category)
    elif args.clear_categories:
        payload["categories"] = []
    if args.access_mode is not None:
        payload["access_mode"] = args.access_mode
    if args.status is not None:
        payload["status"] = args.status
    if args.cover_item_id is not None:
        payload["cover_item_id"] = args.cover_item_id
    elif args.auto_cover:
        payload["cover_item_id"] = None
    if args.item_id is not None:
        if len(args.item_id) != len(set(args.item_id)):
            raise api.ClientError("--item-id 不能重复，且必须提供完整资料顺序")
        payload["item_ids"] = args.item_id
    if len(payload) == 1:
        raise api.ClientError("update 至少需要一个修改字段")
    return payload


def prepare(args):
    if args.command in {"list", "show"}:
        path = read_path(args)
        return {"method": "GET", "path": path, "summary": {"method": "GET", "path": path}}
    if args.command == "create":
        base = operation_request_id(args.request_id)
        name = validate_text(args.name.strip(), MAX_NAME_RUNES, "名称", required=True)
        description = validate_text(args.description, MAX_DESCRIPTION_RUNES, "描述")
        payload = {"name": name, "description": description, "categories": normalize_categories(args.category), "access_mode": args.access_mode, "client_request_id": base}
        items = build_create_items(args, base)
        return {"method": "POST", "path": API_PATH, "payload": payload, "items": items, "request_id": base,
                "summary": {"method": "POST", "path": API_PATH, "name": name, "categories": payload["categories"], "access_mode": args.access_mode,
                            "request_id": base, "items": [item.summary() for item in items], "activate_after_upload": bool(items)}}
    if args.command == "update":
        payload = update_payload(args)
        path = API_PATH + "/" + args.id
        return {"method": "PATCH", "path": path, "payload": payload, "summary": dict(payload, method="PATCH", path=path)}
    if args.command in {"upload", "add-text", "add-url"}:
        base = operation_request_id(args.request_id)
        if args.command == "upload":
            items = build_append_plans("file", args.file, args.title, base)
        elif args.command == "add-text":
            items = build_append_plans("text", args.file, args.title, base)
        else:
            items = build_append_plans("url", args.url, args.title, base)
        if not items:
            raise api.ClientError("至少提供一条资料")
        path = API_PATH + "/" + args.id
        return {"method": "POST", "path": path, "items": items, "request_id": base,
                "summary": {"method": "POST", "path": path, "request_id": base, "items": [item.summary() for item in items]}}
    path = API_PATH + "/" + args.id
    if args.command == "delete-item":
        path += "/items/" + args.item_id
    payload = {"revision": args.revision}
    return {"method": "DELETE", "path": path, "payload": payload,
            "summary": {"method": "DELETE", "path": path, "revision": args.revision}}


def business_call(client, method, path, sensitive, payload=None, headers=None, body=None):
    request_headers = dict(headers or {})
    if payload is not None:
        request_headers["Content-Type"] = "application/json"
        body = json_body(payload)
    response = client.call(method, path, headers=request_headers, body=body)
    return api.business_result(response, sensitive)


def validate_manual(value, expected_id=None):
    if not isinstance(value, dict) or not isinstance(value.get("id"), str) or type(value.get("revision")) is not int:
        raise api.ClientError("说明书响应格式无效")
    if expected_id is not None and value["id"] != expected_id:
        raise api.ClientError("说明书读回 ID 不一致")
    if not isinstance(value.get("items", []), list):
        raise api.ClientError("说明书资料列表格式无效")
    return value


def append_item(client, manual_id_value, plan, sensitive):
    if plan.kind == "file":
        body, content_type = build_manual_multipart(plan)
        result = business_call(client, "POST", f"{API_PATH}/{manual_id_value}/files", sensitive, headers={"Content-Type": content_type}, body=body)
    else:
        payload = {"kind": plan.kind, "title": plan.title, "client_request_id": plan.request_id}
        payload["text" if plan.kind == "text" else "url"] = plan.text if plan.kind == "text" else plan.url
        result = business_call(client, "POST", f"{API_PATH}/{manual_id_value}/items", sensitive, payload=payload)
    if not isinstance(result, dict) or not isinstance(result.get("item"), dict) or not isinstance(result["item"].get("id"), str) or type(result.get("revision")) is not int:
        raise api.ClientError("资料追加响应格式无效")
    return result


def read_manual(client, manual_id_value, sensitive):
    result = business_call(client, "GET", f"{API_PATH}/{manual_id_value}", sensitive)
    return validate_manual(result, manual_id_value)


def verify_update(snapshot, payload):
    for key in ("name", "description", "categories", "access_mode", "status", "cover_item_id"):
        if key in payload and snapshot.get(key) != payload[key]:
            raise api.ClientError(f"修改后读回字段 {key} 不一致")
    if "item_ids" in payload:
        item_ids = [item.get("id") for item in snapshot.get("items", []) if isinstance(item, dict)]
        if item_ids != payload["item_ids"]:
            raise api.ClientError("修改后读回资料顺序不一致")
    return snapshot


def verification_snapshot(client, manual_id_value, sensitive):
    try:
        return read_manual(client, manual_id_value, sensitive)
    except api.ClientError as error:
        return {"verified": False, "message": api.sanitize_message(error, sensitive)}


def execute_batch(client, manual_id_value, items, sensitive, context):
    successful = []
    for index, item in enumerate(items):
        try:
            result = append_item(client, manual_id_value, item, sensitive)
        except (api.ClientError, OSError) as error:
            failure = dict(context)
            failure.update({
                "manual_id": manual_id_value,
                "successful_items": successful,
                "pending_items": [pending.summary() for pending in items[index:]],
                "readback": verification_snapshot(client, manual_id_value, sensitive),
                "next_action": "不要自动重试整个批次；固定当前 endpoint，仅用 pending_items 中原 request_id 继续",
            })
            raise PartialFailure(str(error), failure) from error
        successful.append({"request_id": item.request_id, "item": result["item"], "revision": result["revision"]})
    try:
        manual = read_manual(client, manual_id_value, sensitive)
    except api.ClientError as error:
        raise PartialFailure(str(error), dict(context, manual_id=manual_id_value, successful_items=successful,
                                             next_action="追加响应已成功；停止重试并读取说明书核对结果")) from error
    known_ids = {item.get("id") for item in manual.get("items", []) if isinstance(item, dict)}
    if any(entry["item"]["id"] not in known_ids for entry in successful):
        raise PartialFailure("资料写入已返回，但读回详情未包含全部新增资料", dict(context, manual_id=manual_id_value, successful_items=successful, readback=manual,
                           next_action="停止重试并人工核对说明书详情"))
    return successful, manual


def execute_create(client, plan, sensitive):
    created = business_call(client, "POST", API_PATH, sensitive, payload=plan["payload"])
    manual = validate_manual(created)
    manual_id_value = manual["id"]
    context = {"request_id": plan["request_id"], "endpoint_locked": True}
    if not plan["items"]:
        return {"request_id": plan["request_id"], "manual": read_manual(client, manual_id_value, sensitive), "successful_items": []}
    successful, snapshot = execute_batch(client, manual_id_value, plan["items"], sensitive, context)
    latest_revision = successful[-1]["revision"] if successful else snapshot["revision"]
    try:
        business_call(client, "PATCH", f"{API_PATH}/{manual_id_value}", sensitive, payload={"revision": latest_revision, "status": "active"})
        final = read_manual(client, manual_id_value, sensitive)
    except api.ClientError as error:
        raise PartialFailure(str(error), {
            "request_id": plan["request_id"], "manual_id": manual_id_value, "successful_items": successful,
            "readback": verification_snapshot(client, manual_id_value, sensitive), "endpoint_locked": True,
            "next_action": "资料已上传；不要重新创建或重复上传。读取最新 revision 后再决定是否激活",
        }) from error
    if final.get("status") != "active":
        raise PartialFailure("激活请求已返回，但读回状态不是 active", {
            "request_id": plan["request_id"], "manual_id": manual_id_value, "successful_items": successful,
            "readback": final, "endpoint_locked": True, "next_action": "停止重试并人工核对说明书状态",
        })
    try:
        verify_update(final, dict(plan["payload"], status="active"))
    except api.ClientError as error:
        raise PartialFailure(str(error), {
            "request_id": plan["request_id"], "manual_id": manual_id_value, "successful_items": successful,
            "readback": final, "endpoint_locked": True, "next_action": "停止重试并人工核对说明书详情",
        }) from error
    return {"request_id": plan["request_id"], "manual": final, "successful_items": successful}


def execute_delete(client, args, plan, sensitive):
    result = business_call(client, "DELETE", plan["path"], sensitive, payload=plan["payload"])
    if args.command == "delete-item":
        snapshot = read_manual(client, args.id, sensitive)
        ids = {item.get("id") for item in snapshot.get("items", []) if isinstance(item, dict)}
        if args.item_id in ids:
            raise PartialFailure("删除资料已返回，但读回仍存在该资料", {"manual_id": args.id, "item_id": args.item_id, "result": result,
                                                               "readback": snapshot, "next_action": "停止重试并人工核对"})
        return {"result": result, "manual": snapshot}
    response = client.transport.request("GET", f"{API_PATH}/{args.id}", headers={"Authorization": f"Bearer {client.access_token}"})
    if response.status != 404:
        raise PartialFailure("删除说明书已返回，但读回未得到 404", {"manual_id": args.id, "result": result,
                                                           "readback_status": response.status, "next_action": "停止重试并人工核对"})
    return {"result": result, "verified_deleted": True}


def execute(client, args, plan, sensitive):
    if args.command in {"list", "show"}:
        return business_call(client, "GET", plan["path"], sensitive)
    if args.command == "create":
        return execute_create(client, plan, sensitive)
    if args.command in {"upload", "add-text", "add-url"}:
        successful, manual = execute_batch(client, args.id, plan["items"], sensitive,
                                           {"request_id": plan["request_id"], "endpoint_locked": True})
        return {"request_id": plan["request_id"], "manual": manual, "successful_items": successful}
    if args.command == "update":
        written = business_call(client, "PATCH", plan["path"], sensitive, payload=plan["payload"])
        validate_manual(written, args.id)
        try:
            snapshot = read_manual(client, args.id, sensitive)
        except api.ClientError as error:
            raise PartialFailure(str(error), {"manual_id": args.id, "result": written,
                                               "next_action": "修改响应已成功；停止重试并读取说明书核对结果"}) from error
        if snapshot["revision"] != written["revision"]:
            raise PartialFailure("修改已返回，但读回 revision 不一致", {"manual_id": args.id, "result": written, "readback": snapshot,
                                                            "next_action": "停止重试并核对是否有并发修改"})
        try:
            return verify_update(snapshot, plan["payload"])
        except api.ClientError as error:
            raise PartialFailure(str(error), {"manual_id": args.id, "result": written, "readback": snapshot,
                                               "next_action": "停止重试并人工核对说明书详情"}) from error
    return execute_delete(client, args, plan, sensitive)


# Authenticate once, lock the selected endpoint for the whole workflow, and
# never retry a write. Partial failures retain every request ID needed to resume.
def main(argv=None):
    sensitive = ()
    selected = None
    mutation_attempted = False
    plan = None
    try:
        args = parse_args(argv)
        config = manage.load_config(args.config, args.endpoint)
        plan = prepare(args)
        if args.dry_run:
            selected = config["endpoint"]
            result = {"endpoint": selected, "origin": config.get(selected + "_url"), "result": dict(plan["summary"], dry_run=True)}
        else:
            credentials = api.load_credentials(config.get("credentials_file"), os.environ)
            sensitive = (credentials.access_key, credentials.secret_key)
            selected, origin = manage.select_endpoint(config)
            transport = api.HTTPTransport(origin, timeout=config["timeout"], ca_file=config.get("ca_file"))
            client = api.HomeServerAPI(transport, credentials)
            client.exchange_token()
            sensitive += (client.access_token,)
            mutation_attempted = plan["method"] != "GET"
            data = execute(client, args, plan, sensitive)
            result = {"endpoint": selected, "origin": origin, "result": data}
        print(json.dumps(api.redact_output(result, sensitive), ensure_ascii=False, indent=2))
        return 0
    except manage.EndpointUnavailable as error:
        print(json.dumps({"error": "endpoint_unavailable", "message": str(error), "probes": error.probes}, ensure_ascii=False), file=sys.stderr)
        return 3
    except PartialFailure as error:
        result = {"error": "partial_failure", "message": api.sanitize_message(error, sensitive), "mutation_attempted": True}
        result.update(error.context)
        if selected is not None:
            result["endpoint"] = selected
        print(json.dumps(api.redact_output(result, sensitive), ensure_ascii=False, indent=2), file=sys.stderr)
        return 1
    except (api.ClientError, OSError, ValueError) as error:
        message = api.sanitize_message(error, sensitive) if isinstance(error, api.ClientError) else "本地文件、配置或 TLS 初始化失败"
        result = {"error": "request_failed", "message": message, "mutation_attempted": mutation_attempted}
        if selected is not None:
            result["endpoint"] = selected
        if plan and plan.get("request_id"):
            result["request_id"] = plan["request_id"]
        if mutation_attempted:
            result["next_action"] = "未自动重试；固定当前 endpoint，先读取说明书核对结果，再使用相同 request ID 决定是否继续"
        print(json.dumps(api.redact_output(result, sensitive), ensure_ascii=False, indent=2), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
