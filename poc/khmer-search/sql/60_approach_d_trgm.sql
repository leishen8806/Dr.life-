-- CMP-19 / P0-4 — 方案 D：pg_trgm
--
-- 【审批状态】已获董事会批准
--   审批单 5512ff90-e25b-41d5-9f20-46c660dd86ad，approved @ 2026-10-16T09:21:12Z
--   批准内容：在一次性 PoC 容器内 CREATE EXTENSION pg_trgm / unaccent。
--   两个扩展已随 postgres:16-alpine 镜像自带，无需联网下载。
--
-- 为什么需要这一组对照：方案 C（纯 SQL bigram）在 10 万商品上 p95 已到 694 ms，
-- 根因是 `&&` 粗筛会返回最多 6.4 万候选行，再在 SQL 里逐行算 coverage。
-- pg_trgm 把「相似度过滤 + 打分」下推到 C 实现与 GIN 索引内部，
-- 正是针对这个瓶颈的对照方案。

create extension if not exists pg_trgm;
create extension if not exists unaccent;

select extname, extversion from pg_extension where extname in ('pg_trgm', 'unaccent');

-- ---------------------------------------------------------------------------
-- 100 商品准确率评测用索引
-- ---------------------------------------------------------------------------
drop index if exists poc.idx_doc_trgm;
create index idx_doc_trgm on poc.doc using gin (body gin_trgm_ops);
analyze poc.doc;

-- ---------------------------------------------------------------------------
-- 方案 D1：similarity（% 运算符）—— 整串相似度
-- ---------------------------------------------------------------------------
-- 已知局限：similarity() 是整串比较。短查询（只打一个词根）对长商品名的
-- 整串相似度很低，因此 % 对「部分匹配」天生不利。D2 才是对的工具。
create or replace function poc.search_trgm(q text, k int default 10)
returns table (id int, rnk int, score float)
language sql stable as $$
  select d.id,
         (row_number() over (order by similarity(d.body, poc.norm(q)) desc,
                                      char_length(d.body), d.id))::int,
         similarity(d.body, poc.norm(q))::float
  from poc.doc d
  where d.body % poc.norm(q)
  order by similarity(d.body, poc.norm(q)) desc, char_length(d.body), d.id
  limit k
$$;

-- ---------------------------------------------------------------------------
-- 方案 D2：word_similarity（<% 运算符）—— 最佳子区间相似度
-- ---------------------------------------------------------------------------
-- word_similarity(a, b) = a 的三元组集合与 b 中**任意连续区间**的最大相似度。
-- 注意：它按「区间」而非「空格分词」来找最佳匹配段，
-- 所以对**无词间空格的高棉语**同样有效 —— 这是选它的关键理由。
create or replace function poc.search_trgm_word(q text, k int default 10)
returns table (id int, rnk int, score float)
language sql stable as $$
  select d.id,
         (row_number() over (order by word_similarity(poc.norm(q), d.body) desc,
                                      char_length(d.body), d.id))::int,
         word_similarity(poc.norm(q), d.body)::float
  from poc.doc d
  where poc.norm(q) <% d.body
  order by word_similarity(poc.norm(q), d.body) desc, char_length(d.body), d.id
  limit k
$$;

-- ---------------------------------------------------------------------------
-- 方案 D3：word_similarity + 同义词/拉丁转写 + 子串精确优先（与方案 E 同构）
-- ---------------------------------------------------------------------------
create or replace function poc.search_trgm_syn(q text, k int default 10)
returns table (id int, rnk int, score float)
language sql stable as $$
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
  exact as (
    select d.id, char_length(d.body) as len
    from poc.doc d, terms t
    where d.body like '%' || t.t || '%'
  ),
  fuzzy as (
    select d.id, char_length(d.body) as len,
           max(word_similarity(t.t, d.body)) as score
    from poc.doc d, terms t
    where t.t <% d.body
    group by d.id, char_length(d.body)
  ),
  merged as (
    select e.id, 0 as tier, 1.0::float as score, e.len from exact e
    union all
    select f.id, 1, f.score, f.len from fuzzy f
    where f.id not in (select id from exact)
  )
  select m.id,
         (row_number() over (order by m.tier, m.score desc, m.len, m.id))::int,
         m.score
  from merged m
  order by m.tier, m.score desc, m.len, m.id
  limit k
$$;

-- ---------------------------------------------------------------------------
-- 规模化（10 万）延迟对照
-- ---------------------------------------------------------------------------
set check_function_bodies = off;

create or replace function poc.search_scale_trgm_word(q text, k int default 10)
returns table (id int, rnk int, score float)
language sql stable as $$
  select d.id,
         (row_number() over (order by word_similarity(poc.norm(q), d.body) desc, d.id))::int,
         word_similarity(poc.norm(q), d.body)::float
  from poc.doc_scale d
  where poc.norm(q) <% d.body
  order by word_similarity(poc.norm(q), d.body) desc, d.id
  limit k
$$;

create or replace function poc.build_scale_trgm_index()
returns table (object text, build_ms numeric, size_pretty text, size_bytes bigint)
language plpgsql as $$
declare t0 timestamptz;
begin
  drop index if exists poc.idx_scale_trgm;
  t0 := clock_timestamp();
  create index idx_scale_trgm on poc.doc_scale using gin (body gin_trgm_ops);
  build_ms := round((extract(epoch from (clock_timestamp() - t0)) * 1000)::numeric, 1);
  object := 'idx_scale_trgm';
  size_bytes := pg_relation_size('poc.idx_scale_trgm');
  size_pretty := pg_size_pretty(size_bytes);
  analyze poc.doc_scale;
  return next;
end
$$;

create or replace function poc.bench_scale_trgm(k int default 10, reps int default 2)
returns table (
  target text, samples int,
  avg_ms numeric, p50_ms numeric, p95_ms numeric, max_ms numeric
)
language plpgsql as $$
declare
  r record; i int; t0 timestamptz; dur double precision;
  arr double precision[] := '{}';
begin
  for i in 1..reps loop
    for r in select qq.q from poc.query qq order by qq.qid loop
      t0 := clock_timestamp();
      perform count(*) from poc.search_scale_trgm_word(r.q, k);
      dur := extract(epoch from (clock_timestamp() - t0)) * 1000.0;
      arr := arr || dur;
    end loop;
  end loop;

  target := 'trgm_word'; samples := cardinality(arr);
  select round(avg(x)::numeric, 2),
         round((percentile_cont(0.50) within group (order by x))::numeric, 2),
         round((percentile_cont(0.95) within group (order by x))::numeric, 2),
         round(max(x)::numeric, 2)
    into avg_ms, p50_ms, p95_ms, max_ms
  from unnest(arr) as t(x);
  return next;
end
$$;
