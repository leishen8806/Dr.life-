# Dr.life+ 图片搜索技术方案（T-03c）

- 卡号：CMP-658
- 作者：Architect
- 日期：2026-10-29
- 状态：草案，待董事会审阅、项目负责人确认
- 范围：本文只出技术方案，不含业务代码。文中标【待批准】的项目（新依赖、新服务、下载模型权重）在董事会批准前不得实施。

## 0. 结论先行（架构推荐）

| 编号 | 决策 | 推荐 | 一句话理由 |
| --- | --- | --- | --- |
| D1 | 推理放在哪 | **独立内网推理服务 `imgvec`（HTTP），不走 Ollama** | Ollama 的图片向量只有 `embeddinggemma-2` 一个模型，且只实现在 MLX（Mac）runner，Linux/Windows 连拉取都会失败（见 §2.1 证据）。 |
| D2 | 用哪个模型 | **`google/siglip2-base-patch16-224`（Apache-2.0，768 维）**；备选 `facebook/dinov2-base` | Apache-2.0 可商用、768 维适合 pgvector、能读包装上的文字与品牌（建材/日用品关键），且留下"文字搜图"的后路。 |
| D3 | 向量存哪 | **自建 PostgreSQL 16 + pgvector 0.8.7，`halfvec(768)` + HNSW cosine，单独表** | 生产库是自建的，可装扩展；不引入第二套存储；halfvec 省一半空间并避开 TOAST。**不用 IVFFlat**（有 CVE 且需先有数据）。 |
| D4 | 怎么入库 | F-14b 上传校验通过 → outbox → asynq `ai` 队列 → 写向量，带 `model_rev` | 复用既有 outbox + asynq 可靠投递，换模型时双写再切换。 |
| D5 | 买家照片 | **检索完立即删除 + 对象存储 24 小时生命周期兜底**，独立前缀，日志不留图 | 照片可能含人脸与住址，按最小留存处理。 |
| D6 | 接口形态 | `POST /v1/public/search/image` 收 **file_id**（不收 multipart） | 与既有"文件不经过后端"的设计一致（`storage` 包注释：*never proxies file bytes*）。 |
| D7 | 先验证再开发 | 先做 PoC 卡（200 张样本，top-5 ≥ 70%），不达标不进入开发 | 与 P0-4 文字搜索先验证的做法一致。 |

不建议的做法：把买家照片发往任何外部 AI 服务（OpenAI/Gemini/阿里云识图等）；用 Ultralytics YOLO 做主体检测（AGPL-3.0，会传染到整个后端）；为图片搜索单独引入 Milvus/Qdrant（多一套要运维的有状态组件，当前量级不值得）。

---

## 1. 功能定义

### 1.1 买家侧（Mini App）

1. **拍照/上传找同款**
   - 入口：搜索框右侧相机图标（交互照搬淘宝：相机图标 → 拍照 / 从相册选 → 取景框 → 结果页顶部保留缩略图与"重新框选"）。视觉与品牌素材不照搬。
   - 流程：选图 → 前端压缩到长边 ≤ 1280px、JPEG 质量 0.85 → 向 F-14b 取预签名 URL → 直传对象存储 → 拿到 `file_id` → 调 `POST /v1/public/search/image`。
   - 主体识别：服务端默认跑一次主体检测，返回 `boxes[]`（带置信度与裁剪后缩略图坐标），前端在取景框上画出候选框，默认选中置信度最高的一个；用户可拖动手动框选，手动框选时带 `box` 参数重新检索。
   - 结果：按相似度降序返回在售商品（`status='on'`），可叠加 `category_id`、`price_min`/`price_max` 筛选与排序切换（相似度 / 价格 / 销量）。
2. **"找相似"按钮**
   - 位置：商品详情页主图下方、搜索结果卡片长按菜单（照搬淘宝位置）。
   - 实现：`GET /v1/public/products/{public_id}/similar`，直接用该商品主图已入库的向量做 KNN，**不再跑推理**（延迟 ~20ms 级，纯数据库查询）。

### 1.2 相似度门槛与兜底

- 距离用 cosine，`score = 1 - distance`，范围 0~1。
- 门槛 `IMGVEC_MIN_SCORE` 默认 **0.62**（PoC 后以样本集实测的 F1 拐点替换，不要照抄这个数）。
- 分三档展示（照搬淘宝"宝贝"与"相似推荐"的分区感）：
  - `score ≥ 0.80`：标"可能是同款"，置顶。
  - `0.62 ≤ score < 0.80`：标"相似商品"。
  - `< 0.62`：不返回。
- 兜底顺序（每一级都要有明确文案，不能白屏）：
  1. 有结果但全部低于门槛 → 返回空列表 + `fallback: "category"`，附带检测到的主体类别关键词，引导用户点一下转成文字搜索。
  2. 检测不到主体 → 用整图检索，并提示"没识别到主体，已按整张图搜索，可手动框选"。
  3. 推理服务不可用 → 返回错误码 `2008 ImageSearchUnavailable`，三语文案"图片搜索暂时不可用，请用文字搜索"。**文字搜索路径完全不受影响**（§2.4）。

---

## 2. 模型与推理服务

### 2.1 为什么不用 Ollama（重要）

