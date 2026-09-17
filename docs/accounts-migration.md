# 账号迁移与通用动态配置运维说明

迁移工具目前验证的数据库目标为 MariaDB 10.11，结构检查使用其 INFORMATION_SCHEMA。本文适用于已运行旧版配置用户体系，并已完成网页与应用凭证表迁移的站点。示例账号为 `owner`、存量 ID 为 `1001`，域名使用 `home.example.com`；路径、数据库名和账号均为模板，实施时必须替换。

本次代码交付包含迁移工具和验证方法。编译、单测、合成数据库演练不等于正式迁移，正式切换需要另行安排维护窗口。

## 1. 迁移边界

| 对象 | 迁移动作 | 保持的行为 |
| --- | --- | --- |
| 配置中的存量用户 | 保留 ID、用户名、原 bcrypt 哈希，导入用户名/邮箱/手机登录别名 | 不重新注册同名账号，不重新哈希旧哈希；成功登录后由认证服务升级低成本哈希 |
| 用户角色与授权 | CLI 显式指定初始管理员、家庭藏书和 WebDAV 授权清单 | 管理员角色不会自动获得藏书或 WebDAV 权限 |
| 用户会话 | 导入事务将用户 `auth_version` 从 1 增至 2；旧令牌补列默认 1，并将 `is_expired` 设为 1 | 所有旧用户会话失效，必须重新登录；重跑不再次增加版本 |
| 应用凭证和应用 Token | 保留 ID、owner、密钥摘要、scope、revision、过期时间 | 新认证仍检查账号状态、模块开关和能力授权，管理员名下应用没有管理权限 |
| 网页项目与文件 | 保留 owner、slug、状态、可见性、版本 ID 与文件路径；增加审核字段 | 不搬文件，不把停用网页改为启用；已删除项目按旧有效保留天数固定 `purge_after` |
| 家庭藏书与 WebDAV | 原共享数据保持；账号单独授权 | 藏书不改为个人库存，WebDAV 不迁成个人目录 |
| 动态配置 | 当前有效值导入 7 个分组，revision/generation/epoch 初始 1 | 注册初始关闭；服务启动不会用 YAML 覆盖数据库 |

原配置没有可信创建日期时，数据库 `create_time` 使用导入时间，`source=config_import` 与 `imported_at` 标明来源；不能把该时间解释为真实注册日期。邮箱/手机联系方式与登录别名分开保存，自助更新联系方式不迁移旧登录别名。

WebDAV Basic 是持续支持的客户端认证协议。`/webdav/` 和 `/webdav_dev/` 直接接受 HTTP Basic 用户名/密码，不要求先调用网站登录 API，也不要求 Cookie、CSRF、Origin 或 AK/SK。缺少凭据返回 `401` 和 `WWW-Authenticate: Basic`，不能跳转登录页面。关闭注册、关闭应用凭证、网站会话过期或“退出全部登录”不影响有效 Basic 请求；修改账号密码后，客户端需使用新密码。账号封禁/删除、WebDAV 模块关闭、独立授权及读写权限仍由服务端检查。

匿名登录、已认证网站会话的密码确认、Basic 分别使用独立的失败计数与容量上限，匿名登录失败不会阻断已登录管理员的处置操作，也不会连带阻断 Basic；同一账号的 Basic 登录别名和两条 WebDAV 路由共享其自身限速。获准账号可直接使用尚未过期的初始密码连接客户端，不必先完成网站首次改密；该例外不解除网站会话的首次改密要求，过期初始密码仍被拒绝。

当前少量好友场景接受上传 HTML 与主站同源的风险，部署结构保持不变。管理员访问他人页面仍应按该风险边界操作。

## 2. 通用动态配置

动态配置使用代码注册的类型与约束、数据库 current/history、全局 generation、不可变运行快照。新增业务字段需在 `config.Registry` 注册并接入消费者；不能在数据库直接添加任意 key 使之自动生效。

