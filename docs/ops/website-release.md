# 官网发布与回滚（VPS nginx）

适用站点：`apps/website` 的 Astro 静态产物。nginx 独立托管 `settlecore.org`；`www.settlecore.org` 保留路径和查询参数并 301 到主域名。页面仍按**草案上线**：方括号占位和“草案，待律师审核”横幅原样发布。

## 架构与安全

- 站点配置在 `deploy/nginx/settlecore.org.conf`，静态根为 `/srv/settlecore/current`；网站目录、低权限发布账号和后端目录必须分离。静态文件为只读，nginx 仅有读取权限。
- 配置含自定义 `404.html`、gzip/brotli、安全响应头、与旧 `_headers` 等价的 CSP（`font-src 'self'`，不放行 Google Fonts）、哈希资源及 `fonts/` 一年不可变缓存、HTML `no-cache`、隐藏文件拒绝和目录列表关闭。
- Cloudflare DNS 中根域和 `www` 记录保持不变，SSL/TLS 模式设为 **Full (strict)**。源站使用 Cloudflare Origin CA 证书；证书和私钥只由项目负责人放在服务器 `/etc/nginx/tls/`，不提交仓库。
- 可选加固：在服务器防火墙仅允许 Cloudflare 公布的 IP 段访问 80/443，并按 Cloudflare 官方 IP 段变更流程维护；这会改变防火墙，须另行审批。

## 项目负责人一次性准备（均只写名称，不写值）

1. 在 VPS 安装 nginx（含 Brotli 模块）、建立 `settlecore-deploy` 低权限账号和 `/srv/settlecore/releases`；该账号只可写该发布目录与切换 `current` 符号链接，不能读后端、证书或其他服务目录。
2. 安装本仓库 `deploy/nginx/settlecore.org.conf` 为独立站点；放置 Cloudflare Origin CA 证书与私钥；`nginx -t` 后由获批变更执行 reload。不要改动后端站点。
3. 以只读权限给 nginx 访问 `/srv/settlecore`，并安装 `deploy/website/rollback-current.sh` 为受控的回滚命令（仅允许该命令的 sudo 权限）。
4. GitHub Environment：`website-production`，配置 required reviewers，禁止自行审批。
5. GitHub Actions secrets：`WEBSITE_DEPLOY_HOST`、`WEBSITE_DEPLOY_USER`、`WEBSITE_DEPLOY_SSH_PRIVATE_KEY`、`WEBSITE_DEPLOY_KNOWN_HOSTS`。部署公钥仅授予发布目录权限；known-hosts 使用已核验的主机指纹。
6. Cloudflare：保持两条官网 A 记录为代理状态，SSL/TLS 改为 Full (strict)。

## 发布

`website.yml` 在 PR 与 `main` push 均构建 artifact；仅 `main` push 可进入部署门禁。门禁先检查四个部署 secrets，缺任一项时 `ready=false`，`deploy-production` 被 skipped。满足时仍必须等待 `website-production` 的人工批准，批准前不会下载 artifact、连接服务器或写入文件。

批准后的作业通过专用 SSH 账号把 artifact 上传至 `releases/.incoming-<时间戳>-<运行号>-<提交号>`；上传完成才改名为正式 release，并以 `mv -Tf .current.next current` 原子切换符号链接。随后仅保留最新五个 release。每次发布后的核验：无缓存请求首页、`/privacy`、`/terms`，记录状态码、安全头与三语切换结果。

## 回滚

回滚是生产变更，须先取得董事会审批。登录服务器后，执行受控命令：

```sh
sudo /usr/local/sbin/settlecore-website-rollback
```

它把 `current` 原子切至按发布时间排序的上一 release。然后无缓存核验首页、`/privacy`、`/terms`、404 页和响应安全头，并在故障记录写审批编号、目标 release、时间和结果。若没有上一 release、审批或受控权限，停止并升级。

## 状态分列

| 项目 | 状态 |
| --- | --- |
| 本地提交 | 待本卡验收后提交 |
| 本地测试 | 见任务验收证据 |
| CI | 未运行（本地不能代表 GitHub Actions） |
| 预览 | 未部署（PR 仅产出 artifact） |
| 生产 | 未部署 |
