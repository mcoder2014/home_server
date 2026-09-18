# Home Server 前端

Vue 3 与 Element Plus 页面。浏览器使用同源 HttpOnly Cookie 登录，身份和 CSRF 信息由 `/api/auth/me` 返回；页面刷新后重新读取服务端身份。账号管理与受邀注册需要后端已切换到数据库账号模式。

## 页面与操作

| 入口 | 用户可执行的操作 | 接口范围 |
| --- | --- | --- |
| `/login`、`/register` | 密码登录、校验邀请码、受邀注册 | `/api/auth/login`、`registration-policy`、`invitations/validate`、`register` |
| `/account` | 更新昵称、联系邮箱和电话；裁剪上传／移除头像；查看账号与功能权限 | `GET /api/auth/me`、`PATCH /api/account/profile`、`POST/DELETE /api/account/avatar` |
| `/account/security` | 验证当前密码后改密，退出全部会话；初始密码账号必须先改密 | `/api/auth/change-password`、`logout-all` |
| `/account/sessions` | 查看当前及其他有效登录；退出选中或其他全部登录 | `GET /api/account/sessions`、`sessions/revoke`、`sessions/revoke-others` |
| `/invitations` | 查看本月剩余额度、生成单次邀请码、复制一次性邀请码或链接、撤销未用邀请 | `/api/account/invitations`、`/:id/revoke` |
| `/admin/users` | 搜索用户、有效会话统计与详情；创建、封禁、恢复、删除、重置密码、昵称／头像、退出全部会话；管理员、藏书和 WebDAV 授权 | `/api/admin/users`、`/:id`、`/:id/sessions`、`/:id/reset-profile` 及原管理动作 |
| `/admin/web-share` | 按所有者、名称、发布状态、可见范围和审核状态筛选全站网页；版本预览、下架、删除、恢复、解除审核锁 | `/api/admin/web-share`、`/:id`、`/:id/:action`、管理员版本预览路径 |
| `/admin/config` | 根据 schema 生成分组表单；校验、查看差异、发布、查看历史、回滚、检查运行生效状态 | `/api/admin/config/schema`、`config`、`status`、`/:namespace`、`validate`、`history`、`rollback` |
| `/admin/audit-logs` | 按目标类型与数字 ID 查询操作记录及脱敏变化 | `/api/admin/audit-logs` |

原 `/applications`、`/web-share`、`/web-projects` 兼容入口、图书与扫码页面保留。网页托管与应用管理请求也使用 Cookie 和 CSRF；应用 scope 选择器包含网页托管、网页评论、藏书和 WebDAV 权限，并按当前用户能力限制可授予范围。网页编辑页可在增强容器与原始页面之间切换；新项目默认增强，旧项目缺少字段时按原始页面显示。应用有效期表单读取 `/me.application_policy` 的默认值及上限，随当前站点策略变化。应用统计区分有效与未吊销数量，网页统计区分发布与未删除数量。

## 账号与权限边界

- 登录信息只保存在页面内存，启动时移除旧 `localStorage` Token 和用户名。浏览器不写入密码、CSRF、登录 Token、邀请码或 Secret Key 到持久存储。
- 受保护的页面导航重新请求 `/me`；管理员角色、临时密码状态和能力开关由服务端响应决定。后端仍是全部权限检查的最终边界。旧会话的迟到响应不能覆盖新登录身份。
- 同源请求发送 Cookie，写操作携带 `X-CSRF-Token`。资料、账号、网页与配置修改同时发送 `If-Match`；版本冲突保留个人资料或配置草稿，重新查看最新值后再决定如何提交。
- 自助改密成功后退出登录，管理员重置或创建的初始密码只在成功面板展示一次。账号删除需要输入目标用户名；高风险管理操作需要原因和操作者当前密码。
- 邀请链接采用 `/register#invite=...`；读取后清除地址片段，校验和注册通过请求体传递完整邀请码。生成请求使用稳定 `request_id` 处理结果不明确时的重试。

个人联系方式不自动变成登录别名或找回密码渠道。管理员身份不自动开通家庭藏书或 WebDAV。WebDAV Basic 客户端独立于网页会话和首次改密流程，获准账号可直接使用未过期密码；网页登录失败限速不连带影响 Basic。HTML 预览继续同源执行，界面明确提示这一已接受的信任边界；预览不声称提供脚本沙箱。

昵称最多 64 个 Unicode 字符，留空显示用户名。头像仅支持 JPEG／PNG、2 MiB、4096×4096 以内且总像素不超过 1600 万；客户端处理方向并裁剪，服务端独立检查并输出 256×256 JPEG。冲突时保留昵称与裁剪草稿；跨标签页仅广播刷新事件，不广播凭据。资料、成员、邀请、所有者与评论统一显示当前身份；管理记录保留稳定用户名／ID，普通评论不会回退历史违规昵称。

