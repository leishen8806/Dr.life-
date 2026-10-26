# CMP-19 / P0-4 高棉语搜索 PoC — 可复现运行脚本（Windows / PowerShell）
#
# 用法：
#   .\run.ps1                # 跑准确率报告（快，约 10 秒）
#   .\run.ps1 -Scale         # 额外跑 10 万商品规模化压测（约 2 分钟）
#   .\run.ps1 -Scale -Mult 100   # 1 万商品
#   .\run.ps1 -Teardown      # 跑完删掉容器
#
# 依赖：只需要 Docker。镜像 postgres:16-alpine，pg_trgm / unaccent 随镜像自带。
# 不装任何 Go / Python / npm 依赖。
#
# 扩展审批：pg_trgm + unaccent 已获董事会批准
#   审批单 5512ff90-e25b-41d5-9f20-46c660dd86ad，approved @ 2026-10-16T09:21:12Z

param(
  [switch]$Scale,
  [int]$Mult = 1000,
  [switch]$Teardown,
  [string]$Container = 'drlife-poc-khmer',
  [int]$Port = 55439
)

$ErrorActionPreference = 'Stop'
$SqlDir = Join-Path $PSScriptRoot 'sql'

function Psql([string]$File, [string[]]$ExtraArgs = @()) {
  $name = Split-Path $File -Leaf
  docker cp $File "${Container}:/tmp/$name" | Out-Null
  $args = @('exec', $Container, 'psql', '-U', 'poc', '-d', 'khmer_poc', '-v', 'ON_ERROR_STOP=1')
  $args += $ExtraArgs
  $args += @('-f', "/tmp/$name")
  & docker @args
  if ($LASTEXITCODE -ne 0) { throw "psql failed on $name (exit $LASTEXITCODE)" }
}

# ---------------------------------------------------------------------------
# 1. 起一次性容器（独立名字/端口，不碰任何现有数据库）
# ---------------------------------------------------------------------------
$exists = docker ps -a --filter "name=^/$Container$" --format '{{.Names}}'
if (-not $exists) {
  Write-Host "==> 启动一次性 PostgreSQL 容器 $Container (127.0.0.1:$Port)" -ForegroundColor Cyan
  docker run -d --name $Container `
    -e POSTGRES_PASSWORD=pocpoc -e POSTGRES_USER=poc -e POSTGRES_DB=khmer_poc `
    -e POSTGRES_INITDB_ARGS=--encoding=UTF8 `
    -p "127.0.0.1:${Port}:5432" postgres:16-alpine | Out-Null
} else {
  Write-Host "==> 复用已有容器 $Container" -ForegroundColor Cyan
  docker start $Container | Out-Null
}

Write-Host '==> 等待数据库就绪' -ForegroundColor Cyan
$ready = $false
foreach ($i in 1..60) {
  docker exec $Container pg_isready -U poc -d khmer_poc *> $null
  if ($LASTEXITCODE -eq 0) { $ready = $true; break }
  Start-Sleep -Milliseconds 700
}
if (-not $ready) { throw '数据库在 42 秒内未就绪' }

# ---------------------------------------------------------------------------
# 2. 按顺序加载
# ---------------------------------------------------------------------------
$files = @(
  '00_schema.sql',            # 表结构 + 高棉语规范化 + n-gram
  '10_corpus.sql',            # 100 商品三语 + 同义词/拉丁转写表
  '20_queries.sql',           # 50 查询 + ground truth
  '30_search.sql',            # 方案 A/B/C
  '40_hybrid.sql',            # 方案 E
  '50_threshold_sweep.sql',   # 阈值敏感性
  '90_eval.sql',              # 评测函数
  '60_approach_d_trgm.sql',   # 方案 D（pg_trgm，已获批）
  '70_scale.sql',             # 规模化压测基建
  '80_approach_f_rerank.sql'  # 方案 F（推荐）
)
foreach ($f in $files) {
  Write-Host "==> 加载 $f" -ForegroundColor DarkGray
  Psql (Join-Path $SqlDir $f) @('-q')
}

# ---------------------------------------------------------------------------
# 3. 报告
# ---------------------------------------------------------------------------
Write-Host '==> 准确率报告' -ForegroundColor Green
Psql (Join-Path $SqlDir '99_report.sql')

if ($Scale) {
  Write-Host "==> 规模化压测（商品数 = 100 * $Mult）" -ForegroundColor Green
  Psql (Join-Path $SqlDir '98_scale_report.sql') @('-v', "mult=$Mult")
}

if ($Teardown) {
  Write-Host "==> 删除容器 $Container" -ForegroundColor Yellow
  docker rm -f $Container | Out-Null
} else {
  Write-Host "==> 容器 $Container 保留。清理：docker rm -f $Container" -ForegroundColor Yellow
}
