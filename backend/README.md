# backend

Go 模块化单体后端，采用 chi、pgx、sqlc 与 goose。
接口规范先写 OpenAPI，再生成代码；业务模块边界以独立 schema 为准。
技术依据：`Dr.life+第一版交付形态与技术架构.md` 第三、六章。

Prometheus、Grafana、Sentry 与指标暴露约束见 `../deploy/observability/README.md`。

## 账务对账命令

服务和 `ledgerctl` 都从环境变量读取数据库配置。每日快照日界线由
`LEDGER_RECON_TIMEZONE` 控制，默认 `Asia/Phnom_Penh`。

```sh
go run ./cmd/ledgerctl recon --day 2026-10-14
go run ./cmd/ledgerctl recon --day 2026-10-14 --force --reason "verified repair"
go run ./cmd/ledgerctl snapshot --day 2026-10-14
go run ./cmd/ledgerctl freeze list
go run ./cmd/ledgerctl freeze release --id 1 --by 2002 --reason "finance verified"
```

命令退出码为：`0` 表示成功且对账通过，`1` 表示运行失败或发现异常，`2`
表示参数错误。输出只包含对账、异常计数和冻结元数据，不输出收款账户等敏感信息。
