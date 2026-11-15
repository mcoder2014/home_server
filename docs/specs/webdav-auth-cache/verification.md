# WebDAV Basic 密码校验缓存验证

## 验证环境与范围

验证日期：2026-11-15。运行环境为 Linux amd64、Go 1.21.12、Intel Xeon E5-2680 v4 2.40 GHz，`GOMAXPROCS=4`。源码与日志使用独立临时目录；数据库使用专用 MariaDB 10.11 容器和合成账号、文件及会话，不使用线上数据库或运行配置。

生产修改集中在密码验证函数选择和 WebDAV 专用成功缓存。数据库结构、客户端协议、密码哈希成本、Nginx、文件操作及线上服务控制均无变更。

## 功能与并发结果

| 验证 | 结果 |
| --- | --- |
| 原实现负向对照 | 将密码验证路径替换为最新 master 原实现后，4 项新增缓存生命周期测试按预期失败 |
| 五个相关包回归 | 201 个测试及子用例通过，0 失败，2 个可选用例跳过 |
| 新增成功缓存边界 | 正确/错误密码、账号和哈希隔离、别名复用、固定过期、容量满与回收、失败预算、并发均通过 |
| 热缓存后的 HTTP 失效 | 改密、封禁、撤回 WebDAV 权限、关闭模块、临时密码过期 5 种场景均通过；两个 WebDAV 路由别名均验证 |
| 服务层竞态检查 | 账号服务、中间件及 WebDAV：102 个测试及子用例通过，未报告数据竞争 |
| HTTP 竞态检查 | WebDAV 协议、热缓存失效及相关账号兼容：15 个测试及子用例通过，未报告数据竞争 |
| 构建与静态检查 | `build.sh` 和相关包 `go vet` 均通过；后端二进制已生成 |
| 流程图 | Mermaid CLI 渲染成功，生成的 SVG 通过 XML 解析 |

跳过项为 Redis 统计专用测试与账号现场 fixture 导出测试；它们未启用对应的可选测试条件。WebDAV 缓存及本次新增 HTTP 场景没有跳过。

## 性能结果

基准使用合成密码和 cost 12 bcrypt 哈希，每项运行 3 次、每次 `benchtime=1s`，取中位数。优化后的三个分支都先执行一次成功 WebDAV 校验，再分别测量自身作用域，验证网站和已登录密码确认仍执行完整 bcrypt。

| 密码验证路径 | 中位耗时 |
| --- | ---: |
| 原实现 WebDAV | 293.000969 ms/op |
| 优化后 WebDAV 热缓存 | 0.007173 ms/op，即 7.173 µs/op |
| 优化后网站密码验证 | 293.247294 ms/op |
| 优化后已登录密码确认 | 293.032840 ms/op |

WebDAV 热缓存的密码验证函数耗时约为原来的 1/40,848。该测量包含既有失败预算检查及缓存摘要/查询，不包含数据库账号查询、HTTP/TLS、文件系统和 Finder 其他元数据操作；不能将此倍数解释为完整目录浏览的加速倍数。首次请求、缓存过期或密码哈希变化后仍需执行完整 bcrypt。

## 发布边界

验证版本尚未部署到线上。发布后需通过真实 macOS Finder 验收首次连接、连续目录切换，以及改密和撤权后的客户端表现。缓存无需配置或迁移；回滚后端二进制会恢复原逐请求计算方式。

## 验证命令

以下命令在独立测试目录执行。数据库环境变量必须指向专用合成数据库，测试日志应使用独立 `HOME_SERVER_LOG_FILE`。

```sh
go test -count=1 -p 2 ./domain/service/accounts ./domain/service/passport ./api/middleware ./api/webdav ./api/accounts

go test -race -count=1 -p 2 ./domain/service/accounts ./api/middleware ./api/webdav

go test -race -count=1 ./api/accounts \
  -run '^TestHTTP(WebDAV|CapabilitiesAreIndependentAndRevocable|BanRestoreAndDeleteDoNotReviveCredentials|ChangePasswordInvalidatesEveryOldSession)'

go test -run='^$' -bench='^BenchmarkVerifyPasswordWebDAV' \
  -benchtime=1s -count=3 -benchmem ./domain/service/accounts

./build.sh
go vet ./domain/service/accounts ./domain/service/passport ./api/middleware ./api/webdav ./api/accounts
```