现有架构文档把内网 Ollama 作为 AI 网关的后端（`Dr.life+第一版交付形态与技术架构.md` 第五章，示例地址 `http://192.168.1.50:11434`，仅内网、防火墙白名单）。图片搜索**不能**复用它：

- Ollama `/api/embed` 的多模态入参（`{"text":..., "image": base64}`）确实已在文档中，但**唯一支持的模型是 `embeddinggemma-2`**（740M，768 维）。
- 该能力只实现在 **MLX runner**（PR ollama/ollama#18820，v0.40.0-rc6）。Linux/Windows 上拉取 `embeddinggemma-2:740m` 直接报 `this model requires MLX support, but the MLX runtime is not available`（issue ollama/ollama#18825，截至查证时仍未关闭，维护者表示 Linux/Windows 支持"尚未提供"）。
- Ollama **不提供任何 CLIP / SigLIP / DINOv2 图片向量模型**。

结论：图片向量必须自建推理服务。Ollama 继续只做文本/LLM 用途，两者同机不同端口，互不影响。

### 2.2 候选模型对比（已核实的数据）

参数量/体积来自 HuggingFace 文件实测，许可证来自仓库 license 字段；标"未核实"的不要写进对外材料。

| 模型 | 体积(fp32) | 维度 | 输入 | 许可证 | 可商用 | 多语言文本塔 | 关键数据 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **google/siglip2-base-patch16-224** | 1500 MB | **768** | 224 | Apache-2.0 | ✅ | 弱（训练数据 10% 非英文；**高棉语覆盖未核实**） | ImageNet 0-shot 79.1%（B/16@256）；SigLIP2 在公开商品检索基准上是开源最好一档 |
| facebook/dinov2-base | 346 MB | 768 | 224 | Apache-2.0 | ✅ | 无（纯图搜图） | 实例检索 Oxford-Hard mAP 49.5（OpenCLIP ViT-G 仅 19.7）；但在商品基准上 R@1 33.0 < SigLIP2 43.4 |
| laion CLIP ViT-B/32 | 605 MB | 512 | 224 | MIT | ✅ | 无（英文） | ImageNet 0-shot 66.6%，图像塔 8.82 GFLOPs（最省 CPU） |
| google/siglip-so400m-patch14-384 | 3512 MB | 1152 | 384 | Apache-2.0 | ✅ | 否 | 图像塔 670 GFLOPs ≈ ViT-B/16 的 19 倍，**CPU 不可用于交互查询** |
| jinaai/jina-clip-v2 | 1731 MB(fp16) | 1024 | 512 | **CC-BY-NC-4.0** | ❌ | 是（语言列表含 `km`） | 唯一明确覆盖高棉语的，但权重禁止商用，只能买 API → **排除** |
| OFA-Sys/chinese-clip-vit-base-patch16 | 753 MB | 512 | 224 | MIT（在 GitHub，HF 卡无 license 字段） | ✅ | 仅中文 | 中文检索强，但柬埔寨场景用不上 |
| Marqo/marqo-fashionSigLIP | 813 MB | 768 | 224 | Apache-2.0 | ✅ | 无 | 服装专精；Dr.life+ 试点是建材/日用品，不匹配 |

**推荐：`google/siglip2-base-patch16-224`**，理由按权重排序：

1. Apache-2.0，权重可自托管，无商用限制；
2. 在公开商品检索基准上 SigLIP2 ≥ DINOv3 > DINOv2，是开源里最稳的一档；
3. **能读图里的文字/品牌**——建材与日用品大量靠包装文字区分规格（DINOv2 对图中文字基本"失明"，MIEB 的 Visual STS 上接近随机）；
4. 768 维，`halfvec` 下单行 1544 B 可内联存储，索引也小；
5. 文本塔在同一向量空间，未来可以做"文字搜图"，而高棉语文字检索正是 V1 的弱项——这是一条有价值的后路。

**不要照抄的网上说法**："DINOv2 在商品匹配上一定优于 CLIP 系"在公开基准上不成立（Visual Product Search Benchmark：DINOv2-L R@1 33.0 vs SigLIP2 43.4；Benchmarking Image Embeddings for E-Commerce：SigLIP 在 6 个数据集中 5 个最优）。DINOv2 的优势集中在刚性物体换视角的实例重识别。因此 **DINOv2-base 列为备选**：如果 PoC 显示瓷砖/型材这类"无文字、靠纹理"的品类命中率差，再做"SigLIP2 + DINOv2 双向量取最大分"的加强版（新开卡，不在 v1）。

### 2.3 主体检测（裁剪）

- 检测器：**RT-DETR（Apache-2.0）**，权重 `PekingU/rtdetr_r50vd`，从 `lyuwenyu/RT-DETR` 或 HF transformers 加载。
- **禁止**：Ultralytics 包（YOLOv8/v11）为 **AGPL-3.0**，会传染调用方；同一个 RT-DETR 架构经 `ultralytics` 包加载同样受 AGPL 约束。导入路径决定许可义务，代码审查时必须检查 import。
- 裁剪闸门（任一条不满足就丢弃该框、退回整图）：置信度 < 0.45；框面积 > 整图 90% 或 < 2%；框贴边（任一边距 < 2px）；长宽比 > 5:1。
- **向量合并规则：整图向量与裁剪框向量各算一次，取"每个候选商品的最大分"，严禁对多个视角向量求平均**（求平均会把精确匹配抹平，这是公开踩坑记录里反复出现的问题）。
- 第一版可配置开关 `IMGVEC_DETECT_ENABLED`，默认开；检测失败或超时一律降级整图，不报错。

