-- CMP-19 / P0-4 高棉语搜索 PoC — 表结构与规范化层
-- 只在一次性 PoC 容器里跑，不属于生产迁移，不碰 catalog 表结构。
--
-- 设计约束：
--   * 本文件**不使用任何扩展**，可在未获批扩展的情况下独立运行。
--   * pg_trgm / unaccent 相关内容集中在 60_approach_d_trgm.sql，受审批门控。

drop schema if exists poc cascade;
create schema poc;

-- ---------------------------------------------------------------------------
-- 1. 商品与评测集表
-- ---------------------------------------------------------------------------

create table poc.product (
  id             int primary key,
  category       text not null,
  name_zh        text not null,
  name_en        text not null,
  name_km        text not null,
  -- 高棉语译名可信度：high = 常用词，我有把握；
  -- needs-native-review = 需要母语者复核，不得当成定稿。
  km_confidence  text not null
                 check (km_confidence in ('high', 'needs-native-review'))
);

-- 同义词表（任务表交付物点名要求）。双向展开在查询侧完成。
create table poc.synonym (
  id        serial primary key,
  lang      text not null check (lang in ('zh', 'en', 'km')),
  term      text not null,
  canonical text not null
);

-- 评测集：每条查询带人工标注的相关商品 id（ground truth）。
create table poc.query (
  qid        int primary key,
  lang       text not null check (lang in ('zh', 'en', 'km', 'mixed')),
  -- 查询类别，用于分类统计召回率
  class      text not null,
  q          text not null,
  expect_ids int[] not null,
  note       text
);

-- ---------------------------------------------------------------------------
-- 2. 高棉语规范化
-- ---------------------------------------------------------------------------
-- 方法学声明（重要，避免自欺）：
-- 规范化**只做通用、与评测集无关的处理**——去不可见字符、统一数字、统一大小写、
-- 压空白。**故意不**把评测集里那些拼写变体（如 ទូរស័ព្ទ/ទូរសព្ទ）写成折叠规则，
-- 否则等于把答案抄进预处理里，召回率会被人为抬高。拼写变体必须由模糊匹配算法
-- 自己扛住，这才是真实线上情形。

-- 用户在高棉语里会不一致地输入的零宽/不可见字符：
--   U+200B ZWSP（高棉语常用作词边界，但经常被省略）
--   U+200C ZWNJ、U+200D ZWJ、U+00A0 NBSP、U+FEFF BOM
create or replace function poc.strip_invisible(t text) returns text
language sql immutable strict as $$
  select regexp_replace(t, '[\u200B\u200C\u200D\u00A0\uFEFF]', '', 'g')
$$;

-- 高棉语数字 ០..៩ (U+17E0..U+17E9) -> ASCII 0..9
create or replace function poc.fold_digits(t text) returns text
language sql immutable strict as $$
  select translate(t, '០១២៣៤៥៦៧៨៩', '0123456789')
$$;

-- 统一规范化入口：三语共用
create or replace function poc.norm(t text) returns text
language sql immutable strict as $$
  select btrim(
           regexp_replace(
             lower(poc.fold_digits(poc.strip_invisible(t))),
             '\s+', ' ', 'g'))
$$;

-- ---------------------------------------------------------------------------
-- 3. 字符 n-gram（纯 SQL，无扩展）
-- ---------------------------------------------------------------------------
-- 按 Unicode 码位切分。对高棉语而言，码位 n-gram 会把基辅音与其元音符号/下标
-- 辅音拆开，这正好让「少打一个符号」的拼写变体仍有大量公共 n-gram，
-- 从而优雅降级——这是本方案能扛变体的根本原因。
-- 已知局限：码位 n-gram 不是字素簇（grapheme cluster）n-gram，排序精度略差，
-- 见 RESULT.md「已知风险」。

create or replace function poc.ngrams(t text, n int) returns text[]
language sql immutable strict as $$
  with c as (
    select poc.norm(t) as s
  ), ch as (
    select s, regexp_split_to_array(s, '') as a from c
  )
  select case
           when s = '' then array[]::text[]
           when char_length(s) < n then array[s]
           else (
             select array_agg(distinct substr(s, i, n) order by substr(s, i, n))
             from generate_series(1, char_length(s) - n + 1) as g(i)
           )
         end
  from ch
$$;

-- 本 PoC 固定用 n=2（bigram）。高棉语词素短，bigram 比 trigram 召回更高；
-- trigram 的对照数据见 RESULT.md。
create or replace function poc.bigrams(t text) returns text[]
language sql immutable strict as $$
  select poc.ngrams(t, 2)
$$;
