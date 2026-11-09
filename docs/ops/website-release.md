# 官网发布与回滚（VPS nginx）

适用 `apps/website` 的 Astro 静态产物。页面仍按“草案上线”发布，方括号占位与草案横幅不得在本流程中替换。站点为独立 nginx 站点：`drlifeplus.com` 提供静态文件，`www` 由源站 301 到主域名；备用域名 `settlecore.org` 的保留路径、查询参数 301 跳转全部由 Cloudflare Redirect Rules 处理。

## 架构与安全

- 配置：主站 `deploy/nginx/drlifeplus.com.conf` 及 `deploy/nginx/drlifeplus-security-headers.conf`。主站配置同时包含 80/443 的 `default_server`，未知主机名返回 444；443 兜底使用 `drlifeplus.com` 的 Origin CA 证书。源站只启用 gzip；Cloudflare 在访客边缘按浏览器能力压缩（含 Brotli），无需 Brotli 模块或 `load_module`。
- TLS 使用 Cloudflare Origin CA，Cloudflare SSL/TLS 设为 **Full (strict)**。证书、私钥、部署密钥和主机指纹不得进仓库。
- 可选的“仅 Cloudflare IP 段访问源站”防火墙规则是独立生产变更，须另行审批。
- 仅 `/_astro/` 长缓存；HTML 与 `/fonts/` 下全部文件均为 `no-cache`。全部响应（包括缓存资源和 404）都带安全头与 CSP，`font-src 'self'`，不放行 Google Fonts。

## 项目负责人一次性准备（获批后执行）

### 步骤 1：准备目录和占位 release

安装 nginx 和 rsync；不要安装 Brotli 模块。创建根目录、`releases`、`incoming`、`staging` 和占位 release：`/srv/drlifeplus`、`/srv/drlifeplus/releases` 由 `root:root` 所有且为 `0755`；`/srv/drlifeplus/incoming` 由 `drlifeplus-deploy:drlifeplus-deploy` 所有且为 `0700`；`/srv/drlifeplus/staging` 由 `root:root` 所有且为 `0700`，部署账号不得进入。创建仅含临时 `index.html` 与 `404.html` 的 `/srv/drlifeplus/releases/bootstrap`，由 `root:root` 所有、目录 `0755`、文件 `0644`；把 `current` 建为指向 `releases/bootstrap` 的符号链接。这样 reload 到首次批准发布之间不会全站 404。

### 步骤 2：安装 nginx 配置

安装 `drlifeplus.com` 站点配置和安全头 include，并放置该域名的 Origin CA 证书与私钥；不在源站安装 `settlecore.org` 站点或证书。`default_server` 已包含在 `drlifeplus.com.conf`，无需安装第二份兜底配置。先执行 `nginx -t`，通过后才由获批变更执行 reload；若报告已有 `default_server` 冲突，立即停止并协调共用服务器的配置，不得删除或修改其他项目站点。不要改后端站点。裸 `/privacy` 预期为 301 到 `/privacy/`，配置已关闭绝对主机和端口重写。

### 步骤 3：安装发布脚本和 sudoers

将 `publish-release.sh`、`rollback-current.sh` 分别安装为 `/usr/local/sbin/drlifeplus-website-publish`、`/usr/local/sbin/drlifeplus-website-rollback`，均为 `root:root`、`0755`；部署账号对两脚本、`/srv/drlifeplus`、`staging` 和 `releases` 无写权限。sudoers 示例：`drlifeplus-deploy ALL=(root) NOPASSWD: /usr/local/sbin/drlifeplus-website-publish *, /usr/local/sbin/drlifeplus-website-rollback`。不得配置 `env_keep` 或使用 `sudo -E`；两个脚本都固定 `/srv/drlifeplus`，不接受环境变量覆盖。

#### sudoers 生效值信息核对

安装上一步的 sudoers 示例行后，执行下列**信息性核对**并记录输出。两个发布脚本已自行固定 `PATH`，因此这不是上线的安全前提；它用于发现 sudoers 被显式关闭或污染的配置。

```sh
# sudoers-check-start
(
sudo_list=$(sudo -l -U drlifeplus-deploy || true)
items=$(printf '%s\n' "$sudo_list" | tr -d '\\' | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -E '^!?secure_path' || true)
if test -z "$items"; then
  if printf '%s\n' "$sudo_list" | grep -q 'Matching Defaults entries'; then
    printf '%s\n' 'secure_path missing or commented out' >&2
    exit 1
  fi
  printf '%s\n' 'secure_path=unknown (not reported by sudo)'
else
  printf '%s\n' "$items"
  test "$(printf '%s\n' "$items" | wc -l)" = 1 && printf '%s\n' "$items" | grep -qE '^secure_path=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin(:/snap/bin)?$'
fi
)
# sudoers-check-end
```

