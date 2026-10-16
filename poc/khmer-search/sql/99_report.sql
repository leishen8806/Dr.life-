-- CMP-19 / P0-4 — 汇总报告：一次性产出 RESULT.md 需要的全部真实数字
-- 由 run.ps1 / run.sh 调用。前置：00/10/20/30/40/50/60/80 已加载。

\pset border 2
\set ON_ERROR_STOP on

\echo
\echo ############################################################
\echo # 0. 环境
\echo ############################################################
select version();
select extname, extversion from pg_extension
where extname in ('pg_trgm', 'unaccent') order by extname;
select current_setting('server_encoding') as server_encoding;

\echo
\echo ############################################################
\echo # 1. 语料与评测集
\echo ############################################################
select (select count(*) from poc.product) as products,
       (select count(*) from poc.query)   as queries,
       (select count(*) from poc.synonym) as synonyms;

\echo -- 高棉语译名可信度分布（needs-native-review = 需母语者复核）
select km_confidence, count(*) from poc.product group by 1 order by 2 desc;

\echo -- 评测集分类
select class, count(*) as queries from poc.query group by 1 order by 1;

\echo
\echo -- ground truth 自检：子串类查询的人工标注必须等于真实包含集合
with actual as (
  select q.qid, q.class, q.expect_ids,
         coalesce(array_agg(p.id order by p.id)
                  filter (where p.name_zh like '%'||q.q||'%'
                             or p.name_en ilike '%'||q.q||'%'
                             or p.name_km like '%'||q.q||'%'), array[]::int[]) as contains_ids
  from poc.query q left join poc.product p on true
  where q.class in ('km-partial','zh-partial','km-exact','zh-exact','en-exact')
  group by 1,2,3
)
select count(*) as checked,
       count(*) filter (where expect_ids = contains_ids) as matching,
       count(*) filter (where expect_ids <> contains_ids) as mismatching
from actual;

\echo
\echo ############################################################
\echo # 2. 准确率总分（100 商品 / 50 查询 / k=10）
\echo ############################################################
select * from poc.summary_total('ilike')      union all
select * from poc.summary_total('fts')        union all
select * from poc.summary_total('trgm')       union all
select * from poc.summary_total('trgm_word')  union all
select * from poc.summary_total('trgm_syn')   union all
select * from poc.summary_total('bigram')     union all
select * from poc.summary_total('bigram_syn') union all
select * from poc.summary_total('hybrid')     union all
select * from poc.summary_total('rerank');

\echo
\echo ############################################################
\echo # 3. 分类召回率 recall@10
\echo ############################################################
select c.class, c.queries,
       i.avg_recall  as ilike,
       f.avg_recall  as fts,
       t.avg_recall  as trgm_sim,
       tw.avg_recall as trgm_word,
       b.avg_recall  as bigram,
       h.avg_recall  as hybrid,
       rr.avg_recall as rerank
from poc.summary_by_class('bigram') c
join poc.summary_by_class('ilike')      i  using (class)
join poc.summary_by_class('fts')        f  using (class)
join poc.summary_by_class('trgm')       t  using (class)
join poc.summary_by_class('trgm_word')  tw using (class)
join poc.summary_by_class('bigram')     b  using (class)
join poc.summary_by_class('hybrid')     h  using (class)
join poc.summary_by_class('rerank')     rr using (class)
order by c.class;

\echo
\echo ############################################################
\echo # 4. 分类首位命中率 first-hit
\echo ############################################################
select c.class, c.queries,
       i.first_hit_rate  as ilike,
       f.first_hit_rate  as fts,
       tw.first_hit_rate as trgm_word,
       b.first_hit_rate  as bigram,
       h.first_hit_rate  as hybrid,
       rr.first_hit_rate as rerank
from poc.summary_by_class('bigram') c
join poc.summary_by_class('ilike')      i  using (class)
join poc.summary_by_class('fts')        f  using (class)
join poc.summary_by_class('trgm_word')  tw using (class)
join poc.summary_by_class('bigram')     b  using (class)
join poc.summary_by_class('hybrid')     h  using (class)
join poc.summary_by_class('rerank')     rr using (class)
order by c.class;

\echo
\echo ############################################################
\echo # 5. 推荐方案（rerank）剩余失败用例 —— 不藏短板
\echo ############################################################
select qid, class, q, n_expect, n_hit, recall, first_hit
from poc.evaluate('rerank', 10) where recall < 1.0 order by qid;

\echo
\echo -- 为什么中文同音错别字无法靠字符 n-gram 救：共享 bigram 数
select q, correct,
       poc.bigrams(q) as typo_grams, poc.bigrams(correct) as correct_grams,
       cardinality(array(select unnest(poc.bigrams(q))
                         intersect select unnest(poc.bigrams(correct)))) as shared_bigrams
from (values ('电风善','电风扇'), ('牙高','牙膏'), ('雨散','雨伞')) v(q, correct);

\echo
\echo ############################################################
\echo # 6. coverage 阈值敏感性（avg_returned = 噪声代理指标）
\echo ############################################################
\echo -- hybrid
select * from poc.sweep('hybrid', 10);

\echo
\echo ############################################################
\echo # 7. 真实高棉语查询样例（抽查，看实际返回）
\echo ############################################################
\echo -- 7.1 拼写变体 ទូរសព្ទ（漏写 ័，正确为 ទូរស័ព្ទ）
select r.rnk, r.id, p.name_km, p.name_zh, round(r.score::numeric,3) as score
from poc.search_rerank('ទូរសព្ទ', 5) r join poc.product p on p.id = r.id order by r.rnk;

\echo -- 7.2 无空格连写 ម៉ាស៊ីនលាងសម្លៀកបំពាក់（商品名含空格）
select r.rnk, r.id, p.name_km, p.name_zh, round(r.score::numeric,3) as score
from poc.search_rerank('ម៉ាស៊ីនលាងសម្លៀកបំពាក់', 5) r join poc.product p on p.id = r.id order by r.rnk;

\echo -- 7.3 词根部分匹配 សាប៊ូ（肥皂）
select r.rnk, r.id, p.name_km, p.name_zh, round(r.score::numeric,3) as score
from poc.search_rerank('សាប៊ូ', 10) r join poc.product p on p.id = r.id order by r.rnk;

\echo -- 7.4 拉丁转写 sabou（无高棉文键盘），靠同义词表救回
select r.rnk, r.id, p.name_km, p.name_zh, round(r.score::numeric,3) as score
from poc.search_rerank('sabou', 10) r join poc.product p on p.id = r.id order by r.rnk;

\echo -- 7.5 下标辅音顺序颠倒 កាហេ្វបំពង（正确为 កាហ្វេបំពង）
select r.rnk, r.id, p.name_km, p.name_zh, round(r.score::numeric,3) as score
from poc.search_rerank('កាហេ្វបំពង', 5) r join poc.product p on p.id = r.id order by r.rnk;

\echo
\echo ############################################################
\echo # 8. 索引大小（100 商品语料）
\echo ############################################################
select * from poc.index_sizes;
