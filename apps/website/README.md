# DR.LIFE+ 官网

这是 `drlifeplus.com` 的 Astro 静态站。

## 本地运行

```sh
pnpm -F @drlife/website dev
```

### Responsive screenshot evidence

To build the static site and generate the same evidence matrix used in CI,
install the fixed Playwright dependency's Chromium once and run:

```sh
pnpm --filter @drlife/website exec playwright install chromium
pnpm --filter @drlife/website capture:screenshots
```

The command writes 66 full-page PNGs (11 widths × 3 languages × the homepage
and `/merchant/`) plus `screenshots/manifest.txt` under `apps/website/`.
GitHub Actions runs this evidence in a separate `screenshots` job, so a failed
capture cannot block the production deployment job. It uploads this directory
as the `website-screenshots` artifact for 14 days and lists every file in the
job summary. Screenshots must not show
server addresses; the local capture server binds only to `127.0.0.1` and is
closed after capture. Playwright is pinned to `1.56.1` and is distributed
under the Apache-2.0 license; Chromium is installed by Playwright's official
installer and is not committed to the repository.

## 语言

站点使用查询参数路由：`?lang=zh`、`?lang=en`、`?lang=km`。中文为默认；静态 HTML 先渲染中文，浏览器脚本会根据该参数替换集中在 `src/i18n/` 的词条。该方案保留法律页的固定路径，例如 `/privacy` 与 `/terms`。

Bot 用户名、Telegram 外链及官网域名集中在 `src/constants.ts`。联系方式、运营主体和法律版本仍为设计文档要求保留的方括号占位。

### 已知发布限制

`?lang=en` 与 `?lang=km` 依赖浏览器端脚本替换词条：静态 HTML 和禁用 JavaScript 的访问者仍会看到中文，且当前没有独立的 `/en/`、`/km/` 页面或 `hreflang`。上线前须由董事会决定是否改为静态语言前缀页。

确认稿新增的主页、商家页和法律页字段已接入 `src/i18n/page-copy.ts`。本工作树未提供 Localizer 的对应 Khmer `copy.md`，这些新增字段暂按任务契约回落中文；补齐高棉语并复验后才能发布。

### Khmer copy review

The Khmer copy for the homepage **GROW WITH US** partner cards is a working draft and remains pending native-speaker review before release.
