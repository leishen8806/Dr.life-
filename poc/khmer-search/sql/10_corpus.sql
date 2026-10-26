-- CMP-19 / P0-4 — 评测语料：100 个商品（中/英/高棉三语）
--
-- 高棉语译名由本地化工程师（本 agent）撰写。诚实声明：
--   km_confidence = 'high'                 常用词，我有把握
--   km_confidence = 'needs-native-review'  需要母语者复核，**不得当成定稿**
-- 需复核的条目多为中国供应链特有品类（充电宝、钢化膜、订书机等），
-- 柬埔寨本地常用口语名可能与字面直译不同。
--
-- 注意：部分商品名里保留了**自然的高棉语短语空格**与数字，
-- 这是为了评测「用户省略空格」「高棉语数字 vs 阿拉伯数字」两类真实查询，
-- 不是排版失误。

truncate poc.product;

insert into poc.product (id, category, name_zh, name_en, name_km, km_confidence) values
-- 1. 手机数码
(  1, '手机数码', '手机壳',      'Phone case',          'ស្រោមទូរស័ព្ទ',             'high'),
(  2, '手机数码', '钢化膜',      'Screen protector',    'កញ្ចក់ ការពារអេក្រង់',      'needs-native-review'),
(  3, '手机数码', '数据线',      'USB cable',           'ខ្សែ USB',                 'high'),
(  4, '手机数码', '充电器',      'Charger',             'ឆ្នាំងសាកថ្ម',              'needs-native-review'),
(  5, '手机数码', '充电宝',      'Power bank',          'ថ្មបម្រុង',                 'needs-native-review'),
(  6, '手机数码', '蓝牙耳机',    'Bluetooth earphones', 'កាសប្លូធូស',               'high'),
(  7, '手机数码', '手机支架',    'Phone holder',        'ជើងទម្រទូរស័ព្ទ',           'needs-native-review'),
(  8, '手机数码', '内存卡',      'Memory card',         'កាតមេម៉ូរី',                'needs-native-review'),
(  9, '手机数码', '智能手表',    'Smart watch',         'នាឡិកាឆ្លាតវៃ',             'high'),
( 10, '手机数码', '自拍杆',      'Selfie stick',        'ដំបងថតរូប',                'needs-native-review'),
-- 2. 家用电器
( 11, '家用电器', '电风扇',      'Electric fan',        'កង្ហារអគ្គិសនី',            'high'),
( 12, '家用电器', '电饭锅',      'Rice cooker',         'ឆ្នាំងដាំបាយអគ្គិសនី',       'high'),
( 13, '家用电器', '电热水壶',    'Electric kettle',     'ឆ្នាំងដុតទឹកអគ្គិសនី',       'high'),
( 14, '家用电器', '电磁炉',      'Induction cooker',    'ចង្ក្រានអគ្គិសនី',          'high'),
( 15, '家用电器', '吹风机',      'Hair dryer',          'ម៉ាស៊ីនសម្ងួតសក់',          'high'),
( 16, '家用电器', '洗衣机',      'Washing machine',     'ម៉ាស៊ីន លាងសម្លៀកបំពាក់',   'high'),
( 17, '家用电器', '冰箱',        'Refrigerator',        'ទូរទឹកកក',                 'high'),
( 18, '家用电器', '电视机',      'Television',          'ទូរទស្សន៍',                'high'),
( 19, '家用电器', '空调',        'Air conditioner',     'ម៉ាស៊ីនត្រជាក់',            'high'),
( 20, '家用电器', '榨汁机',      'Juicer',              'ម៉ាស៊ីនកិនផ្លែឈើ',          'needs-native-review'),
-- 3. 厨房用品
( 21, '厨房用品', '不粘锅',      'Non-stick pan',       'ខ្ទះមិនជាប់',               'needs-native-review'),
( 22, '厨房用品', '菜刀',        'Kitchen knife',       'កាំបិតចែកម្ហូប',            'needs-native-review'),
( 23, '厨房用品', '砧板',        'Cutting board',       'ក្តារចិញ្ច្រាំ',             'high'),
( 24, '厨房用品', '保鲜盒',      'Food container',      'ប្រអប់ ដាក់អាហារ',          'high'),
( 25, '厨房用品', '筷子',        'Chopsticks',          'ចង្កឹះ',                   'high'),
( 26, '厨房用品', '不锈钢勺',    'Steel spoon',         'ស្លាបព្រាដែកអ៊ីណុក',        'needs-native-review'),
( 27, '厨房用品', '陶瓷碗',      'Ceramic bowl',        'ចានដីឥដ្ឋ',                'needs-native-review'),
( 28, '厨房用品', '玻璃杯',      'Glass cup',           'កែវកញ្ចក់',                 'high'),
( 29, '厨房用品', '保温杯',      'Thermos flask',       'ដបរក្សាកំដៅ',               'needs-native-review'),
( 30, '厨房用品', '洗碗布',      'Dish cloth',          'ក្រណាត់ លាងចាន',           'high'),
-- 4. 家居寝具
( 31, '家居寝具', '枕头',        'Pillow',              'ខ្នើយ',                    'high'),
( 32, '家居寝具', '床垫',        'Mattress',            'ពូក',                      'high'),
( 33, '家居寝具', '蚊帐',        'Mosquito net',        'មុង',                      'high'),
( 34, '家居寝具', '毛巾',        'Towel',               'កន្សែង',                   'high'),
( 35, '家居寝具', '窗帘',        'Curtain',             'វាំងននបង្អួច',              'high'),
( 36, '家居寝具', '塑料凳',      'Plastic stool',       'កៅអីជ័រ',                  'high'),
( 37, '家居寝具', '折叠桌',      'Folding table',       'តុបត់',                    'needs-native-review'),
( 38, '家居寝具', '衣架',        'Clothes hanger',      'ទំពក់ព្យួរអាវ',             'needs-native-review'),
( 39, '家居寝具', '收纳箱',      'Storage box',         'ធុងដាក់ឥវ៉ាន់',             'high'),
( 40, '家居寝具', '台灯',        'Desk lamp',           'អំពូលតុ',                  'high'),
-- 5. 个护美妆
( 41, '个护美妆', '洗发水',      'Shampoo',             'សាប៊ូកក់សក់',               'high'),
( 42, '个护美妆', '沐浴露',      'Body wash',           'សាប៊ូងូតទឹក',               'high'),
( 43, '个护美妆', '牙膏',        'Toothpaste',          'ថ្នាំដុសធ្មេញ',             'high'),
( 44, '个护美妆', '牙刷',        'Toothbrush',          'ច្រាសដុសធ្មេញ',             'high'),
( 45, '个护美妆', '香皂',        'Soap bar',            'សាប៊ូដុំ',                  'high'),
( 46, '个护美妆', '洗面奶',      'Face wash',           'សាប៊ូលាងមុខ',               'high'),
( 47, '个护美妆', '防晒霜',      'Sunscreen',           'ក្រែមការពារកំដៅថ្ងៃ',       'high'),
( 48, '个护美妆', '口红',        'Lipstick',            'ក្រែមលាបបបូរមាត់',          'needs-native-review'),
( 49, '个护美妆', '香水',        'Perfume',             'ទឹកអប់',                   'high'),
( 50, '个护美妆', '指甲剪',      'Nail clipper',        'កន្ត្រៃកាត់ក្រចក',          'high'),
-- 6. 食品饮料
( 51, '食品饮料', '速溶咖啡',    'Instant coffee',      'កាហ្វេបំពង 200g',           'needs-native-review'),
( 52, '食品饮料', '绿茶',        'Green tea',           'តែបៃតង',                   'high'),
( 53, '食品饮料', '矿泉水',      'Mineral water',       'ទឹកសុទ្ធ 500ml',            'high'),
( 54, '食品饮料', '炼乳',        'Condensed milk',      'ទឹកដោះគោកំប៉ុង',            'needs-native-review'),
( 55, '食品饮料', '方便面',      'Instant noodles',     'មីកញ្ចប់',                  'high'),
( 56, '食品饮料', '大米',        'Rice',                'អង្ករ',                    'high'),
( 57, '食品饮料', '食用油',      'Cooking oil',         'ប្រេងចម្អិនអាហារ',          'high'),
( 58, '食品饮料', '鱼露',        'Fish sauce',          'ទឹកត្រី',                  'high'),
( 59, '食品饮料', '白糖',        'White sugar',         'ស្ករស',                    'high'),
( 60, '食品饮料', '盐',          'Salt',                'អំបិល',                    'high'),
-- 7. 母婴用品
( 61, '母婴用品', '纸尿裤',      'Diapers',             'ក្រដាសខោទារក',              'needs-native-review'),
( 62, '母婴用品', '奶瓶',        'Baby bottle',         'ដបទឹកដោះកូន',               'needs-native-review'),
( 63, '母婴用品', '奶粉',        'Milk powder',         'ទឹកដោះគោម្សៅ',              'high'),
( 64, '母婴用品', '婴儿湿巾',    'Baby wipes',          'ក្រដាសសើមទារក',             'needs-native-review'),
( 65, '母婴用品', '婴儿车',      'Stroller',            'រទេះរុញ ទារក',             'high'),
( 66, '母婴用品', '婴儿浴盆',    'Baby bathtub',        'អាងងូតទឹកទារក',             'high'),
( 67, '母婴用品', '儿童玩具车',  'Toy car',             'ឡានប្រដាប់ក្មេងលេង',        'needs-native-review'),
( 68, '母婴用品', '积木',        'Building blocks',     'ប្លុកតម្រៀប',               'needs-native-review'),
( 69, '母婴用品', '安抚奶嘴',    'Pacifier',            'ក្បាលដោះកៅស៊ូ',             'needs-native-review'),
( 70, '母婴用品', '婴儿背带',    'Baby carrier',        'ខ្សែពាក់ទារក',              'needs-native-review'),
-- 8. 服饰鞋包
( 71, '服饰鞋包', 'T恤',         'T-shirt',             'អាវយឺត',                   'high'),
( 72, '服饰鞋包', '牛仔裤',      'Jeans',               'ខោជិន',                    'high'),
( 73, '服饰鞋包', '运动鞋',      'Sneakers',            'ស្បែកជើងកីឡា',              'high'),
( 74, '服饰鞋包', '拖鞋',        'Slippers',            'ស្បែកជើងផ្ទាត់',            'high'),
( 75, '服饰鞋包', '雨伞',        'Umbrella',            'ឆ័ត្រ',                    'high'),
( 76, '服饰鞋包', '双肩包',      'Backpack',            'កាបូបស្ពាយ',                'high'),
( 77, '服饰鞋包', '钱包',        'Wallet',              'កាបូបលុយ',                 'high'),
( 78, '服饰鞋包', '皮带',        'Belt',                'ខ្សែក្រវាត់',               'high'),
( 79, '服饰鞋包', '帽子',        'Hat',                 'មួក',                      'high'),
( 80, '服饰鞋包', '袜子',        'Socks',               'ស្រោមជើង',                 'high'),
-- 9. 文具办公
( 81, '文具办公', '圆珠笔',      'Ballpoint pen',       'ប៊ិច',                     'high'),
( 82, '文具办公', '铅笔',        'Pencil',              'ខ្មៅដៃ',                   'high'),
( 83, '文具办公', '笔记本',      'Notebook',            'សៀវភៅកត់ត្រា',              'high'),
( 84, '文具办公', '订书机',      'Stapler',             'ម៉ាស៊ីនស្នូល',              'needs-native-review'),
( 85, '文具办公', '剪刀',        'Scissors',            'កន្ត្រៃ',                  'high'),
( 86, '文具办公', '胶带',        'Adhesive tape',       'កាវបិទ',                   'needs-native-review'),
( 87, '文具办公', '文件夹',      'File folder',         'ហ្វ្រាប់ឯកសារ',             'needs-native-review'),
( 88, '文具办公', '计算器',      'Calculator',          'ម៉ាស៊ីនគិតលេខ',             'high'),
( 89, '文具办公', '白板笔',      'Whiteboard marker',   'ប៊ិចសរសេរក្តារខៀន',         'needs-native-review'),
( 90, '文具办公', '书包',        'Schoolbag',           'កាតាបសិស្ស',                'needs-native-review'),
-- 10. 五金清洁
( 91, '五金清洁', '扫把',        'Broom',               'អំបោស',                    'high'),
( 92, '五金清洁', '拖把',        'Mop',                 'ឈុតសម្អាតឥដ្ឋ',             'needs-native-review'),
( 93, '五金清洁', '垃圾桶',      'Trash bin',           'ធុងសំរាម',                  'high'),
( 94, '五金清洁', '洗衣粉',      'Laundry powder',      'ម្សៅសាប៊ូ',                 'high'),
( 95, '五金清洁', '螺丝刀',      'Screwdriver',         'ទួណឺវីស',                  'needs-native-review'),
( 96, '五金清洁', '卷尺',        'Tape measure',        'មែត្រវាស់',                 'needs-native-review'),
( 97, '五金清洁', '锤子',        'Hammer',              'ញញួរ',                     'high'),
( 98, '五金清洁', '电钻',        'Electric drill',      'ម៉ាស៊ីនស្វាន',              'needs-native-review'),
( 99, '五金清洁', '手套',        'Gloves',              'ស្រោមដៃ',                  'high'),
(100, '五金清洁', '绝缘胶带',    'Insulating tape',     'កាវអ៊ីសូឡង់',               'needs-native-review');

