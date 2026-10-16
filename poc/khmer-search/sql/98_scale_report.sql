-- CMP-19 / P0-4 — 规模化报告（索引大小 + 查询延迟）
-- 单独一个文件，因为建 10 万行语料要 ~35 秒，不该每次跑准确率都等它。
-- 用法：psql -v mult=1000 -f 98_scale_report.sql   （mult=1000 -> 10 万行）

\pset border 2
\set ON_ERROR_STOP on
\if :{?mult}
\else
  \set mult 1000
\endif

\echo
\echo ############################################################
\echo # 规模化压测：商品数 = 100 * :mult
\echo ############################################################
\echo -- 为什么要做：100 个商品索引只有几十 kB、查询亚毫秒，
\echo -- 那个量级的数字对生产选型没有参考价值。

select * from poc.build_scale(:mult);
select * from poc.build_scale_indexes();
select * from poc.build_scale_trgm_index();

\echo
\echo -- 表与索引实际大小
select (select count(*) from poc.doc_scale)                       as rows,
       pg_size_pretty(pg_relation_size('poc.doc_scale'))           as table_size,
       pg_size_pretty(pg_relation_size('poc.idx_scale_tsv'))       as gin_tsv,
       pg_size_pretty(pg_relation_size('poc.idx_scale_grams'))     as gin_bigram,
       pg_size_pretty(pg_relation_size('poc.idx_scale_trgm'))      as gin_trgm;

\echo
\echo -- 查询延迟（全部 50 条查询，毫秒）
select * from poc.bench_scale('fts', 10, 2)   union all
select * from poc.bench_scale('bigram', 10, 2);
select * from poc.bench_scale_trgm(10, 2);
select * from poc.bench_scale_rerank(10, 3, 100) union all
select * from poc.bench_scale_rerank(10, 3, 300) union all
select * from poc.bench_scale_rerank(10, 3, 1000);

\echo
-- 注意：psql 的 \echo 会把反引号当 shell 命令执行，所以这里不能写反引号。
\echo -- 关键证据：<% 运算符走索引，word_similarity() 函数式写法不走索引
set pg_trgm.word_similarity_threshold = 0.3;
\echo ---- 运算符写法 ----
explain (analyze, costs off, timing off, summary off)
select count(*) from poc.doc_scale d where poc.norm('សាប៊ូ') <% d.body;
\echo ---- 函数写法（反例）----
explain (analyze, costs off, timing off, summary off)
select count(*) from poc.doc_scale d
where word_similarity(poc.norm('សាប៊ូ'), d.body) >= 0.3;

\echo
\echo -- LIKE '%...%' 也能被 gin_trgm_ops 加速（精确子串层因此可索引）
explain (analyze, costs off, timing off, summary off)
select count(*) from poc.doc_scale d where d.body like '%សាប៊ូ%';