| 分组 | 管理的配置 | 生效范围 |
| --- | --- | --- |
| `registration` | 注册总开关 | 生成/兑换时实时查询；关闭会永久增加邀请码 epoch |
| `auth` | 应用模块、Token TTL、凭证期限、每用户数量 | 模块实时检查，其余用于新操作；不延长旧凭证 |
| `web_projects` | 网页模块、上传限制、并发、版本数量、每用户项目/总字节配额、磁盘保留空间、清理开关、删除保留天数 | 请求或清理批次读取；已有删除期限不随配置缩短 |
| `library` / `webdav` | 全站模块开关 | 与每个用户的能力授权共同检查 |
| `site` | 网站标题、纯文本公告 | 普通快照刷新，允许白名单公开展示 |
| `account_policy` | 会话 TTL、同时有效登录上限（默认 5、可配 1–100）、临时密码期限、密码最低长度、bcrypt 成本 | 用于新签发会话与新设置密码；调低上限保留已有会话，满额拒绝新签发 |

每月 3 个邀请码、有效 7 天、`Asia/Singapore` 月份计算是固定规则，管理页面只读展示。

新增资源策略默认每用户 10 个项目、10 GiB 总配额、至少保留 20 GiB 磁盘空间。总配额必须不小于单项目上限；这些新策略会进入迁移 plan 并由管理员审核。配额收紧不会直接删除存量内容，限制由创建/上传流程执行。对应存量版本查询新增 `(uploaded_by,status,id)` 索引，不搬迁文件。

数据库连接、监听端口、存储路径、可信 Origin/代理、外部服务密钥及部署上传硬上限保留在受限 YAML。动态上传限制不能超过 `upload_hard_limit_bytes`，也不能代替代理入口的 body limit 调整。

管理页面使用完整分组值、`If-Match` revision、唯一 `request_id` 和修改原因发布。同一 revision 并发发布只有一个成功；同请求重试不能创建第二次修改；复用请求 ID 改内容会冲突。回滚读取历史并创建新 revision，仍执行当前校验。

普通参数默认每 5 秒刷新，发布后立即尝试一次刷新。页面区分“已保存”和“运行实例已加载”，刷新失败显示 stale 并保留旧快照；安全开关的操作检查仍查询数据库。数据库启动模式要求全部分组、schema 版本、校验和、revision/generation/epoch 均有效，失败时不得回退到允许性默认值。

配置回滚不会恢复旧邀请码、被撤销的账号能力、旧密码、旧会话、吊销的应用凭证或已清理文件。

存量账号策略缺少 `max_active_sessions` 时，读取先校验原始 JSON 摘要，再在副本补 5；不会写回当前或历史记录。新发布要求完整字段。V1 账号导入继续使用原四字段策略计算摘要；扩展运行时默认值不会使已完成导入失效。已发布新策略后，代码回滚必须使用识别新字段的兼容后端；更旧版本需先按其 schema 准备可验证的策略文档。

## 3. 编译与合成测试

在获准构建主机的独立源码目录执行。非交互 SSH 必须先加载远端 shell 环境；不得在生产 release 目录编译。

```bash
ssh build-host 'source ~/.zshrc && cd /tmp/home-server-accounts-build && go build -o /tmp/accounts-migrate ./cmd/accounts-migrate'
```

纯内存测试不需要数据库。集成测试必须显式配置独立合成 MariaDB 数据库；测试保护要求库名以 `home_server_accounts_migration_test_` 开头，并且会清空该库的表。严禁用生产备份库跑该测试。

```bash
go test ./internal/accountsmigrate ./cmd/accounts-migrate -count=1

# 该连接仅指向自行创建的合成测试库，测试账号只授予该库权限。
ACCOUNTS_MIGRATION_TEST_DSN='fixture:synthetic-password@tcp(127.0.0.1:3306)/home_server_accounts_migration_test_fixture?parseTime=true&loc=Local' \
  go test -race ./internal/accountsmigrate ./cmd/accounts-migrate -count=1
```

集成用例覆盖只读 plan、哈希/数据库确认、部分 DDL 恢复、结构冲突、未知 owner、源配置和文件漂移、旧密码/ID/应用版本保留、固定删除期限、初始配置导入，以及重跑不覆盖已改密码或再次撤销会话。

## 4. 只读预检与计划

源 YAML 必须是普通文件，权限为 0600 或更严，不能是符号链接。工具不会输出 DSN、密码哈希正文、Token、应用密钥、用户名或联系人；日志中的数据库原始错误也被屏蔽。

```bash
/tmp/accounts-migrate \
  --config /etc/home-server/server.yaml \
  --binary /srv/home-server/release/home-server \
  --admin-user-ids 1001 \
  --library-user-ids 1001 \
  --webdav-write-user-ids 1001 \
  --out /var/backups/home-server/accounts-plan.json
```

