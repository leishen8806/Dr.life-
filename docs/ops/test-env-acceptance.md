# E-09 测试环境验收清单

本清单用于经批准的测试环境，不包含服务器地址、内部域名、凭据或真实客户数据。
所有域名由负责人在执行时填入；仓库中的命令只使用占位符和环境变量。

## 前置条件

- E-01 至 E-08 已按对应任务卡完成并记录结果。
- 测试环境的 Bot、数据库备份和访问权限由负责人提供；不在本清单中记录令牌。
- 本机只运行接口桩或 `--dry-run`；真实环境执行需由负责人按 `docs/ops/staging.md` 审批。

## 验收项目

| 编号 | 项目 | 命令/操作 | 期望结果 | 记录 |
| --- | --- | --- | --- | --- |
| E-09.1 | 三个前端可打开 | `SMOKE_API_BASE_URL=<占位符> SMOKE_BUYER_URL=<占位符> SMOKE_MERCHANT_URL=<占位符> SMOKE_SHOP_URL=<占位符> deploy/staging/smoke.sh` | API `/healthz` 为 200；三个前端为 200 且包含 app root | PASS / FAIL / NOT_RUN；时间：；执行人： |
| E-09.2 | API 健康检查 | 同上，观察 `smoke.sh` 的 API 输出 | `PASS: API .../healthz returned HTTP 200`，不输出 cookie、token 或完整响应体 | PASS / FAIL / NOT_RUN；证据： |
| E-09.3 | 测试 Bot 登录 | 使用负责人提供的测试账号，在三端各完成一次登录与退出；不得把验证码或令牌写入日志 | 登录成功，权限与测试账号范围一致，退出后会话失效 | PASS / FAIL / NOT_RUN；负责人记录： |
| E-09.4 | 备份恢复演练 | `SOURCE_DATABASE_URL=<占位符> RESTORE_DATABASE_URL=postgres://<占位符>/drlife_restore_drill_<批次> deploy/staging/restore-drill.sh`；先用 `--dry-run` 验证 | 自定义格式备份恢复到固定前缀临时库；schema/table row count 相同；退出时删除临时库；凭据不出现在输出 | PASS / FAIL / NOT_RUN；时间：；执行人： |

## 结果记录

| 日期（UTC+7） | 项目 | 结果 | 证据链接/运行编号 | 未执行原因与解除条件 |
| --- | --- | --- | --- | --- |
| YYYY-MM-DD HH:mm | E-09.1 | PASS / FAIL / NOT_RUN | `<链接或编号>` | `<如需负责人提供测试环境>` |
| YYYY-MM-DD HH:mm | E-09.2 | PASS / FAIL / NOT_RUN | `<链接或编号>` | `<原因>` |
| YYYY-MM-DD HH:mm | E-09.3 | PASS / FAIL / NOT_RUN | `<链接或编号>` | `<原因>` |
| YYYY-MM-DD HH:mm | E-09.4 | PASS / FAIL / NOT_RUN | `<链接或编号>` | `<原因>` |

脚本说明与占位符见 [`staging.md`](./staging.md)。
