# CQ Home Server

本项目仅用于家庭服务器的 HTTP 服务，仅用于个人学习测试，非本人授权，不得用于商用或其他任何用途。

## 计划结构

- DDNS 设备客户端：已迁移到 `life_tools/cli/cq_ddns_client`，独立安装和运行。
- 服务端：系统主体部分
- 前端：提供管理员用户界面

## 计划能力

- DDNS：服务端保留原有 HTTP 接口；设备定时更新 Cloudflare DNS 的能力由 `life_tools` 中的 `cq_ddns_client` 提供。
- Watch Dog: 或者叫 heart beat，用于记录设备心跳数据，包含一些辅助数据，可以快速发现设备是否掉线；
- 家庭图书管理能力: 以 isbn 为基础，快速管理家庭中的纸质书及电子书；
- WebDAV: 使用同一套账号体系，通过 WebDAV 协议实现外部文档访问，地址为 [https://www.server.com:port/webdav](https://www.server.com:port/webdav)；
- 网页托管：动态管理 HTML 文档和纯前端工具，通过同域 `/p/{slug}/` 访问，由服务端控制公开或指定账号可见。

## 安装说明

### 前端页面

编译前端文件，将 dist 目录下的文件拷贝到服务器的 /var/www/html 目录下，即可访问。

```shell
npm run build
```

管理站点统一使用 **CQ Home Server** 品牌，首页提供网页托管、家庭藏书与图书录入入口。导航、登录页、项目列表和设置页共用配色与组件样式；窄屏下导航分行，项目列表显示为卡片。

浏览器标题采用 `CQ Home Server · 页面内容`：

| 页面 | 标题示例 |
| --- | --- |
| 首页、登录 | `CQ Home Server · 首页`、`CQ Home Server · 登录` |
| 网页托管列表、创建 | `CQ Home Server · 网页托管`、`CQ Home Server · 新建网页托管` |
| 项目详情 | 加载后显示项目名称，保存改名后同步更新。 |
| 图书页面 | `CQ Home Server · 图书列表`、`CQ Home Server · 录入图书`等。 |

标题随前端路由切换，HTML 模板提供加载前的默认标题和站点图标。托管在 `/p/{slug}/` 的用户网页保留各自的 HTML 标题，不改写上传内容。

### 前后端服务管理

通用部署示例和回滚说明见 [deploy/pi/README.md](deploy/pi/README.md)。前端通过独立 Nginx 服务运行在 `192.0.2.10:18081`，后端为 `18080`；TLS 网关使用 `https://home.example.com:8080` 提供同源页面与 API。

`home_server.target` 统一控制前后端启动、停止和重启，也可以分别操作 `home_server.service` 与 `home_server_frontend.service`。示例使用文档地址与保留域名，实际环境配置不得提交。模板包含开机自启动、网络就绪等待、版本目录与私有配置；安装前需要完成数据库结构核对和备份。

### home_server

执行 `./build.sh` 构建服务端二进制文件。构建前准备 `config/config.yaml`；不再需要客户端配置。

### DDNS 客户端迁移

`home_client` 已迁移到 [life_tools 的 cq_ddns_client](https://github.com/mcoder2014/life_tools/blob/master/docs/cli/cq_ddns_client.md)。客户端源码、构建产物和 systemd 模板统一在 `life_tools` 维护，服务端 DDNS HTTP 接口保持兼容。

| 项目 | 旧入口 | 新入口 |
|---|---|---|
| 命令 | `home_client` | `cq_ddns_client` |
| 默认配置 | 部署时通过 `-conf` 指定 YAML | `/etc/life_tools/cq_ddns_client.json` |
| 配置参数 | `-conf` | `-config`，保留 `-conf` 别名及 `.yaml/.yml` 读取 |
| systemd | `home_client.service` | `cq_ddns_client.service` |

旧二进制和正在运行的服务不会因源码迁移自动替换。先安装新客户端，用旧配置执行只读预检，再按迁移文档转换 JSON、验证新服务并切换；不能同时长期运行两个客户端。

```bash
cq_ddns_client -conf /etc/home_server/client_config.yaml -dry-run
```

回滚时停用新服务，再启动原 `home_client.service`。切换验证完成前保留旧二进制、配置和 unit。

## 网页托管

### 使用方式

从首页或导航进入“网页托管”，创建项目，上传单个 HTML 或静态文件 ZIP，再发布。新增、改名、下线及删除只更新数据库，不需要逐项目修改 Nginx 或重启 home_server。

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

先检查现有 `login_token` 的索引；网页资源会逐请求验证 token，需要以 `token` 为首列的索引。首次安装的 `domain/dal/migrations/20260909_web_projects.sql` 包含普通 token 索引和三张新表，已有等价 token 索引时跳过相应建索引语句，不假设历史 token 数据唯一。已安装旧 PR 文字枚举表的环境改用 `20260912_web_projects_refactor.sql`，两者不能对同一个库重复执行；升级脚本遇到未知枚举会中止，修正数据后可重跑。迁移在目标数据库执行并核对后，再添加服务端配置：

```yaml
web_projects:
  enabled: true
  storage_root: /var/lib/home_server/web-projects
  site_origin: https://home.example.com
```

`site_origin` 必须是浏览器实际使用的 HTTPS Origin，非默认端口需要一并填写，不能包含路径。旧配置缺少 `web_projects` 时模块默认关闭。

数据目录需要服务账号有写权限，并且必须位于 WebDAV 共享目录、管理前端 `dist` 及其他公共静态目录之外。数据库保存托管网页、成员和版本信息，文件按所有者用户 ID 隔离；应同时备份数据库和文件。

```text
storage_root/
  <user_id>/upload/html/
    .staging/                           # 本用户的上传、解包和下载临时文件
    <project_id>/releases/
      <release_id>/content/              # 完整的静态网页版本
```

`user_id` 由服务端验证后的所有者身份确定。应用身份使用应用所属用户 ID；上传文件名、分享 URL 和请求参数不能覆盖它。目录创建权限为 0700、文件为 0600，用户目录中的软链接不作为存储路径。其他用户即使拥有查看权限，也不会在自己的目录复制该网页。修改网页名称或 slug 只修改元数据，不搬动版本文件。

新上传只写入上述用户目录；已有 `projects/<project_id>/releases/<release_id>/content` 版本保持受控读取、下载和清理兼容。离线迁移完成前不删除旧目录。

在提供前端的现有 HTTPS Nginx `server` 中引入 `config/nginx/web_projects_locations.conf` 和 `config/nginx/api_auth_locations.conf`，根据实际位置修改该文件中的后端地址。保留现有 Vue 的页面 fallback。该片段让 `/p/` 和新 API 经过服务端，并禁止代理缓存；不能另外用 `root` 或 `alias` 直接暴露项目文件。

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

版本下载先在所属用户的私有 `<user_id>/upload/html/.staging` 目录生成完整 ZIP，并核对文件数和总字节数，再开始发送。入口缺失、文件读写失败或元数据不符时返回 503；请求结束后删除本次生成的临时 ZIP。

### 登录与自动化接口

管理页面使用 `/web-share`，主 API 使用 `/api/web-share`。旧 `/web-projects` 页面地址和 `/api/web-projects` API 保留兼容；写 API 直接调用相同处理逻辑，不依赖重定向。已有 `/p/{slug}/` 分享链接不变，已授权的 `web-projects:read/write` scope 值也保持不变。

用户管理 API 继续使用现有 `passport` 请求头；授权应用可使用短期 Bearer Token。前端调用同源 `POST /api/auth/browser-login` 建立通用 `__Host-cq_session` Cookie，`/api/web-share/browser-login` 和旧 `/api/web-projects/browser-login` 保留为兼容别名。Cookie 只保存不透明用户 token，带 Secure、HttpOnly、SameSite=Lax 和 Path=/；不把 user_id/user_name 等声明当作认证依据。浏览器访问 `/p/` 自动携带 Cookie；Cookie 不能代替管理 API 的显式凭证，应用不能建立用户 Cookie。

登录或切换账号时，先用新 token 同步内容 Cookie，再提交浏览器本地身份。同步失败时停止切换，避免页面显示新账号却沿用旧账号的内容权限。

退出操作只有在服务端确认 token 已失效后才清理本地身份。服务端报错或网络失败时保留当前登录状态并提示重试，避免界面显示已退出而内容 Cookie 仍有效。

| API | 用途 |
| --- | --- |
| `POST /api/web-share` | 创建项目。 |
| `GET/PATCH/DELETE /api/web-share/{id}` | 读取、修改或删除自己的项目。 |
| `POST /api/web-share/{id}/releases` | 用 multipart `file` 上传 HTML/ZIP。 |
| `POST /api/web-share/{id}/publish` | 指定 `release_id` 发布或回滚。 |
| `POST /api/web-share/{id}/disable`、`restore` | 下线或恢复。 |
| `GET /api/web-share/eligible-users` | 取得可见用户选择所需的账号 ID 和用户名。 |

更新、发布、下线、删除及恢复需要 `If-Match` 携带详情返回的 `revision`。旧 revision 返回 409，调用者需要重新读取当前状态。创建可使用 `client_request_id`，上传可使用 `Idempotency-Key`，避免网络重试重复生成项目或版本。

AI 工具可以复用这些接口和已授权的登录 token。凭证通过受控运行环境提供，不能写入网页、ZIP 或分享 URL。上传成功只表示版本就绪；还需核对发布成功及当前版本，才能认为网页已经更新。

### 验证与恢复

验证时使用独立数据库、数据目录和空闲端口，从运行配置派生副本，不覆盖正在使用的 systemd 配置。现有部分测试会写数据库或更新 DNS，不能直接使用运行凭证执行全量测试。

GitHub Actions 使用 Go 1.21.12 构建全部 Go 包，运行已核对的离线 API、app、model、repository、领域服务及公共工具测试，并运行 Python AI 客户端测试。依赖数据库、外部 RPC/DNS 或特定网卡的集成测试需要单独配置隔离条件；CI 的离线测试不能替代数据库及 HTTPS 链路验收。

服务端可使用 `-host 127.0.0.1 -port 18180 -conf /path/to/test-config.yaml` 只监听本机测试端口；省略 `-host` 保持既有监听行为。前端隔离构建可设置 `VUE_APP_API_BASE_URL=https://127.0.0.1:18443`，使登录请求也进入测试 HTTPS 代理；未设置时沿用原 API 地址。

测试进程设置 `HOME_SERVER_LOG_FILE=/path/to/test-logs/run.log`，将日志写入独立目录；未设置时仍使用 `/var/log/home_server/run.log`。验证过程不需要修改系统挂载或现有服务日志。

服务数据库不可用或当前版本文件缺失时返回错误，不绕过鉴权或自动切回其他版本。公开转私有后新的授权检查使用新规则，但已经发送给浏览器的内容无法收回。恢复备份时先关闭内容入口，核对数据库指针与文件，并处理备份中登录凭证可能重新生效的问题。

#### 存储引用审计

进程可能在完整版本目录完成原子移动后、数据库版本记录提交前异常退出。维护者可以在停止网页托管写入后运行只读审计命令，查找生成目录与数据库引用不一致的候选：

```shell
go build -o output/bin/web-projects-audit ./cmd/web-projects-audit
./output/bin/web-projects-audit -conf /etc/home_server/conf.yaml -min-age 1h
```

命令同时扫描旧 `projects/<project_id>/releases/<release_id>` 和新 `<user_id>/upload/html/<project_id>/releases/<release_id>` 目录元数据，批量核对数据库版本引用、上传者和网页所有者。不读取网页内容，不启动周期清理，也不修改或删除文件。默认忽略最近一小时创建的目录，并跳过非数字目录、异常目录和软链接。数据库查询失败时命令返回失败，不会把数据库当成空库。

JSON 输出包含候选目录、原因和扫描统计。候选可能表示缺少数据库引用、路径不一致或所有者不匹配，不等于可以删除；回收前应停止上传，核对数据库与文件备份，并由维护者按明确目录人工处理。该命令没有自动删除能力，也不提供删除参数。

#### 旧版本迁入用户目录

新服务可直接读取旧目录；只有需要统一已有文件布局时才运行迁移。先备份数据库和文件，并停止所有会访问此存储根的 home_server 实例、上传脚本、定时清理和其他迁移进程；从预览开始到恢复服务前始终保持停止状态。

```shell
go build -o output/bin/web-projects-storage-migrate ./cmd/web-projects-storage-migrate

# 单版本预览：不创建目录，不搬文件，不修改数据库。
./output/bin/web-projects-storage-migrate -conf /etc/home_server/conf.yaml -project-id 101 -release-id 201

# 核对 owner_id、源目录和目标目录后，执行同一版本的迁移。
./output/bin/web-projects-storage-migrate -conf /etc/home_server/conf.yaml -project-id 101 -release-id 201 -apply
```

| 情况 | 处理 |
| --- | --- |
| 预览通过 | 返回 `planned`，源文件和数据库保持不变。 |
| 执行成功 | 返回 `migrated`；只迁移版本目录与 `storage_key`，URL、权限、revision 和当前版本指针不变。 |
| 目标已存在、软链接或所有者不符 | 拒绝执行，不合并目录、不覆盖文件。 |
| 文件已搬动，数据库结果不确定 | 返回失败并保留目标目录及 `<user_id>/upload/html/.migration` 中的私有日志；保持服务停止，用相同参数重跑。 |

迁移日志绑定用户、网页、版本、源目标路径和目录的设备号/inode。重跑只接受匹配的日志和目录，不能手工删除日志或随意改动目标路径。恢复机制面向进程中断，不代替数据库与文件备份；遇到日志损坏、存储卷损坏或身份不一致，需要根据备份人工核对。迁移完成后先运行只读审计，再恢复服务并验证网页访问与版本下载。

## 应用身份与 AK/SK

### 创建和保管

登录后从“应用凭证”进入管理页。每个应用固定归属于创建用户，其他用户无法列表、读取、修改、轮换或吊销它。应用身份只能使用授予的业务权限，不能管理凭证、退出用户账号或建立用户浏览器 Session。

AK 是公开标识，SK 是高熵秘密。两者由 `crypto/rand` 生成；数据库仅保存 SK 和访问 Token 的 SHA-256 摘要，不能读取回原文。创建或轮换时只返回一次 SK；关闭弹窗后无法再次查看，遗失时需要轮换。前端不把 SK 存进 localStorage、URL 或日志。

| 设置 | 行为 |
| --- | --- |
| 默认权限 | 仅 `web-projects:read`，写权限需要显式选择；同一业务的 write 包含 read。 |
| 到期 | 凭证默认 90 天，配置默认上限 365 天。Token 默认 900 秒，可在服务端配置，最大 3600 秒。 |
| 停用 / 启用 | 停用后不能换取或使用 Token；重新启用也不会让旧 Token 复活。 |
| 轮换 | 返回新 SK，旧 SK 和旧 Token 的后续鉴权立即失败。更新客户端凭证后重新换取 Token。 |
| 吊销 | 永久停止该应用，不能恢复；需要时创建新应用。 |
| 并发修改 | PATCH、轮换、吊销必须携带 `If-Match: revision`；旧修订返回 409。 |

每用户默认最多 20 个未吊销应用。数据库唯一 active slot 与事务共同保护并发上限，调低配置时原高位 slot 仍计入总数。`last_issued_at` 表示最近一次成功换取 Token，不是每次业务调用时间；业务鉴权不为此逐请求写数据库。

### 调用与权限边界

通过 HTTPS 向 `POST /api/auth/token` 发送 `application/x-www-form-urlencoded`：`grant_type=client_credentials`，并使用 HTTP Basic `AK:SK`。也支持 form 的 `client_id` / `client_secret`，两种方式不可混传。返回标准 `access_token`、`token_type`、`expires_in`、`scope`，没有 refresh token。服务仅实现所需的 client_credentials 子集，不提供授权码登录、开放客户端注册或每请求 HMAC 签名。

后续使用 `Authorization: Bearer <access_token>` 调用业务接口。SK/Token 不放在 URL；重复、混合或无效的显式认证信息不会降级到其他身份。换发接口不接受 query，访问日志对凭证字段和值脱敏；代理认证接口关闭访问日志，应用审计仅记录用户/应用 ID、动作和结果。

| 接口范围 | 应用权限 | 保持的资源边界 |
| --- | --- | --- |
| `/api/web-share` 及子接口、受保护的 `/p/` | `web-projects:read` / `web-projects:write` | 应用继承所属用户的项目管理或成员读取权限，不能访问其他用户的私有项目。 |
| `/bookinfo/query`、`/library/*` | `library:read` / `library:write` | 沿用现有图书共享语义。 |
| `/webdav/*`、`/webdav_dev/*` | `webdav:read` / `webdav:write` | 沿用现有共享目录；GET/HEAD/OPTIONS/PROPFIND 为读，其他已注册方法为写。 |
| `/api/applications*`、用户退出、browser-login | 不允许应用身份 | 需要用户 `passport` token；浏览器 Cookie 也不能代替它。 |
| 原公开 DDNS、ping 等 | 保持原公开行为 | 不把公开接口当成应用权限隔离的一部分。 |

AK/SK 管理在服务端按用户隔离；应用不是新的共享管理员账号。已有图书和 WebDAV 共享规则没有改成每用户独立存储。同源网页仍仅适合受信任代码：同源脚本并不具备浏览器级隔离，凭证管理不能改变这一既定边界。

长时间运行的 AI/SDK 应在有效期内复用 Token，避免每个业务请求都重新换取。当前换发接口没有专门的速率限制；过期记录按索引有界回收，客户端应处理 401、403、409、429 和 503。

### 管理 API

| 方法与路径 | 用途 |
| --- | --- |
| `GET/POST /api/applications` | 按用户分页列表 / 创建，创建响应包含一次性 `secret_key`。 |
| `GET/PATCH /api/applications/{id}` | 详情 / 名称、描述、权限、期限及启停设置。 |
| `POST /api/applications/{id}/rotate` | 轮换 SK，返回一次性 `secret_key`。 |
| `DELETE /api/applications/{id}` | 永久吊销。 |

创建请求为 `{name, description, scopes, expires_in_days}`；省略有效期使用默认值，显式 0 无效。应用视图中的 ID 使用十进制字符串，状态和 scope 使用可读字符串；列表和详情从不返回 SK、摘要或 Token。

### 配置和迁移

1. 已有旧网页托管表先执行 `domain/dal/migrations/20260912_web_projects_refactor.sql`；首次安装仅执行新的 `20260909_web_projects.sql`。升级保留 HTTP 字符串枚举，但数据库使用 INT；name/slug/幂等键为 256，entry_file 为 2048，诊断信息进入 `extra TEXT` JSON。
2. 启用应用身份前执行 `domain/dal/migrations/20260912_applications.sql`，创建应用和访问 Token 表。数据库账号仅授予目标库权限，先在隔离库验证，不直接导入包含 `USE home_server` 的历史建表文件。
3. 在配置增加下列字段，更新同源 Nginx 片段并设置实际后端端口，再部署后端和前端。

```yaml
auth:
  applications_enabled: true
  site_origin: https://home.example.com
  token_ttl_seconds: 900
  default_credential_ttl_days: 90
  max_credential_ttl_days: 365
  max_applications_per_user: 20
  trusted_proxy_cidrs:
    - 127.0.0.1/32
    - ::1/128
```

浏览器 Origin 未设置且网页托管已启用时，会在路由注册前复用 `web_projects.site_origin`；停用模块的残留 Origin 不会自动启用 Session。只信任真实 TLS 或明确配置的代理地址提供的 `X-Forwarded-Proto: https`，不信任任意客户端伪造该头。

Nginx 引入 `config/nginx/api_auth_locations.conf`，将固定 `/api/auth/*`、`/api/applications*` 及既有受保护业务 API 转给后端。两个模块停用时也应保留代理，让后端明确返回 404，避免 API 落入 SPA fallback。不要启用请求/响应体或认证头日志。

### AI 客户端

仓库提供无第三方依赖的 `script/home_server_api.py`。从管理页下载凭证 JSON 后放到 Git 之外的受控路径，并设置权限为 600。也可以通过 `CQ_HOME_SERVER_ACCESS_KEY`、`CQ_HOME_SERVER_SECRET_KEY` 和 `CQ_HOME_SERVER_BASE_URL` 由受控环境注入；不支持把 SK 写成命令行参数。

```bash
chmod 600 ~/.config/cq-home-server/credentials.json
python3 script/home_server_api.py \
  --base-url https://home.example.com \
  --credentials-file ~/.config/cq-home-server/credentials.json \
  --method GET --path '/api/web-share?limit=20'
```

上传网页版本使用 `--method POST --path /api/web-share/项目ID/releases --upload-file /path/to/site.zip`；ZIP 可加 `--entry-file index.html`。写 JSON 用 `--json-file`，修改时加 `--if-match`；下载二进制用 `--output`。只有单 HTML/ZIP 上传会构造 multipart，它不是通用 WebDAV 上传器。

客户端校验证书和主机名，自签名测试证书使用 `--ca-file`；不提供关闭校验的选项。它固定同一 HTTPS origin，不跟随重定向，不自动重试写请求，不把短期 Token 存盘，终端输出会脱敏。

### 代码结构与公共约定

HTTP 适配位于 `api/`，网页和应用管理用例位于 `app/`，领域规则、校验与文件操作位于 `domain/service/`；`domain/repository/` 负责事务与多表结果组合，`domain/dal/` 只执行显式列、有界单表 SQL。网页回收不再使用 JOIN、EXISTS、COUNT/SUM 聚合，关联和汇总在 Go 中完成。

通用错误码统一位于 `errors/`，新 API 使用 `utils/ginfmt.Success/Fail`。旧接口保留原 HTTP 约定并正确传递包装后的错误码；新模块不再私设 501～510 的重复类别。Cookie 和身份上下文分别由 `utils/session.go`、`utils.Principal` 统一管理，应用 Token 不进入用户 Session。

协议依据：[OAuth 2.0 client_credentials](https://www.rfc-editor.org/rfc/rfc6749#section-4.4)、[Bearer Token](https://www.rfc-editor.org/rfc/rfc6750#section-2.1)、[OWASP 密钥生命周期](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)。

## FAQ

##### Q: 为什么服务没有使用 SSL?

A: 本项目是家庭服务，内网只暴露了 http，外网通过一个虚拟机的 nginx 反向代理实现，nginx 处配置了 SSL。其实服务就可以写的简化些，不需要使用 SSL。

反向代理的配置文件可以参考`config/nginx/home_server_backend.conf`。

## 版权信息 MIT LICENSE

本项目为个人兴趣，目的在于满足个人需求，不提供技术支持，使用本系统造成数据丢失、机器损坏等损失概不负责。

### Cloudflare 集成测试配置

服务端 Cloudflare RPC 集成测试默认跳过外部调用。使用专用测试区域时，通过未提交配置提供凭据，并显式设置 `HOME_SERVER_TEST_CLOUDFLARE_DOMAIN`（DNS 区域的域名，不是 zone ID）。未设置时在任何 RPC 前跳过；测试日志只记录返回数量，不输出 DNS 记录明细。客户端测试与部署说明已迁移至 `life_tools`，不在本仓库维护。
