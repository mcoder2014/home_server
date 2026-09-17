# 个人中心实施与验证计划

**目标**：落实个人资料、头像、有效登录管理及管理员资料重置；保持现有认证和授权兼容；在 Pi 的隔离环境验证并提交 PR。

**架构**：扩展现有 accounts 服务和 login_token，不引入第二套认证。头像使用独立 user_avatar 表；账号 revision 与头像版本在同一事务更新。有效登录列表与统计共用有效性条件和服务端时间。

**技术栈**：Go / Gin / GORM / MariaDB，Vue 3 / Element Plus，现有 Cookie、Origin、CSRF 和审计。

基线：远端 master `8404303ab2c5cdde9a5cb6449db49d44c7f36730`。详细产品方案见 design.md。

| 步骤 | 文件与内容 | 验证 |
|---|---|---|
| 1 | 新增 20260917_account_center.sql；扩展 domain/model/account.go、domain/dal/account.go；兼容 accountsmigrate 的严格 DDL 校验 | Pi 基线测试、新旧迁移、重复应用 |
| 2 | domain/service/accounts/sessions.go、service.go、queries.go；api/accounts/sessions.go；登录入口和账号中间件携带元数据与当前会话 ID；动态配置默认同时登录上限 5 | 有效性边界、跨用户撤销、分页、批量统计、并发签发／撤销、旧配置及导入摘要兼容 |
| 3 | domain/service/accounts/avatars.go；api/accounts/avatars.go；mutations.go 的 reset-profile 和删除清理 | 真图像解码、字节和像素限制、版本冲突、权限、旧 URL 失效、审计原子性 |
| 4 | 最小用户展示对象；项目成员、邀请、审计、评论补充当前昵称和头像 | 批量查询、管理员重置后展示更新、不返回头像字节或密钥 |
| 5 | Account.vue、AdminUsers.vue、共享身份和会话组件；accounts API；网页容器展示 | 资料草稿冲突、头像裁剪、退出选择、管理员处置、窄屏浏览器验证 |
| 6 | nginx 精确头像路由；accounts-maintain 元数据清理；操作文档 | Pi nginx 配置检查、过期清理幂等、EXPLAIN |
| 7 | Pi 独立目录和 synthetic MariaDB：远端 Go 构建、全量测试、前端构建及实际 HTTP/浏览器验证 | 不使用生产数据库、不替换在线服务；证据记录后 commit、push、PR |

边界：账号配置模式保持旧登录协议；数据库专属新接口明确拒绝。初始改密会话不能访问个人中心和头像。WebDAV Basic 的逐请求认证保持原行为。普通撤销不影响应用凭证。

验证命令：Pi 中 `./build.sh`、CI 定义的应用与存储包测试（显式配置 HTTP、会话、迁移、分析及缓存合成库 DSN）、`npm run test:frontend`、`npm run build`；浏览器测试使用独立服务及合成账号。每一步先补行为测试，观察失败，再落实实现。所有检查通过后提交 PR，由仓库所有者合并。
