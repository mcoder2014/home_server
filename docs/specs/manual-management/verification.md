# 说明书管理验证记录

日期：2026-09-19。基线：`176bce3`。本记录是提交前验证快照；GitHub CI 与生产发布验收另以 PR 和部署记录中的精确提交 SHA 为准。

## 最终功能验证

| 对象 | 环境 / 命令 | 结果 |
| --- | --- | --- |
| Go 构建 | pi 隔离源码 v3，Go 1.21.13，`go build -p 2 ./...` 和主程序构建 | 通过，Linux x86_64 产物生成；发布时仍从最终提交重新构建 |
| 完整 Go CI 主测试 | 隔离 MariaDB 10.11 / Redis，复用 CI 包与环境变量清单 | 34 包、325 个顶层用例通过，20 个既有 opt-in 用例跳过，0 失败；169.52 秒 |
| DAL 回归 | 主测试结束后串行执行 `go test ./domain/dal -run 'Test(ReadCache|Manual)' -count=1` | 13 个顶层用例通过，0 失败；包含真实三表迁移、字面名称、多分类及未分类检索 |
| 跨实例容量配额 | 同 owner、不同精确会话、两个独立 Application 实例，定向 `-count=10` | 10 次通过、0 失败；每次恰好一项写入成功，另一项受配额限制 |
| 独立文件边界 | pi 实际 pdftoppm 与格式解码 | PDF 预览最长边 ≤480、损坏 PNG 拒绝且无最终目录、损坏 PDF 保留原件并标记不可预览、超限文件拒绝均通过 |
| 上传预留并发 | 32 路并发、10 轮 | 每轮 1 成功、31 限流；该项与数据库配额测试分别验证 |
| 前端行为 | macOS，`npm run test:frontend` | 92/92 通过 |
| 说明书浏览器 | 实际 Vue 页面与 Chrome，`npm run test:manuals-browser` | 9/9 通过，覆盖 320 px、多次选图追加、上传及排序缩略图、object URL 释放、伪 PDF 脚本隔离、生产同款 CSP 下的内嵌 PDF 阅读、慢上传期间写互斥、部分失败重试、409、revision 类型及混合资料阅读 |
| 账号浏览器回归 | Chrome，`npm run test:account-center-browser` | 6/6 通过 |
| 前端构建与语法 | 变更文件 ESLint；`VUE_APP_API_BASE_URL=/ npm run build` | 通过；保留既有 bundle 大小及 caniuse-lite 警告 |
| Python 与 skill | 实际本机合成 HTTPS fixture，`test_manuals_skill`、`test_web_hosting_skill`、`test_home_server_api` | 33/33 通过：新 skill 5 项、旧 skill 15 项、客户端 13 项 |
| Skill 独立前向验证 | 合成 HTTPS、假应用凭据及临时 CA | 2 张图片、PDF、文本、URL、多分类、失败恢复、排序/封面/激活、筛选与旧命令兼容通过；未请求生产凭据 |
| 网关候选配置 | 实际 TLS 网关，备份原配置并对候选配置执行 `nginx -t` | 通过；提交前未替换生效配置或 reload |
| 文档与差异 | mmdc + Chrome、公开部署模板检查、`git diff --check` | 1 张 Mermaid 成功渲染，模板与格式检查通过 |

本次交互调整先运行新增浏览器用例并观察到 2 项预期失败：编辑器不存在缩略图节点，详情页不存在弱化元信息和 PDF iframe。独立审查随后发现仅凭 `.pdf` 后缀预览会把 HTML MIME 的 blob 放进同源 iframe；新增主动脚本载荷后旧实现为 8/9，通过按 `application/pdf` MIME 决定本地 PDF 预览关闭该链路，同时覆盖无扩展名的合法 PDF。有效 PDF fixture 返回与生产一致的 `Content-Security-Policy: sandbox`，测试确认 Chrome 内置 PDF 阅读器 frame 已加载；另用真实 Chrome 独立截图确认文档页实际可见。最终说明书浏览器测试 9/9 通过；完整前端 Node 测试 92/92 通过；变更文件 ESLint 通过；生产构建成功，只有既有 caniuse-lite 和 bundle 大小提示。全仓 `npm run lint` 仍会在未修改的 `Common.vue`、`AddBook.vue` 和 `ScanCodePage.vue` 报告 4 个基线错误，因此不把全仓 lint 记为通过。

完整主测试覆盖 DB 与 legacy/config 会话撤销、重复创建回放、上传中撤销、重复上传时撤销或删除、同说明书条目配额、跨说明书用户字节配额、分类二页游标、真实 pdftoppm PDF 最长边不超过 480 px，以及超过 4 KiB 的 APP1 EXIF 方向处理。测试中的真实权限矩阵同时检查原件、缩略图、HEAD、Range、所有者禁用和草稿/删除状态。

