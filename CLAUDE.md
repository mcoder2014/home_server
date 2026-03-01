# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目简介

家庭服务器 HTTP 服务（Go + Gin），包含三个组件：
- **home_server**：后端服务（main.go 入口）
- **home_client**：家庭设备客户端，负责 DDNS 更新（client/main.go 入口）
- **front_vue**：Vue 3 管理前端

## 常用命令

```bash
# 构建（输出到 output/bin/）
./build.sh

# 运行服务端
go run ./ -conf ./config/config.yaml -port 8080

# 运行客户端
go run ./client -conf ./client/config/config.yaml

# 使用脚本运行测试（会设置 TEST_CONFIG_PATH 环境变量）
./ut.sh

# 运行单个包的测试
go test -v ./rpc/...
go test -v -run TestFuncName ./path/to/package/...

# 前端构建
cd front_vue && npm install && npm run build
```

注意：`build.sh` 和 `ut.sh` 都依赖 `config/config.yaml` 存在，首次使用需从 example 复制：
```bash
cp config/config_example.yaml config/config.yaml
cp client/config/config_example.yaml client/config/config.yaml
```

### 启动测试环境

系统通过 systemd 运行了正式服务（端口 18080），测试环境使用独立配置和端口 8081，可与正式服务并行运行：

```bash
# 启动测试环境（端口 8081）
go run ./ -conf ./config/config_test_env.yaml -port 8081

# 或先编译再运行
go build -o output/bin/home_server . && ./output/bin/home_server -conf ./config/config_test_env.yaml -port 8081
```

- 测试配置文件：`config/config_test_env.yaml`（基于 `config.yaml`，端口改为 8081）
- 测试环境与正式环境共享同一 MySQL 数据库和 WebDAV 存储路径

## 架构分层

```
api/            → HTTP 处理层（handler + middleware）
  ├── middleware/   CORS、LogID、Passport 鉴权
  ├── library/     图书管理接口
  ├── passport/    登录/鉴权接口
  ├── fileshare/   文件分享接口
  ├── webdav/      WebDAV 协议接口
  └── handler.go   统一路由初始化入口
domain/         → 领域层
  ├── model/       数据模型（Gorm 结构体）
  ├── dal/         数据访问层（含 create_table.sql）
  ├── db/          数据库初始化 + BaseDAO
  └── service/     业务逻辑（passport、webdav、book 等）
rpc/            → 外部服务调用（Cloudflare DNS、IP 检测、ISBN 查询）
route/          → Gin Engine 初始化
data/           → 路由注册容器（AddRoute/ForRange）
utils/          → 工具库（日志、配置加载、加密、ginfmt 响应格式化等）
config/         → 服务端配置 + Nginx/systemd 部署模板
```

## 路由注册模式

新增 HTTP 接口的步骤：
1. 在 `api/<module>/` 下实现 handler 函数
2. 在该模块的 `InitRouter` 中调用 `data.AddRoute()` 或 `data.AddRouteV2()` 注册路由
3. 确保模块的 `InitRouter` 被 `api/handler.go` 的 `InitRouter()` 调用

## 技术栈

- **Go 1.18+**，框架：Gin、Gorm(MySQL)、Logrus
- 测试：`stretchr/testify`
- 外部 API：Cloudflare DNS（DDNS）、聚美 ISBN 查询
- 前端：Vue 3 + Element Plus + Vuex + Vue Router

## 配置

- 服务端配置：`config/config.yaml`（默认路径 `/etc/home_server/conf.yaml`，`-conf` 可覆盖）
- 客户端配置：`client/config/config.yaml`（默认路径 `/etc/home_server/client_config.yaml`）
- 配置文件含密钥，已被 `.gitignore` 忽略，仅 `*_example.yaml` 入库
- WebDAV 缩略图缓存上限：`webdav.cache_max_size_mb`（默认 256MB），控制 `ImageCache` 的 LRU 内存缓存大小

## 错误码

定义在 `errors/errorcode.go`：0=成功、1=未知、2=参数错误、101-106=RPC、201-204=业务、301=数据库、401-404=鉴权。

## CI

GitHub Actions（`.github/workflows/go.yml`）：push/PR 到 master 时执行 `go build` 和 `go test`。
