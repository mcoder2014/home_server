# CQ Home Server

本项目仅用于家庭服务器的 HTTP 服务，仅用于个人学习测试，非本人授权，不得用于商用或其他任何用途。

## 计划结构

- 设备客户端：用于在家庭硬件设备上运行，提供部分运维能力（上报系统负载、watch dog）
- 服务端：系统主体部分
- 前端：提供管理员用户界面

## 计划能力

- DDNS: 用于动态 dns 注册，由于小米路由器尚未具备 ipv6 的 ddns 能力，home server 可以为家庭硬件设备提供 ddns 能力；
- Watch Dog: 或者叫 heart beat，用于记录设备心跳数据，包含一些辅助数据，可以快速发现设备是否掉线；
- 家庭图书管理能力: 以 isbn 为基础，快速管理家庭中的纸质书及电子书；
- WebDAV: 使用同一套账号体系，通过 WebDAV 协议实现外部文档访问，地址为 [https://www.server.com:port/webdav](https://www.server.com:port/webdav)；
- 网页项目：动态管理 HTML 文档和纯前端工具，通过同域 `/p/{slug}/` 访问，由服务端控制公开或指定账号可见。

## 安装说明

### 前端页面

编译前端文件，将 dist 目录下的文件拷贝到服务器的 /var/www/html 目录下，即可访问。

```shell
npm run build
```

管理站点统一使用 **CQ Home Server** 品牌，首页提供网页项目、家庭藏书与图书录入入口。导航、登录页、项目列表和设置页共用配色与组件样式；窄屏下导航分行，项目列表显示为卡片。

浏览器标题采用 `CQ Home Server · 页面内容`：

| 页面 | 标题示例 |
| --- | --- |
| 首页、登录 | `CQ Home Server · 首页`、`CQ Home Server · 登录` |
| 网页项目列表、创建 | `CQ Home Server · 网页项目`、`CQ Home Server · 新建网页项目` |
| 项目详情 | 加载后显示项目名称，保存改名后同步更新。 |
| 图书页面 | `CQ Home Server · 图书列表`、`CQ Home Server · 录入图书`等。 |

标题随前端路由切换，HTML 模板提供加载前的默认标题和站点图标。托管在 `/p/{slug}/` 的用户网页保留各自的 HTML 标题，不改写上传内容。

### home_server

执行 `./build.sh` 构建 server 和 client 的二进制文件。

### home_client
1. 运行 `./build.sh`，编译程序;
2. 仿照 `client/config_example.yaml` 编写一份自己的配置文件；
3. 将二进制文件复制到指定文件夹 `sudo cp/bin/home_client /usr/local/bin/home_client`;
4. 将配置文件放在指定文件 `/etc/home_server/client_config.yaml`；
5. 将 systemd 配置文件复制到指定路径 `sudo cp script/systemd/home_client.service /etc/systemd/system`
6. 启动 `sudo systemctl start home_client.service`

#### 查看日志

```shell
sudo journalctl --unit home_client.service
```

## 网页项目

### 使用方式

从首页或导航进入“网页项目”，创建项目，上传单个 HTML 或静态文件 ZIP，再发布。新增、改名、下线及删除只更新数据库，不需要逐项目修改 Nginx 或重启 home_server。

| 操作 | 结果 |
| --- | --- |
| 创建 `report` | 得到 `/p/report/`，默认仅自己可见，发布前为草稿。 |
| 上传并发布 | 单 HTML 成为 `index.html`；ZIP 保留目录结构，发布时切换完整版本。 |
| 更改 URL | 修改 slug，文件目录和用户授权保持不变；旧地址不再指向该项目。 |
| 指定用户 | 创建者和选中的有效账号可读，其他账号不可读。 |
| 下线或删除 | 后续页面和资源请求返回 404；删除有 7 天回收期。 |
| 回滚 | 重新发布历史版本，只恢复文件，不恢复旧 URL 或旧权限。 |

删除不会自动释放原路径，回收期到期只清理版本文件，保留项目元数据。需要让新项目复用某个路径时，应先修改旧项目的路径，再删除旧项目；新项目的访问权限独立配置。

访问范围为 `owner`（仅自己）、`members`（指定用户）、`authenticated`（全部登录用户）和 `public`（匿名公开）。查看权限不包含修改、上传或删除权限。账号来自现有 `passport.mock_data`，使用稳定用户 ID 关联项目。

项目共享浏览器源，适合本人和受信任维护者上传的网页。服务端检查每个 HTML、JS、图片和附件请求，但不提供不可信 JavaScript 的项目间隔离。

### 部署配置

先检查现有 `login_token` 的索引；网页资源会逐请求验证 token，需要以 `token` 为首列的索引。增量迁移 `domain/dal/migrations/20260909_web_projects.sql` 包含普通 token 索引和三张新表，已有等价 token 索引时跳过相应建索引语句，不假设历史 token 数据唯一。迁移在目标数据库执行并核对后，再添加服务端配置：

```yaml
web_projects:
  enabled: true
  storage_root: /var/lib/home_server/web-projects
  site_origin: https://home.example.com
```

`site_origin` 必须是浏览器实际使用的 HTTPS Origin，非默认端口需要一并填写，不能包含路径。旧配置缺少 `web_projects` 时模块默认关闭。

数据目录需要服务账号有写权限，并且必须位于 WebDAV 共享目录、管理前端 `dist` 及其他公共静态目录之外。数据库保存项目、成员和版本信息，文件保存在 `storage_root` 的独立版本目录中；应同时备份数据库和文件。

在提供前端的现有 HTTPS Nginx `server` 中引入 `config/nginx/web_projects_locations.conf`，根据实际位置修改该文件中的后端地址。保留现有 Vue 的页面 fallback。该片段让 `/p/` 和新 API 经过服务端，并禁止代理缓存；不能另外用 `root` 或 `alias` 直接暴露项目文件。

升级顺序为迁移、后端和同源代理、最后前端。模块关闭时仍应保留同源 API 代理，由后端返回明确的 404，兼容旧登录；不能让登录同步请求落到静态页面 fallback。

默认上传包上限为 50 MiB，解压后 200 MiB、5,000 个文件、16 层目录；每项目默认 1 GiB、最多 10 个成功版本。完整参数见 `config/config_example.yaml`。增大上传限制时同时调整 Nginx 的 `client_max_body_size`，当前版本不会被历史清理删除。

### 前端产物约定

页面从 `/p/{slug}/` 运行，资源和站内链接应使用相对路径：

```html
<script src="./assets/app.js"></script>
<link rel="stylesheet" href="./assets/style.css">
<a href="./guide.html">使用说明</a>
```

`/assets/app.js` 会访问整个站点的根路径，不能自动定位到某个项目。Vue/React 项目应在上传前构建完成，并配置相对资源路径；SPA 优先使用 Hash 路由。首版不自动改写代码或适配任意 History 路由，Service Worker 注册不受支持。

ZIP 根目录直接放入口文件和资源，即打包 `dist/` 的内容。平台不在服务器执行包中的 npm、Python、Shell 或其他后端代码。

版本下载先在私有 `staging` 目录生成完整 ZIP，并核对文件数和总字节数，再开始发送。入口缺失、文件读写失败或元数据不符时返回 503；请求结束后删除本次生成的临时 ZIP。

### 登录与自动化接口

管理 API 继续使用现有 `passport` 请求头。前端调用同源 `POST /api/web-projects/browser-login`，校验该 token 后设置 `Secure`、`HttpOnly` 的内容 Cookie；浏览器打开 `/p/` 及加载资源时自动携带。Cookie 不能代替管理 API 的请求头认证，退出原账号后内容访问也随 token 失效。

登录或切换账号时，先用新 token 同步内容 Cookie，再提交浏览器本地身份。同步失败时停止切换，避免页面显示新账号却沿用旧账号的内容权限。

退出操作只有在服务端确认 token 已失效后才清理本地身份。服务端报错或网络失败时保留当前登录状态并提示重试，避免界面显示已退出而内容 Cookie 仍有效。

| API | 用途 |
| --- | --- |
| `POST /api/web-projects` | 创建项目。 |
| `GET/PATCH/DELETE /api/web-projects/{id}` | 读取、修改或删除自己的项目。 |
| `POST /api/web-projects/{id}/releases` | 用 multipart `file` 上传 HTML/ZIP。 |
| `POST /api/web-projects/{id}/publish` | 指定 `release_id` 发布或回滚。 |
| `POST /api/web-projects/{id}/disable`、`restore` | 下线或恢复。 |
| `GET /api/web-projects/eligible-users` | 取得可见用户选择所需的账号 ID 和用户名。 |

更新、发布、下线、删除及恢复需要 `If-Match` 携带详情返回的 `revision`。旧 revision 返回 409，调用者需要重新读取当前状态。创建可使用 `client_request_id`，上传可使用 `Idempotency-Key`，避免网络重试重复生成项目或版本。

AI 工具可以复用这些接口和已授权的登录 token。凭证通过受控运行环境提供，不能写入网页、ZIP 或分享 URL。上传成功只表示版本就绪；还需核对发布成功及当前版本，才能认为网页已经更新。

### 验证与恢复

验证时使用独立数据库、数据目录和空闲端口，从运行配置派生副本，不覆盖正在使用的 systemd 配置。现有部分测试会写数据库或更新 DNS，不能直接使用运行凭证执行全量测试。

GitHub Actions 使用 Go 1.21.12 构建全部 Go 包，只运行可离线执行的 `api/webprojects`、`domain/service/webprojects`、`domain/service/passport`、`domain/service/rsa`、`utils` 和 `utils/md` 测试。依赖数据库、外部 RPC/DNS 或特定网卡的集成测试需要单独配置隔离条件；CI 的离线测试不能替代数据库及 HTTPS 链路验收。

服务端可使用 `-host 127.0.0.1 -port 18180 -conf /path/to/test-config.yaml` 只监听本机测试端口；省略 `-host` 保持既有监听行为。前端隔离构建可设置 `VUE_APP_API_BASE_URL=https://127.0.0.1:18443`，使登录请求也进入测试 HTTPS 代理；未设置时沿用原 API 地址。

测试进程设置 `HOME_SERVER_LOG_FILE=/path/to/test-logs/run.log`，将日志写入独立目录；未设置时仍使用 `/var/log/home_server/run.log`。验证过程不需要修改系统挂载或现有服务日志。

服务数据库不可用或当前版本文件缺失时返回错误，不绕过鉴权或自动切回其他版本。公开转私有后新的授权检查使用新规则，但已经发送给浏览器的内容无法收回。恢复备份时先关闭内容入口，核对数据库指针与文件，并处理备份中登录凭证可能重新生效的问题。

#### 存储引用审计

进程可能在完整版本目录完成原子移动后、数据库版本记录提交前异常退出。维护者可以在停止网页项目写入后运行只读审计命令，查找生成目录与数据库引用不一致的候选：

```shell
go build -o output/bin/web-projects-audit ./cmd/web-projects-audit
./output/bin/web-projects-audit -conf /etc/home_server/conf.yaml -min-age 1h
```

命令只读取 `storage_root/projects/<project_id>/releases/<release_id>` 目录元数据和数据库中的 `id`、`project_id`、`storage_key`，不读取网页内容，不启动周期清理，也不修改或删除文件。默认忽略最近一小时创建的目录，并跳过非数字目录、异常目录和软链接。数据库查询失败时命令返回失败，不会把数据库当成空库。

JSON 输出包含候选目录、原因和扫描统计。候选只表示“缺少数据库引用”或“`storage_key` 不一致”，不等于可以删除；回收前应停止上传，核对数据库与文件备份，并由维护者按明确目录人工处理。该命令没有自动删除能力，也不提供删除参数。

## FAQ

##### Q: 为什么服务没有使用 SSL?

A: 本项目是家庭服务，内网只暴露了 http，外网通过一个虚拟机的 nginx 反向代理实现，nginx 处配置了 SSL。其实服务就可以写的简化些，不需要使用 SSL。

反向代理的配置文件可以参考`config/nginx/home_server_backend.conf`。

## 版权信息 MIT LICENSE

本项目为个人兴趣，目的在于满足个人需求，不提供技术支持，使用本系统造成数据丢失、机器损坏等损失概不负责。
