# 配置、安装与沙箱

## 私有配置

默认读取 `~/.config/cq-home-server/web-share.json`。优先级：`--config` > `CQ_HOME_SERVER_SKILL_CONFIG` > 默认路径。显式使用绝对 `--config` 路径，保证沙箱内外、任意工作目录读取同一份配置。

复制同目录的 [config.example.json](config.example.json) 到私有位置并修改。示例地址为保留域名，不能直接访问：

```bash
mkdir -p "$HOME/.config/cq-home-server"
chmod 700 "$HOME/.config/cq-home-server"
cp /absolute/path/skills/home-server-web-share/references/config.example.json \
  "$HOME/.config/cq-home-server/web-share.json"
chmod 600 "$HOME/.config/cq-home-server/web-share.json"
```

| 配置项 | 说明 |
| --- | --- |
| `internal_url` / `external_url` | 至少一个 HTTPS origin，包含需要的端口，不带 API 路径；两者必须属于同一实例。建议填写同时提供页面和 API 的网关入口 |
| `endpoint` | `auto` 默认先内网后外网；可固定 `internal` 或 `external`，命令行 `--endpoint` 优先 |
| `credentials_file` | 管理页下载的凭证 JSON 路径，内容含 `access_key`、`secret_key`。相对路径以配置文件所在目录为基准 |
| `ca_file` | 可选，可信私有 CA 的 PEM 文件路径。仍检查证书和主机名；公网证书无需填写 |
| `connect_timeout` | `/ping` 探测每次 socket 操作的超时秒数，默认 3；不是整个命令的总超时 |
| `timeout` | API 请求每次 socket 操作的超时秒数，默认 30；大文件上传可适当增加 |

配置和凭证文件必须属于当前用户、是普通文件且权限为 `600`（不接受符号链接）。不能将下载凭证打印到聊天中。直接把管理页下载文件放入配置指向的路径，并执行 `chmod 600`。

不配置 `credentials_file` 时，脚本使用已有的 `CQ_HOME_SERVER_ACCESS_KEY`、`CQ_HOME_SERVER_SECRET_KEY` 环境变量；从受控环境注入，不把真实 SK 写入 shell 命令或历史。凭证文件优先于环境变量。为不同用户准备独立配置与凭证文件，不能共享其他用户的 AK/SK。

默认配置放在 Git 之外。如果工具只能读取项目目录，可在**授权可读的范围内**使用 `skills/home-server-web-share/local/` 存放配置，目录已加入 `.gitignore`；不要擅自把沙箱不可读的凭证复制进去。提交前用 `git status --short` 和 `git diff --cached` 确认真正提交的内容不含配置、凭证及测试产物。

## 沙箱内外使用

```bash
python3 /absolute/path/skills/home-server-web-share/scripts/manage.py \
  --config /absolute/path/private/web-share.json doctor
```

同一条命令可在普通终端和 AI 工具沙箱运行。Python 脚本自行读取配置、验证 TLS、换取短期 Token；不依赖 GUI、钥匙串解锁、shell 初始化、第三方依赖安装或环境代理。网络连接直达配置的 HTTPS 入口，不读取 `HTTP_PROXY` / `HTTPS_PROXY`。

`doctor` 不携带 AK/SK，只有 `/ping` 返回 `200 {"message":"pong"}` 才认为入口可用。`auto` 探测完选定入口后，单次命令的认证和业务请求固定在该入口；不会跟随 30x，也不会在请求失败后自动重放写操作。探测遇到明确的权限拒绝立即停止，不再尝试另一个入口。跨多条命令的部署流程应传入选定的 `--endpoint`。

| 执行环境 | 行为 |
| --- | --- |
| 沙箱允许文件读取和目标网络 | 直接执行；内网可达时优先走内网 |
| 内网 DNS / 路由不可达，外网可达 | `auto` 可在发送凭据前选择外网；路由器缺少 NAT 回环时通常应走内网域名 |
| 沙箱禁止目标网络 / 配置读取 | 脚本失败退出，不能自行解除沙箱限制。先判明工具是否报告权限拒绝；普通 DNS 或路由错误不等于沙箱拒绝 |
| 平台支持审批后在沙箱外执行 | 使用平台提供的权限参数申请执行原命令，例如 Codex `exec_command` 的 `sandbox_permissions="require_escalated"`，说明具体目标和操作；不是脚本里的 sudo 或自动重试 |
| 平台未提供提权机制或审批拒绝 | 保留 `--dry-run` 摘要，由用户在普通终端执行，或由用户调整授权。不得通过其他网络工具、代理或后台进程绕过拒绝 |

若业务写请求已经发出，先读取项目及版本核对结果，不能仅因为后来获准在沙箱外运行就重放写请求。`doctor` 仅说明 DNS / TCP / TLS / 探活可用；鉴权是否成功用 `list` 验证，发布结果用 `show` 读回。

## 让 AI 发现 skill

在仓库中可直接要求 AI 读取 `skills/home-server-web-share/SKILL.md`。需要 Codex 自动发现时，可由用户安装一个指向稳定仓库目录的符号链接：

```bash
mkdir -p "$HOME/.codex/skills"
ln -s /absolute/path/home_server/skills/home-server-web-share \
  "$HOME/.codex/skills/home-server-web-share"
```

随后重新加载技能并使用 `$home-server-web-share`。不要覆盖已有同名安装。脚本会解析符号链接得到实际仓库路径，因此不依赖运行命令时的当前目录。此 skill 复用仓库中的 API 客户端，安装时保留整个仓库，不能只复制 skill 子目录；不要让长期安装指向之后会删除的临时 worktree。
