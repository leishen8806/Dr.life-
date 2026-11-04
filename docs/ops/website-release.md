# 官网发布与回滚（Cloudflare Pages）

适用站点：`apps/website`（Astro 静态产物），生产域名 `settlecore.org`。本流程只使用 Cloudflare Pages；不使用托管机柜或应用服务器。

## 流水线与权限

`website.yml` 在官网文件的 PR 与合并到 `main` 时运行。构建作业使用 Node 22，依次执行锁定依赖安装、lint、类型检查、既有测试和静态构建，并保留 `website-dist` artifact 14 天。

PR 只构建和上传 artifact，绝不发布。合并后的生产部署作业同时满足以下条件才会创建：`main` push、两个 Cloudflare secrets 和 Pages 项目变量均已配置。该作业声明 GitHub Environment `website-production`；必须由该 Environment 的保护规则人工批准，批准前不会执行下载或 `wrangler pages deploy`。

## 发布前检查清单

- [ ] GitHub Actions 的 `Website / Build website` 成功；下载并检查本次 `website-dist` artifact。
- [ ] 发布口径与项目负责人 2026-11-04 的决定一致：官网以**草案**上线，保留方括号占位与「草案，待律师审核」横幅，原样发布；发布过程中不得替换占位，也不得填入任何未经项目负责人或律师确认的值。三种语言页面可用。
- [ ] 律师审核后的**正式版**不适用上一条：发布前须先完成 CMP-1078（权威输入与律师确认）、CMP-1079（高棉语母语审校）、CMP-1080（三语字典回填），并按公司章程完成内容审批与董事会确认。
- [ ] 审核 `_headers` 与现行页面资源一致。字体、图标均从本站静态产物加载；CSP 不得放行 `fonts.googleapis.com` 或 `fonts.gstatic.com`。
- [ ] 已在 GitHub Environment `website-production` 设置 required reviewers，并以设置页截图或文字记录核对其生效；未配置时该 Environment 会直接放行部署，尚不得配置或启用生产 secrets。
- [ ] 已在 Cloudflare Pages 中确认 `settlecore.org` 为生产自定义域名。
- [ ] 已确认上一成功 Pages deployment 可供回滚，并记录其部署时间/标识于本次发布记录（不在仓库保存账号或令牌）。
- [ ] 在 GitHub Environment 审批页面确认本次合并、artifact 和批准人，然后才批准 `website-production`。

## 生产发布

1. 将已审查的 PR 合并到 `main`。不要手动运行命令发布。
2. 在 GitHub Actions 核对 build 成功后，按 Environment 保护规则人工批准 `website-production`。
3. 作业完成后，访问 `https://settlecore.org/`、`/privacy`、`/terms`；记录状态码、响应安全头和三语切换结果。
4. 生产未配置 secrets/项目变量时，部署作业应显示为 skipped；这不是发布成功，须先由项目负责人完成下节准备项。

## 回滚

前置：回滚属于生产变更，须取得董事会批准并在故障记录中填写审批编号；实施人须拥有 Cloudflare Pages 项目的 deployment rollback 权限，以及取消或停止未批准 GitHub Environment 作业的权限。权限或批准缺失时不得继续操作。

1. 停止或取消任何尚未批准的 `website-production` 作业。
2. Cloudflare Dashboard → Workers & Pages → Pages 项目 → Deployments，找到上一成功生产 deployment，选择 **Rollback to this deployment** 并确认。
3. 用无缓存请求核对首页、`/privacy`、`/terms` 均成功响应，且响应安全头符合随站发布的 `_headers`；在故障记录中写入回滚时间、恢复的 deployment 标识和核验结果。
4. 保留相关 GitHub `website-dist` artifact 至少 14 天。若 Pages 控制台无法回滚，在董事会批准后才可用对应 artifact 重新发布；本卡不授权该操作。
5. 创建故障记录，说明触发条件、影响、恢复版本和后续修复负责人。

## 项目负责人需手动准备（只写名称，不写值）

GitHub：

- Environment：`website-production`（配置 required reviewers，禁止自行审批）。
- Actions secret：`CLOUDFLARE_API_TOKEN`。
- Actions secret：`CLOUDFLARE_ACCOUNT_ID`。
- Actions variable：`CLOUDFLARE_PAGES_PROJECT`。

Cloudflare：

- Pages 项目：名称与 `CLOUDFLARE_PAGES_PROJECT` 一致。
- Pages 自定义域名：`settlecore.org` 与 `www.settlecore.org`。
- Bulk Redirect：源 `www.settlecore.org`，目标 `https://settlecore.org`，状态 `301`；启用保留查询字符串、子路径匹配和路径后缀。Cloudflare Pages `_redirects` 不支持域名级重定向，故仓库的 `_redirects` 仅保留该限制和此配置指引。

## 状态分列

| 项目 | 状态 |
| --- | --- |
| 本地提交 | 待本卡验收后提交 |
| 本地测试 | 见任务验收证据 |
| CI | 未运行（本地无法访问 GitHub 凭据） |
| 预览 | 未部署；PR 仅上传 artifact |
| 生产 | 未部署 |