默认只执行 SELECT 和文件读取，不建表、不更新数据。`--out` 必须是新文件，工具按 0600 创建且拒绝覆盖已有文件；省略则将脱敏 JSON 输出到 stdout。

| 计划字段 | 核对内容 |
| --- | --- |
| `database`、`source_sha256`、`binary_sha256` | 目标库、实际源配置和部署二进制 |
| `ddl_checksums`、`schema_sha256`、`pending_steps` | 嵌入 DDL、当前结构、剩余增量步骤 |
| `source_user_ids`、`alias_count`、`grants` | 保留的 ID、别名数量、明确授权清单 |
| `tables` | 10 张旧表的逐表行数与数据哈希，额外旧字段也参与计算 |
| `files` | ready 版本的文件数、体积和内容清单哈希；不输出文件名或绝对路径 |
| `runtime_values` | 从实际有效旧值生成的完整配置，注册强制关闭 |
| `data_already_imported`、`import_sha256`、`plan_sha256` | 是否已导入、导入绑定值与可供执行确认的整体计划哈希 |

跨用户的用户名/邮箱/手机别名冲突、未知资源所有者、断开的项目关联、重复或空 Token ID、同名异定义的新结构、未完成标记下的配置数据、缺失或非法网页文件均使预检失败。源文件、旧表任意数据、DDL 或 ready 文件变化后，原计划不能继续应用。

独立恢复演练可用 `--database home_server_rehearsal_20260913` 覆盖库名，仍从受限文件读取认证信息。演练 YAML 的文件根必须指向独立恢复目录，不能指向生产共享根。演练计划不能用于正式库。

旧 RSA 登录在解码前限制来源频率和 16 KiB 正文；维护型 DDNS 记录接口需管理员身份，来源地址发现保持公开。WebDAV 的两个路径使用共享锁表。部署需同时更新代理片段，详见 `deploy/pi/README.md`。

## 5. 正式维护窗口与备份

正式执行前必须完成独立库恢复演练，并由操作者确认维护窗口、备份与下列基线。工具不会自动备份、停止服务或确认代理是否停止流量。

| 顺序 | 操作与证据 |
| --- | --- |
| 1 | 全部兼容域名/端口进入维护，停止旧后端写入，确认在途请求与数据库事务已结束 |
| 2 | 备份完整数据库、受限 YAML、匹配二进制/前端、systemd 和实际代理配置 |
| 3 | 在停写后备份网页目录并核对清单；WebDAV 共享根保持原样，不为本次迁移搬盘 |
| 4 | 在独立库恢复数据库并核对表数、行数、索引、关联和网页文件；压缩包能解压不代表数据库能恢复 |
| 5 | 从正式停写状态重新生成 plan，核对初始管理员/藏书/WebDAV 清单及实际动态配置 |

数据库可使用带受限客户端配置文件的 `mariadb-dump --single-transaction`。该选项不能保护并发 DDL，也不包含文件系统，因此必须先停写并完成独立恢复。备份应在权限受限目录，另行保留跨磁盘副本；同盘备份不能覆盖单盘损坏。

## 6. 显式应用与阶段恢复

执行时使用计划输出中的完整 `plan_sha256`，并填写完全相同的数据库名和授权参数。

```bash
/tmp/accounts-migrate \
  --config /etc/home-server/server.yaml \
  --binary /srv/home-server/release/home-server \
  --admin-user-ids 1001 \
  --library-user-ids 1001 \
  --webdav-write-user-ids 1001 \
  --apply \
  --maintenance-confirmed \
  --confirm-database home_server \
  --plan-sha256 '<reviewed-plan-sha256>' \
  --out /var/backups/home-server/accounts-applied.json
```

`--maintenance-confirmed` 是操作者对停写与备份的明确声明，工具不能用它自动证明流量已经排空。执行前会取得库级迁移互斥锁并重算整个计划；不匹配时不会执行 DDL。

MariaDB 的 CREATE/ALTER 会隐式提交。工具先建立 `schema_migration`，为每条 CREATE 或 ADD 写入 `started`，执行后核对实际列、索引、检查约束，再标为 `completed`。已有相同结构可以跳过；同名但定义不同立即停止。旧业务表的额外字段保留，新建账号/配置表必须与已审核定义完全一致。

账号、别名、显式授权、配置 current/history/state、旧会话版本递增与旧 Token 废弃、删除期限回填和导入完成标记在一个数据事务内提交。失败会回滚这一事务，已经执行的 DDL 仍然存在。

