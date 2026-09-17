# 个人中心升级与会话元信息维护

适用范围：已具备数据库账号结构的 Home Server，验证数据库版本为 MariaDB 10.11。所有路径、库名和服务用户名均为公开模板；真实连接信息只保存在服务器的受限 YAML。增量升级保留原账号、密码、授权、应用凭证、登录期限及审计。

## 1. 工具与安装顺序

| 场景 | 操作顺序 |
| --- | --- |
| 首次启用数据库账号 | 先按 [账号迁移说明](accounts-migration.md) 完成旧结构及账号／动态配置导入，再运行个人中心增量升级 |
| 已完成数据库账号导入 | 直接使用 `accounts-maintain` 计划并应用个人中心增量升级，无需旧 `mock_data` |
| 增量 DDL 中断 | 保持维护，重新生成计划，核对已生效结构与逐步标记，再应用剩余阶段 |
| 日常隐私维护 | 显式运行 `--clear-session-metadata`；可由每日 systemd timer 调用 |

`accounts-migrate` 的两个已发布 DDL 文件及导入校验清单保持原样。`accounts-maintain` 只读取 `20260917_account_center.sql`，不读取旧身份数据，不重新导入账号，不改变历史导入标记。

在已授权构建主机的独立源码目录生成维护工具，随后将产物安装到匹配的 release `bin/`。不得在运行中的 release 目录编译或覆盖正在执行的二进制内容。

```bash
ssh build-host 'source ~/.zshrc && cd /tmp/home-server-account-center-build && go build -o /tmp/accounts-maintain ./cmd/accounts-maintain'
```

工具读取 `--config` 所指向的普通 YAML，权限为 0600 或更严；拒绝符号链接、可执行或其他用户可读的配置。文件最多 4 MiB，必须含 `mysql.master_db`，不要求 `passport.mock_data` 或旧授权清单。DSN、配置正文、密码、Token、IP／UA及头像内容不会写入计划、结果或错误。

## 2. 增量结构与预检

| 对象 | 新增内容 | 兼容行为 |
| --- | --- | --- |
| `user_account` | `avatar_version BIGINT NOT NULL DEFAULT 0` | 存量资料保持；0 表示默认头像 |
| `user_avatar` | 用户 ID 主键、版本、MIME、`MEDIUMBLOB`、字节数、更新时间 | 每用户一张当前标准头像；常规账号查询不读取图像 |
| `login_token` | `login_ip`、`user_agent`、`client_name`、`os_name`、`device_type`、`login_source` | 六个字段均 `NOT NULL DEFAULT ''`，旧会话允许未知描述 |
| 会话列表索引 | `idx_login_token_session_page (user_id,auth_version,is_expired,create_time,id)` | 时间／ID 游标分页；保留旧 `idx_login_token_user` |
| 保留期索引 | `idx_login_token_metadata_expiry (expire_time,id)` | 按自然到期时间和 ID 扫描清理批次 |
| 有效会话范围索引 | `idx_login_token_effective_range (user_id,auth_version,is_expired,expire_time,create_time,id)` | 固定账号、当前认证版本和未撤销标记后，直接定位自然期限内的集合；列表可对该集合按创建时间／ID 排序 |

旧 `idx_login_token_user (user_id,is_expired,expire_time)` 可以跳过自然过期记录，但仍会读取尚未自然到期的旧认证版本。新增范围索引同时限定当前认证版本，避免这些历史记录放大有效会话查询。列表、单用户计数及登录限额查询显式带入同一快照已读取的账号 ID／版本，数据库仍核对账号状态、撤销标记、自然期限及会话用途。管理员分页聚合一次传入最多 100 组已读账号 ID／版本组合，并保留实际账号版本的 JOIN 校验。

原列表索引继续保留。列表与游标在同一事务计数不超过 100 时使用 `USE INDEX (idx_login_token_effective_range)`，先读取当前版本未自然过期的小集合，再按创建时间／ID 排序；超过 100 的合法存量保留自然索引选择，使原排序索引仍可提前返回，避免固定范围索引后排序全部有效历史。该列表策略有条件索引提示，计数、管理员聚合和无排序的登录限额查询由优化器自然选择索引。

存量未到期会话可能超过新的登录限额；计数保留全部有效会话，不以限额截断，也不自动撤销存量记录。

升级前核对数据库实际版本、表结构、会话规模、DDL 锁表成本、全部入口代理及可回滚的数据库账号兼容版本。先完成数据库备份和独立恢复演练，再停止后端及其他写入者、暂停每日维护任务，并确认在途写请求已排空。工具不会自动停服务或执行备份。

默认命令只读取结构和阶段标记，生成新计划文件：

```bash
/opt/home_server/current/bin/accounts-maintain \
  --config /etc/home_server/server.yaml \
  --out /var/backups/home_server/account-center-plan.json
```

`--out` 按 0600 新建文件且拒绝覆盖；省略时输出脱敏 JSON。计划包含目标库、嵌入 DDL 摘要、实际结构摘要、尚未生效的 `pending_steps`、尚未完成的 `pending_markers`、每阶段摘要与状态、`plan_sha256`。计划不读取业务数据；在线登录或资料变化不参与哈希，因此应用时仍必须实际停写。

演练时可用 `--database home_server_rehearsal_account_center` 覆盖库名。连接账号只应被授予演练库权限，演练计划不能用于正式库。正式库名始终取受限配置中的 DSN 或操作者明确指定的覆盖值。

## 3. 应用、重复运行与阶段恢复

核对目标库、11 个独立增量阶段及全部摘要后，使用审核过的哈希：

```bash
/opt/home_server/current/bin/accounts-maintain \
  --config /etc/home_server/server.yaml \
  --apply-migration \
  --maintenance-confirmed \
  --confirm-database home_server \
  --plan-sha256 '<reviewed-additive-plan-sha256>' \
  --out /var/backups/home_server/account-center-applied.json
```

