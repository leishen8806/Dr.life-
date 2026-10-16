-- CMP-19 / P0-4 — 评测集：50 条查询 + 人工标注 ground truth
--
-- 分类（class）设计覆盖真实柬埔寨用户行为，不是随手凑数：
--   km-exact            高棉语整名精确
--   km-variant          高棉语**真实拼写变体**（漏符号 / 辅音混淆 / 下标辅音顺序颠倒）
--   km-nospace          用户省掉了商品名里的短语空格（高棉语无词间空格，空格位置随意）
--   km-zwsp             查询里夹了零宽空格 U+200B（高棉文输入法常自动插入）
--   km-digit            用户打高棉语数字 ០-៩，商品名里是阿拉伯数字
--   km-partial          只打一个词根（最常见的真实行为）
--   km-romanized        **没装高棉文键盘，直接打拉丁转写**
--   zh-exact/partial/typo   中文整名 / 词根 / 同音错别字
--   en-exact/typo       英文整名 / 拼写错误
--
-- ZWSP 用 U&'\200B' 写入，避免文件里出现看不见的字符。

truncate poc.query;

insert into poc.query (qid, lang, class, q, expect_ids, note) values
-- ============ km-exact (5) ============
( 1, 'km', 'km-exact', 'កង្ហារអគ្គិសនី',   array[11],  '电风扇 整名'),
( 2, 'km', 'km-exact', 'ថ្នាំដុសធ្មេញ',    array[43],  '牙膏 整名'),
( 3, 'km', 'km-exact', 'ឆ័ត្រ',            array[75],  '雨伞 整名（单词）'),
( 4, 'km', 'km-exact', 'អង្ករ',            array[56],  '大米 整名（单词）'),
( 5, 'km', 'km-exact', 'កាបូបស្ពាយ',       array[76],  '双肩包 整名'),

-- ============ km-variant (8) —— 本 PoC 的核心难点 ============
-- 均为柬埔寨用户真实高频拼写偏差，不是我编造的随机噪声。
( 6, 'km', 'km-variant', 'ទូរសព្ទ',              array[1,7],   '漏写 ័：ទូរស័ព្ទ -> ទូរសព្ទ，极常见'),
( 7, 'km', 'km-variant', 'សាបូកក់សក់',           array[41],    '漏写 ៊：សាប៊ូ -> សាបូ'),
( 8, 'km', 'km-variant', 'នាលិកាឆ្លាតវៃ',        array[9],     '辅音混淆 ឡ -> ល：នាឡិកា -> នាលិកា'),
( 9, 'km', 'km-variant', 'ក្រីមការពារកំដៅថ្ងៃ',  array[47],    '元音混淆 ែ -> ី：ក្រែម -> ក្រីម'),
(10, 'km', 'km-variant', 'ស្បែកជើងកិឡា',         array[73],    '元音混淆 ី -> ិ：កីឡា -> កិឡា'),
(11, 'km', 'km-variant', 'ទឹកដោះគោមសៅ',          array[63],    '漏写下标符 ្：ម្សៅ -> មសៅ'),
(12, 'km', 'km-variant', 'ធុងសរាម',              array[93],    '漏写 ំ：សំរាម -> សរាម'),
(13, 'km', 'km-variant', 'កាហេ្វបំពង',           array[51],    '下标辅音与元音顺序颠倒：ហ្វេ -> ហេ្វ（真实编码变体）'),

-- ============ km-nospace (5) ============
(14, 'km', 'km-nospace', 'ខ្សែUSB',                  array[3],   '省掉 ខ្សែ 与 USB 之间的空格'),
(15, 'km', 'km-nospace', 'កញ្ចក់ការពារអេក្រង់',      array[2],   '省掉短语空格'),
(16, 'km', 'km-nospace', 'ម៉ាស៊ីនលាងសម្លៀកបំពាក់',   array[16],  '省掉短语空格'),
(17, 'km', 'km-nospace', 'ប្រអប់ដាក់អាហារ',          array[24],  '省掉短语空格'),
(18, 'km', 'km-nospace', 'រទេះរុញទារក',              array[65],  '省掉短语空格'),

