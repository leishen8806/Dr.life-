-- CMP-19 / P0-4 — coverage 阈值敏感性分析
--
-- 为什么要做这个：min_coverage 是方案 C/E 唯一的可调旋钮，直接决定
-- 召回率与误召回（噪声）的平衡。只报一个阈值下的漂亮数字是不负责任的。
--
-- 同时报告「平均返回条数」作为噪声代理指标：阈值越低，召回越高，
-- 但返回的无关商品越多，用户越难找到想要的东西。

create or replace function poc.sweep(approach text, k int default 10)
returns table (
  min_coverage numeric, avg_recall numeric, first_hit_rate numeric, avg_returned numeric
)
language plpgsql stable as $$
declare
  th float;
begin
  if approach not in ('bigram', 'bigram_syn', 'hybrid') then
    raise exception 'sweep only supports coverage-based approaches, got %', approach;
  end if;

  foreach th in array array[0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 1.0] loop
    min_coverage := th::numeric;
    execute format($q$
      with per_query as (
        select qq.expect_ids,
               (select coalesce(array_agg(s.id order by s.rnk), array[]::int[])
                  from poc.search_%s(qq.q, $1, $2) s) as got,
               (select min(s.id) filter (where s.rnk = 1)
                  from poc.search_%s(qq.q, $1, $2) s) as top1
        from poc.query qq
      )
      select round(avg((select count(*) from unnest(got) g
                        where g = any(expect_ids))::numeric
                       / nullif(cardinality(expect_ids), 0)), 3),
             round(avg(case when top1 is not null and top1 = any(expect_ids)
                            then 1 else 0 end)::numeric, 3),
             round(avg(cardinality(got))::numeric, 2)
      from per_query
    $q$, approach, approach)
    into avg_recall, first_hit_rate, avg_returned
    using k, th;

    return next;
  end loop;
end
$$;