应用必须同时提供维护声明、精确库名和计划摘要。工具在专用连接取得与原账号迁移共用的库级互斥锁，再核对真实连接库名并重算计划。结构、阶段标记、目标库或新 DDL 变化导致哈希不一致时，在执行 DDL 前停止。默认总超时为 5 分钟，`--timeout` 可调整，但必须大于 0 且最多 1 小时。

MariaDB DDL 会隐式提交。工具复用严格 DDL 解析与实际列／索引／约束检查，为每个阶段记录 `started`，执行并读回后标为 `completed`。新增结构不能被假定为事务回滚。

有效会话范围索引是追加的 `20260917_account_center.sql:011`，前 10 阶段的 ID、SQL 和摘要保持不变。已完成前 10 阶段的演练库重新生成计划后只需应用第 11 阶段，保留原阶段完成时间和历史导入摘要。DDL 文件摘要已变化，追加索引前审核的旧计划不能继续使用。

| 检查结果 | 处理 |
| --- | --- |
| 结构尚未生效，标记缺失或 `started` | 应用该增量阶段，再读回和完成标记 |
| 结构已精确匹配，标记缺失或 `started` | 跳过 DDL，补全完成标记 |
| 结构精确匹配且标记 `completed` | 安全重跑，保留资料、头像及认证状态 |
| 完成标记对应的结构缺失 | 拒绝，保持维护并排查人为删除或恢复错误 |
| 同名结构定义不同、摘要冲突或未知标记阶段 | 拒绝，修复原因后重新生成并审核计划 |

中断后不得复用旧计划或删除已完成标记；保持维护并重新生成计划。成功返回 `action=migration_applied_and_verified`，`pending_steps` 与 `pending_markers` 都为空。验收新旧登录、资料、头像、会话撤销及管理员审计后，再启动每日维护任务和正常业务。

## 4. 每日元信息清理

```bash
/opt/home_server/current/bin/accounts-maintain \
  --config /etc/home_server/server.yaml \
  --clear-session-metadata \
  --timeout=5m
```

清理使用同一次服务端时间，条件严格为 `expire_time < now - 30天`。按 `expire_time,id` 游标查询，每批最多 500 行，在独立事务清空六个新增描述字段；自然到期时间缺失的记录不被推测处理。刚撤销但尚未自然到期的会话仍按自然期限保留元信息。

工具不删除 `login_token` 行，不改变令牌原文／摘要、失效标记、认证版本、用途、登录或到期时间，不修改管理审计。重复执行没有额外数据变化；中断时已提交批次保持清空，下次继续处理未清空记录。内置总上限为 5 分钟，较短 `--timeout` 仍生效。清理与迁移互斥；清理失败不改变会话认证规则。

成功返回 `action=session_metadata_cleared`，结果只含目标库、实际清空行数和批次数。数据库错误隐藏服务端原始详情；失败以非零状态退出。

## 5. systemd 模板与运行检查

模板为 [accounts-maintain.service](../deploy/pi/accounts-maintain.service) 和 [accounts-maintain.timer](../deploy/pi/accounts-maintain.timer)。服务账号示例为 `home-server`，工具路径为 `/opt/home_server/current/bin/accounts-maintain`，配置路径为 `/etc/home_server/server.yaml`。部署者需先核对实际路径、账号以及受限 YAML 的读取权限。

安装到匹配的 `/etc/systemd/system/` 后，在部署主机校验并启用：

```bash
sudo systemd-analyze verify /etc/systemd/system/accounts-maintain.service /etc/systemd/system/accounts-maintain.timer
sudo systemctl daemon-reload
sudo systemctl enable --now accounts-maintain.timer
sudo systemctl start accounts-maintain.service
systemctl list-timers accounts-maintain.timer
systemctl show accounts-maintain.service -p Result -p ExecMainStatus
journalctl -u accounts-maintain.service --since '2 days ago'
```

timer 每天在 `Asia/Singapore` 03:15 后随机延迟最多 15 分钟执行，支持停机后补跑。清理服务有 330 秒 systemd 启动上限，仅数据库写入，不需要写应用目录或配置文件。模板本身不安装或启用生产单元。

将该服务接入现有告警系统：失败保留脱敏错误；连续失败超过 48 小时，或已经启用 timer 但超过 48 小时没有成功执行，触发告警。告警内容使用任务状态、时间、失败次数和数据库名，不包含连接凭据或会话描述。通知渠道由部署环境配置。

## 6. 合成验证与回滚

Go 测试与构建在获准远端独立源码目录运行。迁移集成测试要求 `ACCOUNTS_MIGRATION_TEST_DSN` 指向前缀为 `home_server_accounts_migration_test_` 的专属合成库，且会清空该库全部表；该环境变量应来自 0600 临时文件。

```bash
# 在获准远端、仅加载专属合成库凭据后执行。
go test -race ./internal/accountsmigrate ./cmd/accounts-maintain ./cmd/accounts-migrate -count=1
```

行为用例覆盖首次结构安装、已导入库升级、重复应用、DDL 已生效但标记未完成的恢复、计划漂移与结构冲突、原迁移计划在新结构上重验、过期 30 天边界、500 行批次和清理幂等。清理查询及会话查询须在同版本合成库上通过 `EXPLAIN` 核对索引和排序，再依据实际规模评估维护窗口。

回滚保留新增列、表和索引，先撤回新前端，再退到支持数据库账号的兼容后端；已撤销的会话不会复活。清空元信息和移除头像属于真实删除，普通代码回滚不恢复其内容。不要通过恢复旧数据库备份回滚程序，旧备份可能复活已经撤销的凭据。
