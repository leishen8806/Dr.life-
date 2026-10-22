禁止对生产运行

# Dr.life+ 容量与韧性压测

本目录只用于 `local` / 显式允许的 `test` 目标。脚本在模块加载时调用环境守卫：`TARGET_ENV` 必须是 `local` 或 `test`，`BASE_URL` 的主机必须在允许列表内；`TARGET_ENV=prod`、外网域名、`localhost.evil.example` 等都会立即抛错并以非 0 退出。生产域名不会进入允许列表。

## 环境

- Docker 29.8.1 或兼容的 Docker Compose v2
- 压测器固定使用官方容器镜像 `grafana/k6:2.3.0`，不要求本机安装 k6
- `loadtest/docker-compose.yml` 固定 PostgreSQL、两个 Redis、应用 A/B、worker、Prometheus、Grafana 镜像标签
- 真实令牌、真实 initData、真实用户数据一律禁止提交；登录风暴使用测试签发工具生成的合成 initData

## 启动

PowerShell：

```powershell
docker compose -f loadtest/docker-compose.yml up -d
$env:TARGET_ENV='local'
$env:BASE_URL='http://localhost:18080'
docker run --rm -i -v "${PWD}:/work" -w /work --network drlife-loadtest_default grafana/k6:2.3.0 run loadtest/scenarios/smoke.js
```

bash：

```bash
docker compose -f loadtest/docker-compose.yml up -d
TARGET_ENV=local BASE_URL=http://localhost:18080 \
  docker run --rm -i -v "$PWD:/work" -w /work --network drlife-loadtest_default \
  grafana/k6:2.3.0 run loadtest/scenarios/smoke.js
```

守卫自证：

```powershell
$env:TARGET_ENV='prod'
docker run --rm -i -v "${PWD}:/work" -w /work --network drlife-loadtest_default grafana/k6:2.3.0 run loadtest/scenarios/smoke.js
# 必须非 0 退出
```

## 场景

| 脚本 | 目标 | 默认负载 |
| --- | --- | --- |
| `scenarios/smoke.js` | 健康、就绪、公开接口、鉴权边界、安全头 | 1 VU / 10s |
| `scenarios/login-storm.js` | 60s 登录风暴，auth P95、拒绝率、Retry-After、critical/health | 2000 次 |
| `scenarios/mixed.js` | 读 85% / 写 10% / 重 5%，10 分钟 | 20 VUs |
| `scenarios/financial-write.js` | 提现/入账并发、幂等重放、无死锁、无负余额 | 40 VUs / 60s |
| `scenarios/overload3x.js` | 3 倍过载，heavy 503+Retry-After，critical/health 不受影响 | 120 VUs / 60s |
| `scenarios/retry-storm.js` | 无退避立即重试 vs 指数退避+抖动 | 80 压力 VUs |
| `scenarios/cache-stampede.js` | 热点键过期 1000 并发，回源增量 <= 1 | 1000 VUs |
| `scenarios/queue-flood.js` | 10 万 notify 任务，critical 等待、发件箱滞后 | 200 VUs |
| `scenarios/soak.js` | 10 分钟长稳，goroutine/内存趋势 | 1 VU |
| `scenarios/security-under-load.js` | 越权、幂等重放、限流、slowloris 配套探针 | 4 VUs |

所有场景在 `setup()` 先做目标探测；目标缺失、状态码不符、Prometheus 指标缺失都会抛错，不会因为可选链或空返回而假通过。

## 故障演练

```powershell
$env:TARGET_ENV='local'
$env:BASE_URL='http://localhost:18080'
node loadtest/drills/run-drill.mjs redis-cache-down
node loadtest/drills/run-drill.mjs redis-queue-down
node loadtest/drills/run-drill.mjs replica-lag
node loadtest/drills/run-drill.mjs kill-worker
node loadtest/drills/run-drill.mjs kill-app
```

每个演练写出 `loadtest/evidence/<name>.json`，包含期望、实际、结论和时间。失败时退出非 0。

## 断言有效性

`LOADTEST_MUTATION_PROOF=1` 会给 smoke 加一个永远不会增长的 `mutation_proof` 计数器，并声明 `count>0` 阈值；该次运行必须变红。其余守卫的变红证明在 `tests/` 的 `mutation:` 测试中逐个执行：改坏环境、改坏主机、删除安全头、删除指标、放宽阈值都会失败。

## CI 烟雾版

- 手动压测：`.github/workflows/loadtest.yml`，只有 `workflow_dispatch`。
- 轻量烟雾：`.github/workflows/loadtest-smoke.yml`，只在 `loadtest/**` 变更时触发；它每次先跑 `TARGET_ENV=prod` 负向守卫（必须非 0），再跑 1 VU 的 smoke。
- **不要**把任何一个加入分支保护成为必需检查；压测不是 PR 门禁。

## 证据

`loadtest/evidence/` 下的运行产物不提交。Docker 或目标不可用时，只能记录失败输出并写“无法验证”，不得声称通过。
## 报告

运行后把机器配置、镜像版本、每场景结果表和瓶颈填入 `REPORT.md`。没有真实命令输出时不得写“通过”。