### 2.4 推理服务接口契约

服务名 `imgvec`，部署在现有内网 AI 机，与 Ollama 同机不同端口（建议 `18081`），只监听内网地址，防火墙只放通两台应用服务器。技术栈 Python 3.12 + FastAPI + **onnxruntime**（模型导出为 ONNX，不在生产机装 PyTorch）。

```
POST /internal/v1/embed
Content-Type: application/json
{
  "image_b64": "<base64>",        // 二者其一，必填其一
  "image_url": "<预签名 GET URL>",  // 推荐：服务自己去对象存储拉，字节不经过 Go 后端
  "box": {"x":0.1,"y":0.2,"w":0.5,"h":0.4},  // 可选，归一化坐标，给定则只嵌入该区域
  "detect": true                   // 是否跑主体检测
}
200 {
  "model_rev": "siglip2-base-p16-224@onnx-1",
  "dim": 768,
  "vectors": [                      // 顺序：[0] 整图，[1..] 各候选框
    {"kind":"full","vec":[...],"box":null,"confidence":null},
    {"kind":"crop","vec":[...],"box":{...},"confidence":0.87,"label":"tile"}
  ],
  "elapsed_ms": 243
}
4xx {"ok":false,"error":{"code":"unsupported_media"|"image_too_large"|"decode_failed"}}
503 {"ok":false,"error":{"code":"model_not_loaded"}}
```

- 另有 `GET /internal/v1/healthz`（进程存活）、`GET /internal/v1/readyz`（模型已加载且跑通一次固定测试图，返回 `model_rev`）。
- 鉴权：内网 + 防火墙白名单 + 固定 header `X-Imgvec-Token`（来自 sops 管理的密钥，与现有密钥管理一致）。
- **向量归一化在推理服务侧完成**（返回单位向量），这样数据库侧可以用更省的内积排序，且避免 Go 与 Python 两边各自实现一遍归一化。

Go 侧调用参数（全部可配置，给出推荐默认值）：

| 配置项 | 默认 | 说明 |
| --- | --- | --- |
| `IMGVEC_BASE_URL` | 空 | 空 = 图片搜索整体关闭（接口直接返回 2008），不影响其它功能 |
| `IMGVEC_TIMEOUT_QUERY` | 1500ms | 买家查询路径，超时即降级 |
| `IMGVEC_TIMEOUT_INDEX` | 10s | 队列入库路径，可以慢 |
| `IMGVEC_MAX_CONCURRENT` | 4 | 与 asynq `ai` 队列并发 4 对齐，避免推理机被打满 |
| `IMGVEC_BREAKER_FAILS` / `_WINDOW` / `_COOLDOWN` | 5 / 30s / 60s | 熔断：窗口内 5 次失败 → 冷却 60s，期间查询直接返回 2008，不再打推理机 |

降级与隔离（必须成立的性质，写成测试）：

1. 推理不可用时 `POST /v1/public/search/image` 返回 2008，**文字搜索 `GET /v1/public/products` 的成功率与延迟不受影响**；
2. 推理慢时不占用 `read`/`critical` 并发桶——图片搜索路由归类为 `heavy`（见 §6.3），brownout 降级时 `heavy` 并发自动减半，`critical` 一律不受影响（已有 `overload.Brownout` 行为）；
3. 入库失败只影响该图的向量，不影响商品上架。

### 2.5 CPU / GPU 需求

公开实测锚点：2 核 Xeon + ONNX 跑 CLIP ViT-B/32（8.82 GFLOPs）约 117 ms/图；M1 Pro 跑 DINOv2-B 约 50 ms/图；T4 跑 ViT-B/32 批量约 2.6 ms/图；RTX 3090 跑 ViT-B/16 约 6.7 ms/图。

SigLIP2-base 图像塔与 ViT-B/16 同级（约 35 GFLOPs）。由此**推算**（标注为推算，必须在 PoC 卡实测替换）：

- CPU-only，4 核现代 CPU + ONNX：**约 200~400 ms/图**；加 INT8 量化可再快 2~4 倍。
- 单张入门 GPU（T4/3060 级）：**约 7~15 ms/图**。

结论：
- 试点期（5 商户、数百 SKU、买家查询峰值个位数 QPS）**CPU-only 可行**，1500ms 查询超时留有余量。
- 入库与全量重建跑在同一服务，用 `batch` 队列低优先级，避免挤占查询。
- GPU 不是 v1 必需；当 P95 超标或并发超过约 10 QPS 时再加卡（现有架构文档里 Ollama 机已经是带 GPU 的机器，可共用，但要给 imgvec 设显存上限，别把 LLM 挤掉）。

---

## 3. 向量存储

### 3.1 选型：pgvector（推荐）

生产库是自建 PostgreSQL 16（主 + 备流复制，自己有 superuser），**可以装扩展**，这是选 pgvector 的前提条件，已成立。

