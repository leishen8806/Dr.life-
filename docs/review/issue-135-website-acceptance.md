# Issue #135 官网渲染收尾验收

## 范围

- 基线：`main`，`f27c69a4c660fce2f66ae4489767c770f123bc90`
- 分支：`fix/135-website-release-polish`
- 页面：`apps/website` 首页及共享 Header
- 规则：保持三语文案、占位符和“草案，待律师审核”提示，不引入未确认宣传语

## 差异清单

| 项目 | 结果 | 处理 |
| --- | --- | --- |
| 1440px 桌面导航与首屏 | PASS | 保持桌面导航和 CTA 布局 |
| 768px 平板导航 | PASS | 隐藏挤压式桌面导航，启用 44px 触控汉堡菜单 |
| 390px 手机导航 | PASS | 菜单入口可见，菜单面板有层级、边框和触控间距 |
| 移动端 Hero 标题 | PASS | 按设计恢复 60px，并保留换行保护 |
| 三语/占位符/内部链接 | PASS | 现有静态检查全部通过 |
| Lighthouse/线上部署 | NOT_RUN | 未部署；按任务规则不执行生产发布 |

## 自动化门禁

使用 Node 22 执行：

- `typecheck` PASS
- `lint` PASS
- `test` PASS（26/26）
- `build` PASS（7 个静态路由）

本地生产预览截图：

- 1440px：`/Users/suchen/.codex/visualizations/2026/11/06/01a1af31-1ca5-7182-9946-8fc44a09246a/dr-life-135-home-1440-final.png`
- 768px：`/Users/suchen/.codex/visualizations/2026/11/06/01a1af31-1ca5-7182-9946-8fc44a09246a/dr-life-135-home-768-final.png`
- 390px：`/Users/suchen/.codex/visualizations/2026/11/06/01a1af31-1ca5-7182-9946-8fc44a09246a/dr-life-135-home-390-final.png`

## 未处理事项

没有发现需要设计确认的当前差异。线上 Lighthouse、部署和真实设备验收留待有权限的发布/验收流程执行。
