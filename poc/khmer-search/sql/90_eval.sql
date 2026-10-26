-- CMP-19 / P0-4 — 评测脚本：召回率 / 首位命中率 / 延迟 / 索引大小

-- ---------------------------------------------------------------------------
-- 逐查询评测
-- ---------------------------------------------------------------------------
-- recall@k     = |取回@k ∩ 标注相关| / |标注相关|
-- first_hit    = 排名第 1 的结果是否属于标注相关集（1/0）
create or replace function poc.evaluate(approach text, k int default 10)
returns table (
  qid int, class text, q text,
  n_expect int, n_hit int, recall float, first_hit int
)
language plpgsql stable as $$
declare
  r record;
  got  int[];
  top1 int;
begin
  -- 白名单校验：approach 要拼进动态 SQL，必须限定取值，防注入。
  if approach not in ('ilike', 'fts', 'bigram', 'bigram_syn', 'hybrid', 'trgm', 'trgm_word', 'trgm_syn', 'rerank') then
    raise exception 'unknown approach: %', approach;
  end if;

  for r in select qq.qid, qq.class, qq.q, qq.expect_ids from poc.query qq order by qq.qid loop
    -- 单次调用取回全部结果：array_agg 拿 top-k 列表，
    -- min(...) filter (rnk=1) 拿首位，避免重复执行检索函数。
    execute format(
      'select coalesce(array_agg(s.id order by s.rnk), array[]::int[]),
              min(s.id) filter (where s.rnk = 1)
       from poc.search_%s($1, $2) s', approach)
      into got, top1 using r.q, k;

    qid       := r.qid;
    class     := r.class;
    q         := r.q;
    n_expect  := cardinality(r.expect_ids);
    n_hit     := (select count(*) from unnest(got) g where g = any(r.expect_ids));
    recall    := n_hit::float / nullif(n_expect, 0);
    first_hit := case when top1 is not null and top1 = any(r.expect_ids) then 1 else 0 end;
    return next;
  end loop;
end
$$;

-- ---------------------------------------------------------------------------
-- 按类别汇总
-- ---------------------------------------------------------------------------
create or replace function poc.summary_by_class(approach text, k int default 10)
returns table (class text, queries int, avg_recall numeric, first_hit_rate numeric)
language sql stable as $$
  select e.class,
         count(*)::int,
         round(avg(e.recall)::numeric, 3),
         round(avg(e.first_hit)::numeric, 3)
  from poc.evaluate(approach, k) e
  group by e.class
  order by e.class
$$;

-- 总分
create or replace function poc.summary_total(approach text, k int default 10)
returns table (approach_name text, queries int, avg_recall numeric, first_hit_rate numeric)
language sql stable as $$
  select approach,
         count(*)::int,
         round(avg(e.recall)::numeric, 3),
         round(avg(e.first_hit)::numeric, 3)
  from poc.evaluate(approach, k) e
$$;

-- ---------------------------------------------------------------------------
-- 延迟实测
-- ---------------------------------------------------------------------------
-- 对全部 50 条查询各跑 reps 次，报告单次查询的 平均 / p50 / p95 / max（毫秒）。
-- 用 clock_timestamp()（墙钟，不随事务冻结）。
create or replace function poc.bench(approach text, k int default 10, reps int default 20)
returns table (
  approach_name text, samples int,
  avg_ms numeric, p50_ms numeric, p95_ms numeric, max_ms numeric
)
language plpgsql as $$
declare
  r record; i int; t0 timestamptz; dur double precision;
  arr double precision[] := '{}';
begin
  if approach not in ('ilike', 'fts', 'bigram', 'bigram_syn', 'hybrid', 'trgm', 'trgm_word', 'trgm_syn', 'rerank') then
    raise exception 'unknown approach: %', approach;
  end if;

  for i in 1..reps loop
    for r in select qq.q from poc.query qq order by qq.qid loop
      t0 := clock_timestamp();
      execute format('select count(*) from poc.search_%s($1, $2)', approach)
        using r.q, k;
      dur := extract(epoch from (clock_timestamp() - t0)) * 1000.0;
      arr := arr || dur;
    end loop;
  end loop;

  approach_name := approach;
  samples := cardinality(arr);
  select round(avg(x)::numeric, 3),
         round((percentile_cont(0.50) within group (order by x))::numeric, 3),
         round((percentile_cont(0.95) within group (order by x))::numeric, 3),
         round(max(x)::numeric, 3)
    into avg_ms, p50_ms, p95_ms, max_ms
  from unnest(arr) as t(x);
  return next;
end
$$;

-- ---------------------------------------------------------------------------
-- 索引与表大小
-- ---------------------------------------------------------------------------
create or replace view poc.index_sizes as
select c.relname                                as object,
       case c.relkind when 'r' then 'table' when 'i' then 'index' end as kind,
       pg_size_pretty(pg_relation_size(c.oid))  as size_pretty,
       pg_relation_size(c.oid)                  as size_bytes
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = 'poc'
  and c.relkind in ('r', 'i')
order by pg_relation_size(c.oid) desc;
