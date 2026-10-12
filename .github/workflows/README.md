# CI 工作流

`ci.yml` 在目标为 `main` 的 Pull Request 和推送到 `main` 时运行。默认权限只有 `contents: read`；同一 PR 的新提交会取消旧运行，`main` 上的运行不会互相取消。

## 检查项

| Job | 内容 | PR 运行条件 |
|---|---|---|
| `changes` | 根据 PR 基线与 HEAD 的差异输出文件分类 | 始终 |
| `light-checks` | gofmt、go vet、go mod tidy、密钥扫描、根 README 文档索引 | 始终 |
| `backend-tests` | 排除 Docker 集成用例后执行 `go test -race` | 后端或 CI 基础设施变化 |
| `backend-integration` | testcontainers 启动全新 PostgreSQL，执行迁移和权限边界测试 | 后端或 CI 基础设施变化 |
| `docker-image` | 构建镜像，以真实 PostgreSQL 启动，检查 health、ready、Docker 健康状态和 uid 10001 | 后端或 CI 基础设施变化 |
| `openapi-check` | 存在 `backend/scripts/check-generated.sh` 时检查生成代码，否则成功跳过 | API、后端变化；F-04 合并后自动生效 |
| `frontend-check` | 存在 `pnpm-workspace.yaml` 时安装、类型检查、构建和检查三语词条，否则成功跳过 | 前端变化；F-06 合并后自动生效 |
| `vuln-scan` | 执行 `govulncheck ./...` | 后端变化；目前告警但不阻塞 |
| `ci-gate` | 汇总所有结果，必需任务失败或取消时失败，条件跳过视为通过 | 始终 |

推送到 `main` 时不会按路径跳过任何重型任务。纯文档 PR 只运行 `changes`、`light-checks` 和 `ci-gate`。`vuln-scan` 在连续 10 个 PR 中稳定运行且团队完成现有漏洞分级后，再取消 `continue-on-error` 并纳入阻塞结果。

工作流只使用以下官方 Actions，均固定到主版本标签：`actions/checkout@v4`、`actions/setup-go@v5`、`actions/setup-node@v4`、`docker/setup-buildx-action@v3`、`docker/build-push-action@v6`。

## 本地复现

在 Linux 或可运行 Linux 容器的环境执行：

```sh
cd backend
test -z "$(gofmt -l .)"
go vet ./...
go mod tidy && git diff --exit-code -- go.mod go.sum
go test -race -count=1 -skip '^TestDatabaseFramework$' ./...
go test -count=1 -run '^TestDatabaseFramework$' -v ./internal/platform/db
cd ..
backend/scripts/check-secrets.sh
python3 backend/scripts/check-doc-index.py
docker build -t drlife-backend:ci backend
```

使用 Docker 验证工作流语法：

```sh
docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:1.7.7
```

## 观察指标

F-08 上线后观察前 10 个 PR：完整 CI 中位耗时目标不超过 8 分钟，基础设施误报率低于 5%。超过目标时先检查 Go 模块缓存、BuildKit 缓存、镜像拉取和 testcontainers 启动耗时，再决定是否调整任务拆分；不得通过降低测试断言或跳过必需测试缩短时间。

## 设置合并门槛

仓库管理员在 GitHub **Settings → Branches** 为 `main` 添加保护规则，启用 **Require a pull request before merging** 和 **Require status checks to pass**，必需检查只选择 `ci-gate`。Codex 不修改分支保护设置。

<!-- F-13 docs-only CI demonstration; branch will be closed and deleted. -->