- 版本：**必须 ≥ 0.8.7**。0.8.6 及以前存在 **CVE-2026-103484**：IVFFlat 建索引时缓冲区溢出，任何能建 IVFFlat 索引的角色都可能以数据库 OS 用户身份执行代码。
- 开发环境【待批准，改依赖】：`docker-compose.dev.yml` 把 `postgres:16-alpine` 换成 **`pgvector/pgvector:pg16`**（Debian 基础镜像，含 0.8.7）。原因：Alpine 社区包只有 0.6.2（v3.21）/0.8.1（edge），**都带上述 CVE**，且 edge 的包是按 PG17/18 构建的。
- `CREATE EXTENSION vector` **需要 superuser**（`vector.control` 里没有 `trusted = true`，已核对源文件）。因此 goose 迁移要拆成两条：扩展安装单独一条、由 DBA/超级用户角色执行（或在生产由 DBA 预先 `CREATE EXTENSION`，迁移里用 `CREATE EXTENSION IF NOT EXISTS` 幂等兜底）；后续建表建索引不需要 superuser。
- 不新增 Milvus/Qdrant/Elasticsearch：当前量级（万级向量）pgvector 绰绰有余，而多一套有状态组件就多一套备份、监控、故障演练。什么时候该重新评估：向量数 > 500 万，或需要多模态混合检索打分，或 PG 主库 CPU 被向量查询吃掉 30% 以上。

### 3.2 表与索引

**单独一张表**，不加在 `catalog.product` 上。原因：HNSW 索引存在时，对同一行**任何**列的 UPDATE 都要付索引维护代价（PostgreSQL 整行重写），而商品元数据是频繁变更的（价格、库存、评分）。

```sql
-- 迁移 A（superuser）
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;

-- 迁移 B
CREATE TABLE catalog.product_image_vector (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id  bigint NOT NULL REFERENCES catalog.product(id) ON DELETE CASCADE,
    file_id     bigint NOT NULL,               -- ops.file 主键（F-14b）
    is_primary  boolean NOT NULL DEFAULT false,-- 是否商品主图
    model_rev   text    NOT NULL,              -- 例 siglip2-base-p16-224@onnx-1
    embedding   halfvec(768) NOT NULL,
    source      text    NOT NULL DEFAULT 'full'
                CHECK (source IN ('full','crop')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (file_id, model_rev, source)
);

CREATE INDEX product_image_vector_pid_idx  ON catalog.product_image_vector (product_id);
CREATE INDEX product_image_vector_rev_idx  ON catalog.product_image_vector (model_rev);

-- 建索引前：SET maintenance_work_mem='2GB'; SET max_parallel_maintenance_workers=7;
CREATE INDEX product_image_vector_hnsw_idx
    ON catalog.product_image_vector
    USING hnsw (embedding halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64);
```

迁移编号注意：当前 `backend/migrations/` 为 00001–00020，**缺 00015**（在未合并分支上）。新迁移必须先确认未合并分支占用的编号，避免重复编号导致 goose 冲突。

### 3.3 为什么 halfvec + HNSW

- **`halfvec(768)` = 1544 B/行**，`vector(768)` = 3080 B/行。`vector(768)` 必然被 TOAST 外存（阈值约 2032 B，pgvector 的 `vector` 是 `STORAGE external`），每行查询多一次 detoast；`halfvec(768)` 可内联。实测（Katz，1536 维）halfvec 把索引从 7.55 GiB 降到 3.78 GiB，默认检索参数下召回基本不变。
- **HNSW 而非 IVFFlat**：HNSW 速度-召回曲线更好、可以在空表上建索引（IVFFlat 要先有数据做 k-means，重建流程更脆）；而且 IVFFlat 正是 CVE-2026-103484 的受影响组件。代价是建索引慢、插入慢（§3.5）。
- 维度上限：`vector` 可索引 2000 维，`halfvec` 4000 维。768 维远在限内，但如果以后换 so400m（1152 维）也仍然安全。
- 距离：向量已归一化，`halfvec_cosine_ops` + `<=>` 排序即可；也可以用 `<#>`（负内积，更省一次除法），但 `<#>` 返回负值，**不要把它的原始值当相似度返回给客户端**。

### 3.4 容量估算

| 场景 | 向量数 | 表数据(halfvec 768) | HNSW 索引(≈1.3×) | 合计 |
| --- | --- | --- | --- | --- |
| 试点（5 商户 × 300 SKU × 3 图 × 2 source） | 约 9,000 | 约 14 MB | 约 18 MB | **约 32 MB** |
| 中期（2 万 SKU × 4 图 × 2） | 16 万 | 约 247 MB | 约 321 MB | 约 570 MB |
| 设计上限 | 100 万 | 1.54 GB | 约 2.0 GB | 约 3.5 GB |

索引必须常驻内存/页缓存才有性能，100 万向量规模要给 `shared_buffers` + OS cache 预留 ≥ 4 GB。`maintenance_work_mem`：试点规模 ≥ 256 MB（默认 64 MB 已经不够），100 万向量需 ≈ 2.6 GB。**建索引时如果日志出现 `NOTICE: hnsw graph no longer fits into maintenance_work_mem`，必须当成失败处理并告警**——goose 会照常成功，但索引构建已严重退化。

### 3.5 已知运维坑（要写进 runbook）

