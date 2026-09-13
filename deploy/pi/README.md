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

## 安装顺序

1. 核对并备份既有数据库、配置、服务单元和二进制；备份含凭据，只保存在目标服务器的私有目录。
2. 核对表结构与索引，再执行缺失的增量 DDL。首次启用运行 `20260909_web_projects.sql` 与 `20260912_applications.sql`；已有 token 索引时跳过重复创建。已有旧网页表时按其真实结构选择升级脚本，不混用首次建表与旧表升级。
3. 从指定 master revision 构建 release，记录源码 revision、构建参数及产物 SHA-256；不要覆盖正在执行的二进制文件内容。
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

保留每次部署的旧 systemd 单元、旧后端配置、旧二进制或 release 指针，以及网关站点备份。回滚时恢复匹配的旧配置与程序，校验配置后重启对应服务；已有入口应恢复到备份记录的位置。

本次新增表和索引是兼容性增量。回滚程序时不要 DROP 新表、清空数据库或覆盖新产生的数据；数据库恢复必须单独核对业务写入时间与影响范围。前端和后端应使用匹配的 release，避免新页面请求旧接口。
