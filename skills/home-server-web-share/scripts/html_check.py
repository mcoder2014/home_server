"""Bounded, offline checks for the static hosting and HTML comment contracts."""

from __future__ import annotations

import hashlib
from html.parser import HTMLParser
import io
import posixpath
import re
import stat
from urllib.parse import unquote, urlsplit
import zipfile
import zlib


MAX_FILE = 50 * 1024 * 1024
MAX_EXPANDED = 200 * 1024 * 1024
MAX_ENTRIES = 5000
EXTENSIONS = set("avif bmp css csv eot gif html ico jpeg jpg js json map md mjs otf pdf png svg txt wasm webmanifest webp woff woff2 xml".split())
STABLE_ID = re.compile(r"[a-z][a-z0-9]*(?:-[a-z0-9]+)*\Z")
VOID = set("area base br col embed hr img input link meta param source track wbr".split())
IGNORED = set("script style noscript template input textarea select button form iframe canvas svg".split())


class Issues(list):
    def __init__(self):
        super().__init__()
        self.counts = {"hosting": 0, "enhanced": 0, "warning": 0}
        self.total = 0

    def append(self, item):
        self.total += 1
        self.counts[item["scope"] if item["severity"] == "error" else "warning"] += 1
        if len(self) < 200:
            super().append(item)


def safe_path(name):
    components = name.split("/")
    return bool(name and len(name.encode("utf-8")) <= 2048 and not any(char in name for char in "\\\x00")
                and not name.startswith("/") and posixpath.normpath(name) == name and name not in {".", ".."}
                and not name.startswith("../") and len(components) <= 16
                and all(len(part.encode("utf-8")) <= 255 for part in components))


def add_issue(issues, code, message, suggestion, *, file, line=0, scope="enhanced", severity="error"):
    issues.append({"code": code, "severity": severity, "scope": scope, "file": file,
                   "line": line, "message": message, "suggestion": suggestion})


