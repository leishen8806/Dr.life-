-- CMP-19 / P0-4 — 方案 E：混合（子串 ∪ bigram+同义词）
--
-- 由 30_search.sql 的实测结果反推出来的，修两个已定位的真实缺陷：
--
-- 缺陷 1：单字 CJK 查询（如「洗」）必然 0 召回。
--         根因：查询长度 < n，bigram 集合是 {洗}，而文档 bigram 是 {洗衣,衣机,...}，
--         两者永不相交。**这不是调阈值能解决的，是 n-gram 的结构性缺陷。**
--         对策：子串精确匹配兜底（单字查询走 LIKE）。
--
-- 缺陷 2：中文同音错别字（电风善 vs 电风扇）只共享 1/2 个 bigram，coverage=0.5，
--         被 0.6 阈值挡掉。属阈值问题，见 50_threshold_sweep.sql 的敏感性分析。
--
-- 排序策略：子串精确命中**永远排在**模糊命中之前（精确优先，避免模糊结果
-- 把精确结果挤到后面），层内再按原有规则排序。

create or replace function poc.search_hybrid(
  q text, k int default 10, min_coverage float default 0.6)
returns table (id int, rnk int, score float)
language sql stable as $$
  with exact as (
    select d.id, char_length(d.body) as len
    from poc.doc d
    where d.body like '%' || poc.norm(q) || '%'
  ),
  fuzzy as (
    select s.id, s.score
    from poc.search_bigram_syn(q, k, min_coverage) s
  ),
  merged as (
    -- tier 0 = 子串精确命中，tier 1 = 模糊命中
    select e.id, 0 as tier, 1.0::float as score, e.len
    from exact e
    union all
    select f.id, 1, f.score, char_length(d.body)
    from fuzzy f join poc.doc d on d.id = f.id
    where f.id not in (select id from exact)
  )
  select m.id,
         (row_number() over (order by m.tier, m.score desc, m.len, m.id))::int,
         m.score
  from merged m
  order by m.tier, m.score desc, m.len, m.id
  limit k
$$;

-- 同义词展开版的混合方案也要能被 poc.evaluate 调用，名字对齐白名单。
-- （evaluate 的白名单在 90_eval.sql 里，这里追加 hybrid 两个名字。）
