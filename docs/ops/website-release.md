# 官网发布与回滚（VPS nginx）

## 第 3 步 sudoers 前置校验（必须先执行）

在安装 sudoers 示例行之前，先仅作只读校验，确认 `settlecore-deploy` 的**生效值**：

```sh
sudo -l -U settlecore-deploy | grep -E '^[[:space:]]*secure_path=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin[[:space:]]*$'
```

预期输出恰为一行 `secure_path=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`（可带行首空白）。若没有该输出，立即停止，不继续后续步骤，并把命令输出交给项目负责人；被注释的 `secure_path`、`Defaults !secure_path` 或仅 `env_keep+=PATH` 都不算通过。服务器为 Debian/Ubuntu，发布脚本以 GNU `cp -R --no-dereference` 复制；复制前后都会校验非常规文件，任一失败会删除临时目录、写入 stderr 并非零退出，且不改动 `releases/` 或 `current`。Nginx 另以 `disable_symlinks on from=$document_root;` 拒绝跟随站点根目录下的符号链接。

适用 `apps/website` 的 Astro 静态产物。页面仍按“草案上线”发布，方括号占位与草案横幅不得在本流程中替换。站点为独立 nginx 站点：`settlecore.org` 提供静态文件，`www` 保留路径和查询参数并 301 到主域名。

## 架构与安全

- 配置：`deploy/nginx/settlecore.org.conf` 及 `deploy/nginx/settlecore-security-headers.conf`。源站只启用 gzip；Cloudflare 在访客边缘按浏览器能力压缩（含 Brotli），无需 Brotli 模块或 `load_module`。
- TLS 使用 Cloudflare Origin CA，Cloudflare SSL/TLS 设为 **Full (strict)**。证书、私钥、部署密钥和主机指纹不得进仓库。
- 可选的“仅 Cloudflare IP 段访问源站”防火墙规则是独立生产变更，须另行审批。
- 仅 `/_astro/` 长缓存；HTML 与 `/fonts/` 下全部文件均为 `no-cache`。全部响应（包括缓存资源和 404）都带安全头与 CSP，`font-src 'self'`，不放行 Google Fonts。

## 项目负责人一次性准备（获批后执行）

1. 安装 nginx 和 rsync；不要安装 Brotli 模块。创建根目录、`releases`、`incoming`、`staging` 和占位 release：`/srv/settlecore`、`/srv/settlecore/releases` 由 `root:root` 所有且为 `0755`；`/srv/settlecore/incoming` 由 `settlecore-deploy:settlecore-deploy` 所有且为 `0700`；`/srv/settlecore/staging` 由 `root:root` 所有且为 `0700`，部署账号不得进入。创建仅含临时 `index.html` 与 `404.html` 的 `/srv/settlecore/releases/bootstrap`，由 `root:root` 所有、目录 `0755`、文件 `0644`；把 `current` 建为指向 `releases/bootstrap` 的符号链接。这样 reload 到首次批准发布之间不会全站 404。
2. 安装 nginx 站点配置和安全头 include；放置 Origin CA 证书与私钥；先执行 `nginx -t`，再由获批变更执行 reload。不要改后端站点。裸 `/privacy` 预期为 301 到 `/privacy/`，配置已关闭绝对主机和端口重写。
3. 将 `publish-release.sh`、`rollback-current.sh` 分别安装为 `/usr/local/sbin/settlecore-website-publish`、`/usr/local/sbin/settlecore-website-rollback`，均为 `root:root`、`0755`；部署账号对两脚本、`/srv/settlecore`、`staging` 和 `releases` 无写权限。sudoers 示例：`settlecore-deploy ALL=(root) NOPASSWD: /usr/local/sbin/settlecore-website-publish *, /usr/local/sbin/settlecore-website-rollback`。不得配置 `env_keep` 或使用 `sudo -E`；两个脚本都固定 `/srv/settlecore`，不接受环境变量覆盖。
4. GitHub Environment 创建 `website-production`，配置 required reviewers，禁止自行审批。
5. 部署公钥仅允许部署账号；项目负责人必须在 VPS 本机用 `ssh-keygen -lf` 核验主机公钥指纹，再把已核验的 known-hosts 条目写入 GitHub secret。禁止在 runner 上使用 `ssh-keyscan`。
6. 保持两条官网 A 记录为 Cloudflare 代理状态。完成 nginx 校验、reload 和占位 `current` 后，才将 Cloudflare SSL/TLS 切到 Full (strict)；首次真实发布仍须等待合并及 Environment 人工批准。

## 发布

`website.yml` 在 PR 与 main push 均构建 artifact；只有 main push 能通过 secrets 门禁。缺任一部署 secret 时部署作业跳过。门禁通过后仍必须等待 `website-production` 人工批准。

批准后，专用账号仅可把 artifact 上传到 `/srv/settlecore/incoming/<release-id>/`。工作流随后通过受控 root 脚本发布：脚本验证 release-id，先把 incoming 原子移动到仅 root 可进入的 `staging/`，拒绝符号链接和非常规文件；再在 staging 内创建 root 所有的新目录并复制内容，设为 `root:root`（目录 `0755`、文件 `0644`）后移动到 `releases/`，删除旧 staging 内容，原子替换 `current` 符号链接并只保留最新五个 release。复制使线上文件拥有新的 inode，即使上传账号在移动前已打开写句柄，之后的句柄写入也不会影响线上文件。发布后，部署账号不能修改或新增已有 release 的文件；发布核验应以该账号分别尝试追加 `index.html`、新增文件及保留的写句柄写入，三者都必须失败或不影响线上内容。

发布后无缓存核验首页、`/privacy`（预期 301→`/privacy/`）、`/terms`、404、`/_astro/` 和字体资源；记录状态码、安全头、gzip、缓存头和三语切换结果。

## 性能验证（正式访客路径）

性能复测一律通过 Cloudflare 代理后的 `https://settlecore.org`，记录测试时间、协议、是否启用压缩、HTTP 版本及各项分数。即使源站与 Cloudflare 间使用 HTTP/1.1，浏览器仍可能经 Cloudflare 使用 HTTP/2 或 HTTP/3。若因排障直连源站，结论必须标注“直连源站、非访客路径”，不能当作访客性能结论。

## 回滚

回滚是生产变更，须先获得董事会审批：

```sh
sudo /usr/local/sbin/settlecore-website-rollback
readlink /srv/settlecore/current
```

脚本只回到当前 release 在时间序列中更早的一个 release；已经是最早 release 时会非零退出且不改动 `current`。回滚后记录 `readlink` 目标，并无缓存核验首页、`/privacy`、`/terms`、404 与安全头。无上一 release、无审批或无受控权限时停止并升级。

## 状态分列

| 项目 | 状态 |
| --- | --- |
| 本地提交 | 待本卡验收后提交 |
| 本地测试 | 见验收证据 |
| CI | 未运行（本地不能代表 GitHub Actions） |
| 预览 | 未部署（PR 仅产生 artifact） |
| 生产 | 未部署 |