多个 pending 项恢复测试逐项保留原 request ID，断言无重复资料且最终顺序完整；不能把剩余多项重新组合成批次重编号。独立前向验证另外实际执行了 8 次 token exchange 和 16 次 HTTPS 业务调用。

### 独立复核

开发、验证与 Code Review 均由 `gpt-5.6-sol / max` 子 Agent 执行。参与 Go 修复的审查者没有把自审计作独立结论，修复由验证 Agent 重新读码和运行测试；前端及 skill 另有非实现者复核。最终静态审查没有未处理的 P1/P2。

| 风险 | 最终规则与证据 |
| --- | --- |
| 撤销会话后慢请求仍写入 | 写事务重新锁定并校验精确 session；JSON、文件、重复创建与旧配置身份均有撤销后拒绝用例 |
| RR 旧快照突破用户配额 | 先锁已知 owner，再锁 session，避免锁前非锁定读；追加测试曾实证两项均成功，修复后跨实例连续 10 次通过 |
| 文件失败与预览边界 | 未知提交只回查本次生成的 item ID；确定性撤权/删除错误不转换为旧条目成功；EXIF 大段和 PDF 严格尺寸回归通过 |
| 分类遗漏与上传竞态 | 分类 cursor 分页取全，异常游标拒绝；上传期间禁止其他写操作；独立前端静态检查及目标 Node 10/10 通过 |
| 发布包不一致 | 在 DDL/配置/切换前核对完整目录摘要，覆盖二进制、全部前端文件、SQL 和代理片段；嵌套 JS 变化可检出，symlink 拒绝 |

## 查询执行计划

在隔离 MariaDB 10.11 写入 10,002 份说明书和 8,336 条分类关系后执行真实 EXPLAIN。公开列表使用 `idx_manuals_access_status_id`，估算候选 8,000 行；分类加名称子串先取得相同候选，再用分类表 PRIMARY 做 `eq_ref`。名称包含及字面 `%` 匹配均为残余过滤，没有名称索引命中。未分类查询会物化扫描 8,336 条分类主键记录。这是家庭规模的明确成本边界，不能宣称子串检索具有索引查找性能。

## 源码与日志证据

Go v3 使用明确允许的源文件清单同步；46/46 文件 SHA-256 匹配。生产配置和 DSN、node_modules、前端 dist、未列入清单的工作区文件不进入远端测试副本。API 与 DAL 用例共享合成数据库但严格分步串行运行，避免并发 DROP。

| 证据 | SHA-256 |
| --- | --- |
| v3 源文件摘要清单 | `50d786494a52d56e121763caf183809e529da5b69fe38ce29593f1ed10064e4c` |
| v3 全仓构建日志 | `505d5f7502ba6c84d50d993c08cc3a931b26c7cec2a222d96ddb4b4138befac8` |
| v3 完整主测试日志 | `98525fde3d7a44c91b65cd9853a1d8e11a4d190b13e38c1de3252e0f8f2ea1c7` |
| v3 DAL 日志 | `a14cf86e16df90367f8209e20d14ba6c7bbd6cfa5dac8717deff505197b1d7e6` |
| v3 跨实例配额 10 次日志 | `0f332087e36431e15b96ddca168cbaa8baf1ad42a1d956f18ac594fa1f4f0a87` |
| v3 独立文件与预留边界 | `53bbd78d96a2710146d45760b8a8f54b263c942c8eb1c4eea2936d0874589eca` |
| v3 EXPLAIN 日志 | `2af98119c338b785a9dab9664d3544e30fdef9225caa6ad0d7ff140f2c5c79b6` |

Go 测试限制包并发 2、CPU 150%、内存 3 GiB；数据库和 Redis 仅绑定 loopback 并设置资源上限。主机系统 Go 和生产服务不参与验证环境改造。

## 基线与验证边界

修改前精确 `git archive 176bce3` 的关联 Go 基线为 16 包、208 PASS、20 SKIP、0 FAIL；Node 82/82、Python 客户端 13/13、旧 skill 15/15、原代理静态测试 5/5 通过。这些基线只用于识别回归，不计作新功能覆盖。

- 20 个跳过项是原 CI 未启用的显式 opt-in 数据库或 live fixture，不能算作已覆盖。
- iOS/Android 系统相册选择器尚未实机验证；Chrome 窄屏与文件选择测试不替代系统相册验收。HEIC/HEIF 需要先转换为 JPEG/PNG。
- 未执行数据库断连、进程崩溃或 commit 结果未知的故障注入；相关补偿路径已独立代码审查，一般幂等和撤权用例不等同故障注入证明。
- 生产发布需固定 PR 提交、备份、增量迁移、Nginx 校验与新旧入口冒烟；提交前的隔离测试不替代该记录。