1. **HNSW 插入随数据量变慢**（公开 issue 有 10k 批从 6 分钟退化到 40 分钟的报告）。对策：全量重建时先 `DROP INDEX` → 批量灌入 → `CREATE INDEX CONCURRENTLY`；日常增量按单条写入，量小无妨。
2. **删除会留下图索引垃圾**，HNSW 的 VACUUM 很慢。对策：大批下架/换图之后，按 runbook 执行 `REINDEX INDEX CONCURRENTLY product_image_vector_hnsw_idx`（在 `VACUUM` 之前）。
3. **带筛选的 ANN 召回会掉**：pgvector 的近似索引是"先扫索引再过滤"，`WHERE status='on' AND category_id=...` 可能导致返回数远少于 `LIMIT`。对策（按优先级）：
   - 先取 `LIMIT 200` 候选再在外层过滤并截取 Top-N（当前量级最简单、推荐 v1 这么做）；
   - pgvector 0.8 的迭代扫描：`SET hnsw.iterative_scan = strict_order; SET hnsw.max_scan_tuples = 20000;`（**会话级 GUC，必须在每个连接上设置，不随复制传播**）；
   - 提高 `hnsw.ef_search`（默认 40，经验值取目标 k 的 1.5~2 倍）。
4. **索引用不上**：只有写成 `ORDER BY <距离算子> ... LIMIT n` 升序才会走索引。sqlc 的查询文本里 `::halfvec(768)` 的转换写法必须与索引表达式**完全一致**。
5. **只读副本可以跑向量查询**（pgvector 全程 WAL 记录，流复制无特殊配置）；但建 HNSW 索引会产生大量 WAL，副本会出现回放延迟，刚写入的向量要读主库。

---

## 4. 数据流水线

### 4.1 入库（商品主图）

```
商户上传主图
  → F-14b: 取预签名 PUT → 直传对象存储 → 回调/确认，ops.file 落库（校验 content-type、大小、校验和、剥离 EXIF）
  → 校验完成（file.status = verified）时，在同一事务写 outbox 事件 catalog.product_image.verified
  → outbox relay（FOR UPDATE SKIP LOCKED，已有实现）投递 asynq
  → 队列 ai，任务类型 catalog:image_embed:v1，TaskID = catalog:image_embed:<file_id>:<model_rev>:v1
  → worker：取预签名 GET URL → 调 imgvec /internal/v1/embed → UPSERT catalog.product_image_vector
```

- **payload 只带 ID**（`file_id`、`product_id`、`model_rev`），不带图片、不带 URL、不带个人信息——与既有队列规范一致（数据库是唯一真相来源）。
- 幂等：`TaskID` 去重 + 表上 `UNIQUE (file_id, model_rev, source)` 做 UPSERT，重复投递无副作用。
- 重试：指数退避 + jitter，最多 5 次；`unsupported_media`/`decode_failed` 这类 4xx 属永久错误，直接进死信不重试；`model_not_loaded`/超时属暂时错误，重试。
- 死信增长要告警（沿用既有死信告警与 `queuectl` 重放）。

### 4.2 变更与删除

| 事件 | 处理 |
| --- | --- |
| 换主图 | 新 `file_id` 走入库流程；旧图对应向量由 `catalog:image_vector_purge:v1` 删除 |
| 商品下架（`status` → `off`/`rejected`） | **不删向量**，查询侧按 `status='on'` 过滤即可（重新上架无需重算，省一次推理） |
| 商品删除 | `ON DELETE CASCADE` 自动清理 |
| 文件删除 | `catalog:image_vector_purge:v1`（`batch` 队列），按 `file_id` 删 |
| 换模型/换版本 | 新 `model_rev` 全量重算 → 双 rev 并存 → 配置 `IMGVEC_ACTIVE_REV` 切换 → 确认稳定后删旧 rev 的行 |

### 4.3 全量重建

- 任务 `catalog:image_reindex:v1`，`batch` 队列（并发 4、权重 1、可暂停）。
- 游标分页（按 `file_id` 升序，每批 200），进度与断点写 `ops` 侧的任务状态表，可中断可续跑。
- 入口：`queuectl` 子命令触发；禁止 HTTP 公网触发。
- 大批量场景按 §3.5(1) 先删 HNSW 索引再重建。

---

## 5. 安全与隐私

### 5.1 买家上传的查询图

- **独立前缀**：`query/{yyyymmdd}/{ulid}`，与商品图（`product/...`）隔离，便于单独设生命周期与权限。
- **留存**：检索完成后**立即删除**（best effort，失败不影响返回）；对象存储生命周期规则 **24 小时**兜底过期删除。两道都要有——只靠即时删除，进程崩溃就会留图。
- **不建索引、不入向量表**：查询图的向量只在内存中存在，不落库（避免"删了图但留了可反推的向量"）。
- **EXIF 剥离**：F-14 已有此要求；查询图路径必须确认生效（GPS 就在 EXIF 里）。

### 5.2 校验与限制

