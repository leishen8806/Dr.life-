# packages/ui

前端应用共享的 React 与 TypeScript 组件。

## 多语言排版

- 中文与英文使用系统字体栈；高棉语依次使用 `Noto Sans Khmer`、`Khmer UI`、`Leelawadee UI` 和系统 sans-serif。
- 不从外部 CDN 加载字体。当前不携带字体文件；待决策：是否在获得许可与体积评估后，自托管 Noto Sans Khmer 子集。
- `:lang(km)` 正文行高为 1.7，标题行高为 1.5，字号不低于 13px，避免上下标音符被裁切。
- 语言按钮使用内容宽度、允许换行且保持可见。关键按钮、标签和导航不得使用固定宽度、`overflow: hidden` 或省略号截断。
- `LanguageSwitcher` 提供中、英、高棉语切换，立即更新根元素 `lang` 和本地偏好，并保留 `onChange` 供 A-04 上报账户语言。

高棉语真实字体渲染仍需在 Windows、Android 和 iOS/Telegram WebView 人工目检；jsdom 无法验证字形与裁切，因此本任务使用 CSS 静态约束和词条长度警告，不引入 Playwright。

设计依据：`Dr.life+交互文档.md` 第 1.5 节及 `AGENTS.md` 第 4、6 节。
