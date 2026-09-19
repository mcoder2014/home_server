# 前后端部署示例

这里使用文档专用地址 `192.0.2.10`、`192.0.2.20` 和保留域名 `home.example.com`，不对应实际部署。服务账号 `home-server` 也只是示例，需由部署者创建或替换。

实际 IP、域名、系统账号、证书路径和数据库配置应保存在服务器或被 Git 忽略的 `deploy/local/`，不要回填到公开模板。复制示例并替换所有环境值后，再执行配置校验与部署。

| 组件 | 路径与职责 |
| --- | --- |
| 后端 release | `/opt/home_server/releases/<revision>/bin/home_server`；`current` 符号链接选择当前版本。 |
| 前端产物 | 同一 release 的 `frontend/`，用 `VUE_APP_API_BASE_URL=/ npm run build` 构建。 |
| 后端配置 | `/etc/home_server/server.yaml`，由未提交的私有配置生成，保留既有用户、DB、RPC 与 WebDAV 配置，权限 0600。 |
| 前端服务 | `home_server_frontend.service` 运行独立 Nginx，HTTP `192.0.2.10:18081`；不加载系统 Nginx 或测试站点。 |
| 统一控制 | `home_server.target` 管理前端与后端；不包含 DDNS 的 home_client。 |
| 网关 | `gateway-frontend.conf` 示例提供 8080 站点，转发到 18081；`gateway-api.conf` 保留原 HTTPS 18080 API。 |

后端沿用程序默认的 IPv4/IPv6 双栈监听，保持既有接入方式。

前端同源代理需要把 `config/nginx/web_projects_locations.conf` 和 `api_auth_locations.conf` 安装到 `/etc/home_server/locations/`，将其中 `X-Forwarded-Proto $scheme` 替换为 `$cq_forwarded_proto`，将 `X-Forwarded-For $remote_addr` 替换为 `$cq_client_ip`。图书/WebDAV 的 location 正则应覆盖资源根：`^/(library|bookinfo|webdav|webdav_dev)(/|$)`。

只有来自示例网关 `192.0.2.20` 的请求可以向前端断言 HTTPS。后端 `auth.trusted_proxy_cidrs` 使用 `127.0.0.1/32`、`::1/128`、`192.0.2.20/32`；`auth.site_origin` 与 `web_projects.site_origin` 均为 `https://home.example.com:8080`。前端 18081 提供内网 HTTP 入口，应用凭证和浏览器安全 Cookie 功能需要使用 TLS 网关入口。

新网页存储根使用 `/var/lib/home_server/web-share`，归服务用户所有、权限 0700，并保持在 WebDAV 共享树之外。

## 账号接口的同源代理

账号版本上线时，同时更新 `/etc/home_server/locations/api_auth_locations.conf`，让站点引导、个人账号和管理中心请求进入后端。`frontend-nginx.conf` 保留未知 `/api/` 的 404 兜底；新接口由更具体的 `^~` 前缀匹配，管理员预览中的 HTML、JS、CSS 也经过后端鉴权。

| 请求范围 | Pi 前端代理请求体上限 | 用途 |
| --- | --- | --- |
| `/api/site/` | 16 KiB | 站点公开引导信息。 |
| `/api/account/` | 16 KiB | 本人资料和邀请码管理。 |
| `/api/admin/` | 64 KiB | 用户、网页审核、审计和通用配置；配置发布的整个 JSON 请求体上限与后端一致。 |
| `/api/auth/`、`/api/applications` 及其子路径 | 保留 16 KiB | 登录、注册、会话和应用凭证。 |
| 网页上传、藏书和 WebDAV | 保留各自原有限制 | 继续使用独立 location，网页版本上传和 WebDAV 流式请求不受账号接口的小请求体上限影响。 |

两份网关模板为 `/api/(site|account|admin)/` 增加 64 KiB 的受控代理，分别沿用 `8080 → 18081`、`18080 → 18080` 的目的端口。该规则不匹配 `/api/accounting/`、`/api/administrator/` 等相似名称。若现场配置还有静态扩展名的正则 location，应保持账号接口匹配优先，避免管理员预览资源进入静态文件分支。

代理保留 `$http_host` 中的域名和端口。TLS 网关覆盖 `X-Forwarded-Proto` 和 `X-Forwarded-For`；Pi 前端只采信受信任网关，并在安装的 location 副本中使用 `$cq_forwarded_proto`、`$cq_client_ip`。新增账号 location 关闭访问日志、代理缓存和错误页拦截，并发送 `Cache-Control: no-store`，保证鉴权错误及 JSON 响应原样送回客户端。Authorization 沿用现有转发方式，WebDAV 的独立 Basic 协议保留。

