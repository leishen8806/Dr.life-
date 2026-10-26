-- CMP-19 / P0-4 — 方案实现（**全部不依赖任何扩展**）
--
-- 方案 A  ILIKE 子串          基线，无索引
-- 方案 B  内置全文检索 FTS    to_tsvector('simple') + GIN
-- 方案 C  纯 SQL 字符 bigram  text[] + GIN（array_ops，内置算子类）
-- 方案 D  pg_trgm             见 60_approach_d_trgm.sql（受审批门控，单独文件）
--
-- 统一检索文档 = 中文名 + 英文名 + 高棉语名 拼接。
-- 这样「三语互搜」天然成立：任何一种语言的查询都能命中同一商品。

-- ---------------------------------------------------------------------------
-- 检索文档物化表
-- ---------------------------------------------------------------------------
drop table if exists poc.doc cascade;

create table poc.doc as
select p.id,
       p.name_zh || ' ' || p.name_en || ' ' || p.name_km            as raw,
       poc.norm(p.name_zh || ' ' || p.name_en || ' ' || p.name_km)  as body,
       to_tsvector('simple',
         poc.norm(p.name_zh || ' ' || p.name_en || ' ' || p.name_km)) as tsv,
       poc.bigrams(p.name_zh || ' ' || p.name_en || ' ' || p.name_km) as grams
from poc.product p;

alter table poc.doc add primary key (id);

-- 方案 B 的索引：内置 GIN on tsvector
create index idx_doc_tsv   on poc.doc using gin (tsv);
-- 方案 C 的索引：内置 GIN on text[]（array_ops 是内置算子类，无需扩展）
create index idx_doc_grams on poc.doc using gin (grams array_ops);

analyze poc.doc;

-- ---------------------------------------------------------------------------
-- 同义词 / 拉丁转写展开
-- ---------------------------------------------------------------------------
-- 拉丁词条要求词边界匹配（否则 'ac' 会污染一切）；
-- 高棉文/中文无词边界概念，用子串匹配。
create or replace function poc.expand(q text) returns text
language sql stable strict as $$
  select poc.norm(q) ||
         coalesce(' ' || (
           select string_agg(distinct s.canonical, ' ')
           from poc.synonym s
           where case
                   when poc.norm(s.term) ~ '^[a-z0-9 ]+$'
                     then poc.norm(q) ~ ('\m' || poc.norm(s.term) || '\M')
                   else poc.norm(q) like '%' || poc.norm(s.term) || '%'
                 end
         ), '')
$$;

-- ---------------------------------------------------------------------------
-- 方案 A：ILIKE 子串基线
-- ---------------------------------------------------------------------------
create or replace function poc.search_ilike(q text, k int default 10)
returns table (id int, rnk int, score float)
language sql stable as $$
  select d.id,
         (row_number() over (order by char_length(d.body), d.id))::int,
         1.0::float
  from poc.doc d
  where d.body like '%' || poc.norm(q) || '%'
  order by char_length(d.body), d.id
  limit k
$$;

-- ---------------------------------------------------------------------------
-- 方案 B：内置全文检索
-- ---------------------------------------------------------------------------
-- 'simple' 配置按非字母数字边界切词。中文与高棉文**没有词间空格**，
-- 因此整条中文名/高棉语名会变成一个巨型 token —— 这正是要实测的关键局限。
create or replace function poc.search_fts(q text, k int default 10)
returns table (id int, rnk int, score float)
language sql stable as $$
  with tq as (select plainto_tsquery('simple', poc.norm(q)) as query)
  select d.id,
         (row_number() over (order by ts_rank(d.tsv, tq.query) desc,
                                      char_length(d.body), d.id))::int,
         ts_rank(d.tsv, tq.query)::float
  from poc.doc d, tq
  where tq.query is not null
    and numnode(tq.query) > 0
    and d.tsv @@ tq.query
  order by ts_rank(d.tsv, tq.query) desc, char_length(d.body), d.id
  limit k
$$;

-- ---------------------------------------------------------------------------
-- 方案 C：纯 SQL 字符 bigram
-- ---------------------------------------------------------------------------
-- 排序分 = coverage = |查询 n-gram ∩ 文档 n-gram| / |查询 n-gram|
-- 用 coverage 而不是 Jaccard：短查询 vs 长商品名时 Jaccard 会被文档长度惩罚，
-- 而「只打一个词根」恰恰是最常见的真实行为。
-- 同长 coverage 再按文档长度升序（越短越具体）打破平局。
create or replace function poc.search_bigram(
  q text, k int default 10, min_coverage float default 0.6)
returns table (id int, rnk int, score float)
language sql stable as $$
  with qg as (select poc.bigrams(q) as g),
  scored as (
    select d.id,
           char_length(d.body) as len,
           (select count(*) from unnest(qg.g) x where x = any(d.grams))::float
             / nullif(cardinality(qg.g), 0) as coverage
    from poc.doc d, qg
    where d.grams && qg.g          -- GIN 索引可用的前置过滤
  )
  select s.id,
         (row_number() over (order by s.coverage desc, s.len, s.id))::int,
         s.coverage::float
  from scored s
  where s.coverage >= min_coverage
  order by s.coverage desc, s.len, s.id
  limit k
$$;

-- 方案 C+：bigram + 同义词/拉丁转写展开
create or replace function poc.search_bigram_syn(
  q text, k int default 10, min_coverage float default 0.6)
returns table (id int, rnk int, score float)
language sql stable as $$
  -- 展开后按「原查询」与「每个同义词」分别算 coverage，取最大值。
  -- 不能把展开结果拼成一个长串再算 coverage：那样 n-gram 集合被稀释，
  -- 反而把分数拉低。
  with terms as (
    select poc.norm(q) as t
    union
    select s.canonical
    from poc.synonym s
    where case
            when poc.norm(s.term) ~ '^[a-z0-9 ]+$'
              then poc.norm(q) ~ ('\m' || poc.norm(s.term) || '\M')
            else poc.norm(q) like '%' || poc.norm(s.term) || '%'
          end
  ),
  scored as (
    select d.id,
           char_length(d.body) as len,
           max((select count(*) from unnest(poc.bigrams(t.t)) x
                where x = any(d.grams))::float
               / nullif(cardinality(poc.bigrams(t.t)), 0)) as coverage
    from poc.doc d, terms t
    where d.grams && poc.bigrams(t.t)
    group by d.id, char_length(d.body)
  )
  select s.id,
         (row_number() over (order by s.coverage desc, s.len, s.id))::int,
         s.coverage::float
  from scored s
  where s.coverage >= min_coverage
  order by s.coverage desc, s.len, s.id
  limit k
$$;
