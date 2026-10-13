# packages/i18n

买家、商家、司机、仓储与平台后台共用的中文、英文和高棉语词条。发布所需词条必须三语齐全。

## 词条约定

- key 使用扁平的 `模块.含义` 格式，例如 `common.confirm`、`error.1002`；不要按页面复制同义词。
- 参数写作 `{name}`，参数名只用字母、数字和下划线。三语中的参数集合必须一致。
- 中文没有复数变化；英文和高棉语使用显式的 `.one` / `.other` key。调用方用 `plural(count, oneKey, otherKey)` 选择 key，再调用 `t()`。不引入 ICU 运行时。
- `TranslationKey` 直接由 `en.json` 推导，不存在的 key 会在 TypeScript 编译时失败。当前参数对象为通用 `Record`，尚未根据每个 key 在类型层强制参数名。
- 未经母语审核的高棉语以 `[TODO-KM-REVIEW] ` 开头；`pnpm i18n:check` 会汇总这些 key。

## 语言解析与本地偏好

`resolveLanguage(accountLanguage, telegramLanguage)` 的回退顺序是：账户语言、Telegram 语言、`en`。它接受 `zh-CN`、`zh_TW`、`km-KH` 和 `EN-us` 等地区形式。

F-07 尚未连接账户语言接口。`LanguageSwitcher` 把选择写入 `localStorage` 的 `drlife_lang`，应用启动时把这个本地值作为临时的账户语言候选；存储不可用时静默回退。A-04 接入后由服务端登记的账户语言取代本地候选，组件的 `onChange` 可用于上报。

## 校验

```bash
pnpm i18n:check
```

检查会拒绝缺 key、多余 key、空值、占位符不一致，以及 `apps/*` 和 `packages/ui` 的 `.tsx` 中用户可见的 CJK/高棉文字字面量。测试文件与词条 JSON 不参与硬编码扫描；语言自称只保存在 `language.*` 词条中。检查同时对明显偏长的高棉语词条给出警告，但不会使构建失败。

各应用与 `packages/i18n`、`packages/ui` 的 `build` 都先运行该检查。负向单元测试覆盖缺失、多余、空值和占位符不一致。

设计依据：`Dr.life+交互文档.md` 第 1.5 节、`Dr.life+开发任务拆分与验收.md` F-07 与 `AGENTS.md` 第 4、6 节。
