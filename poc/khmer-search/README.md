# 高棉语搜索 PoC（CMP-19 / P0-4）

**结论请直接看 [`RESULT.md`](./RESULT.md)。** 本文件只讲怎么跑和文件结构。

这是一个 PoC，**不是生产代码**：不实现 T-03 的搜索接口，不改 `catalog` 表结构，
所有对象都建在一次性容器的 `poc` schema 里。

## 前置条件

只需要 **Docker**。镜像 `postgres:16-alpine`（本机已有），
`pg_trgm` / `unaccent` 随镜像自带，无需联网下载。

**不需要** Go / Python / npm 依赖 —— 整个 PoC 由 `psql` + 纯 SQL 驱动，
`backend/go.mod` 未改动。

> 扩展 `pg_trgm` + `unaccent` 已经董事会批准：
> 审批单 `5512ff90-e25b-41d5-9f20-46c660dd86ad`，approved @ 2026-10-16T09:21:12Z。

## 跑起来

```powershell
# Windows / PowerShell
cd poc\khmer-search
.\run.ps1                    # 准确率报告（约 10 秒）
.\run.ps1 -Scale             # 额外跑 10 万商品性能压测（约 2 分钟）
.\run.ps1 -Scale -Mult 100   # 1 万商品
.\run.ps1 -Teardown          # 跑完删掉容器
```

```bash
# Linux / macOS
cd poc/khmer-search
./run.sh
SCALE=1 ./run.sh
SCALE=1 MULT=100 ./run.sh
TEARDOWN=1 ./run.sh
```

容器名 `drlife-poc-khmer`、端口 `127.0.0.1:55439`，**独立于任何现有数据库**，
跑完可用 `docker rm -f drlife-poc-khmer` 清掉。

手工连进去看：

```bash
docker exec -it drlife-poc-khmer psql -U poc -d khmer_poc
```

```sql
-- 试一个高棉语拼写变体（漏写 ័）
select r.rnk, p.name_km, p.name_zh, round(r.score::numeric,3)
from poc.search_rerank('ទូរសព្ទ', 5) r join poc.product p on p.id = r.id
order by r.rnk;
```

## 文件结构

| 文件 | 作用 |
| --- | --- |
| `sql/00_schema.sql` | 表结构；高棉语规范化（去零宽字符、数字折叠）；字符 n-gram 函数 |
| `sql/10_corpus.sql` | 100 个商品（中/英/高棉三语）+ 同义词与拉丁转写表 |
| `sql/20_queries.sql` | 50 条查询 + 人工标注 ground truth，分 12 类 |
| `sql/30_search.sql` | 方案 A（ILIKE）、B（内置全文检索）、C（纯 SQL bigram） |
| `sql/40_hybrid.sql` | 方案 E：精确子串 ∪ bigram+同义词 |
| `sql/50_threshold_sweep.sql` | coverage 阈值敏感性分析 |
| `sql/60_approach_d_trgm.sql` | 方案 D：pg_trgm（`%` / `<%` / +同义词）**受审批门控** |
| `sql/70_scale.sql` | 规模化压测基建（交叉生成 N 条商品、建索引、计时） |
| `sql/80_approach_f_rerank.sql` | **方案 F（推荐）**：pg_trgm 粗筛 + bigram 精排 |
| `sql/90_eval.sql` | 评测函数：recall@k、首位命中率、延迟、索引大小 |
| `sql/98_scale_report.sql` | 规模化报告（索引大小 + 延迟 + EXPLAIN 证据） |
| `sql/99_report.sql` | 准确率汇总报告 |

加载顺序由 `run.ps1` / `run.sh` 固定，不要单独乱序执行
（`60_` 要在 `90_` 之后加载，因为 `70_`/`80_` 依赖前面的函数）。

## 评测指标定义

- `recall@k` = |取回前 k 条 ∩ 人工标注相关| / |人工标注相关|
- `first_hit` = 排名第 1 的结果是否属于标注相关集（1/0），即任务表要求的「首位命中率」
- `avg_returned` = 平均返回条数，作为噪声代理指标（阈值越低召回越高但噪声越多）

## 注意事项（踩过的坑）

- **`run.ps1` 必须存为 UTF-8 with BOM**。Windows PowerShell 5.1 对无 BOM 的 `.ps1`
  按 ANSI 解析，中文注释会变乱码并导致语法错误。
- **psql 的 `\echo` 会把反引号当 shell 命令执行**，`\echo` 行里不要写反引号。
- **`word_similarity(a,b) >= x` 不走索引**，必须用 `<%` 运算符 + GUC 阈值，
  详见 `RESULT.md` §3.6。