有效会话按服务端认证条件统计，受限改密会话单列；剩余时间根据服务器响应计算，所有时间显示 UTC+8。登录 IP 与 UA 是登录时的描述，不能作为设备指纹。选择退出保护当前会话并覆盖所选记录；退出其他全部涵盖未加载页。管理员数量达到 10 仅提示核查，不自动封禁。配置用户模式保留既有登录与评论；数据库专属管理接口拒绝调用。

“站点设置 → 账号策略 → 每个账号同时有效的网站登录上限”默认 5，允许 1–100 个网站会话，普通与受限改密会话均占名额。达到上限保留已有登录并拒绝新登录；调低上限不会自动退出旧会话。登录管理显示有效数量和网站上限。应用凭证、Basic 和同一 Token 的 Cookie 兑换不占名额。

## 动态配置表单

配置字段由服务端 schema 定义。布尔值使用开关、整数使用带范围的输入框、文本使用普通文本表单；固定邀请规则只读且不进入发布请求。发布前显示字段差异和生效影响，回滚创建新版本。数据库中已保存的版本与运行中已加载的版本分别展示，页面每 15 秒刷新运行状态。

关闭注册会使旧的未使用邀请码永久失效，重新开启或版本回滚不复活旧码。配置页的发布和回滚使用 `request_id`、`If-Match`、原因及管理员当前密码。密码不进入配置值或历史。

## 网页访问统计

所有者打开 `/web-share/:id` 后，可在编辑页底部查看访问统计。面板只在所有者详情接口读取成功后显示，再通过 `GET /api/web-share/:id/stats?days=30` 获取统计；服务端仍须复核所有者权限，公开网页和普通成员不因此获得统计权限。

| 展示 | 口径与状态 |
| --- | --- |
| 累计 PV / UV | 启用统计以来的长期累计；UV 使用服务端全历史去重值，不相加每日 UV |
| 今日 PV / UV | 按 `Asia/Shanghai` 自然日；没有可信数据时显示 `—` |
| 7 / 30 / 90 日趋势 | 展示每日 PV、近似 UV 与区间 PV；只连接 `ok` 日期，未知和采集不完整日期保留缺口 |
| 每日明细 | 显示日期、已知计数、质量状态及服务端原因；未知日期不会补成可信的 0 |
| 统计开始 / 最近持久化 | 展示上海时间；正常约延迟 60 秒，停止采集不清除已保存的历史累计 |

UV 是浏览器标识的近似去重，不等于真实人数。清除 Cookie、切换浏览器或设备会产生新访客；内外网不同 hostname 不共享标识，同一个浏览器可能计为两个访客。首版不展示任意区间去重 UV。

统计加载失败只影响统计面板，使用“刷新”重试。切换日期、项目或离开页面后，旧统计请求不会覆盖当前结果。面板没有新增图表依赖，使用原生 SVG 绘制。

## 本地开发与验证

```sh
npm ci
npm run test:frontend
npm run serve
npm run build
```

`npm run lint` 使用 Vue CLI 的既有 lint 命令。只读检查可以使用 `./node_modules/.bin/eslint --no-fix <files>`，避免自动修改与本次需求无关的旧页面。

API 默认固定同源 `/`，需要由开发代理或 Nginx 将 `/api/`、`/library/`、旧 `/passport/` 和网页内容入口转发给后端。Cookie 登录依赖 HTTPS 与服务端明确允许的 Origin；不要通过本地持久化 Token 回退到旧浏览器登录流程。服务器地址与私有信息不要写进公开源码。

### Pi 独立构建验证

只在独立临时目录执行，源码目录不应是生产部署目录。先同步前端源码与 `config/nginx/web_projects_locations.conf` 测试夹具，再执行：

```sh
ssh pi 'source ~/.zshrc && cd /tmp/home-server-accounts-config-frontend-src/front_vue && npm run test:frontend && npm run build && test -s dist/index.html'
```

现有 lockfile 包含内网镜像地址。外部网络不可达时，可在**临时构建副本**中将 `resolved` 下载地址改为公开 npm 镜像，并核对版本和 `integrity` 全部保持不变；源码 lockfile 不作镜像批量替换。不关闭 TLS 校验，不替换已锁定的包版本。

前端测试覆盖账号权限与站点开关、密码及资料边界、Cookie/CSRF/版本请求、资料和配置冲突保留、一次性凭证清理、邀请码校验竞态、登录/退出的迟到响应以及旧网页管理导航。浏览器交互测试可以使用合成账号和独立后端，不应用真实账号进行封禁或密码重置验证。

### 统计组件本地预览

以下命令生成并托管真实统计组件的模拟页面，不连接后端。浏览器打开 `http://127.0.0.1:18764` 后，可选择正常、未开始、关闭、不完整、未知缺口和权限拒绝场景，检查日期筛选与每日明细。

```sh
node test/web-projects-stats-preview.cjs /private/tmp/home-server-stats-preview
python3 -m http.server 18764 --bind 127.0.0.1 --directory /private/tmp/home-server-stats-preview
```

统计测试包含上海跨日、累计 UV 不相加、缺口断线、停用及未开始状态、请求乱序、项目切换和权限拒绝。后端真实 Cookie、所有者鉴权、统计采集与落库仍需独立集成验证。
