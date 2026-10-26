#!/usr/bin/env bash
# CMP-19 / P0-4 高棉语搜索 PoC — 可复现运行脚本（Linux / macOS）
#
# 用法：
#   ./run.sh                 # 准确率报告（约 10 秒）
#   SCALE=1 ./run.sh         # 额外跑 10 万商品规模化压测（约 2 分钟）
#   SCALE=1 MULT=100 ./run.sh    # 1 万商品
#   TEARDOWN=1 ./run.sh      # 跑完删容器
#
# 依赖：只需要 Docker。pg_trgm / unaccent 随 postgres:16-alpine 镜像自带。
# 不装任何 Go / Python / npm 依赖。
#
# 扩展审批：pg_trgm + unaccent 已获董事会批准
#   审批单 5512ff90-e25b-41d5-9f20-46c660dd86ad，approved @ 2026-10-16T09:21:12Z

set -euo pipefail

CONTAINER="${CONTAINER:-drlife-poc-khmer}"
PORT="${PORT:-55439}"
MULT="${MULT:-1000}"
SQL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/sql"

psql_f() {
  local file="$1"; shift
  local name; name="$(basename "$file")"
  docker cp "$file" "${CONTAINER}:/tmp/${name}" >/dev/null
  docker exec "$CONTAINER" psql -U poc -d khmer_poc -v ON_ERROR_STOP=1 "$@" -f "/tmp/${name}"
}

if ! docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
  echo "==> 启动一次性 PostgreSQL 容器 $CONTAINER (127.0.0.1:$PORT)"
  docker run -d --name "$CONTAINER" \
    -e POSTGRES_PASSWORD=pocpoc -e POSTGRES_USER=poc -e POSTGRES_DB=khmer_poc \
    -e POSTGRES_INITDB_ARGS=--encoding=UTF8 \
    -p "127.0.0.1:${PORT}:5432" postgres:16-alpine >/dev/null
else
  echo "==> 复用已有容器 $CONTAINER"
  docker start "$CONTAINER" >/dev/null
fi

echo '==> 等待数据库就绪'
for _ in $(seq 1 60); do
  if docker exec "$CONTAINER" pg_isready -U poc -d khmer_poc >/dev/null 2>&1; then
    ready=1; break
  fi
  sleep 0.7
done
[ "${ready:-0}" = 1 ] || { echo '数据库在 42 秒内未就绪' >&2; exit 1; }

for f in 00_schema.sql 10_corpus.sql 20_queries.sql 30_search.sql 40_hybrid.sql \
         50_threshold_sweep.sql 90_eval.sql 60_approach_d_trgm.sql 70_scale.sql \
         80_approach_f_rerank.sql; do
  echo "==> 加载 $f"
  psql_f "$SQL_DIR/$f" -q
done

echo '==> 准确率报告'
psql_f "$SQL_DIR/99_report.sql"

if [ "${SCALE:-0}" = 1 ]; then
  echo "==> 规模化压测（商品数 = 100 * $MULT）"
  psql_f "$SQL_DIR/98_scale_report.sql" -v "mult=$MULT"
fi

if [ "${TEARDOWN:-0}" = 1 ]; then
  echo "==> 删除容器 $CONTAINER"
  docker rm -f "$CONTAINER" >/dev/null
else
  echo "==> 容器 $CONTAINER 保留。清理：docker rm -f $CONTAINER"
fi