class CommentHTML(HTMLParser):
    """Inspect declarative metadata only; never run scripts or change the DOM."""

    def __init__(self, file, issues, resources):
        super().__init__(convert_charrefs=True)
        self.file, self.issues, self.resources = file, issues, resources
        self.stack, self.roots, self.targets = [], [], []
        self.ids, self.html_count, self.page_id = set(), 0, ""

    def issue(self, code, message, suggestion, severity="error", scope="enhanced"):
        add_issue(self.issues, code, message, suggestion, file=self.file, line=self.getpos()[0], scope=scope, severity=severity)

    def check_csp(self, policy):
        directives = {}
        for part in policy.split(";"):
            values = part.split()
            if values:
                directives.setdefault(values[0].lower(), values[1:])
        for name, fallback in (("script-src-elem", "script-src"), ("connect-src", "default-src"), ("style-src-elem", "style-src")):
            sources = directives.get(name, directives.get(fallback, directives.get("default-src")))
            if sources is None:
                continue
            sources = [value.lower() if value.startswith("'") else value for value in sources]
            signed = any(value.startswith(("'nonce-", "'sha256-", "'sha384-", "'sha512-")) for value in sources)
            hashed = any(value.startswith(("'sha256-", "'sha384-", "'sha512-")) for value in sources)
            blocked = not sources or sources == ["'none'"] or (name == "script-src-elem" and
                       ("'strict-dynamic'" in sources or all(value.startswith("'") for value in sources) and "'self'" not in sources))
            if name == "style-src-elem":
                blocked = not hashed and (signed or "'unsafe-inline'" not in sources)
            if blocked:
                self.issue("csp-container-blocked", f"CSP {name} 会阻止容器脚本、评论请求或容器样式。", "保留 CSP 并明确允许容器的同源脚本、请求及样式；无法调整时使用 raw 模式。")
            elif name == "style-src-elem" and hashed:
                self.issue("csp-style-hash-review", "CSP 样式哈希需与平台注入的样式内容匹配。", "核对当前容器样式哈希；平台升级后需重新核对，无法维护时使用 raw。", "warning")
            elif name == "style-src-elem":
                continue
            elif "'self'" not in sources and "*" not in sources:
                self.issue("csp-origin-review", "CSP 的来源列表需要结合实际托管域名核对。", "核对 /api/web-share/container.js 与同源评论请求是否获准。", "warning")

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        parent = self.stack[-1] if self.stack else {"ignored": False, "preserved": False, "root": False}
        own_ignored = tag in IGNORED or "data-hs-comment-ignore" in attrs or "data-hs-container" in attrs or (
            "contenteditable" in attrs and attrs["contenteditable"] != "false")
        ignored, preserved = parent["ignored"] or own_ignored, parent["preserved"]
        root = parent["root"] or "data-hs-comment-root" in attrs
        if tag == "html":
            self.html_count += 1
            self.page_id = attrs.get("data-hs-page-id") or ""
            schema = attrs.get("data-hs-comment-schema")
            if schema is not None and schema not in {"", "1"}:
                self.issue("unknown-schema", "评论结构版本无法识别，容器会降级整页评论。", '使用 data-hs-comment-schema="1"。')
            if self.page_id and (len(self.page_id) > 96 or not STABLE_ID.fullmatch(self.page_id)):
                self.issue("invalid-page-id", "页面 ID 不符合稳定标识格式。", "使用 1～96 位小写语义 ID，单词以连字符分隔。")
        if tag == "meta":
            if (attrs.get("http-equiv") or "").lower() == "content-security-policy":
                self.check_csp(attrs.get("content") or "")
            if (attrs.get("charset") or "utf-8").lower() not in {"utf-8", "utf8"}:
                self.issue("encoding-review", "页面声明了其他字符编码。", "统一 UTF-8 产物与编码声明，避免原文锚点乱码。", "warning", "hosting")
        if "data-hs-comment-root" in attrs and not ignored and not preserved:
            self.roots.append(self.getpos()[0])
        target = attrs.get("data-hs-comment-id") or ""
        if target:
            if ignored or preserved:
                self.issue("unreachable-target", "评论目标位于忽略或原交互保护子树内，运行时不会索引它。", "把模块标记放在已有外层容器，或移除不可达的评论目标标记。", "warning")
            else:
                if target in self.ids:
                    self.issue("duplicate-target-id", "同一页面存在重复评论目标 ID。", "为不同语义对象使用不同 ID；同一对象更新时保留原 ID。")
                self.ids.add(target)
                if len(target) > 96 or not STABLE_ID.fullmatch(target):
                    self.issue("invalid-target-id", "评论目标 ID 无法被运行时识别。", "使用 1～96 位小写语义 ID，单词以连字符分隔。")
                if attrs.get("data-hs-comment-kind") not in {"text", "image", "module"}:
                    self.issue("invalid-target-kind", "评论目标缺少有效类型。", "在同一节点设置 data-hs-comment-kind=text/image/module。")
                self.targets.append((root, self.getpos()[0]))
        interaction = attrs.get("data-hs-comment-interaction")
        if interaction is not None and interaction != "preserve":
            self.issue("unknown-interaction", "原交互保护属性无法识别。", '使用 data-hs-comment-interaction="preserve"。')
        if interaction == "preserve" and not ignored and (not target or attrs.get("data-hs-comment-kind") != "module"):
            self.issue("preserve-module-target", "保护区没有有效外层模块目标，只能保留原交互。", "在现有外层节点补充稳定 ID 和 kind=module。", "warning")
        if (tag in {"canvas", "iframe"} or "contenteditable" in attrs and attrs["contenteditable"] != "false") and not (
                parent["ignored"] or preserved or "data-hs-comment-ignore" in attrs or interaction == "preserve"):
            self.issue("interactive-module-review", "交互组件内部默认跳过，无法直接关联组件评论。", "在已有外层节点使用 module + preserve，或声明 ignore。", "warning")
        if "data-hs-container" in attrs or attrs.get("id") == "hs-web-container":
            self.issue("embedded-container", "上传内容含平台容器标记，可能重复注入。", "去掉页面内的模拟容器；平台在响应时注入。")
        for name in ("src", "href", "action", "poster"):
            value = attrs.get(name)
            if value and (tag != "a" or name != "href"):
                self.check_resource(value)
        source = attrs.get("src") or ""
        if tag == "script" and "/api/web-share/container.js" in source:
            self.issue("embedded-container", "页面自行引入了平台容器脚本。", "删除该引用，由增强托管响应注入。")
        if tag == "script" and "amap.com" in source.lower():
            self.issue("amap-module-review", "检测到高德 SDK，静态扫描不能判断地图实例的交互边界。", "地图已有外层模块使用 module + preserve；核对原拖拽、缩放和 Marker 交互。", "warning")
        if tag == "base":
            self.issue("base-url-review", "base URL 会改变相对资源的解析位置。", "核对托管路径 /p/slug/ 下的资源地址与导航。", "warning", "hosting")
        if tag not in VOID:
            self.stack.append({"tag": tag, "ignored": ignored, "preserved": preserved or interaction == "preserve", "root": root})

    def handle_startendtag(self, tag, attrs):
        self.handle_starttag(tag, attrs)
        if tag not in VOID:
            self.handle_endtag(tag)

    def handle_endtag(self, tag):
        for index in range(len(self.stack) - 1, -1, -1):
            if self.stack[index]["tag"] == tag:
                del self.stack[index:]
                break

    def check_resource(self, value):
        if value.startswith("/") and not value.startswith("//"):
            self.issue("root-relative-resource", "资源使用站点根路径，托管项目路径下可能无法访问。", "使用项目内相对路径；业务 API 的站点根路径应单独核对。", "warning", "hosting")
            return
        try:
            parsed = urlsplit(value)
        except ValueError:
            self.issue("invalid-resource-url", "资源地址无法按标准 URL 解析。", "核对资源地址格式。", "warning", "hosting")
            return
        if self.resources is not None and not parsed.scheme and not parsed.netloc and parsed.path:
            target = posixpath.normpath(posixpath.join(posixpath.dirname(self.file), unquote(parsed.path)))
            if target not in self.resources:
                self.issue("missing-relative-resource", "相对资源未在 ZIP 普通文件中找到。", "检查构建产物和路径；动态地址需要在浏览器中核对。", "warning", "hosting")

    def handle_data(self, data):
        if self.stack and self.stack[-1]["tag"] == "script" and re.search(r"serviceWorker\s*\.\s*register\s*\(", data):
            self.issue("service-worker-review", "脚本文本含 Service Worker 注册调用，托管环境默认不支持该能力。", "移除注册依赖，或核对静态页面在注册失败时是否仍可用。", "warning", "hosting")
        if self.stack and self.stack[-1]["tag"] == "style" and re.search(r"url\(\s*['\"]?/(?!/)", data):
            self.issue("root-relative-css", "内联 CSS 使用站点根路径资源。", "改用项目内相对资源路径。", "warning", "hosting")

    def finish(self):
        if self.html_count > 1:
            self.issue("multiple-html-elements", "同一文档声明了多个 html 元素。", "保留一个文档根，避免元数据与浏览器修复后的 DOM 不一致。")
        if len(self.roots) > 1:
            self.issue("multiple-comment-roots", "可索引正文根超过一个，容器会降级整页评论。", "保留一个正文根，其余业务导航使用 ignore。")
        if self.roots:
            for in_root, line in self.targets:
                if not in_root:
                    add_issue(self.issues, "target-outside-root", "评论目标在正文根外，不会被运行时索引。", "将标记放在正文根内的现有语义节点。", file=self.file, line=line)
        if not self.page_id or not self.roots or not self.ids:
            self.issue("legacy-anchors", "缺少稳定页面 ID、正文根或目标 ID，跨版本定位只能保守匹配。", "生成或修改页面时遵循 HTML 评论结构标准；现成页面无需自动重写。", "warning")


