# 网页访问统计存储

该包接收经过 HTTP 响应口径筛选的项目访问事件。HTTP 层负责浏览器 Cookie、HMAC 和权限；这里仅接收项目 ID、访客摘要和服务端响应完成时间。`Record` 返回值表示成功进入内存队列，不能当作已经持久化。

## 接入和生命周期

| 接口 | 行为 |
|---|---|
| `New(redis.UniversalClient, *gorm.DB, Options)` | 只校验本地参数，不执行 DDL，不因 Redis 离线阻止应用启动 |
| `Start(context.Context)` | 启动一个串行消费 goroutine；重复调用无副作用 |
| `Record(projectID, visitorHash, at) bool` | 非阻塞入队；队列满或服务关闭时拒绝并记录缺口，Redis 故障后仍接受后续尝试 |
| `Flush(context.Context)` | 排空一个有界批次，提交全部脏日期及累计快照；可在测试或关闭时直接调用 |
| `Close(context.Context)` | 停止接纳，等待消费者退出，再排空和落库；调用方必须设置总截止时间，建议 5 秒；失败可以再次调用 |
| `Stats(context.Context, projectID, days)` | 只读 SQL 已提交数字；支持 7/30/90 日；运行期已知且未落库的缺口会覆盖质量状态 |
| `Health()` | 队列长度、accepted/dropped/unknown、快照重试、恢复次数、最近成功时间、质量与不可用原因 |

默认采用 `Asia/Shanghai`、90 个自然日日汇总、Redis 近 3 日日桶、4096 条队列、60 秒刷盘、1000 个事件提前刷盘、5 秒最大排队时间。Redis 操作与初始化/单项目刷盘默认预算 2 秒；HTTP 不等待这些操作。

累计 UV 使用独立的永久 HLL，每日 UV 相加不等于累计 UV。`tracking_started_at` 来自实际采集/恢复状态；没有持久化记录时返回 `not_started` 和空日期数组。日期窗口内、开始统计之前的天明确标为 `not_started`；开始之后的健康无访问日期补零。

## 存储协议

| 层 | 数据和约束 |
|---|---|
| Redis | `<prefix>:analytics:v1:{project_id}:state` 保存绝对 PV、seq、ack、日序号与质量；`uv:YYYY-MM-DD` 和 `uv:all` 保存 HLL；`dirty` 和 `projects` 是项目集合 |
| 写入 | 只使用普通命令和事务 pipeline，不执行 Lua/EVAL；序号预留与计数批次允许在故障下部分完成，最终返回错误时用 degraded 缺口表达不确定性 |
| SQL | `web_project_stat_total` 按项目 ID 加锁，以较大的 `last_seq` 条件覆盖；累计和全部相关日行在同一事务提交；相同或旧序号不写 |
| 确认和清理 | SQL 成功后按序号确认 Redis；发生新事件就保留 dirty；仅删除已持久化且超出 3 日热窗口的日桶，不给未落库统计设置 TTL |
| 90 日窗口 | 查询最多返回今天及前 89 天；每日物理清理每批最多 500 行，走 `(stat_date, project_id)` 索引；永久累计行和 HLL不参与清理 |

Redis 完整且序号不旧于 SQL 时保留未落库增量。缺 key、类型错误或较旧完整快照都从同一 SQL 事务读取累计/日 HLL 恢复，并标记可能丢失区间。每次刷盘前重新核对 SQL 序号，因此 Redis 重启载入旧镜像也不会覆盖较新的 MySQL 快照。恢复通过项目范围 SCAN 清理遗留日 HLL。

业务层收到 Redis 写入最终错误后不主动重放该次 PV 增量，并保留缺口标记；SQL 提交响应丢失可以重放绝对快照。生产客户端允许一次有界重试：若首次命令已执行而重试成功，极少量重复计数无法与正常访问区分，也不会自动产生 degraded 标记。历史 `degraded` 状态不会因后续成功访问自动清除。

## 启用前提

- 仅支持一个后端写者进程和 standalone Redis。启用 `ContextTimeoutEnabled`；客户端可以做有界重试，代价是极少数回包丢失场景可能重复计数。
- 不要求 `noeviction`，也不执行 `CONFIG GET/SET`。key 淘汰后从 MySQL 最近快照恢复；趋势数据可能在恢复窗口出现可见误差。
- Redis ACL 只需允许 EXISTS、SCAN、MULTI/EXEC 和对应 Hash/Set/HLL/String 普通命令；明确不需要 PING、Lua、EVAL/EVALSHA、FUNCTION、INFO 或 CONFIG 权限。
- 先手动执行 `domain/dal/migrations/20260914_web_project_stats.sql`。DDL 使用 `CREATE TABLE IF NOT EXISTS`，不会自动随应用启动运行；切换时区或格式版本会暂停恢复，不能直接重新解释已有日期。
- 这是趋势统计。进程硬崩溃前仅在内存队列中的事件可能丢失，命令部分完成或重试也可能少量漏计/重复计；Redis 与 SQL 同时不可用时不能保证零丢失。单项目最多接受 366 个可恢复日期状态。

## 验证

测试仅在提供 `HOME_SERVER_TEST_REDIS_ADDR` 和数据库名以 `home_server_analytics_test_` 开头的 `HOME_SERVER_ANALYTICS_TEST_DSN` 时运行；每个用例使用独立 Redis 前缀和项目 ID，不执行 `FLUSHDB`。真实 Redis/MariaDB 用例覆盖无脚本命令约束、刷新与跨日 UV、提交回包丢失、乱序快照、脏标记与新事件交错、key 淘汰恢复、Redis 旧镜像、无 TTL 的脏桶、90 日边界、500 行清理、队列满、并发关闭和淘汰策略兼容。远端使用 `go test -race ./domain/service/webanalytics`，不要本地执行 Go 编译或测试。