保留现有的两个域名及 `auth.site_origin` / `auth.site_origins`。Origin 必须包含实际 HTTPS 端口；同一域名的 8080 和 18080 是不同 Origin。模板继续保留两个 `server_name`，Cookie 仍按各自主机保存。

仓库静态回归命令为 `node --test front_vue/test/web-projects-nginx.test.cjs`，覆盖新增路径、请求体上限、转发头、禁缓存、双入口和 WebDAV 规则。安装现场仍须对实际配置执行独立前端 Nginx 的 `-t -c /etc/home_server/frontend-nginx.conf` 及网关的 `nginx -t`，再验证两个入口下的 `/api/site/bootstrap`、已登录个人中心、管理员配置请求，以及原有 WebDAV Basic 访问。

## 说明书模块

说明书的部署配置独立于数据库中的动态 namespace，缺省关闭。启用前先执行精确的 `domain/dal/migrations/20260919_manuals.sql` 增量建表，检查配置与数据备份，然后在服务配置中设置 `manuals.enabled` 和 `manuals.storage_root`。存储目录建议 `/var/lib/home_server/manuals`，权限 0700、归服务用户所有，不能位于 WebDAV、网页托管或前端静态目录中。

安装 `config/nginx/manuals_locations.conf` 到 `/etc/home_server/locations/`，在 Pi 副本中将 `$scheme` / `$remote_addr` 转为现有的 `$cq_forwarded_proto` / `$cq_client_ip`，并让独立前端 Nginx include 该文件。API 根路径限制 512 KiB，子路径允许 52 MiB 以容纳 50 MiB 文件和 multipart 元数据；后端仍按请求类型独立限制大小。两份网关模板也增加了 `/api/manuals` 精确匹配与 `/api/manuals/` 优先前缀，保留原有目标端口及 TLS 头。

| 检查 | 要求 |
| --- | --- |
| PDF 预览 | 核对 `pdftoppm` 与外部进程资源限制工具可用，使用服务账号验证首页缩略图；缺工具或转换失败不得让 PDF 上传状态冒充预览成功 |
| 私有存储 | 目录可写、容量及保留空间满足限制；禁止为原件或缩略图增加静态 alias |
| 数据与搜索 | 自动封面查询使用窗口函数，已核对环境为 MariaDB 10.11；其他数据库版本需先核对窗口函数兼容性；新表精确增量创建；在隔离数据中检查列表、分类及名字匹配 SQL 的 EXPLAIN |
| 身份 | 应用凭证显式加入 `manuals:read/write`；保持原应用其他 scopes，不提取用户会话替代应用授权 |
| 升级 | 构建匹配的前后端 release、记录源码 SHA 与产物哈希，Nginx 校验通过后切 current 并重启 |
| 回滚 | 切回完整旧 release 和旧配置；保留新增表及全部说明书文件，不在程序回滚时 DROP 或覆盖数据 |

部署后除 `/ping` 外，还需验证公开目录、私有说明书未登录拒绝、原件/缩略图鉴权、PDF Range、图片多选和名称＋分类过滤，以及原网页托管、登录、藏书与 WebDAV 入口。真实手机相册的交互应记录实际验收设备，不能以桌面文件选择测试冒充。

## 安装顺序

1. 核对并备份既有数据库、配置、服务单元和二进制；备份含凭据，只保存在目标服务器的私有目录。
2. 核对表结构与索引，再执行缺失的增量 DDL。首次启用运行 `20260909_web_projects.sql` 与 `20260912_applications.sql`；已有 token 索引时跳过重复创建。已有旧网页表时按其真实结构选择升级脚本，不混用首次建表与旧表升级。
3. 从通过验证的指定 revision 构建 release（未合并功能使用明确的 PR 提交 SHA），记录源码 revision、构建参数及产物 SHA-256；不要覆盖正在执行的二进制文件内容。
4. 安装后端配置、前端配置与三个 systemd 文件，运行 `systemd-analyze verify` 和独立 Nginx 配置检查，再切换 `current` 链接。
5. `systemctl daemon-reload`，启用前后端与 target，再启动或重启。通过 `.10:18081` 检查 SPA、同源 API 和静态资源。
6. 备份网关原站点文件，将两个网关模板安装到对应站点的实际目标文件；`nginx -t` 成功后 reload。通过域名 SNI 验证 TLS、登录入口、API 和原始路径防护。

## 运行控制