-- ============ km-zwsp (3) ============
(19, 'km', 'km-zwsp', 'កន្' || U&'\200B' || 'សែង',          array[34], '词内插入 ZWSP'),
(20, 'km', 'km-zwsp', 'ធុង' || U&'\200B' || 'សំរាម',        array[93], '词间 ZWSP 代替空格'),
(21, 'km', 'km-zwsp', 'អំ' || U&'\200B' || 'បោស',           array[91], '词内插入 ZWSP'),

-- ============ km-digit (2) ============
(22, 'km', 'km-digit', 'ទឹកសុទ្ធ ៥០០',     array[53], '高棉语数字 ៥០០ vs 商品名 500'),
(23, 'km', 'km-digit', 'កាហ្វេបំពង ២០០',   array[51], '高棉语数字 ២០០ vs 商品名 200'),

-- ============ km-partial (5) ============
(24, 'km', 'km-partial', 'សាប៊ូ',       array[41,42,45,46,94],  '「肥皂」词根，应召回全部含 សាប៊ូ 商品'),
(25, 'km', 'km-partial', 'ស្បែកជើង',    array[73,74],           '「鞋」词根'),
(26, 'km', 'km-partial', 'ម៉ាស៊ីន',     array[15,16,19,20,84,88,98], '「机器」词根'),
(27, 'km', 'km-partial', 'កាបូប',       array[76,77],           '「包」词根'),
(28, 'km', 'km-partial', 'ទារក',        array[61,64,65,66,70],  '「婴儿」词根'),

-- ============ km-romanized (5) —— 预期全军覆没，结论关键 ============
(29, 'km', 'km-romanized', 'kanhchak',  array[2,28],            'កញ្ចក់ 的拉丁转写'),
(30, 'km', 'km-romanized', 'sabou',     array[41,42,45,46,94],  'សាប៊ូ 的拉丁转写'),
(31, 'km', 'km-romanized', 'kafe',      array[51],              'កាហ្វេ 的拉丁转写'),
(32, 'km', 'km-romanized', 'chhatr',    array[75],              'ឆ័ត្រ 的拉丁转写'),
(33, 'km', 'km-romanized', 'angkor',    array[56],              'អង្ករ 的拉丁转写'),

-- ============ zh-exact (5) ============
(34, 'zh', 'zh-exact', '电风扇',   array[11], null),
(35, 'zh', 'zh-exact', '牙膏',     array[43], null),
(36, 'zh', 'zh-exact', '雨伞',     array[75], null),
(37, 'zh', 'zh-exact', '大米',     array[56], null),
(38, 'zh', 'zh-exact', '双肩包',   array[76], null),

-- ============ zh-partial (3) ============
(39, 'zh', 'zh-partial', '手机', array[1,7],              '应召回手机壳、手机支架'),
(40, 'zh', 'zh-partial', '婴儿', array[64,65,66,70],      null),
(41, 'zh', 'zh-partial', '洗',   array[16,30,41,46,94],   '单字词根'),

-- ============ zh-typo (3) ============
(42, 'zh', 'zh-typo', '电风善', array[11], '同音错别字 扇->善'),
(43, 'zh', 'zh-typo', '牙高',   array[43], '同音错别字 膏->高'),
(44, 'zh', 'zh-typo', '雨散',   array[75], '同音错别字 伞->散'),

-- ============ en-exact (3) ============
(45, 'en', 'en-exact', 'electric fan', array[11], null),
(46, 'en', 'en-exact', 'toothpaste',   array[43], null),
(47, 'en', 'en-exact', 'umbrella',     array[75], null),

-- ============ en-typo (3) ============
(48, 'en', 'en-typo', 'tooth paste', array[43], '多一个空格'),
(49, 'en', 'en-typo', 'umbrela',     array[75], '漏一个 l'),
(50, 'en', 'en-typo', 'shampo',      array[41], '漏一个 o');