中断后保持维护状态，读取脱敏错误和 `schema_migration` 阶段。生成新的 plan，核对剩余步骤再 apply；不复用中断前的计划哈希，也不手工删除完成标记。重跑必须使用已保留且哈希匹配的原始受限配置快照，不能改用已删除 mock_data 的新启动配置。重复执行不会覆盖用户已经改过的密码、资料和权限，也不会重新导入动态配置或再次增加 `auth_version`。

成功输出 `action=applied_and_verified`，并重新读取结构、关联、文件和导入完成标记。导入工具不能代替新服务 HTTP 行为验收。

## 7. 切换配置来源与验收

只有迁移读回验证成功，才修改启动配置并启动新后端。新配置仍保留原 DSN、Origin、代理、RPC 密钥和文件根。

```yaml
identity_source: database
config_source: database
# 必须与已核对的全部入口代理上限一致，示例为 50 MiB。
upload_hard_limit_bytes: 52428800
```

注册初始关闭。用原 `owner` 账号登录后，按以下顺序验证：

1. 原密码可验证、ID 和资源归属保持；旧 Cookie/用户 Token 被拒绝，新登录态可用。
2. 原应用凭证保持原 scope 和 expiry；普通用户、用户 Token、应用 Bearer、WebDAV Basic 均受数据库状态与能力授权约束。
3. 藏书和 WebDAV 的获准账号可访问，未授权账号与撤权后的旧凭证不能绕过；用独立测试文件验证写权限。
4. 原网页状态、可见性和版本不变；管理员可审核风险页面，普通账号和管理员名下应用不能使用管理 API。
5. 修改站点标题检查动态生效、分组历史和回滚；验证关闭注册后不能生成或兑换旧邀请码。

验收使用明确标记的合成账号/项目/文件，不能重置真实账号密码、删除真实页面或覆盖真实库存。通过后移除活动 YAML 的 `passport.mock_data`，受限备份保留；最后由管理员明确开启邀请注册。

## 8. 回滚限制

| 所处阶段 | 可采用的恢复方式 |
| --- | --- |
| 仅新增结构，尚未切换来源 | 保留新增结构，旧程序仍可运行；不 DROP 新表来“回滚”DDL |
| 导入后，尚未接受新业务数据 | 可停止新程序并恢复匹配旧配置/二进制；工具已将旧 Token 标为过期，回到忽略 `auth_version` 的旧程序也不能重新接受这些旧令牌 |
| 已有注册、改密、封禁或动态配置发布 | 只能退回支持数据库账号和动态配置的兼容版本；只识别旧 mock_data 的程序会丢失新状态 |
| 配置值错误 | 通过配置历史生成新 revision，不能手改 current/history |
| DDL 中断或结构冲突 | 维持维护，依据阶段记录修复与重新计划；不能假定全部回滚 |
| 必须整库恢复 | 单独核对恢复点和之后的数据损失，再执行恢复流程 |

正式切换前需准备可回滚的兼容二进制。迁移工具不执行整库恢复、不撤销用户授权，也不恢复已经失效的凭证。

## 9. 个人中心的独立增量升级

个人中心使用新增的 `20260917_account_center.sql`，由 `accounts-maintain` 生成脱敏计划并在维护窗口应用。首次安装先完成本文的账号／动态配置迁移，再运行个人中心增量升级；已完成账号导入的站点只运行增量升级。操作、阶段恢复与每日元信息清理见 [个人中心运维说明](account-center-operations.md)。

个人中心共有 11 个独立阶段，最后追加 `idx_login_token_effective_range (user_id,auth_version,is_expired,expire_time,create_time,id)`，用于同时限定当前认证版本与自然期限。此前已完成 10 阶段的演练库需重新生成计划并应用第 11 阶段；原阶段标记与历史账号导入摘要保持不变。

`accounts-migrate` 仍只读取 `20260913_runtime_config.sql` 和 `20260913_accounts.sql`。这两个已发布文件及历史 `import_sha256` 的 DDL 清单保持原样；不得将新文件加入旧数据导入选项，否则已迁移库会发生导入摘要冲突。

原 `user_account` 建表检查仅允许精确定义的 `avatar_version BIGINT NOT NULL DEFAULT 0` 扩展；未知额外列、错误类型／默认值、未知索引或约束仍被拒绝。原导入工具可以在升级后的结构上重新核验，不能因此重新导入用户或再次撤销登录。
