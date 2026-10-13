# backend

Go 模块化单体后端，采用 chi、pgx、sqlc 与 goose。
接口规范先写 OpenAPI，再生成代码；业务模块边界以独立 schema 为准。
技术依据：`Dr.life+第一版交付形态与技术架构.md` 第三、六章。

Prometheus、Grafana、Sentry 与指标暴露约束见 `../deploy/observability/README.md`。
