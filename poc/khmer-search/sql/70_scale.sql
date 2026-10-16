-- CMP-19 / P0-4 — 规模化压测
--
-- 为什么必须做：100 个商品的索引只有 40–72 kB、查询在亚毫秒级，
-- **这个量级的数字对生产选型毫无参考价值**。准确率评测用 100 个商品足够
-- （有人工标注），但索引大小与延迟必须在真实量级上测。
--
-- 做法：用 100 个真实商品名 × 规格/品牌修饰词 交叉生成 N 条商品，
-- 三语字段都参与，n-gram 分布与真实目录接近。

-- 检索函数在 doc_scale 建表之前就要定义（本文件按顺序执行），
-- 关掉创建时的函数体校验；SQL 函数体是延迟解析的，这样也不会登记成
-- doc_scale 的依赖对象，重建表时不会被级联删掉。
set check_function_bodies = off;

-- 修饰词（三语），用于交叉放大语料
drop table if exists poc.modifier cascade;
create table poc.modifier (i int primary key, zh text, en text, km text);

insert into poc.modifier (i, zh, en, km)
select g,
       (array['大号','小号','加厚','便携','家用','商用','套装','双层','升级','静音'])[1 + (g % 10)]
         || (g / 10 + 1)::text || '型',
       (array['large','small','thick','portable','home','commercial','set','double','upgraded','silent'])[1 + (g % 10)]
         || ' v' || (g / 10 + 1)::text,
       (array['ធំ','តូច','ក្រាស់','ងាយស្រួលយក','ប្រើក្នុងផ្ទះ','ពាណិជ្ជកម្ម','ឈុត','ទ្វេ','កម្រិតខ្ពស់','គ្មានសូរ'])[1 + (g % 10)]
         || ' ' || (g / 10 + 1)::text
from generate_series(0, 999) g;

-- 建规模化检索表：size = 100 * mult（mult <= 1000）
create or replace function poc.build_scale(mult int)
returns table (rows_built bigint, build_ms numeric)
language plpgsql as $$
declare t0 timestamptz;
begin
  t0 := clock_timestamp();

  -- 不用 cascade：索引随表自动删除，而检索函数不应被级联删掉。
  drop table if exists poc.doc_scale;

  create table poc.doc_scale as
  select (row_number() over ())::int                                as id,
         poc.norm(p.name_zh || m.zh || ' ' ||
                  p.name_en || ' ' || m.en || ' ' ||
                  p.name_km || ' ' || m.km)                         as body,
         to_tsvector('simple',
           poc.norm(p.name_zh || m.zh || ' ' ||
                    p.name_en || ' ' || m.en || ' ' ||
                    p.name_km || ' ' || m.km))                      as tsv,
         poc.bigrams(p.name_zh || m.zh || ' ' ||
                     p.name_en || ' ' || m.en || ' ' ||
                     p.name_km || ' ' || m.km)                      as grams
  from poc.product p
  cross join (select * from poc.modifier where i < mult) m;

  alter table poc.doc_scale add primary key (id);

  build_ms   := round((extract(epoch from (clock_timestamp() - t0)) * 1000)::numeric, 1);
  rows_built := (select count(*) from poc.doc_scale);
  return next;
end
$$;

-- 分别建两个 GIN 索引并计时/计大小
create or replace function poc.build_scale_indexes()
returns table (object text, build_ms numeric, size_pretty text, size_bytes bigint)
language plpgsql as $$
declare t0 timestamptz; ms numeric;
begin
  t0 := clock_timestamp();
  create index idx_scale_tsv on poc.doc_scale using gin (tsv);
  ms := round((extract(epoch from (clock_timestamp() - t0)) * 1000)::numeric, 1);
  object := 'idx_scale_tsv'; build_ms := ms;
  size_bytes := pg_relation_size('poc.idx_scale_tsv');
  size_pretty := pg_size_pretty(size_bytes);
  return next;

  t0 := clock_timestamp();
  create index idx_scale_grams on poc.doc_scale using gin (grams array_ops);
  ms := round((extract(epoch from (clock_timestamp() - t0)) * 1000)::numeric, 1);
  object := 'idx_scale_grams'; build_ms := ms;
  size_bytes := pg_relation_size('poc.idx_scale_grams');
  size_pretty := pg_size_pretty(size_bytes);
  return next;

  analyze poc.doc_scale;
end
$$;

-- 规模化版的检索函数（打到 doc_scale 上）
create or replace function poc.search_scale_bigram(
  q text, k int default 10, min_coverage float default 0.5)
returns table (id int, rnk int, score float)
language sql stable as $$
  with qg as (select poc.bigrams(q) as g),
  scored as (
    select d.id, char_length(d.body) as len,
           (select count(*) from unnest(qg.g) x where x = any(d.grams))::float
             / nullif(cardinality(qg.g), 0) as coverage
    from poc.doc_scale d, qg
    where d.grams && qg.g
  )
  select s.id,
         (row_number() over (order by s.coverage desc, s.len, s.id))::int,
         s.coverage::float
  from scored s
  where s.coverage >= min_coverage
  order by s.coverage desc, s.len, s.id
  limit k
$$;

create or replace function poc.search_scale_fts(q text, k int default 10)
returns table (id int, rnk int, score float)
language sql stable as $$
  with tq as (select plainto_tsquery('simple', poc.norm(q)) as query)
  select d.id,
         (row_number() over (order by ts_rank(d.tsv, tq.query) desc, d.id))::int,
         ts_rank(d.tsv, tq.query)::float
  from poc.doc_scale d, tq
  where tq.query is not null and numnode(tq.query) > 0 and d.tsv @@ tq.query
  order by ts_rank(d.tsv, tq.query) desc, d.id
  limit k
$$;

-- 规模化延迟实测
create or replace function poc.bench_scale(which text, k int default 10, reps int default 3)
returns table (
  target text, samples int,
  avg_ms numeric, p50_ms numeric, p95_ms numeric, max_ms numeric
)
language plpgsql as $$
declare
  r record; i int; t0 timestamptz; dur double precision;
  arr double precision[] := '{}';
begin
  if which not in ('bigram', 'fts') then
    raise exception 'unknown: %', which;
  end if;

  for i in 1..reps loop
    for r in select qq.q from poc.query qq order by qq.qid loop
      t0 := clock_timestamp();
      execute format('select count(*) from poc.search_scale_%s($1, $2)', which)
        using r.q, k;
      dur := extract(epoch from (clock_timestamp() - t0)) * 1000.0;
      arr := arr || dur;
    end loop;
  end loop;

  target := which; samples := cardinality(arr);
  select round(avg(x)::numeric, 2),
         round((percentile_cont(0.50) within group (order by x))::numeric, 2),
         round((percentile_cont(0.95) within group (order by x))::numeric, 2),
         round(max(x)::numeric, 2)
    into avg_ms, p50_ms, p95_ms, max_ms
  from unnest(arr) as t(x);
  return next;
end
$$;