| 项 | 规则 |
| --- | --- |
| 格式 | 白名单 `image/jpeg`、`image/png`、`image/webp`；**校验魔术字节**，不信 `Content-Type` 与扩展名 |
| 大小 | ≤ 8 MB（经 `storage.Policy.MaxContentLength` 强制，预签名阶段就拒） |
| 像素 | 解码前检查头部尺寸，长边 > 8192px 或总像素 > 4000 万直接拒（防解压炸弹 / decompression bomb） |
| 重编码 | 推理服务统一解码后重新编码再处理，不保留原始容器里的附加数据 |
| 恶意文件扫描 | **当前缺口**：F-14a 已记录"恶意文件扫描未接入"。本方案要求在图片搜索上线前接入扫描（ClamAV 或对象存储侧扫描），单独开卡；在扫描接入前，查询图只允许进入 imgvec 的内存解码路径，不允许被任何其它服务读取 |
| 频率限制 | 图片搜索单独一个限流桶（既有限流已支持按账号/IP/接口分桶）：IP **10 次/分钟**，账号 **30 次/10 分钟**，超限返回 `1004 TooManyRequests` |
| 并发保护 | 路由归 `heavy` 类，受全局 inflight 与 brownout 自动降级约束 |

### 5.3 日志与可观测

- **日志不得包含**：图片字节、base64、向量数值、预签名 URL（带签名参数）。
- **可以记录**：`request_id`、`file_id`、`model_rev`、框数量、结果条数、最高分、各阶段耗时、错误码。
- 指标：`imgvec_request_total{result}`、`imgvec_latency_ms`（分 detect/embed/search 三段）、`imgvec_breaker_state`、`image_search_empty_rate`、`image_embed_queue_lag`。
- 审计：商户图入库/清理走既有审计链；买家查询**不入审计**（避免沉淀个人照片相关记录）。

### 5.4 本方案不触碰的路径

不涉及账务 ledger、提现、鉴权/IAM、密钥与信封加密的任何逻辑变更。涉及**文件系统/上传/新依赖**，因此按公司规则**必须经安全审查员**评审（见 §8）。

---

## 6. 接口草案

### 6.1 `POST /v1/public/search/image`

收 `file_id`（预签名直传之后），**不收 multipart**：与 `storage` 包"never proxies file bytes"的既有决定一致，也避免 8 MB 请求体打在 API 服务器上。

```yaml
  /v1/public/search/image:
    post:
      operationId: searchByImage
      summary: 以图搜商品（拍照找同款）
      tags: [public, search]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              required: [file_id]
              properties:
                file_id:
                  type: string
                  description: F-14b 上传完成并校验通过的查询图文件 ID（query/ 前缀）
                box:
                  $ref: '#/components/schemas/NormalizedBox'
                  description: 手动框选区域；给定时服务端不再自动检测
                category_id:
                  type: string
                price_min_e4:
                  type: integer
                  format: int64
                price_max_e4:
                  type: integer
                  format: int64
                page:
                  $ref: '#/components/parameters/PageParam'
                page_size:
                  $ref: '#/components/parameters/PageSizeParam'
      responses:
        '200':
          description: 检索结果（可能为空列表）
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ImageSearchSuccessResponse'
        '400': { $ref: '#/components/responses/ErrorResponse' }  # 1002 / 1008
        '413': { $ref: '#/components/responses/ErrorResponse' }  # 1007
        '429': { $ref: '#/components/responses/ErrorResponse' }  # 1004
        '503': { $ref: '#/components/responses/ErrorResponse' }  # 2008 ImageSearchUnavailable

components:
  schemas:
    NormalizedBox:
      type: object
      additionalProperties: false
      required: [x, y, w, h]
      properties:
        x: { type: number, minimum: 0, maximum: 1 }
        y: { type: number, minimum: 0, maximum: 1 }
        w: { type: number, exclusiveMinimum: 0, maximum: 1 }
        h: { type: number, exclusiveMinimum: 0, maximum: 1 }
    ImageSearchData:
      type: object
      additionalProperties: false
      required: [items, page, page_size, total, detected_boxes, fallback]
      properties:
        items:
          type: array
          items: { $ref: '#/components/schemas/ImageSearchItem' }
        page:      { type: integer, minimum: 1 }
        page_size: { type: integer, minimum: 1 }
        total:     { type: integer, minimum: 0 }
        detected_boxes:
          type: array
          description: 自动检测到的主体框，供前端绘制与手动调整
          items:
            type: object
            additionalProperties: false
            required: [box, confidence]
            properties:
              box:        { $ref: '#/components/schemas/NormalizedBox' }
              confidence: { type: number }
              label:      { type: string }
        fallback:
          type: string
          enum: [none, full_image, category, unavailable]
          description: none=正常；full_image=未检出主体已按整图搜；category=无命中已给关键词；unavailable=推理不可用
        suggest_keywords:
          type: array
          items: { $ref: '#/components/schemas/Messages' }
    ImageSearchItem:
      type: object
      additionalProperties: false
      required: [product_id, score, match_level]
      properties:
        product_id:  { type: string, description: catalog.product.public_id (ULID) }
        name:        { $ref: '#/components/schemas/Messages' }
        cover_url:   { type: string, description: 短时效预签名 GET URL }
        price_e4:    { type: integer, format: int64 }
        score:       { type: number, minimum: 0, maximum: 1 }
        match_level: { type: string, enum: [same, similar] }
```

响应统一包在既有 `SuccessEnvelope`（`ok`/`data`/`request_id`）与 `ErrorEnvelope`（`ok`/`error{code,message{zh,en,km}}`/`request_id`）里，分页沿用 `page`/`page_size`/`total`。