```sh
# 一起启动、停止或重启
sudo systemctl start home_server.target
sudo systemctl stop home_server.target
sudo systemctl restart home_server.target

# 单独控制前端或后端
sudo systemctl restart home_server_frontend.service
sudo systemctl restart home_server.service
sudo systemctl status home_server.service home_server_frontend.service

# 开机自启动；不需要为此重启整台机器
sudo systemctl enable home_server.target home_server.service home_server_frontend.service
```

后端现有信号处理函数以状态 1 退出。模板使用 `SuccessExitStatus=1` 和 `Restart=always`：显式停止不会重新拉起，意外退出仍会重启；不会因为将 1 视为正常退出而丢失进程恢复能力。

## 回滚

保留每次部署的旧 systemd 单元、旧后端配置、旧二进制或 release 指针，以及网关站点备份。满足下文密码兼容性前提后，恢复匹配的旧配置与程序，校验配置后重启对应服务；已有入口应恢复到备份记录的位置。

本次新增表和索引是兼容性增量。回滚程序时不要 DROP 新表、清空数据库或覆盖新产生的数据；数据库恢复必须单独核对业务写入时间与影响范围。前端和后端应使用匹配的 release，避免新页面请求旧接口。

## 多个受信任域名

网关 `server_name` 可以列出主域名和内部域名，例如 `home.example.com home.internal.example.com`。证书必须包含所有域名，DNS 地址在私有配置中维护。后端保留 `auth.site_origin` 作为原入口，通过 `auth.site_origins` 添加完整的额外 HTTPS Origin；不要添加通配域或设置共享 Cookie Domain。两个域名使用相同账号和数据库，但浏览器登录状态分别保存。

## 文件分享与密码阅读

文件模块使用独立的 `file_sharing` 部署配置，缺省关闭。旧安装必须先完成 `20260912_applications.sql` 及既有账号迁移，确认 `login_token`、`application` 两表存在；config 身份模式的容量写入锁依赖这些凭据表。上线前执行 `20260919_file_sharing.sql` 和 `20260919_resource_passwords.sql` 两份增量迁移；前者创建文件、分享、成员表，后者为既有网页和说明书提供密码版本记录。密码表迁移必须先于新后端切换，不能在表缺失时以“无密码”降级。

配置 `file_sharing.enabled: true` 与私有 `storage_root`，建议 `/var/lib/home_server/files`，归服务账号所有、权限0700。该路径不得与 WebDAV、网页托管、说明书或前端静态根重叠。默认单文件50MiB、每用户1000个文件和10GiB总量、保留2GiB磁盘空间；并发上传上限为每用户2个、全局4个。文件统一通过后端附件响应，不安装静态alias。

安装 `config/nginx/file_sharing_locations.conf` 到 `/etc/home_server/locations/`，在Pi副本中沿用 `$cq_forwarded_proto` 和 `$cq_client_ip`，并由独立前端Nginx include。TLS网关的两个入口必须同步提供文件API，保留既有原始Host与端口及可信代理，并禁用缓存。仅 `/api/file-shares/` 与 `/s/` 这两个包含分享token的路径关闭访问日志和错误日志，防止upstream错误记录完整URI；其他路径保留错误日志用于诊断。API入口访问 `/s/:token` 时跳转到同一主机的前端8080端口，确保SPA资源和登录回调保持在前端入口；部署使用自定义前端端口时必须同步修改该跳转。实际配置分别执行 `nginx -t` 后生效。

| 验收路径 | 预期 |
|---|---|
| `/files`、`/s/:token` | 前端管理和收件页；登录回跳不丢分享目标 |
| `/api/files` | 未登录不能管理，已登录仅本人文件；上传受应用与代理双重大小限制 |
| `/api/file-shares/:token/download` | 仅POST；身份、密码、时间、次数全部通过才发送；HEAD与Range不消耗次数 |
| 网页与说明书密码 | 旧ACL继续有效；错误密码不注销登录；修改或清除密码使旧授权失效 |
| 内容保护 | 网页子资源与评论、说明书正文/封面/原件/缩略图均不得绕过门禁 |

下载计数表示成功授权并开始传输的次数，客户端断线不会恢复额度。程序回滚时保留四张新增表及私有文件，先核对下一段的密码兼容性条件；不得把数据库快照覆盖到已经产生新业务数据的库。

回滚密码阅读后端前必须检查 `resource_passwords`：旧版本不识别新密码。如果已有非空密码，优先仅回滚前端；回滚后端前先停止相关内容对外访问，不能让密码资源退化为仅原ACL保护。自动回滚只有在确认尚无密码记录时才允许切回旧后端；无法核实则停止服务并保留新release与备份，等待恢复。
