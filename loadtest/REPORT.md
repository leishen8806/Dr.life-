# Load-test report — CMP-135

> 只记录真实观察。Docker 不可用时写“无法验证”，不写“通过”；不通过放宽阈值或跳过缺失目标来制造绿色。

## 1. 结论

| 项目 | 状态 |
| --- | --- |
| 环境守卫（prod / 外网 host / 缺 BASE_URL / 生产字样 host / 缺目标） | 已验证，全部退出 1 |
| 阈值契约（故意放宽 `rejection_duration` 后变红） | 已验证，退出 1 |
| CI 守卫步骤（把 `TARGET_ENV=prod` 改成 local 后变红） | 已验证，退出 1 |
| 单元 / 契约测试 | 129 通过 / 0 失败，退出 0 |
| JS/MJS 语法检查 | 26 个文件，0 失败 |
| `docker compose config` | 退出 0 |
| `smoke.js` / `overload3x.js` / 业务场景 / 5 项故障演练 | **无法验证**（本机 Docker Engine 不可用） |

没有任何“通过”来自放宽断言、跳过目标或可选链。所有场景在模块加载时调用 `validateTargetEnv(__ENV)`，缺少目标时抛错并以非 0 退出。

## 2. 环境

- 日期：2026-10-22（Asia/Bangkok）
- 机器：Intel Core i7-14700KF / 31.8 GB
- 操作系统：Windows 11 专业版 10.0.22631（Build 22631）
- Shell：Windows PowerShell 5.1.22621.6133
- Node：v25.9.0；npm：11.12.1
- Docker 客户端：29.8.1；`com.docker.service` = **Stopped**（StartType=Manual）
- k6：`grafana/k6:2.3.0`（官方容器，本机未安装 k6）
- 基线：`9caeeecf18346a3de51e625813f5f911e3935b3e`（分支 `CMP-135-s-05-k6`；`origin/main` 074c0e7 + 本地集成的 7 个依赖提交）
- 工作区：`E:\VirtualAIOffice\pc-worktrees\CMP-135-s-05-k6`

## 3. 结果表（真实命令与退出码）

| 场景 | 命令 | 退出码 | 结果 |
| --- | --- | --- | --- |
| 单元/契约 | `node --test "loadtest/tests/*.test.mjs"` | 0 | 129 pass / 0 fail |
| 语法 | `node --check` × 26 | 0 | `CHECKED=26 SYNTAX_FAILED=0` |
| compose | `docker compose -f loadtest/docker-compose.yml config --quiet` | 0 | 通过（纯解析，未启动） |
| guard: prod | `TARGET_ENV=prod node loadtest/tools/slowloris.mjs` | 1 | `GUARD: TARGET_ENV must be local or test, got "prod"` |
| guard: 外网 host | `TARGET_ENV=local BASE_URL=https://example.com node loadtest/tools/slowloris.mjs` | 1 | `GUARD: host example.com is not in the allow-list for TARGET_ENV=local` |
| guard: 缺 BASE_URL | `TARGET_ENV=local node loadtest/tools/slowloris.mjs` | 1 | `GUARD: BASE_URL is required` |
| guard: 生产字样 | `TARGET_ENV=test BASE_URL=https://app.production.example LOADTEST_TEST_HOSTS=app.production.example ...` | 1 | `GUARD: refusing production-looking host label "production"` |
| 缺目标 | `TARGET_ENV=local BASE_URL=http://127.0.0.1:1 node loadtest/tools/slowloris.mjs` | 1 | `slowloris: /healthz returned 0 while 64 slow sockets were open` |
| 故障演练 | `TARGET_ENV=prod node loadtest/drills/run-drill.mjs redis-cache-down` | 1 | 守卫在 Docker 之前拒绝 |
| smoke | `TARGET_ENV=local ... k6 run loadtest/scenarios/smoke.js` | — | **无法验证**（见第 4 节） |
| guard negative（k6） | `TARGET_ENV=prod k6 run loadtest/scenarios/smoke.js` | — | **无法验证**；同一 guard 模块已用 Node 工具验证 |
| overload3x | `TARGET_ENV=local ... k6 run loadtest/scenarios/overload3x.js` | — | **无法验证** |
| 5 项故障演练 | `node loadtest/drills/run-drill.mjs <name>` | — | **无法验证** |

## 4. Docker 失败原始输出

```
PS> docker version --format '{{.Server.Os}}/{{.Server.Arch}} {{.Server.Version}}'
（62.3 秒后超时，退出码 124）

PS> docker info --format '{{.ServerVersion}}'
DOCKER_INFO_TIMEOUT_AFTER_25s

PS> Get-Service -Name com.docker.service | Select Name,Status,StartType
Name                Status   StartType
com.docker.service  Stopped  Manual

PS> wsl.exe -e /bin/sh -c 'echo WSL_OK; uname -m'
（64.0 秒后超时，退出码 124）
```