### 6.2 `GET /v1/public/products/{public_id}/similar`

```yaml
  /v1/public/products/{public_id}/similar:
    get:
      operationId: listSimilarProducts
      summary: 找相似（以该商品主图向量检索）
      tags: [public, search]
      parameters:
        - name: public_id
          in: path
          required: true
          schema: { type: string, minLength: 26, maxLength: 26 }
        - $ref: '#/components/parameters/PageSizeParam'
        - name: same_category
          in: query
          schema: { type: boolean, default: false }
      responses:
        '200':
          description: 相似商品列表（不含自身）
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ImageSearchSuccessResponse'
        '404': { $ref: '#/components/responses/ErrorResponse' }  # 1003
        '429': { $ref: '#/components/responses/ErrorResponse' }  # 1004
```

该接口**不调用推理服务**（用已入库向量），因此推理机挂掉时它仍可用；只有当该商品没有任何向量（图片刚上传、还在排队）时返回空列表 + `fallback: "category"`。

### 6.3 契约层面的配套改动

1. **新增错误码 `2008 ImageSearchUnavailable`**（`x-enum-varnames: ImageSearchUnavailable`）。现有 `1005 ServiceUnavailable` 太泛，前端无法据此只降级图片搜索一项。其它情形复用既有码：`1002 InvalidArgument`、`1003 ResourceNotFound`、`1004 TooManyRequests`、`1007 RequestTooLarge`、`1008 UnsupportedMedia`。
2. **路由分类**：`/v1/public/search/image` 的路径里有 `search` 段，现有 `hasHeavySegment` 会自动归入 `heavy`，无需配置。但 `/v1/public/products/{public_id}/similar` 会落到默认的 `read`，必须显式配置（`Bulkhead.Classify` 的规则匹配支持 `{}` 模板）：
   `BULKHEAD_ROUTE_CLASSES=/v1/public/products/{public_id}/similar=heavy`
3. OpenAPI 改动后必须用**固定版本**的 oapi-codegen 重新生成 `internal/api/gen/openapi.gen.go`，并以"重跑生成后 diff 为空"证明规格与实现一致。

---

## 7. 验收与评测

### 7.1 样本集（PoC 卡的前置交付物）

- 覆盖试点品类，每类 ≥ 20 组，合计 **≥ 200 组查询**。每组 = 1 张"买家视角照片"（手机拍、有背景杂物、光线不均、有角度）+ 该商品在库的正确 `product_id`。
- 品类按试点实际商品构成确定，至少包含：有包装文字的（涂料、水泥、瓶装日用品）、无文字靠纹理的（瓷砖、板材）、小五金（管件、螺丝）、外形相近易混的（不同规格同款外观）。
- 另备 **20 张负样本**（库里没有的商品、人像、风景、纯色墙面），用于校准门槛与"无结果"路径。
- 样本集存放在仓库外的内部共享目录，**不进 Git**（含实拍照片）；仓库里只放清单 CSV（文件名 → 正确 product_id）。

### 7.2 目标指标

公开基准（Visual Product Search Benchmark，百万级干扰项）上开源模型 image→image R@1 约 41.9~43.8、R@5 约 60~61。我们的库小得多（试点万级向量），指标应显著高于公开基准，但**不要把下面的数字当成已经成立的结论**，它们是验收门槛：

| 指标 | 目标（试点） | 说明 |
| --- | --- | --- |
| top-1 命中率 | ≥ 50% | 正确商品排第一 |
| **top-5 命中率** | **≥ 70%** | **主验收指标**（任务要求） |
| 负样本正确拒绝率 | ≥ 85% | 20 张负样本中返回空或不含高分同款 |
| 无结果率 | ≤ 15% | 正样本里返回空列表的比例 |

不达标的处置顺序：① 调门槛与 `ef_search`、开启裁剪；② 换 `facebook/dinov2-base` 或做 SigLIP2+DINOv2 双向量取最大分；③ 升级到 siglip2-large/so400m 并配 GPU；④ 把该品类标记为"图片搜索效果有限"，前端引导文字搜索。**任何一步都不许用"把门槛调低让它有结果"来充数。**

### 7.3 压测指标

| 指标 | 目标 |
| --- | --- |
| `/v1/public/search/image` P95（含上传后到返回，不含客户端上传耗时） | ≤ 1500 ms（CPU 推理）/ ≤ 600 ms（GPU） |
| `/v1/public/products/{id}/similar` P95 | ≤ 150 ms |
| 图片搜索并发 | 持续 5 QPS 不超时；20 QPS 冲击下 `heavy` 桶拒绝但 **`critical`/`read` 零影响**（这是必须验证的隔离性，不只是性能数字） |
| 推理不可用演练 | 停掉 imgvec：图片搜索 100% 返回 2008，文字搜索成功率 ≥ 99.9% |
| 入库吞吐 | ≥ 3 图/秒（CPU，batch 队列），1 万图全量重建 ≤ 1 小时 |
| 向量查询 | 10 万向量下单次 KNN（LIMIT 200）P95 ≤ 50 ms |

---

## 8. 工作量拆分（可派发的卡）

