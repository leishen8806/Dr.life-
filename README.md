# Dr.life+

Telegram Mini App 电商平台，首版聚焦商家仓储与配送，并由平台记录订单与结算信息。

## 文档索引

| 文档 | 用途 |
|---|---|
| [本 README](README.md) | 项目入口、目录说明和文档索引 |
| [AGENTS.md](AGENTS.md) | 开发助手须知、不可违反的业务与安全规则 |
| [产品需求文档 PRD](<Dr.life+产品需求文档PRD.md>) | 产品范围、角色与需求基线 |
| [第一版交付形态与技术架构](<Dr.life+第一版交付形态与技术架构.md>) | 首版交付形态、技术选型、域名与部署架构 |
| [技术方案与系统边界](<Dr.life+技术方案与系统边界.md>) | 模块边界、状态机、安全与 PoC |
| [数据库表结构与账务详细设计](<Dr.life+数据库表结构与账务详细设计.md>) | 数据模型、账务规则、迁移与测试要求 |
| [开发任务拆分与验收](<Dr.life+开发任务拆分与验收.md>) | 任务依赖、交付物及验收标准 |
| [平台后台权限与审计规范](<Dr.life+平台后台权限与审计规范.md>) | 后台角色权限矩阵与审计要求 |
| [交互文档](<Dr.life+交互文档.md>) | 各端页面、流程与交互说明 |
| [产品说明：五类角色与数据流](<Dr.life+产品说明-五类角色与数据流.md>) | 用户角色、业务流程及数据流说明 |
| [第一批商户与定价试探方案](<Dr.life+第一批商户与定价试探方案.md>) | 商户访谈、首批客户与定价验证计划 |
| [消息通知、渠道关联、埋点与遗漏清单](<Dr.life+消息通知、渠道关联、埋点与遗漏清单.md>) | 通知机制、渠道关系、埋点与待办 |
| [用户协议与隐私政策草案（三语）](<Dr.life+用户协议与隐私政策草案(三语).md>) | 中英高棉语协议与隐私政策草案 |
| [Bot 资料与高棉语欢迎语](<Dr.life+Bot资料与高棉语欢迎语.md>) | Bot 配置资料及高棉语文案草案 |
| [未来 APP 规划](<Dr.life+未来APP规划文档.md>) | 后续原生/独立 App 方向与边界 |
| [柬埔寨落地方案](<Dr.life+柬埔寨落地方案.md>) | 柬埔寨市场验证、实施路径与业务边界 |
| [Shopee 官方合作仓申请方案](<Dr.life+Shopee官方合作仓申请方案.md>) | 申请成为 Shopee 柬埔寨官方合作仓的前置确认、差距、材料与待办卡片 |
| Codex 任务提示词 | 只保存在本机（`E:\Dr.life+\`），不放进仓库（2026-10-28 起） |
| [开发总路线图与任务状态](<Dr.life+开发总路线图与任务状态.md>) | 全部开发任务的状态、依赖、阻塞与放行顺序，以及提示词文件索引 |
| [API 安全协议、容量保护与队列设计](<Dr.life+API安全协议与容量及队列设计.md>) | 各端与服务端的接口安全协议、过载保护与降级、asynq 队列逻辑（S-01 至 S-06 的规范来源） |
| [backend/](backend/README.md) | Go 后端目录用途与设计依据 |
| [backend/cmd/](backend/cmd/README.md) | Go 可执行程序入口 |
| [backend/internal/](backend/internal/README.md) | 后端内部模块与平台组件 |
| [backend/migrations/](backend/migrations/README.md) | 数据库迁移文件及规则 |
| [backend/api/](backend/api/README.md) | OpenAPI 接口规范 |
| [apps/buyer/](apps/buyer/README.md) | 买家 Telegram Mini App |
| [apps/merchant/](apps/merchant/README.md) | 商家 Telegram Mini App |
| [apps/driver/](apps/driver/README.md) | 司机 Telegram Mini App |
| [apps/admin/](apps/admin/README.md) | 平台管理后台 |
| [apps/wms/](apps/wms/README.md) | 仓储与物流 Web 后台 |
| [packages/ui/](packages/ui/README.md) | 前端共享 UI 组件 |
| [packages/api-client/](packages/api-client/README.md) | OpenAPI 生成的共享接口客户端 |
| [packages/i18n/](packages/i18n/README.md) | 中文、英文与高棉语共享词条 |
| [site/](site/README.md) | settlecore.org 静态官网 |
| [poc/](poc/README.md) | 不进入生产目录的技术验证 |
| [CONTRIBUTING.md](CONTRIBUTING.md) | 分支、提交、评审、迁移与合并规则 |
| [PR 模板](.github/pull_request_template.md) | PR 信息、验收结果与安全检查清单 |

## 从哪里开始读

先读 [AGENTS.md](AGENTS.md)，再按任务提示阅读对应的设计文档章节。技术任务从 [开发任务拆分与验收](<Dr.life+开发任务拆分与验收.md>) 领取，并按任务分支提交 PR。

## 目录结构

```text
backend/       Go 模块化单体后端
apps/          买家、商家、司机、平台与仓储前端
packages/      共享 UI、接口客户端和多语言词条
site/          Astro 静态官网
poc/           技术验证代码
prototype/     可点击交互原型
```

`historical/` 保存历史资料，不纳入 Git 版本库。