补充：本任务前次尝试 `docker run --rm grafana/k6:2.3.0 version` 在 64027 ms 后超时；`docker save grafana/k6:2.3.0` 返回 HTTP 500 `/images/get`。

因为 `docker` CLI 本身挂起，`docker compose up` 与 k6 容器都无法执行。**没有运行 smoke/overload3x，也没有把任何结果写成通过。**

## 5. 断言有效性（故意改错必须变红）

### 5.1 阈值契约：把 `p(95)<200` 放宽成 `p(95)<2000`

```
$ node --test loadtest/tests/scenario-contract.test.mjs
✖ threshold bounds are pinned to the acceptance numbers (2.2922ms)
ℹ tests 14
ℹ pass 13
ℹ fail 1
A_EXIT=1
RESTORED_A=True
```

恢复后：

```
ℹ tests 129
ℹ pass 129
ℹ fail 0
C_EXIT=0
```

### 5.2 CI 守卫步骤：把 `-e TARGET_ENV=prod` 改成 `local`

```
$ node --test loadtest/tests/scenario-contract.test.mjs
✖ the CI smoke workflow is path-filtered and proves the guard every run (1.0197ms)
ℹ tests 14
ℹ pass 13
ℹ fail 1
B_EXIT=1
RESTORED_B=True
```

### 5.3 单元级 mutation

`node --test` 中共 50 个以 `mutation:` 开头的用例，覆盖：守卫拒绝 prod/外网/生产字样、缺状态码列表、3xx/4xx/5xx、Retry-After 缺失或越界、安全头逐项删除、指纹头、指标缺失、阈值被下调、权重失衡、相对/穿越路径、内存泄漏趋势、非有限样本等。改坏输入时这些用例全部变红（见上面的测试列表）。

`LOADTEST_MUTATION_PROOF=1` 的 k6 运行（`count>0` 永不增长）**无法验证**：k6 无法启动。

## 6. 故障演练表

| 演练 | 期望行为 | 实际行为 | 结论 |
| --- | --- | --- | --- |
| `redis-cache-down` | 资金接口 503、普通读降级、限流回退内存 | 未执行 | 无法验证（Docker） |
| `redis-queue-down` | API 仍可入账、发件箱堆积、恢复后无丢失无重复 | 未执行 | 无法验证（Docker） |
| `replica-lag` | 报表池失败不影响 oltp | 未执行 | 无法验证（Docker） |
| `kill-worker` | 任务被重新执行 | 未执行 | 无法验证（Docker） |
| `kill-app` | 另一实例继续服务、`/readyz` 摘除生效 | 未执行 | 无法验证（Docker） |

## 7. 发现的问题

1. **P1 环境**：本机 Docker Engine 未运行，CLI 挂起，WSL 也挂起；容器化验收全部被阻塞。需要恢复 Docker 后重跑本报告第 3 节标记为“无法验证”的命令。
2. **P1 基线业务接口缺失**：静态核对显示当前 HEAD 只注册了 `/healthz`、`/readyz`、`/v1/public/ping`（`backend/internal/platform/server/server.go` 与 `backend/internal/api/gen/openapi.gen.go`）。`login-storm`、`mixed`、`financial-write`、`cache-stampede`、`queue-flood` 的默认目标不存在，预检会按设计非 0 失败；需要 Lead 安排接口或测试夹具后再压。
3. **P2 overload 依赖**：`BULKHEAD_*` / `BROWNOUT_*` 配置在 `backend/internal/platform/config` 已存在；仍需在 Docker 恢复后确认 `/v1/public/ping=heavy`、`/readyz=critical` 的实际分类与 503+`Retry-After` 行为。
4. **P2 CI**：新增 `.github/workflows/loadtest-smoke.yml`，仅在 `loadtest/**` 变更时运行，**不得加入分支保护成为必需检查**；手动压测入口仍是 `loadtest.yml`（仅 `workflow_dispatch`）。
5. **P3 残留物**：`loadtest/evidence/k6.exe` 是早前尝试留下的 Linux ELF（35 094 528 字节），已被 `.gitignore` 忽略，未参与本次验收；删除文件需审批，故保留。

## 8. 复现

```powershell
cd E:\VirtualAIOffice\pc-worktrees\CMP-135-s-05-k6
node --test "loadtest/tests/*.test.mjs"
docker compose -f loadtest/docker-compose.yml config --quiet

$env:TARGET_ENV='prod'; $env:BASE_URL='http://localhost:18080'
node loadtest/tools/slowloris.mjs    # 必须退出 1
```

Docker 恢复后：

```powershell
docker run --rm grafana/k6:2.3.0 version
docker compose -f loadtest/docker-compose.yml up -d --build --wait
$env:TARGET_ENV='local'; $env:BASE_URL='http://localhost:18080'
docker run --rm -i -v "${PWD}:/work" -w /work --network drlife-loadtest_default -e TARGET_ENV -e BASE_URL grafana/k6:2.3.0 run loadtest/scenarios/smoke.js
```