以上片段在子 shell 中执行，因此异常时只结束核对并返回非零状态，不会退出当前交互式 SSH 会话。仅一项固定 `secure_path`（允许 Ubuntu 的 `:/snap/bin` 变体）表示记录正常；`!secure_path`、被注释/缺失的条目（仍有 `Matching Defaults entries` 段）或被污染的值会非零退出，必须停止并把输出交给项目负责人。无 sudoers 条目或 sudo-rs 不报告 Defaults 时输出 `unknown` 且不拦上线；同样记录输出。`env_keep+=PATH` 不是此信息性核对的独立阻断条件，因为脚本内部固定 PATH。服务器为 Debian/Ubuntu，发布脚本以 GNU `cp -R --no-dereference` 复制；复制前后都会拒绝非常规文件和多硬链接文件，任一失败会删除临时目录、写入 stderr 并非零退出，且不改动 `releases/` 或 `current`。本地回归按批准范围使用 `alpine:3.20`，其 BusyBox 工具与生产 Debian/Ubuntu 的 GNU coreutils 并非完全等价；该差异仍是测试保真度风险，生产变更时必须在目标系统复核，不能把本地容器结果当作真实 VPS 证明。Nginx 另以 `disable_symlinks on from=$document_root;` 拒绝跟随站点根目录下的符号链接。

### 步骤 4：配置 GitHub Environment

GitHub Environment 创建 `website-production`，配置 required reviewers，禁止自行审批。

### 步骤 5：配置部署公钥

部署公钥仅允许部署账号；项目负责人必须在 VPS 本机用 `ssh-keygen -lf` 核验主机公钥指纹，再把已核验的 known-hosts 条目写入 GitHub secret。禁止在 runner 上使用 `ssh-keyscan`。

### 步骤 6：切换 Cloudflare

Cloudflare 已设置为 **Full (strict)**，不要切换模式。当前主站返回 521 的原因是源站尚未安装并监听本手册的 nginx 站点；完成 nginx 配置检查、reload 和占位 `current` 后再复测。保持新主域名及备用域名的官网 A 记录为 Cloudflare 代理状态，并在 `settlecore.org` 站点执行以下一次性配置：

1. 进入 **Rules → Redirect Rules**，创建通配符重定向规则；
2. 匹配表达式填写 `*settlecore.org/*`；
3. 目标地址填写 `https://drlifeplus.com/${2}`；
4. 状态码选择 **301**，并启用**保留查询参数**；
5. 分别验证裸域名与 `www` 的路径、查询参数均被保留。

首次真实发布仍须等待合并及 Environment 人工批准。

## 发布

`website.yml` 在 PR 与 main push 均构建 artifact；只有 main push 能通过 secrets 门禁。缺任一部署 secret 时部署作业跳过。门禁通过后仍必须等待 `website-production` 人工批准。

批准后，专用账号仅可把 artifact 上传到 `/srv/drlifeplus/incoming/<release-id>/`。工作流随后通过受控 root 脚本发布：脚本验证 release-id，先清理固定路径上的 `.current.next` 文件、目录或悬空链接，再把 incoming 原子移动到仅 root 可进入的 `staging/`，拒绝符号链接、非常规文件和多硬链接文件；再在 staging 内创建 root 所有的新目录并复制内容，设为 `root:root`（目录 `0755`、文件 `0644`）后移动到 `releases/`，删除旧 staging 内容，原子替换 `current` 符号链接并只保留最新五个 release。复制使线上文件拥有新的 inode，即使上传账号在移动前已打开写句柄，之后的句柄写入也不会影响线上文件。发布后，部署账号不能修改或新增已有 release 的文件；发布核验应以该账号分别尝试追加 `index.html`、新增文件及保留的写句柄写入，三者都必须失败或不影响线上内容。

发布后无缓存核验首页、`/privacy`（预期 301→`/privacy/`）、`/terms`、404、`/_astro/` 和字体资源；记录状态码、安全头、gzip、缓存头和三语切换结果。

## 性能验证（正式访客路径）

性能复测一律通过 Cloudflare 代理后的 `https://drlifeplus.com`，记录测试时间、协议、是否启用压缩、HTTP 版本及各项分数。即使源站与 Cloudflare 间使用 HTTP/1.1，浏览器仍可能经 Cloudflare 使用 HTTP/2 或 HTTP/3。若因排障直连源站，结论必须标注“直连源站、非访客路径”，不能当作访客性能结论。

## 回滚

回滚是生产变更，须先获得董事会审批：

```sh
sudo /usr/local/sbin/drlifeplus-website-rollback
readlink /srv/drlifeplus/current
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
