# WebDAV Basic 密码校验缓存实施计划

目标：在独立 worktree 中消除 WebDAV 重复密码计算，并验证既有身份失效语义。

架构：保持 `ValidateBasicAuth → ValidateUser → Authenticate → VerifyPassword` 调用链，仅在 `VerifyPassword` 选取 WebDAV 专用成功缓存。缓存使用 Go 标准库的 HMAC、随机源、互斥锁和有限 map；不增加 SDK。

技术栈：Go 1.20 模块、Gin、bcrypt、现有 MariaDB 隔离 HTTP fixture。

## 执行步骤

- [x] 从远端最新 master 创建 `fix/cq/webdav_auth_cache` 及独立 worktree。
- [x] 核对当前密码校验、失败预算和账号状态检查；记录设计边界。
- [x] 新增真实 bcrypt 基准及成功缓存边界测试；使用 master 原校验路径确认 4 项缓存测试失败。
- [x] 新增 `domain/service/accounts/webdav_password_cache.go`，在原 `VerifyPassword` 成功判断处选择专用校验函数。
- [x] 补充隔离 HTTP 测试，先热缓存，再验证改密、封禁、权限收回、模块关闭和临时密码过期。
- [x] 在 Pi 独立测试目录执行五个相关包回归；201 个测试及子用例通过，2 个可选用例跳过。
- [x] 在 Pi 执行账号服务、中间件和 WebDAV 包竞态检查；102 个测试及子用例通过。
- [x] 完成 WebDAV HTTP 失效场景竞态检查和原实现/优化实现的 cost 12 基准；HTTP 竞态 15 个测试及子用例通过。
- [x] 在开发机运行 `build.sh` 并检查二进制产物，通过相关包静态检查；同步 README 并渲染流程图。
- [x] 完成独立静态审查，并补充热缓存后的其他密码作用域验证。
- [x] 完成 Pi 本机构建、静态检查和[验证记录](verification.md)；密码验证函数中位数从 293.000969 ms 降为 0.007173 ms。
- [x] 完成最终 diff 核对并准备提交内容与 PR 说明。

## 测试范围

| 层次 | 目标 |
| --- | --- |
| 成功缓存单测 | 正确/错误密码、账号隔离、哈希变更、固定有效期、容量、失败预算、并发 |
| 原有回归 | `domain/service/accounts`、`domain/service/passport`、`api/middleware`、`api/webdav` |
| HTTP 验证 | 使用专用合成数据库运行 `api/accounts` 中与 WebDAV 相关的权限、封禁、改密场景 |
| 性能 | `BenchmarkVerifyPasswordWebDAV` 的网站密码确认、已登录密码确认和 WebDAV 热缓存三个分支；各分支先热 WebDAV 缓存 |
| 构建 | 独立源码目录执行 `./build.sh`，校验 `output/bin/home_server` 存在 |

性能与测试结果以验证记录为准。测试数据和配置不得使用 Pi 生产凭据。