均**不属于**资金 / IAM 路径（不碰 ledger、提现、鉴权、迁移密钥）。但 I-2、I-3、I-5 涉及文件系统 / 上传 / 新依赖，**必须经安全审查员**；全部卡都要先过代码审查员。作者与审查员不得同人。

| 卡 | 标题 | 依赖 | 负责岗位（建议） | 审查链 | 关键交付 |
| --- | --- | --- | --- | --- | --- |
| **I-0** | 图片搜索 PoC：样本集 + 模型选型实测 | 本方案获批 | Go 高级 或 数据/算法 | 代码审查员 | 200 组样本、SigLIP2 与 DINOv2 实测 top-1/top-5、CPU 单图延迟实测、选型结论。**不达标则后续卡不开工** |
| **I-1** | pgvector 引入与向量表迁移 | I-0 通过；**依赖审批**（扩展 + 镜像） | 数据库工程师 | 代码审查员 → 安全审查员 | 扩展迁移（superuser 拆分）、`catalog.product_image_vector`、HNSW 索引、迁移编号与未合并分支核对、dev compose 换镜像、回滚脚本 |
| **I-2** | `imgvec` 推理服务 | I-0 通过；**依赖审批**（Python 服务 + onnxruntime + 模型权重下载） | Go 高级（服务骨架）+ 运维（部署） | 代码审查员 → 安全审查员 | FastAPI + ONNX 服务、`/embed` `/healthz` `/readyz`、token 鉴权、内网防火墙规则、模型权重与许可证登记、Dockerfile、ONNX 导出脚本与 `model_rev` 约定 |
| **I-3** | 向量入库流水线 | I-1、I-2、F-14b(CMP-584) | Go 高级 | 代码审查员 → 安全审查员 | outbox 事件、asynq `catalog:image_embed:v1` / `image_vector_purge:v1` / `image_reindex:v1`、幂等与重试、`queuectl` 子命令、worker 测试 |
| **I-4** | 检索接口（OpenAPI + handler） | I-1、I-2 | Go 高级 | 代码审查员 | `POST /v1/public/search/image`、`GET /v1/public/products/{id}/similar`、错误码 2008、`BULKHEAD_ROUTE_CLASSES` 配置、熔断与降级测试、重跑 oapi-codegen 零 diff |
| **I-5** | 买家端前端（相机入口 / 取景框 / 结果页 / 找相似按钮） | I-4 | 前端 | 代码审查员 → 安全审查员（上传路径） | 相机与相册入口、客户端压缩、预签名直传、框选交互、三档结果展示、三语文案、四种兜底 UI、浏览器实测（含窗口尺寸） |
| **I-6** | 恶意文件扫描接入（补 F-14a 缺口） | — | 运维 + Go 高级 | 代码审查员 → 安全审查员 | 扫描服务接入、未扫描文件不可被消费、告警。**图片搜索上线前必须完成** |
| **I-7** | 压测与上线演练 | I-3、I-4、I-5 | 运维 + QA | 代码审查员 | §7.3 全部指标的真实输出、停推理演练、runbook（REINDEX、全量重建、熔断处置） |

建议排期：I-0 → （I-1 ∥ I-2）→ I-3 → I-4 → I-5 → I-7；I-6 与 I-1/I-2 并行。

### 需要董事会先批准的事项（实施前，逐项）

1. 新依赖：PostgreSQL `vector` 扩展（pgvector 0.8.7）；dev 镜像由 `postgres:16-alpine` 改为 `pgvector/pgvector:pg16`。
2. 新服务与新运行时：`imgvec`（Python 3.12 + FastAPI + onnxruntime），部署到内网 AI 机。
3. 下载外部文件：`google/siglip2-base-patch16-224`、`facebook/dinov2-base`（备选）、`PekingU/rtdetr_r50vd` 模型权重，合计约 3 GB；需登记来源、版本号（pin revision）与许可证。
4. 许可证确认：三者均 Apache-2.0；**明确排除** jina-clip-v2（CC-BY-NC-4.0）与 Ultralytics YOLO（AGPL-3.0）。
5. 样本集拍摄与存放（含实拍商品照，不进 Git）。

---

## 9. 与 ADR-001「替换内核、保留外壳」的关系

图片搜索是新增能力，不改动既有外壳：复用现有 chi 路由与 `SuccessEnvelope`/`ErrorEnvelope` 契约、现有限流与 `overload` 并发桶、现有 asynq 六队列与 outbox、现有 `storage` 预签名上传。新增的只有：一张独立向量表、一个内网推理服务、两个公开读接口、三个队列任务类型。推理服务放在进程外，保证 Go 后端不被 Python 运行时污染；模型可替换（`model_rev` 双写切换），不与业务逻辑耦合。

## 10. 未解问题（需要项目负责人/董事会定）

1. 试点品类的具体构成与样本集由谁拍摄（影响 I-0 排期）。
2. 内网 AI 机是否允许 imgvec 与 Ollama 共用 GPU，以及显存配额上限。
3. 查询图留存是"检索完即删"还是"24 小时"——本方案推荐**两者都做**（即时删 + 生命周期兜底），需确认无运营侧需要回看查询图的需求。
4. 高棉语"文字搜图"是否纳入后续规划（SigLIP2 的高棉语覆盖未核实，需单独验证；现有文字搜索在高棉语上本就偏弱）。
