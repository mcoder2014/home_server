# 文件分享实施计划

目标：在现有账号及存储体系中提供独立文件分享、密码阅读与统一页面体验。

| 工作包 | 文件范围 | 完成标准 |
|---|---|---|
| 后端文件模块 | api/fileshare、app/fileshare、domain/model、domain/dal、config、API初始化 | 上传/列表/多分享/权限/口令/到期/原子次数/撤销/下载闭环，未授权资源不泄露 |
| 阅读密码 | resource_passwords及小型密码服务，api/webprojects、api/manuals、app/manuals | 原ACL不变，全部内容和评论入口保护，列表无摘要泄漏，旧Cookie随版本失效 |
| 前端 | front_vue/src/api、views、router、components、assets | 文件管理和分享落地页；网页/说明书设密解锁；桌面320px均可用；现有功能回归 |
| 集成与部署 | migrations、Nginx、CI、deploy/pi文档 | 精确增量DDL，代理无静态绕过，远端构建和隔离测试通过 |
| 独立审查 | 精确分支diff和PR | GPT-6方案review；Sol max独立code review；修复后重新核对 |
| 文档与发布 | docs/specs/file-sharing | 需求/方案HTML owner模式，PR固定SHA，Pi完整release与回滚路径 |

## 验证顺序

1. 现有前端、浏览器、Python测试作为基线；远端Pi隔离MariaDB/Redis复用历史测试环境，核对不连接生产DB。
2. 后端完成后增加新模块到隔离测试清单；先定向权限、口令、次数并发与资源保护用例，再执行既有CI回归和全仓构建。
3. 前端运行 `npm run test:frontend`、相关浏览器用例与 `VUE_APP_API_BASE_URL=/ npm run build`；实际截图检查桌面和窄屏。
4. 只读独立审查要求/行为与diff，再修复所有P1/P2；提交并推送PR，由Sol max审查PR提交。
5. 对PR SHA生成完整release摘要，生产变更前保存配置/数据库备份；验证SQL执行计划与Nginx后切换，检查旧入口和新功能。

Go测试禁止本地执行。Node依赖优先复用现有安装。不得把生产凭据或数据库复制到开发目录。