def inspect_html(content, file, issues, resources):
    try:
        text = content.decode("utf-8-sig")
    except UnicodeDecodeError:
        add_issue(issues, "non-utf8-html", "HTML 不是有效 UTF-8，无法可靠检查文字锚点。", "将产物转换为 UTF-8 后检查。", file=file, scope="enhanced")
        return ""
    parser = CommentHTML(file, issues, resources)
    parser.feed(text)
    parser.close()
    parser.finish()
    return parser.page_id


def inspect_upload(content, suffix, entry_file=None):
    """Return mode-independent findings from exactly the bytes to be uploaded."""
    issues, html_files, page_ids = Issues(), 0, set()
    entry = entry_file or "index.html"
    if suffix.lower() != ".zip":
        if entry_file and entry_file != "index.html":
            add_issue(issues, "single-html-entry", "单 HTML 上传的入口固定为 index.html，指定入口不会改变存储路径。", "省略 --entry-file 或指定 index.html。", file="index.html", scope="hosting", severity="warning")
        inspect_html(content, "index.html", issues, None)
        html_files = 1
    else:
        checking_file = entry
        try:
            with zipfile.ZipFile(io.BytesIO(content)) as archive:
                entries, names, total = archive.infolist(), set(), 0
                if len(entries) > MAX_ENTRIES:
                    raise ValueError("zip-entry-limit")
                for info in entries:
                    name = info.filename[:-1] if info.is_dir() else info.filename
                    checking_file = name
                    if not safe_path(name) or name in names:
                        raise ValueError("zip-unsafe-or-duplicate-path")
                    names.add(name)
                    if info.is_dir():
                        continue
                    mode = info.external_attr >> 16
                    if stat.S_IFMT(mode) not in {0, stat.S_IFREG} or info.flag_bits & 1:
                        raise ValueError("zip-nonregular-or-encrypted")
                    components = name.lower().split("/")
                    if posixpath.splitext(name)[1][1:].lower() not in EXTENSIONS or any(
                            part in {".git", ".env"} or part.startswith(".env.") for part in components):
                        raise ValueError("zip-unsupported-content")
                    total += info.file_size
                    if info.file_size > MAX_FILE or total > MAX_EXPANDED:
                        raise ValueError("zip-size-limit")
                resources = {info.filename for info in entries if not info.is_dir()}
                checking_file = entry
                if not safe_path(entry) or not entry.lower().endswith(".html") or entry not in resources:
                    raise ValueError("zip-entry-missing-or-invalid")
                for info in entries:
                    if info.is_dir():
                        continue
                    checking_file = info.filename
                    # Read every member with a bound, including assets: validate
                    # CRC, compression support and actual size without extraction.
                    with archive.open(info) as member:
                        data = member.read(min(MAX_FILE, info.file_size) + 1)
                    if len(data) != info.file_size:
                        raise ValueError("zip-declared-size-mismatch")
                    if info.filename.lower().endswith(".html"):
                        html_files += 1
                        page_id = inspect_html(data, info.filename, issues, resources)
                        if page_id and page_id in page_ids:
                            add_issue(issues, "duplicate-page-id", "同一 ZIP 中存在重复稳定页面 ID。", "不同页面分配不同 ID，同一页面更新时保留 ID。", file=info.filename)
                        page_ids.add(page_id)
        except (ValueError, zipfile.BadZipFile, NotImplementedError, RuntimeError, EOFError, OSError, zlib.error) as error:
            reasons = {"zip-entry-limit": "ZIP 项数超过限制。", "zip-unsafe-or-duplicate-path": "ZIP 路径不规范或重复。",
                       "zip-nonregular-or-encrypted": "ZIP 包含非普通文件或加密成员。", "zip-unsupported-content": "ZIP 文件类型不受支持或包含保留路径。",
                       "zip-size-limit": "ZIP 单文件或解压总大小超限。", "zip-entry-missing-or-invalid": "ZIP 入口缺失或不是规范 .html 路径。",
                       "zip-declared-size-mismatch": "ZIP 实际解压大小与声明值不符。"}
            code = str(error) if isinstance(error, ValueError) and str(error) in reasons else "zip-invalid"
            add_issue(issues, code, reasons.get(code, "ZIP 的压缩或校验不符合托管要求。"),
                      "使用规范相对路径与 .html 入口；仅打包构建产物，最多 5000 项、16 层、单文件 50 MiB、解压 200 MiB。", file=checking_file, scope="hosting")
    return {"sha256": hashlib.sha256(content).hexdigest(), "entry_file": "index.html" if suffix.lower() != ".zip" else entry,
            "html_files": html_files, "issues": list(issues), "counts": issues.counts, "issue_count": issues.total}


def report(findings, mode):
    errors = findings["counts"]["hosting"] + (findings["counts"]["enhanced"] if mode == "enhanced" else 0)
    warnings = findings["issue_count"] - errors
    return {"status": "blocked" if errors else "warnings" if warnings else "passed", "mode": mode,
            "sha256": findings["sha256"], "entry_file": findings["entry_file"], "html_files": findings["html_files"],
            "errors": errors, "warnings": warnings, "enhanced_errors": findings["counts"]["enhanced"],
            "issues": findings["issues"], "issues_truncated": findings["issue_count"] > len(findings["issues"]),
            "limitations": ["仅检查静态产物；不执行 JavaScript、不请求外部资源，也不模拟浏览器修复后的 DOM。",
                            "动态 SPA 标记、脚本设置的 CSP、运行时地图与布局交互仍需浏览器核对；服务端可配置更低文件限制。"]}
