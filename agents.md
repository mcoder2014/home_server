# Agents Guide (home_server)

## 项目概览

本仓库是一个家庭服务器项目，主要包含：

- `home_server`：基于 Gin 的 HTTP 服务端，提供登录/鉴权、图书管理、文件分享、WebDAV、DDNS 等接口能力。
- `home_client`：运行在家庭设备上的客户端，定时执行 DDNS 等运维/上报类任务。
- `front_vue`：管理端前端（Vue 3），构建后通过 Nginx 静态资源方式部署。

## 技术栈

- 后端：Go（`go.mod` 指定 `go 1.18`）、Gin、Gorm(MySQL)、Logrus
- 前端：Vue 3、Vue Router、Vuex、Element Plus、Axios

## 目录结构速览

- `main.go`：`home_server` 入口
- `client/main.go`：`home_client` 入口
- `api/`：HTTP API 分组与路由初始化
  - `api/handler.go`：统一初始化各子模块路由
  - `api/middleware/`：Gin 中间件（CORS、logid、passport 等）
  - `api/library/`：图书管理相关接口
  - `api/fileshare/`：文件分享相关接口
  - `api/webdav/`：WebDAV 相关接口
  - `api/passport/`：登录/鉴权相关接口
  - `api/ddns.go`：DDNS 接口入口
- `route/`：Gin Engine 初始化与路由注册入口
- `data/route.go`：路由注册容器（`AddRoute`/`ForRange`）
- `domain/`：领域层
  - `domain/model/`：模型定义
  - `domain/dal/`：数据访问层（含 `create_table.sql`）
  - `domain/db/`：数据库初始化
  - `domain/service/`：业务服务层（passport、rsa、book 等）
- `rpc/`：对外依赖调用（如 Cloudflare、IP、ISBN 等）
- `utils/`：通用工具库（日志、配置加载、ginfmt、加密、测试工具等）
- `config/`：服务端配置与部署模板（Nginx/systemd）
- `script/`：启动脚本与 systemd 示例
- `front_vue/`：前端工程

## 启动与构建

### 服务端（home_server）

默认读取配置路径为 `/etc/home_server/conf.yaml`，也可通过 `-conf` 指定：

```bash
go run ./ -conf ./config/config.yaml
```

本地启动通常先从示例生成配置文件：

```bash
cp config/config_example.yaml config/config.yaml
```

可通过 `-port` 覆盖配置文件中的端口：

```bash
go run ./ -conf ./config/config.yaml -port 8080
```

### 客户端（home_client）

```bash
go run ./client -conf ./client/config/config.yaml
```

本地启动通常先从示例生成配置文件：

```bash
cp client/config/config_example.yaml client/config/config.yaml
```

如果按仓库脚本的输出习惯（`build.sh` + `script/boot_client.sh`），客户端配置文件名通常是 `client_config.yaml`，此时可显式指定：

```bash
go run ./client -conf ./output/client_config.yaml
```

### 构建二进制

仓库提供 `build.sh`，默认输出到 `output/bin/`，并复制启动脚本与配置文件到 `output/`：

```bash
./build.sh
```

注意：`build.sh` 依赖以下文件存在（仓库默认提供的是 `*_example.yaml`）：

- `config/config.yaml`（通常由 `config/config_example.yaml` 复制并改名生成）
- `client/config/config.yaml`（通常由 `client/config/config_example.yaml` 复制并改名生成）

### 前端（front_vue）

```bash
cd front_vue
npm install
npm run build
```

构建产物在 `front_vue/dist/`。仓库 README 中的部署方式是将 dist 拷贝到服务器的 `/var/www/html`，并通过 Nginx 提供静态访问。

## 测试

GitHub Actions 的默认验证为：

```bash
go test -v ./...
```

仓库另有 `ut.sh`，会把 `config/config.yaml` 拷贝到临时目录，并通过环境变量 `TEST_CONFIG_PATH` 暴露给测试使用：

```bash
./ut.sh
```

注意：`ut.sh` 同样要求 `config/config.yaml` 已存在。

## 配置文件

### 服务端配置（`config/`）

示例：`config/config_example.yaml`

- `server.port`：HTTP 端口
- `mysql.master_db`：MySQL DSN（Gorm）
- `rpc.jmisbn.app_code`：ISBN 查询外部服务 AppCode
- `rpc.cloudflare.*`：Cloudflare 配置（用于 DDNS/域名相关能力）
- `passport.mock_data`：可用于本地 Mock 用户数据
- `passport.redirect_login_path`：登录跳转页
- `webdav.share_path`：WebDAV 共享目录

### 客户端配置（`client/config/`）

示例：`client/config/config_example.yaml`

- `cloudflare.api_token/zone`：Cloudflare 访问与域配置
- `ddns_config[]`：需要更新的域名与 IP 版本（ipv4/ipv6）

## 路由与模块扩展约定

项目路由注册采用集中容器方式：

- 在各 API 子模块的 `InitRouter` 中调用 `data.AddRoute(...)` 或 `data.AddRouteV2(...)` 注册路由。
- `api.InitRouter()` 会在进程启动时统一调用各模块初始化。
- `route.InitRoute()` 在 Gin Engine 创建后，通过 `data.ForRange(...)` 批量 `engine.Handle(...)` 挂载。

这意味着新增一个 HTTP 接口的常见步骤是：

1. 在对应 `api/<module>/` 下实现 handler。
2. 在该模块的 `InitRouter` 里注册到 `data` 路由容器。
3. 确保模块的 `InitRouter` 被 `api/handler.go` 纳入初始化列表。

## 部署相关

- Nginx 反向代理示例：`config/nginx/`
- systemd 示例：`config/systemd/` 与 `script/systemd/`
- 启动脚本：`script/boot_server.sh`、`script/boot_client.sh`

## 安全与密钥

- `config/config.yaml` 与 `client/config/config.yaml` 被 `.gitignore` 忽略，避免把密钥提交到仓库。
- `config/*_example.yaml` 中的字段仅用于示例，实际使用时请使用自己的密钥并避免泄漏。