-- ---------------------------------------------------------------------------
-- 同义词表（任务表交付物点名要求）
-- ---------------------------------------------------------------------------
-- 两类用途：
--   1. 语言内真同义词（ឡាន / រថយន្ត、手机 / 行动电话）
--   2. **高棉语拉丁转写 → 高棉文**：柬埔寨用户在没装高棉文键盘时会直接打拉丁字母
--      （sabou、kafe、chhatr）。这类查询对任何纯数据库匹配方案都是 0 召回，
--      只能靠同义词/转写表救。见 RESULT.md「拉丁转写」一节。

truncate poc.synonym restart identity;

insert into poc.synonym (lang, term, canonical) values
-- 语言内同义词
('km', 'រថយន្ត',   'ឡាន'),
('km', 'ទូរស័ព្ទដៃ', 'ទូរស័ព្ទ'),
('zh', '行动电话',  '手机'),
('zh', '电话',      '手机'),
('zh', '冷气机',    '空调'),
('zh', '雪柜',      '冰箱'),
('en', 'cellphone', 'mobile phone'),
('en', 'mobile',    'mobile phone'),
('en', 'ac',        'air conditioner'),
('en', 'fridge',    'refrigerator'),
-- 高棉语拉丁转写（romanization -> 高棉文）
('km', 'sabou',     'សាប៊ូ'),
('km', 'sabu',      'សាប៊ូ'),
('km', 'kafe',      'កាហ្វេ'),
('km', 'kahve',     'កាហ្វេ'),
('km', 'chhatr',    'ឆ័ត្រ'),
('km', 'chhat',     'ឆ័ត្រ'),
('km', 'angkor',    'អង្ករ'),
('km', 'angko',     'អង្ករ'),
('km', 'kanhchak',  'កញ្ចក់'),
('km', 'kanchak',   'កញ្ចក់'),
('km', 'tuk',       'ទឹក'),
('km', 'sbekjeung', 'ស្បែកជើង'),
('km', 'kabob',     'កាបូប'),
('km', 'masin',     'ម៉ាស៊ីន');
