-- CMP-19 / P0-4 — 方案 F：pg_trgm 粗筛 + bigram 精排（两段式）
--
-- 这个方案是被前面的实测数据逼出来的，不是拍脑袋加的：
--
--   * 方案 C（纯 SQL bigram）准确率最高，但 10 万商品 p95 = 694 ms。
--     根因：`&&` 粗筛返回最多 6.4 万候选，再在 SQL 里逐行算 coverage，O(候选数)。
--   * 方案 D（pg_trgm word_similarity）p95 = 55 ms，快 12.6 倍，
--     但高棉语拼写变体 km-variant 只有 0.500（bigram 是 1.000）。
--
-- 两者的强项正好互补：
--   第 1 段 用 pg_trgm GIN 索引把候选压到 N 条（C 实现，走索引，快）
--   第 2 段 只对这 N 条算 bigram coverage 精排（准，且 O(N) 而非 O(全表)）
--
-- 关键参数：粗筛阈值要**放低**（默认 0.6 会直接把拼写变体挡在门外），
-- 靠第 2 段的 coverage 保证精度。

set check_function_bodies = off;

-- ---------------------------------------------------------------------------
-- 准确率评测版（100 商品）
-- ---------------------------------------------------------------------------
create or replace function poc.search_rerank(
  q text, k int default 10,
  cand int default 300,
  trgm_floor float default 0.3,
  min_coverage float default 0.5)
returns table (id int, rnk int, score float)
language plpgsql stable as $$
begin
  return query
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
  -- 第 0 层：子串精确命中，永远优先
  exact as (
    select d.id, char_length(d.body) as len
    from poc.doc d, terms t
    where d.body like '%' || t.t || '%'
  ),
  -- 第 1 段：pg_trgm 粗筛候选（阈值放低，宁滥勿缺）
  candidates as (
    select distinct d.id, d.body, d.grams, char_length(d.body) as len
    from poc.doc d, terms t
    where word_similarity(t.t, d.body) >= trgm_floor
    limit cand
  ),
  -- 第 2 段：只对候选集算 bigram coverage 精排
  reranked as (
    select c.id, c.len,
           max((select count(*) from unnest(poc.bigrams(t.t)) x
                where x = any(c.grams))::float
               / nullif(cardinality(poc.bigrams(t.t)), 0)) as coverage
    from candidates c, terms t
    group by c.id, c.len
  ),
  merged as (
    select e.id, 0 as tier, 1.0::float as sc, e.len from exact e
    union all
    select r.id, 1, r.coverage, r.len
    from reranked r
    where r.coverage >= min_coverage
      -- 必须限定别名：id 同时是本函数的 OUT 参数名，裸写会被 plpgsql 判为歧义
      and r.id not in (select e2.id from exact e2)
  )
  select m.id,
         (row_number() over (order by m.tier, m.sc desc, m.len, m.id))::int,
         m.sc
  from merged m
  order by m.tier, m.sc desc, m.len, m.id
  limit k;
end
$$;

-- ---------------------------------------------------------------------------
-- 规模化版（10 万商品），用于延迟实测
-- ---------------------------------------------------------------------------
create or replace function poc.search_scale_rerank(
  q text, k int default 10,
  cand int default 300,
  trgm_floor float default 0.3,
  min_coverage float default 0.5)
returns table (id int, rnk int, score float)
language plpgsql stable as $$
begin
  -- 关键：必须用 `<%` 运算符，不能写 word_similarity(...) >= x。
  -- 实测（EXPLAIN ANALYZE）：
  --   word_similarity(...) >= x  -> Seq Scan，10 万行逐行算相似度
  --   q <% body                  -> Bitmap Index Scan on idx_scale_trgm
  -- `<%` 的判定阈值只能走 GUC，这里按参数在事务内局部设置。
  perform set_config('pg_trgm.word_similarity_threshold', trgm_floor::text, true);

  return query
  with candidates as (
    select d.id, d.grams, char_length(d.body) as len
    from poc.doc_scale d
    where poc.norm(q) <% d.body
    limit cand
  ),
  reranked as (
    select c.id, c.len,
           (select count(*) from unnest(poc.bigrams(q)) x where x = any(c.grams))::float
             / nullif(cardinality(poc.bigrams(q)), 0) as coverage
    from candidates c
  )
  select r.id,
         (row_number() over (order by r.coverage desc, r.len, r.id))::int,
         r.coverage::float
  from reranked r
  where r.coverage >= min_coverage
  order by r.coverage desc, r.len, r.id
  limit k;
end
$$;

create or replace function poc.bench_scale_rerank(
  k int default 10, reps int default 2, cand int default 300)
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
      perform count(*) from poc.search_scale_rerank(r.q, k, cand);
      dur := extract(epoch from (clock_timestamp() - t0)) * 1000.0;
      arr := arr || dur;
    end loop;
  end loop;

  target := 'rerank(cand=' || cand || ')'; samples := cardinality(arr);
  select round(avg(x)::numeric, 2),
         round((percentile_cont(0.50) within group (order by x))::numeric, 2),
         round((percentile_cont(0.95) within group (order by x))::numeric, 2),
         round(max(x)::numeric, 2)
    into avg_ms, p50_ms, p95_ms, max_ms
  from unnest(arr) as t(x);
  return next;
end
$$;
