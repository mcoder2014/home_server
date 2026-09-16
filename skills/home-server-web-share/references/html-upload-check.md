# HTML 上传检查与处理

`manage.py check-html` 是离线检查。`upload` 自动复用同一检查器，检查内容与发送内容使用同一个普通文件描述符读出的不可变字节串，报告 SHA-256 用于核对产物。检查不改写 HTML、ZIP 或当前线上页面。

## 检查范围

| 范围 | 阻断项 | 提示项 |
| --- | --- | --- |
| 静态托管，所有模式 | ZIP 非规范/重复路径、缺失或非 `.html` 入口、不支持的文件类型、保留路径、非普通文件、加密、压缩或 CRC 校验失败、大小/项数超限 | 站点根路径资源、ZIP 中找不到相对资源、base URL、其他字符编码、Service Worker 文本调用 |
| 增强评论结构 | 未知 schema、无效 page/target ID、重复 page/target ID、多正文根、目标类型无效、目标在显式根外、未知交互保护属性 | 缺少稳定标记、目标位于 ignore/preserve 子树、保护区缺少外层模块目标 |
| 增强容器加载 | 自行嵌入平台容器/脚本、静态 meta CSP 明确阻止脚本/请求/样式、HTML 无法以 UTF-8 检查 | CSP 显式来源或样式哈希待核对、高德 SDK、canvas/iframe/编辑器未声明外层保护 |

ZIP 默认检查上限与服务端默认一致：上传 50 MiB、5000 项、目录 16 层、单文件 50 MiB、解压 200 MiB。服务端可配置更低限制；最终仍以服务端校验为准。单文件 `.html/.htm` 保存为 `index.html`；ZIP 内只允许 `.html` 网页，`.htm` 不在 ZIP 白名单。

报告最多展示前 200 条问题，计数和阻断判断覆盖全部问题，`issues_truncated=true` 表示需分批修正并重新检查。没有问题只表示静态规则通过，不能证明页面运行正确。

## 结果与容器模式

```bash
python3 /absolute/path/skills/home-server-web-share/scripts/manage.py \
  check-html --file /absolute/path/site.zip --entry-file index.html --mode enhanced
```

结果中的 `scope=hosting` 适用于所有模式；`scope=enhanced` 只影响增强容器。`severity=error` 配合实际 mode 判断是否阻断；raw 下的增强错误仍保留原级别与建议，但不计入 `errors`。默认 check mode 为 enhanced；已有 raw 项目可以显式用 `--mode raw`。

| 状态 | 退出码 | 动作 |
| --- | --- | --- |
| `blocked` | 1 | 根据文件、行号与建议修正，不发送上传 |
| `warnings` | 0 | 核对警告，明确需要浏览器验证的范围 |
| `passed` | 0 | 可继续上传，权限和服务端限制仍需核对 |

实际 `upload` 先离线检查托管格式，再认证并读取项目实际模式；有适用阻断项时输出 `error=html_incompatible`、`mutation_attempted=false`，没有发送上传。无法核对模式时停止上传。没有适用阻断项才发送上传，成功输出额外 `html_check`，原有 release 结果结构保持不变。

`--dry-run upload` 仍不联网，报告模式为 `unknown`；增强模式错误列为待核对提示。项目模式在预检与实际上传之间若发生并发改变，服务端仍以实际配置处理，发布前应再次核对项目快照。

## 修正原则

原有语义对象保持稳定 ID；不同对象分配不同 ID。给地图等已有外层容器补充 `kind=module` 和 `interaction=preserve`，不要移动或替换已初始化业务节点。输入与敏感区域用 ignore，不索引表单值。

CSP 检查遵循指令回退顺序，meta 中的多条策略分别生效。服务端注入的脚本没有 nonce/integrity；只允许 nonce/hash 或对 parser-inserted 脚本使用 strict-dynamic 的策略可能阻断它。容器还创建 Shadow DOM 内联 style，需要策略明确允许其样式；样式哈希与当前平台样式一致才可用。应保留 CSP 并明确允许容器所需规则，无法维护时由所有者选择 raw。[W3C CSP 脚本检查规则](https://www.w3.org/TR/CSP3/#script-pre-request)、[CSP 样式规则](https://www.w3.org/TR/CSP3/#directive-style-src-elem)

检查器使用标准库 HTMLParser，不执行 JavaScript、不加载远程资源、不模拟 HTML5 修复后的 DOM。运行时生成的标记、SPA 路由、外部 CSS/JS 中的资源地址、脚本动态改写页面、地图手势、窄屏布局均无法由静态结果证明正确。已由页面脚本设置的 CSP、服务端响应头策略也不在静态文件检查范围内。
