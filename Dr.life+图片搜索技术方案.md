# Dr.life+ 图片搜索技术方案（T-03c）

- 卡号：CMP-658
- 作者：Architect
- 日期：2026-10-29
- 状态：**董事会已审阅通过（2026-10-29）；项目负责人已确认 5 项决定（2026-10-29，董事会转达）**
- 范围：本文只出技术方案，不含业务代码。
- 本版（第 3 版）落实项目负责人的 5 项决定，逐条见 §11 决定台账：
  1. §8 待批准第 1–4 项**全部批准**；先做 I-0 PoC，top-5 ≥ 70% 才进入 I-1 起的开发。
  2. PoC 样本**先用允许商用的公开商品图片数据集**（§7.1），实拍样本日后复测。
  3. `imgvec` 与 Ollama **同机共用 GPU**，显存上限 2 GB，并给 Ollama 加保护（§2.5）。
  4. 买家查询图**保留 7 天**供运营改进搜索（不再"检索完即删"），配独立权限 + 每次查看写审计 + 仅内网；隐私政策须同步改版（§5.1、§5.5）。
  5. **文字搜图试点就做**（§1.3、§2.6、§6.4），I-0 一并实测中/英/高棉语。

## 0. 结论先行（架构推荐）

| 编号 | 决策 | 推荐 | 一句话理由 |
| --- | --- | --- | --- |
| D1 | 推理放在哪 | **独立内网推理服务 `imgvec`（HTTP），不走 Ollama** | Ollama 的图片向量只有 `embeddinggemma-2` 一个模型，且只实现在 MLX（Mac）runner，Linux/Windows 连拉取都会失败（见 §2.1 证据）。 |
| D2 | 用哪个模型 | **`google/siglip2-base-patch16-224`（Apache-2.0，768 维）**；备选 `facebook/dinov2-base` | Apache-2.0 可商用、768 维适合 pgvector、能读包装上的文字与品牌（建材/日用品关键），且留下"文字搜图"的后路。 |
| D3 | 向量存哪 | **自建 PostgreSQL 16 + pgvector 0.8.7，`halfvec(768)` + HNSW cosine，单独表** | 生产库是自建的，可装扩展；不引入第二套存储；halfvec 省一半空间并避开 TOAST。**不用 IVFFlat**（有 CVE 且需先有数据）。 |
| D4 | 怎么入库 | F-14b 上传校验通过 → outbox → asynq `ai` 队列 → 写向量，带 `model_rev` | 复用既有 outbox + asynq 可靠投递，换模型时双写再切换。 |
| D5 | 买家照片 | **对象存储 7 天生命周期自动删除**（项目负责人 2026-10-29 决定，改自原"检索完即删"），独立前缀，运营查看需独立权限 + 审计 + 内网，日志不留图 | 运营要回看查询图来改进搜索。代价是隐私政策必须改版、且要新建一套受控查看通道（§5.1、§5.5）。 |
| D6 | 接口形态 | `POST /v1/public/search/image` 收 **file_id**（不收 multipart） | 与既有"文件不经过后端"的设计一致（`storage` 包注释：*never proxies file bytes*）。 |
| D7 | 先验证再开发 | I-0 PoC 分两段：**P1 公开数据集**出参考基线 → **P2 实拍复测**，**top-5 ≥ 70% 的验收门槛以 P2 为准** | 公开数据集是"目录图搜目录图"，与"买家手机照搜目录图"不是同一个任务，P1 的数字只能用来选模型、不能当上线依据（§7.1）。 |
| D8 | 文字搜图 | **试点就做**：复用同一向量索引，文本塔算查询向量；新增 `GET /v1/public/search/image-by-text` | SigLIP2 文本塔与图像塔同空间（768 维，已核实），不需要第二套索引。 |
| D9 | 高棉语文字搜图 | **v1 直接回退到普通文字搜索**，不做 SigLIP2 高棉语检索 | 实测 SigLIP2 的 Gemma 词表只有 88 个高棉文 token、仅 1 个多字 token，等于逐字符切分，没有任何高棉语子词（§2.6 证据）。 |
| D10 | GPU 共用 | 与 Ollama 同机同卡，`gpu_mem_limit=2GiB`，**imgvec 先起并预热、Ollama 后起** | Ollama 只在模型加载时对空闲显存拍一次快照，之后别的进程抢显存它完全不知道（源码注释自承认，§2.5）。 |

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
  3. 推理服务不可用 → 返回错误码 `2009 ImageSearchUnavailable`，三语文案"图片搜索暂时不可用，请用文字搜索"。**文字搜索路径完全不受影响**（§2.4）。

### 1.3 文字搜图（输入文字按图片内容找商品）

项目负责人 2026-10-29 决定：**试点就做**。

- **它和现有文字搜索的区别**：现有 `GET /v1/public/products` 搜的是商品**标题与描述文本**（pg_trgm）；文字搜图搜的是**商品图片的视觉内容**。所以"白色方形瓷砖""带手柄的蓝色水桶"这类描述——商家标题里往往没写——才能命中。两者是互补，不是替代。
- **入口**：搜索结果页加一个"按图片内容找"的切换；无文字结果时自动提示改用这一项（这是它最大的价值场景：标题没写但图里看得见）。
- **实现**：用户输入文字 → `imgvec` 的**文本塔**算出 768 维查询向量 → **打同一张 `catalog.product_image_vector` 索引**。不需要第二套索引，因为 SigLIP2 的文本塔与图像塔在同一向量空间（已核实：两塔输出都是 768 维、共用一个 `logit_scale`）。
- **语言分流**（依据 §2.6 的实测结论）：

  | 语言 | v1 行为 | 理由 |
  | --- | --- | --- |
  | 英文 | 走文字搜图 | SigLIP2 训练数据 90% 是英文网页（论文原文核实） |
  | 中文 | 走文字搜图，但**上线前必须用自己的商品数据抽测一次 recall** | 中文属于那 10% 非英文里的高资源语言，Gemma 词表有 23767 个 CJK token；但 10% 这个占比值得先验证再开 |
  | 高棉语 | **直接回退普通文字搜索**，不调推理 | 见 §2.6：词表里高棉文只有 88 个 token、1 个多字 token，逐字符切分，没有词汇单元 |

- **门槛与兜底**：文字搜图的分数分布与图搜图不同（跨模态分数普遍更低），**必须单独标定门槛 `IMGVEC_TEXT_MIN_SCORE`，不要复用 0.62**；低于门槛一律回退普通文字搜索并提示"已按商品文字搜索"，不能白屏。
- **文本长度硬限制**：SigLIP2 文本塔的位置编码是 `[64, 768]`，**64 token 是架构上限不是约定**（已核实权重张量形状）；超长查询必须截断，且处理器默认会**转小写**——这两条都要跟 `AutoProcessor` 的默认行为保持一致，否则检索质量下降。

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
| **google/siglip-base-patch16-256-i18n**（mSigLIP，SigLIP **1**） | 1186 MB | **768** | 256 | Apache-2.0 | ✅ | **是，且有高棉语实测分** | Babel-ImageNet 高棉语 top-1 **31.54**，公开模型最高；词表含 2082 个高棉文片段（1988 个多字）。英文弱于 SigLIP2 → 列为**高棉语专用备选**（§2.6） |
| jinaai/jina-clip-v2 | 1731 MB(fp16) | 1024 | 512 | **CC-BY-NC-4.0** | ❌ | 89 种，**卡片未列 `km`** | 权重禁止商用 → **排除**（上一版写"唯一覆盖高棉语"，本版更正：卡片语言表并未列出 `km`，属未核实） |
| OFA-Sys/chinese-clip-vit-base-patch16 | 753 MB | 512 | 224 | MIT（在 GitHub，HF 卡无 license 字段） | ✅ | 仅中文 | 中文检索强，但柬埔寨场景用不上 |
| Marqo/marqo-fashionSigLIP | 813 MB | 768 | 224 | Apache-2.0 | ✅ | 无 | 服装专精；Dr.life+ 试点是建材/日用品，不匹配 |

**推荐：`google/siglip2-base-patch16-224`**，理由按权重排序：

1. Apache-2.0，权重可自托管，无商用限制；
2. 在公开商品检索基准上 SigLIP2 ≥ DINOv3 > DINOv2，是开源里最稳的一档；
3. **能读图里的文字/品牌**——建材与日用品大量靠包装文字区分规格（DINOv2 对图中文字基本"失明"，MIEB 的 Visual STS 上接近随机）；
4. 768 维，`halfvec` 下单行 1544 B 可内联存储，索引也小；
5. 文本塔在同一向量空间，直接支撑 §1.3 的文字搜图（中/英）。**上一版此处写"高棉语文字检索正是 V1 的弱项，这是一条有价值的后路"——本版更正：这条后路对高棉语不成立**，SigLIP2 的高棉语分词已实测不可用（§2.6）。中/英的后路仍然成立。

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
| `IMGVEC_BASE_URL` | 空 | 空 = 图片搜索整体关闭（接口直接返回 2009），不影响其它功能 |
| `IMGVEC_TIMEOUT_QUERY` | 1500ms | 买家查询路径，超时即降级 |
| `IMGVEC_TIMEOUT_INDEX` | 10s | 队列入库路径，可以慢 |
| `IMGVEC_MAX_CONCURRENT` | 4 | 与 asynq `ai` 队列并发 4 对齐，避免推理机被打满 |
| `IMGVEC_BREAKER_FAILS` / `_WINDOW` / `_COOLDOWN` | 5 / 30s / 60s | 熔断：窗口内 5 次失败 → 冷却 60s，期间查询直接返回 2009，不再打推理机 |

降级与隔离（必须成立的性质，写成测试）：

1. 推理不可用时 `POST /v1/public/search/image` 返回 2009，**文字搜索 `GET /v1/public/products` 的成功率与延迟不受影响**；
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
- GPU 不是 v1 必需；但项目负责人已决定与 Ollama **同机共用 GPU**，因此下面的共存规则从一开始就要落地。

### 2.5.1 与 Ollama 共用一张 GPU（项目负责人 2026-10-29 决定）

显存上限按决定取 **2 GB**。实测数字支持这个额度：`onnx-community/siglip2-base-patch16-224-ONNX` 的 `vision_model_fp16.onnx` 只有 **186 MB**（图搜图只需视觉塔），batch=1 激活峰值在几十 MB 量级，2 GiB 有充足余量。

**第一条要记住的事：`gpu_mem_limit` 只约束 arena，不约束进程。** onnxruntime 官方文档原文："*This size limit is only for the execution provider's arena. The total device memory usage may be higher.*" 不受它约束的部分包括 CUDA context（每进程 **300~600 MB**，NVIDIA 明确表示没有可靠的通用测法，必须在目标卡实测）、cuDNN/cuBLAS 句柄与卷积 workspace。**所以向 Ollama 申报的占用必须按 `nvidia-smi` 口径算 ≈ 2 GiB + context ≈ 2.5~3 GB，不是 2 GB。**

imgvec 侧（Python）必须写成这样：

```python
CUDA_OPTS = {
    "device_id": 0,
    "gpu_mem_limit": 2 * 1024 * 1024 * 1024,      # 单位是字节；仅约束 BFC arena
    "arena_extend_strategy": "kSameAsRequested",  # 默认 kNextPowerOfTwo 会按 2 的幂次过度扩张
    "cudnn_conv_algo_search": "HEURISTIC",        # 默认 EXHAUSTIVE 启动慢且额外占显存
    "cudnn_conv_use_max_workspace": "0",          # 默认 1 可能"增加峰值显存达数 GB"（官方文档原话）
}
sess = ort.InferenceSession(path, providers=[("CUDAExecutionProvider", CUDA_OPTS)])
sess.disable_fallback()
```

**第二条坑：onnxruntime 的 `enable_fallback()` 不会接住显存 OOM。** 它的源码只捕 `C.EPFail`，而显存不足抛的是 `Fail`（"*Available memory of … is smaller than requested bytes of …*"、"*Failed to allocate memory for requested buffer of size …*"）或 `RuntimeException`。所以"OOM 时降级到 CPU"必须自己实现：捕 `Fail`/`RuntimeException` → 按消息串匹配 → 转发到一个**预先建好的 CPU session** → 打点并熔断 N 秒后再试 GPU；熔断期间若 CPU 也顶不住就返回 `2009`。另外 `providers=[CUDA, CPU]` 里的 CPU EP **只是图分区期的算子回落，不是运行期故障转移**，不要误以为它管 OOM。

**第三条坑，也是最危险的一条：Ollama 只在模型加载时对空闲显存拍一次快照。** 官方 FAQ 原文："*When loading a new model, Ollama evaluates the required VRAM for the model against what is currently available.*" 而 `server/sched.go` 里有一条维护者自己写的注释直说了盲区：

> `// The risk is if some other GPU intensive app is loaded after we start our first runner, then we'll never account for that`

后果：**如果 Ollama 先加载模型、imgvec 之后才去申请显存，Ollama 会在推理中途 `cudaMalloc failed: out of memory`**（已有实证 issue：ollama/ollama#16147、#3765，后者用户实测"Steam 开着和不开，Ollama 的显存日志一模一样"）。因此：

1. **启动顺序必须是 imgvec 先起、并在健康检查里跑一次真实推理预热**（把 context + 权重 + arena 峰值全部实际分配下去），**然后才起 Ollama**。用 systemd `After=` / `Requires=` 固定住。
2. **imgvec 重启必须连带重载 Ollama**；反之 Ollama 单独重启是安全的（它会重新快照）。
3. **禁止 imgvec 在运行期扩大 batch 或增加并发 worker**——任何运行期显存增长都落在 Ollama 的快照之外。batch 档位与 worker 数固定。

Ollama 侧的保护参数（已逐个核实存在性与语义）：

| 参数 | 建议值 | 说明 |
| --- | --- | --- |
| `OLLAMA_GPU_OVERHEAD` | `3221225472`（3 GiB，**字节**） | "Reserve a portion of VRAM per GPU"。**但有已知失效路径**：走 llama-server runner 的模型，层放置由 llama.cpp 的 `--fit` 决定，Ollama 目前没把 `GpuOverhead()` 折进去（ollama/ollama#18679，查证时仍 Open） |
| `LLAMA_ARG_FIT_TARGET` | `3072`（**MiB**） | 上一条的兜底：直接给 llama.cpp 的 `--fit-target` 设每卡余量（默认只有 1024 MiB） |
| `OLLAMA_MAX_LOADED_MODELS` | `1` | 禁止并发多模型抢显存 |
| `OLLAMA_NUM_PARALLEL` | `1` | 官方 FAQ：显存随 `NUM_PARALLEL × CONTEXT_LENGTH` 线性增长 |
| `OLLAMA_CONTEXT_LENGTH` | 显式钉死（如 `4096`） | 默认按总显存自动选 4k/32k/256k 档，会把余量吃掉 |
| `OLLAMA_KEEP_ALIVE` | `-1`（常驻） | 避免反复卸载/加载时重新快照空闲显存 |
| `OLLAMA_MAX_VRAM` | **不要写** | 已从 `envconfig/config.go` 移除，写了无效 |

**最稳的做法**：在 Modelfile 或请求 options 里写死 `num_gpu`（GPU 层数，`-1` 才是动态）。一旦显式给出，Ollama 不再按"当前空闲显存"推断，占用变成确定值，从根上规避上面整类漂移。层数需在目标卡标定一次。

**不要用的两条路**（都已核实不可行或有害）：
- **MIG**：只支持数据中心卡（H100/A100/RTX PRO Blackwell 等），**GeForce RTX 3090/4090 不在支持列表**，开不了。
- **CUDA MPS 的 `CUDA_MPS_PINNED_DEVICE_MEM_LIMIT`**：这是唯一真正的进程级硬上限（Linux + CUDA 11.5+，消费卡可用），但限额生效后 `cudaMemGetInfo` 会**把限额当成 free、把整卡当成 total 上报**——这恰好会喂错 Ollama 的空闲显存探测，反而制造 OOM。另有一个静默陷阱：变量里若写了本进程看不见的 GPU，**所有** pinned 限额会整体失效。故 v1 不用 MPS，用"软上限 + 启动顺序 + 监控"。
- Linux 的 DMEM cgroup 显存限制只覆盖 `amdgpu`/`xe`/`nouveau`，NVIDIA 闭源驱动不支持（此条为二手来源，标**未核实**）。
- TensorRT EP 也不要用：它没有 `gpu_mem_limit` 等价项，`trt_max_workspace_size` 默认 0 的含义是"最大设备显存"。

显存预算（24 GB 卡示例，context 一项须在目标卡实测替换）：

| 项 | 预算 |
| --- | --- |
| imgvec：CUDA context + 库模块 | 0.6 GB（**推算，待实测**） |
| imgvec：arena（`gpu_mem_limit`） | 2.0 GB |
| **imgvec 小计（`nvidia-smi` 口径）** | **≈ 2.6 GB** |
| 向 Ollama 申报（`OLLAMA_GPU_OVERHEAD` / `LLAMA_ARG_FIT_TARGET`） | **3.0 GB / 3072 MiB** |
| Ollama 可用 | ≈ 21 GB |

监控（I-7 的 runbook 必须含）：采样 `nvidia-smi --query-compute-apps=pid,used_memory`；盯 Ollama 日志里的 `gpu memory ... available/free/minimum/overhead` 与 `model predicted to exceed available memory, evicting` 两行，作为"预算算错"的早期信号。

**文字搜图对显存的额外影响**：文本塔 fp16 是 **565 MB**（比视觉塔 186 MB 大得多，因为 256k 多语言词表）。推荐**文本塔跑 CPU**——64 token、12 层的一次前向在 CPU 上是毫秒级，且文字查询可走缓存（§6.4），没必要占 GPU 预算；若放 GPU，2 GiB 额度要重算。

### 2.6 文字搜图的模型侧结论（含高棉语的实测证据）

本节是本版新增的核实结果，直接决定 §1.3 的语言分流。

**已核实（一手）：**

1. `google/siglip2-base-patch16-224` 的两塔同空间：从 safetensors 头部读到 `text_model.head.weight` 形状 `[768,768]`、`vision_model.head.probe` 形状 `[1,1,768]`，共用一个 `logit_scale`。**文字搜图可以直接打图像向量索引，不需要第二套索引。**
2. 文本长度上限 **64 token 是架构硬限制**：`text_model.embeddings.position_embedding.weight` 形状就是 `[64, 768]`。
3. 分词器是 **Gemma 的 SentencePiece**，`vocab_size=256000`，**查询会被转小写**。
4. 训练数据语言构成：论文原文为 "*90% of the training image-text pairs is sourced from English web pages, and the remaining 10% from non-English web pages*"。**上一版写的"训练数据 10% 非英文"属实。**
5. **SigLIP 2 没有 `-i18n` 变体**（SigLIP 1 才有）；所有 `google/siglip2-*` checkpoint 都是同一个 90/10 混合。

**高棉语：证据指向"做不了"，不是"未核实"。** 上一版把这一项标为"未核实"，本版核实到了，结论是负面的：

- 论文全文**没有出现 "Khmer"**；
- SigLIP 2 唯一的多语评测是 Crossmodal-3600（XM3600），**它的 36 种语言里不含 `km`**；
- 直接下载 `tokenizer.json`（34 MB、256000 词条）逐条统计 U+1780–U+17FF：**高棉文只有 88 个 token，其中多字 token 只有 1 个（`្រ`），最长 2 字符，仅 3 条 BPE 合并规则涉及高棉文**。对照：泰文 1287 个（1160 个多字）、CJK 23767 个（12512 个多字）。也就是说高棉文是**逐字符切分、没有任何词汇单元**（不是字节级碎片，86/128 个码位有独立 token，但仅此而已）。

对照组恰好说明这是 SigLIP 2 的**退步**：SigLIP 1 的 i18n 版（mSigLIP，250k mC4 式词表）里有 **2082 个高棉文片段、1988 个多字片段**，包含 `នាយករដ្ឋមន្ត្រី`（总理）、`រាជធានីភ្នំពេញ`（金边）、`សាកលវិទ្យាល័យ`（大学）这类完整词；它在 Babel-ImageNet 高棉语 top-1 拿 **31.54**，是公开模型里最高的（NLLB-SigLIP-large 30.16、M-CLIP 19.77、AltCLIP 6.72、OpenCLIP H-14 仅 3.16，167 类的随机水平约 0.6%）。

**因此 v1 按 D9 处理：高棉语不调推理，直接回退普通文字搜索。** 两条备选路线留给后续卡，都已确认是 Apache-2.0 可商用，但都不在 v1：

| 路线 | 做法 | 代价 |
| --- | --- | --- |
| A. 高棉语专用第二索引 | `google/siglip-base-patch16-256-i18n`（mSigLIP，Apache-2.0，**同为 768 维**，可共用 `halfvec(768)` 表 + `model_rev` 区分） | 多一份向量与一次全量重建；该模型英文弱于 SigLIP2 |
| B. 先翻译再检索 | 高棉语 → `google/madlad400-3b-mt`（Apache-2.0，MADLAD-400 里 `km` 有 1.1B token）或 `facebook/m2m100_418M`（MIT）→ 英文 → SigLIP2 文本塔 | 多一跳延迟与一个 3B 模型；翻译错了会静默降低召回 |

**排除**：`facebook/nllb-200-*` 与 NLLB-CLIP 系（**CC-BY-NC-4.0，不可商用**，上一版对 NLLB 系许可证的怀疑已核实为真）。

**标注为未核实的项**（不要写进对外材料）：WebLI 的 109 种语言清单是否含高棉语及其占比；Gemma 词表高棉语 fertility 的官方数值（上面的统计是我对实际 tokenizer 文件的实测，不是论文引用）；`jina-clip-v2` 的 89 语言完整清单是否真的排除高棉语（卡片未列全）；Babel-ImageNet 那张表的行列对应为自动提取，对外引用前需二次核对。

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

**本节已按项目负责人 2026-10-29 的决定重写：查询图保留 7 天供运营改进搜索，不再"检索完即删"。**

- **独立前缀**：`query/{yyyymmdd}/{ulid}`，与商品图（`product/...`）隔离，便于单独设生命周期与权限。
- **留存：对象存储生命周期规则 7 天自动删除。** 删除必须由**对象存储的生命周期规则**兜住，不能只靠应用侧定时任务（应用挂了就留图）。前缀按天分片正是为了让生命周期规则可验证——I-7 的演练要实际确认第 8 天的对象确已消失。
- **不建索引、不入向量表**：查询图的向量仍然只在内存中存在，不落库（避免"图删了但留着可反推的向量"）。这一条**没有因为留存 7 天而放松**。
- **EXIF 剥离的要求升级了**：原来图是即时删的，EXIF 只影响推理过程；现在要落盘 7 天，**GPS 坐标会跟着躺 7 天**。因此必须确认 F-14 的 EXIF 剥离发生在**持久化之前**而不是只在推理前，并在 I-0/I-6 里用一张带 GPS 的真实照片实测验证。这是本节最容易被漏掉的一条。

#### 运营查看通道（新增工作量，见 §8 的 I-9）

"保留 7 天供运营改进搜索"意味着员工可以看到买家的照片——照片可能含人脸与住址。因此查看通道不是"给运营开个 S3 链接"，而是一套受控能力：

| 控制项 | 要求 |
| --- | --- |
| 权限 | **独立的后台权限**（如 `ops.image_query.view`），不挂在任何现有管理员角色上，默认无人持有，按需授予；授予与回收本身进入 IAM 审计 |
| 网络 | **仅内网访问**，不经公网入口；与现有运营后台的内网约束一致 |
| 审计 | **每次查看写一条审计**（谁、何时、看了哪个 `file_id`、业务理由），审计记录只增不可删；"看了哪些"本身是敏感信息，审计里只记 `file_id`，不记图片内容或缩略图 |
| 取图方式 | 后台不代理字节，签发**短 TTL（建议 5 分钟）预签名 GET URL**；该 URL 带签名，**绝不进日志**（§5.3） |
| 范围 | 只能按 `file_id` / 时间范围查，**不提供"浏览全部查询图"的列表页**——批量浏览是滥用的温床 |
| 用户删除请求 | A-09 已上线的隐私政策 v1.0 原文承诺"用户可查看、更正、删除、导出数据及撤回同意"，因此 7 天窗口内必须能按账号定位并删除其查询图；这要求查询记录里保留 `account_id` 关联（匿名查询则按 `file_id`） |

**代价要讲清楚**：相比原方案的"即时删"，这条决定新增了一张卡（I-9）、一个新权限、一条审计链、一次隐私政策改版，并把"查询图"从"几乎不存在的中间产物"变成了**一类需要治理的个人数据**。这是产品决定，技术上可行，但上线前置项（§5.5）必须全部完成，否则就是在无政策依据的情况下存了 7 天买家照片。

### 5.2 校验与限制

| 项 | 规则 |
| --- | --- |
| 格式 | 白名单 `image/jpeg`、`image/png`、`image/webp`；**校验魔术字节**，不信 `Content-Type` 与扩展名 |
| 大小 | ≤ 8 MB（经 `storage.Policy.MaxContentLength` 强制，预签名阶段就拒） |
| 像素 | 解码前检查头部尺寸，长边 > 8192px 或总像素 > 4000 万直接拒（防解压炸弹 / decompression bomb） |
| 重编码 | 推理服务统一解码后重新编码再处理，不保留原始容器里的附加数据 |
| 恶意文件扫描 | **当前缺口**：F-14a 已记录"恶意文件扫描未接入"。本方案要求在图片搜索上线前接入扫描（ClamAV 或对象存储侧扫描），单独开卡（I-6）。**7 天留存的决定把这条从"上线前必须"升级为 I-9 的硬前置**：原方案里查询图只进 imgvec 的内存解码路径、几秒后即删；现在它要落盘 7 天，并且**会被运营人员用浏览器打开**——这是一条全新的攻击路径（恶意构造的图片文件 → 员工终端）。因此：扫描未接入前，**不得开启运营查看通道**，即使 7 天留存已启用 |
| 频率限制 | 图片搜索单独一个限流桶（既有限流已支持按账号/IP/接口分桶）：IP **10 次/分钟**，账号 **30 次/10 分钟**，超限返回 `1004 TooManyRequests` |
| 并发保护 | 路由归 `heavy` 类，受全局 inflight 与 brownout 自动降级约束 |

### 5.3 日志与可观测

- **日志不得包含**：图片字节、base64、向量数值、预签名 URL（带签名参数）。
- **可以记录**：`request_id`、`file_id`、`model_rev`、框数量、结果条数、最高分、各阶段耗时、错误码。
- 指标：`imgvec_request_total{result}`、`imgvec_latency_ms`（分 detect/embed/search 三段）、`imgvec_breaker_state`、`image_search_empty_rate`、`image_embed_queue_lag`。
- 审计：商户图入库/清理走既有审计链；**买家的查询动作本身不入审计**（避免沉淀"谁搜了什么照片"的画像），但**运营查看查询图的动作必须入审计**（§5.1）。这两条不矛盾：前者是用户行为，后者是员工对个人数据的访问——后者才是需要留痕的那一方。

### 5.4 本方案不触碰的路径

不涉及账务 ledger、提现、密钥与信封加密的任何逻辑变更。**但本版新增了一处 IAM 接触面**：§5.1 的运营查看通道需要一个新的后台权限（`ops.image_query.view`）。按公司规则，新增权限属 IAM 路径，I-9 必须经安全审查员，并且不得复用任何现有管理员角色来"顺手"获得该权限。其余部分仍只涉及**文件系统/上传/新依赖**，同样必须经安全审查员（见 §8）。

### 5.5 隐私政策与法律文本（上线前置，交 N-07）

保留 7 天必须有政策依据。已核实 A-09（CMP-634，分支 `CMP-634-a-09`）落地的机制，这决定了改版动作的具体形态：

- 迁移 `00026_ops_legal_consent.sql` 建了 `ops.legal_doc`（`kind IN ('privacy','terms')`、`version ~ '^v[0-9]+\.[0-9]+$'`、`body_zh/body_en/body_km` **三语都非空**）与 `ops.consent`（唯一键含 `version`）。
- 已播种 `privacy v1.0`，其正文**只提到"发送外部 AI 前先去除个人信息、聊天原文不用于画像"，完全没有涉及图片留存**——所以现有政策**不能覆盖**本方案。

因此要做的是：

1. 新增一条 `privacy v1.1` 的 `legal_doc`，正文加入项目负责人指定的表述：**"图片搜索的照片保存 7 天用于改进搜索后自动删除"**，并补齐**高棉语**正文（`body_km` 非空是数据库约束，不是可选项）。
2. **这会触发全量重新同意**：`ops.consent` 的唯一键带 `version`，v1.0 的同意记录对 v1.1 无效，A-09 的门禁会对所有买家返回 `2008 CodeConsentRequired`（"请先阅读并同意当前协议"）直到各自重新同意。**这是一次面向全部买家的强制交互，必须和上线排期一起安排，不能在图片搜索上线当天顺手发布。**
3. 顺序约束：**政策 v1.1 生效 → 才允许开启 7 天留存**。在政策生效前，若图片搜索必须先上，则该期间按原设计"检索完即删 + 24 小时兜底"运行（代码里用一个开关控制留存策略，默认即时删），不得先存后补政策。

本项不属本卡交付范围，列为**上线前置**，交 N-07 / 法律文本更新；§8 的 I-9 依赖它。

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
        '503': { $ref: '#/components/responses/ErrorResponse' }  # 2009 ImageSearchUnavailable

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

1. **新增错误码 `2009 ImageSearchUnavailable`**（`x-enum-varnames: ImageSearchUnavailable`）。现有 `1005 ServiceUnavailable` 太泛，前端无法据此只降级图片搜索一项。其它情形复用既有码：`1002 InvalidArgument`、`1003 ResourceNotFound`、`1004 TooManyRequests`、`1007 RequestTooLarge`、`1008 UnsupportedMedia`。

   > **为什么是 2009 而不是 2008**（董事会 2026-10-29 审阅指出）：`main` 的身份段只用到 `2001…2007`，但 `2008` 已被尚未合并的 A-09（CMP-634，分支 `CMP-634-a-09`）用作 `CodeConsentRequired`（"未同意协议"），同分支的 `openapi.yaml` 枚举也已含 2008。只看 `main` 会误判成空闲。
   >
   > **规则：分配任何新错误码前，必须 grep 所有未合并分支的 `backend/internal/platform/httpx`，不能只看 `main`。** 本次核实用的命令（已执行）：
   >
   > ```powershell
   > # 1) 列出所有未合并分支
   > git branch -a --no-merged origin/main
   > # 2) 扫所有分支（本地 + 远端）里 httpx 定义的身份段错误码，确认候选码没被占用
   > foreach ($b in (git for-each-ref --format='%(refname)' refs/heads refs/remotes)) {
   >   git grep -n -E 'ErrorCode = 20(0[89]|[1-9][0-9])' $b -- backend/internal/platform/httpx
   > }
   > # 3) 候选码还要在全仓库（含 openapi.yaml 枚举）确认未使用
   > foreach ($b in (git for-each-ref --format='%(refname)' refs/heads refs/remotes)) {
   >   git grep -n -E '\b2009\b' $b -- backend docs '*.yaml' '*.go'
   > }
   > ```
   >
   > 结果：第 2 步只命中 `CMP-634-a-09` 的 `CodeConsentRequired = 2008`；第 3 步无命中，故 `2009` 可用。I-4 开工时须重跑一遍 —— 届时可能又有新分支占号。
2. **路由分类**：`/v1/public/search/image` 的路径里有 `search` 段，现有 `hasHeavySegment` 会自动归入 `heavy`，无需配置。但 `/v1/public/products/{public_id}/similar` 会落到默认的 `read`，必须显式配置（`Bulkhead.Classify` 的规则匹配支持 `{}` 模板）：
   `BULKHEAD_ROUTE_CLASSES=/v1/public/products/{public_id}/similar=heavy`
3. OpenAPI 改动后必须用**固定版本**的 oapi-codegen 重新生成 `internal/api/gen/openapi.gen.go`，并以"重跑生成后 diff 为空"证明规格与实现一致。

### 6.4 `GET /v1/public/search/image-by-text`（文字搜图，本版新增）

**先说为什么不并入 `GET /v1/public/products` 的参数**——董事会给了"或并入 search 的参数"的选项，但这里有一条硬理由不能并：**bulkhead 是按路由分类的**。`/v1/public/products` 归默认的 `read` 类，而文字搜图每次 miss 都要跑一次文本塔前向。用参数开关切换行为，会让一个 heavy 操作跑在 `read` 并发桶里，直接违反 §2.4 第 2 条"图片搜索不得占用 `read`/`critical` 桶"的隔离目标。独立路径还自带一个好处：路径里有 `search` 段，`hasHeavySegment` 会**自动**归入 `heavy`，不需要配 `BULKHEAD_ROUTE_CLASSES`。

**用 GET 而不是 POST**（与董事会建议的 POST 不同，理由如下，若评审不同意可改）：文本 → 向量是确定性的，同一句话永远得到同一结果，天然适合缓存；`GET` 才能走既有的缓存能力。已核实 `cache` 包的 `Remember` "coalesces concurrent source loads, serves stale values while refreshing, and negative-caches `ErrNotFound`"，这对本接口价值很大——文字搜图的每次 miss 都要付一次推理，合并并发与 stale 回源能直接削掉重复推理。两条约束要注意：该包**只接受 `catalog:`、`config:`、`i18n:` 三个命名空间**，所以缓存键走 `catalog:`；而它明确要求"public caching must never carry … personal data"，因此**缓存值只放商品 ID 列表（目录数据），键用查询串的哈希，绝不缓存向量、绝不缓存图片搜索（买家照片路径一律不缓存）**。这一条请安全审查员在 I-8 复核。

```yaml
  /v1/public/search/image-by-text:
    get:
      operationId: searchImagesByText
      summary: 按图片内容搜商品（文字搜图）
      tags: [public, search]
      parameters:
        - name: q
          in: query
          required: true
          schema: { type: string, minLength: 2, maxLength: 120 }
          description: 自然语言描述；服务端按 SigLIP2 文本塔的约定转小写并截断到 64 token
        - name: lang
          in: query
          required: false
          schema: { type: string, enum: [zh, en, km] }
          description: >
            省略时按请求的 Accept-Language 推断。lang=km 时服务端不调推理，
            直接返回 fallback="text_search"（见 §1.3 与 §2.6）。
        - name: category_id
          in: query
          schema: { type: string }
        - name: price_min_e4
          in: query
          schema: { type: integer, format: int64 }
        - name: price_max_e4
          in: query
          schema: { type: integer, format: int64 }
        - $ref: '#/components/parameters/PageParam'
        - $ref: '#/components/parameters/PageSizeParam'
      responses:
        '200':
          description: >
            检索结果。fallback 取值：null（正常）、"text_search"（高棉语或低于门槛，
            前端应改走 GET /v1/public/products）。
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ImageSearchSuccessResponse'
        '400': { $ref: '#/components/responses/ErrorResponse' }  # 1002
        '429': { $ref: '#/components/responses/ErrorResponse' }  # 1004
        '503': { $ref: '#/components/responses/ErrorResponse' }  # 2009 ImageSearchUnavailable
```

几条实现约束：

- **复用 `2009`**，不再新增错误码：对前端来说"图片搜索这一类能力不可用"是同一种降级。
- 返回体复用 `ImageSearchSuccessResponse`，但 `fallback` 多一个取值 `"text_search"`；高棉语走的就是这条，**且不消耗推理与限流配额**（服务端直接返回，不打 imgvec）。
- 门槛用独立配置 `IMGVEC_TEXT_MIN_SCORE`（§1.3），不复用图搜图的 0.62。
- 限流：与图片搜索**分开计桶**（文字搜图没有上传、成本更低），建议 IP 30 次/分钟；具体值在 I-8 定。

---

## 7. 验收与评测

### 7.1 样本集（PoC 卡的前置交付物）

项目负责人 2026-10-29 决定：**先用允许商用的公开商品图片数据集跑 PoC，实拍样本日后再复测。** 因此 I-0 分两段。

#### 必须先讲清的一件事：P1 测的不是上线要测的那件事

公开数据集里是**目录图**（白底、规整、专业拍摄）。拿目录图检索目录图，测的是"同款目录图匹配"；而买家实际做的是**手机照（有背景、斜角、光线不均）检索目录图**，是跨域检索。前者的命中率系统性高于后者。所以：

- **P1 的数字只能用来选模型、定参数、排除明显不行的候选**，不能当上线依据；
- **top-5 ≥ 70% 的验收门槛以 P2 实拍复测为准**（D7）；
- P1 里要**人为加退化**（随机旋转 ±15°、随机裁剪、降分辨率、加 JPEG 压缩噪声、换背景合成）来模拟手机照，并**同时报告"干净目录图"与"退化图"两组数字**。两组差多少，就是 P2 可能掉多少的早期信号。

#### P1 公开数据集（许可证已逐个核实到一手来源）

| 数据集 | 许可证（一手核实） | 可商用 | 内容与量级 | 在本 PoC 的用途 |
| --- | --- | --- | --- | --- |
| **Amazon Berkeley Objects (ABO)** | **CC BY 4.0**（仓库根目录 `LICENSE-CC-BY-4.0.txt`，AWS Open Data Registry 同口径） | ✅ 仅需署名 | 147702 个商品、**398212 张真实目录图**、576 个 product_type，自托管 | **主力**。唯一同时满足"明确可商用 + 自带图片文件 + 目录级商品摄影"的数据集，家居/厨具/收纳覆盖好 |
| **Open Products Facts / Open Food Facts** | 数据 **ODbL 1.0**、内容 **DbCL 1.0**、**图片 CC BY-SA 3.0**；官方明确写"including commercial use" | ✅ 带 share-alike | 用户实拍的包装商品照，Open Products Facts ≈ 48018 个非食品日用品，可批量下载 | **日用品/瓶装家用品**补充。恰好是"用户实拍"而非目录图，对模拟买家视角有额外价值 |
| **Wikimedia Commons**（经 Commons 分类树 / Wikidata `P18`） | 逐文件 CC0 / PD / CC BY / CC BY-SA；Commons 收录政策**要求**"Commercial use of the work must be allowed" | ✅ 政策级保证，但逐文件义务不同 | 需自建抓取；**唯一有建材/五金深度**（瓷砖、砖、水泥、管件、阀门、螺丝、水龙头、涂料） | 补 ABO 缺失的**建材/五金**类目。只有类目级图片、没有 SKU 级身份标注，用于类目区分度而非同款召回 |

子集选取方法（写进 I-0 的交付物，要可复现）：

1. 从 ABO 的 `product_type` 里挑出家居/厨具/收纳/家装相关的叶子类目，按关键词白名单 + 人工确认，**固定一份类目清单文件**（不是每次跑临时选）。
2. 每类目取 ≥ 20 个商品；**每个商品至少 2 张不同图**（一张进"商品库"建索引，另一张做查询）——这是"同款不同图"的正确构造方式，用同一张图自己搜自己毫无意义。
3. 建材/五金类目用 Commons 补齐，按 Wikidata 类目抓取后**人工剔除**示意图、图表、施工现场照。
4. 日用品类目从 Open Products Facts 按 barcode 取，正面图进库、包装图做查询。
5. 负样本 ≥ 20 张：库里没有的商品、人像、风景、纯色墙面。
6. 合计 **≥ 200 组查询**，与原目标一致。

**三条法律边界（必须遵守，I-0 验收要检查）：**

- **公开数据集的图片与向量只用于 PoC 评测，不得进入生产向量表。** ODbL 的 share-alike 作用在"派生数据库"上，CC BY-SA 3.0 作用在图片上——把它们的向量混进生产索引，会把生产索引暴露在 share-alike 争议下。PoC 用完即删，且与 `catalog.product_image_vector` 物理分离（单独的库或单独的 schema）。
- **不进 Git**：数据集、图片、派生向量都不进仓库；仓库里只放"类目清单 + 下载脚本 + 校验和 + 许可证登记表"。
- **署名与登记**：ABO 要求署名，Commons 逐文件署名、部分文件 share-alike；Open Food Facts 官方另有提醒——照片里的**包装设计、商标不在其许可范围内**，需自行评估。I-0 必须交一份"数据集 → 版本/快照日期 → 许可证 → 义务"的登记表。

**明确不要用的（都是容易被误当成可商用的陷阱，已逐个核实）：**

| 数据集 | 实际许可证 | 为什么是陷阱 |
| --- | --- | --- |
| MVTec AD | **CC BY-NC-SA 4.0**，明写禁止商用 | 类目离谱地贴合（tile、screw、bottle、wood、carpet、metal nut），最容易让人动心 |
| Stanford Online Products | **无一手数据许可证**（官方项目页已下线；大家引用的 "MIT" 是**代码仓库**的） | 12 万张图是从 **eBay listing** 抓的，Stanford 无权转授卖家照片——"看起来最干净、风险最高"的一个 |
| SQID | 仓库写 MIT，但**只提供 amazon.com 图片 URL + 嵌入，没有图片文件** | MIT 覆盖的是 URL 表，不是亚马逊的照片；为商业产品去抓取受亚马逊站点条款约束 |
| Open Images V7 | 标注 CC BY 4.0；**图片仅为"尽力识别"的 CC BY 2.0，Google 明确免责**（"we make no representations or warranties … verify the license for each image yourself"） | 标注与图片的许可证被混为一谈；逐图署名到数千 Flickr 作者是真实义务。且是实景照不是目录图 |
| Unsplash Lite | 官网写 "free for commercial usage"，但 TERMS.md 只授予**内部**训练用途，且禁止再分发任何部分、禁止发布数据集对比 | "commercial" ≠ "可以把图发出去"；Full 版本更是整体非商用（含用它训练出的模型） |
| Products-10K | "can only be used for **non-commercial** research and educational purposes" | 品类本来最合适（含 household commodities） |
| Retail-786k | **CC BY-NC-ND 4.0** | NC 禁商用，**ND 还可能禁止派生**（含处理后的裁剪图与模型产物） |
| RP2K / Freiburg Groceries / MVImgNet | 分别是：官方页 404 无许可证可查 / **仓库无 LICENSE 文件且官方站点已下线** / 数据条款需先提交表单才能看、配套代码 CC BY-**NC** | "没有许可证"不等于"可以用"，默认是全部版权保留 |
| McAuley-Lab/Amazon-Reviews-2023、Marqo/amazon-products-eval | HuggingFace 上**无任何许可证声明** | 类目恰好是 Tools & Home Improvement / Home & Kitchen，极易被选中；"在 Hub 上"不是许可证 |
| Roboflow Universe 的建材/管件数据集 | 标 CC BY 4.0，但**由上传者自行声明**、无溯源 | 只可作为几百张级别的冒烟测试，不能当可依赖的资产 |

#### P2 实拍复测（验收以此为准）

- 构成与上一版相同：每组 = 1 张买家视角实拍照 + 正确 `product_id`；品类至少含有包装文字的（涂料、水泥、瓶装日用品）、无文字靠纹理的（瓷砖、板材）、小五金（管件、螺丝）、外形相近易混的（不同规格同款外观）。
- 实拍样本存放在仓库外的内部共享目录，**不进 Git**；仓库里只放清单 CSV（文件名 → 正确 product_id）。
- **P2 未完成前，图片搜索不得对买家开放**——否则等于拿公开目录图的成绩单给手机照场景背书。

### 7.2 目标指标

公开基准（Visual Product Search Benchmark，百万级干扰项）上开源模型 image→image R@1 约 41.9~43.8、R@5 约 60~61。我们的库小得多（试点万级向量），指标应显著高于公开基准，但**不要把下面的数字当成已经成立的结论**，它们是验收门槛：

| 指标 | 目标（试点） | 说明 |
| --- | --- | --- |
| top-1 命中率 | ≥ 50% | 正确商品排第一 |
| **top-5 命中率** | **≥ 70%** | **主验收指标**（任务要求）。**以 §7.1 的 P2 实拍复测为准**；P1 公开数据集的同一指标只作参考基线，须在报告里分两列并列，不得合并成一个数字 |
| 负样本正确拒绝率 | ≥ 85% | 20 张负样本中返回空或不含高分同款 |
| 无结果率 | ≤ 15% | 正样本里返回空列表的比例 |
| P1 退化图 vs 干净目录图的 top-5 落差 | 记录，无门槛 | 落差越大，P2 掉得越多；> 20 个百分点就要在 P2 前先补裁剪/增强策略 |

**文字搜图（§1.3）的实测要求**，I-0 一并做，按语言分别报告：

| 语言 | 指标 | 目标 | 不达标时 |
| --- | --- | --- | --- |
| 英文 | top-5 命中率 | ≥ 60%（跨模态检索天然低于图搜图，不要套用 70%） | 调 `IMGVEC_TEXT_MIN_SCORE`；仍不行则该语言只保留普通文字搜索 |
| 中文 | top-5 命中率 | ≥ 50% | 同上。中文只占训练数据 10% 非英文的一部分，**必须用自己的商品数据实测，不能靠论文的 XM3600 数字推断** |
| 高棉语 | 仅做一次对照实测，**不设门槛** | — | 预期不达标（§2.6 的词表证据已足以预判）。**实测的目的是留下证据，而不是指望它能过**；一旦确认，v1 按 D9 直接回退普通文字搜索 |

查询样本构造：每个商品由人工写 1 条"只描述图里看得见的东西"的查询（颜色、形状、材质、有无手柄等），**刻意不包含商品标题里的词**——否则测的是文字搜索而不是文字搜图。

不达标的处置顺序：① 调门槛与 `ef_search`、开启裁剪；② 换 `facebook/dinov2-base` 或做 SigLIP2+DINOv2 双向量取最大分；③ 升级到 siglip2-large/so400m 并配 GPU；④ 把该品类标记为"图片搜索效果有限"，前端引导文字搜索。**任何一步都不许用"把门槛调低让它有结果"来充数。**

### 7.3 压测指标

| 指标 | 目标 |
| --- | --- |
| `/v1/public/search/image` P95（含上传后到返回，不含客户端上传耗时） | ≤ 1500 ms（CPU 推理）/ ≤ 600 ms（GPU） |
| `/v1/public/products/{id}/similar` P95 | ≤ 150 ms |
| 图片搜索并发 | 持续 5 QPS 不超时；20 QPS 冲击下 `heavy` 桶拒绝但 **`critical`/`read` 零影响**（这是必须验证的隔离性，不只是性能数字） |
| 推理不可用演练 | 停掉 imgvec：图片搜索与文字搜图 100% 返回 2009（高棉语文字搜图仍正常回退），**普通文字搜索成功率 ≥ 99.9%** |
| GPU 共存演练（新增，§2.5.1） | imgvec 预热占满 2 GiB 后启动 Ollama，两者同时压测 10 分钟：Ollama 无 `cudaMalloc failed`，imgvec 无 OOM；再演练"Ollama 先起、imgvec 后起"的错误顺序，确认能复现问题并被监控告警抓到 |
| 查询图 7 天留存演练（新增，§5.1） | 第 8 天确认对象已被生命周期规则删除；带 GPS 的照片落盘后 EXIF 确已剥离；运营查看通道的审计记录逐条可查 |
| 入库吞吐 | ≥ 3 图/秒（CPU，batch 队列），1 万图全量重建 ≤ 1 小时 |
| 向量查询 | 10 万向量下单次 KNN（LIMIT 200）P95 ≤ 50 ms |

---

## 8. 工作量拆分（可派发的卡）

均**不属于资金路径**（不碰 ledger、提现、迁移密钥）。**I-9 属 IAM 路径**（新增后台权限，见 §5.4），I-1/I-2/I-3/I-5/I-8/I-9 涉及文件系统 / 上传 / 新依赖 / 缓存口径，**必须经安全审查员**；全部卡都要先过代码审查员。作者与审查员不得同人。

| 卡 | 标题 | 依赖 | 负责岗位（建议） | 审查链 | 关键交付 |
| --- | --- | --- | --- | --- | --- |
| **I-0** | 图片搜索 PoC：样本集 + 模型选型实测（**含文字搜图三语实测**） | 本方案获批（**已获批**） | Go 高级 或 数据/算法 | 代码审查员 | **P1**：公开数据集子集（ABO / Open Products Facts / Commons）+ 许可证登记表 + 下载脚本与校验和 + 退化图对照；**P2**：实拍 200 组复测（验收以此为准）；SigLIP2 与 DINOv2 实测 top-1/top-5；中/英/高棉语文字搜图实测；CPU 单图延迟与 GPU context 实测值。**不达标则后续卡不开工** |
| **I-1** | pgvector 引入与向量表迁移 | I-0 通过；**依赖审批**（扩展 + 镜像） | 数据库工程师 | 代码审查员 → 安全审查员 | 扩展迁移（superuser 拆分）、`catalog.product_image_vector`、HNSW 索引、迁移编号与未合并分支核对、dev compose 换镜像、回滚脚本 |
| **I-2** | `imgvec` 推理服务 | I-0 通过；**依赖审批**（Python 服务 + onnxruntime + 模型权重下载） | Go 高级（服务骨架）+ 运维（部署） | 代码审查员 → 安全审查员 | FastAPI + ONNX 服务、`/embed` `/healthz` `/readyz`、token 鉴权、内网防火墙规则、模型权重与许可证登记、Dockerfile、ONNX 导出脚本与 `model_rev` 约定 |
| **I-3** | 向量入库流水线 | I-1、I-2、F-14b(CMP-584) | Go 高级 | 代码审查员 → 安全审查员 | outbox 事件、asynq `catalog:image_embed:v1` / `image_vector_purge:v1` / `image_reindex:v1`、幂等与重试、`queuectl` 子命令、worker 测试 |
| **I-4** | 检索接口（OpenAPI + handler） | I-1、I-2 | Go 高级 | 代码审查员 | `POST /v1/public/search/image`、`GET /v1/public/products/{id}/similar`、错误码 2009、`BULKHEAD_ROUTE_CLASSES` 配置、熔断与降级测试、重跑 oapi-codegen 零 diff |
| **I-5** | 买家端前端（相机入口 / 取景框 / 结果页 / 找相似按钮） | I-4 | 前端 | 代码审查员 → 安全审查员（上传路径） | 相机与相册入口、客户端压缩、预签名直传、框选交互、三档结果展示、三语文案、四种兜底 UI、浏览器实测（含窗口尺寸） |
| **I-6** | 恶意文件扫描接入（补 F-14a 缺口） | — | 运维 + Go 高级 | 代码审查员 → 安全审查员 | 扫描服务接入、未扫描文件不可被消费、告警。**图片搜索上线前必须完成，且是 I-9 运营查看通道的硬前置**（§5.2） |
| **I-7** | 压测与上线演练 | I-3、I-4、I-5、I-9 | 运维 + QA | 代码审查员 | §7.3 全部指标的真实输出、停推理演练、**GPU 共存演练**、**7 天留存与 EXIF 演练**、runbook（REINDEX、全量重建、熔断处置、显存预算核对） |
| **I-8**（新增） | 文字搜图（接口 + 前端切换 + 语言分流） | I-4 | Go 高级 + 前端 | 代码审查员 → 安全审查员（缓存口径） | `GET /v1/public/search/image-by-text`、文本塔部署（建议 CPU）、`IMGVEC_TEXT_MIN_SCORE` 标定、`lang=km` 直接回退且不打推理的测试、`catalog:` 缓存键只存商品 ID 的复核、独立限流桶、三语文案 |
| **I-9**（新增） | 查询图 7 天留存与运营查看通道 | **§5.5 隐私政策 v1.1 生效**（N-07）；I-3 | Go 高级 + 运维 | 代码审查员 → **安全审查员（必过，含 IAM）** | 对象存储 7 天生命周期规则、留存策略开关（默认即时删）、新权限 `ops.image_query.view`、短 TTL 预签名查看、每次查看写审计、仅内网、按账号/`file_id` 的删除请求路径、EXIF 在持久化前剥离的实测证据 |

建议排期：I-0 → （I-1 ∥ I-2）→ I-3 → I-4 → I-5 → I-8 → I-7；I-6 与 I-1/I-2 并行；**I-9 的开工时点由 N-07 的隐私政策 v1.1 决定，不由技术进度决定**。

**本版新增的路径提示**：I-9 因为新增后台权限而**触及 IAM 路径**（§5.4），这是上一版"全部不属资金/IAM 路径"结论的一处修正；其余卡仍不属资金/IAM 路径。

### 审批状态（项目负责人 2026-10-29 已确认）

| # | 事项 | 状态 |
| --- | --- | --- |
| 1 | 新依赖：PostgreSQL `vector` 扩展（pgvector 0.8.7）；dev 镜像由 `postgres:16-alpine` 改为 `pgvector/pgvector:pg16` | **已批准** |
| 2 | 新服务与新运行时：`imgvec`（Python 3.12 + FastAPI + onnxruntime），部署到内网 AI 机 | **已批准** |
| 3 | 下载外部文件：`google/siglip2-base-patch16-224`、`facebook/dinov2-base`（备选）、`PekingU/rtdetr_r50vd` 权重，合计约 3 GB；需登记来源、版本号（pin revision）与许可证 | **已批准**（登记义务不变） |
| 4 | 许可证确认：三者均 Apache-2.0；**明确排除** jina-clip-v2（CC-BY-NC-4.0）与 Ultralytics YOLO（AGPL-3.0） | **已批准** |
| 5 | 样本集 | **改为**：PoC 先用公开可商用数据集（§7.1），实拍样本日后复测。原"拍摄与存放"审批不再是 I-0 的前置 |

**开工闸门（本版更新）：**

1. **I-0 现在可以开工**（第 1–4 项已批准，样本改用公开数据集）。
2. **I-1 起的开发卡，仍需 I-0 的 top-5 ≥ 70% 达标才能开工**，且该门槛以 §7.1 的 **P2 实拍复测**为准，不得用 P1 公开数据集的成绩放行。
3. **I-9 另有一道闸门**：必须等 §5.5 的隐私政策 v1.1 生效。政策生效前，留存策略开关保持"检索完即删 + 24 小时兜底"。
4. 新增依赖项若超出第 1–4 项的范围（例如 §2.6 路线 A 的 mSigLIP 权重、路线 B 的 MADLAD-400 约 12 GB），**需另行报批**，不在本次批准范围内。

---

## 9. 与 ADR-001「替换内核、保留外壳」的关系

图片搜索是新增能力，不改动既有外壳：复用现有 chi 路由与 `SuccessEnvelope`/`ErrorEnvelope` 契约、现有限流与 `overload` 并发桶、现有 asynq 六队列与 outbox、现有 `storage` 预签名上传。新增的只有：一张独立向量表、一个内网推理服务、**三个**公开读接口（图搜图、找相似、文字搜图）、三个队列任务类型，以及 7 天留存决定带来的一个受控运营查看通道与一个新后台权限（§5.1、§5.4）。推理服务放在进程外，保证 Go 后端不被 Python 运行时污染；模型可替换（`model_rev` 双写切换），不与业务逻辑耦合。

## 10. 未解问题

### 10.1 上一版的 4 个问题——已全部由项目负责人答复（2026-10-29）

| 原问题 | 答复 | 落在本文哪里 |
| --- | --- | --- |
| 1. 试点品类构成与样本集由谁拍 | PoC 先用公开可商用数据集，实拍日后复测 | §7.1（P1/P2 两段） |
| 2. imgvec 能否与 Ollama 共用 GPU、显存配额 | 同机共用，显存上限 2 GB，须保护 Ollama | §2.5.1 |
| 3. 查询图留存口径 | 改为**保留 7 天**供运营改进搜索，配独立权限 + 审计 + 内网 | §5.1、§5.5 |
| 4. 高棉语文字搜图是否纳入 | 文字搜图试点就做；高棉语不达标则回退普通文字搜索 | §1.3、§2.6、§6.4 |

### 10.2 本版新产生的待定项

1. **公开数据集的 share-alike 边界**：ODbL（Open Food Facts 数据）与 CC BY-SA 3.0（其图片）在"PoC 向量不进生产索引"的前提下风险可控（§7.1 已这样约束），但若将来想把这些向量并入生产索引，需要法律意见。**建议保持"PoC 用完即删"，不要留这个口子。**
2. **`ops.image_query.view` 权限授予给谁、由谁批**：技术上已约束为"默认无人持有、按需授予"，但具体人员名单与审批人是运营决定（影响 I-9）。
3. **隐私政策 v1.1 的三语正文由谁撰写**，特别是 `body_km`（数据库约束要求非空）；以及全量重新同意的投放时点（§5.5 第 2 条，这是一次面向全部买家的强制交互）。
4. **高棉语图片搜索（不是文字搜图）是否需要额外动作**：图搜图与语言无关，所以高棉语用户用"拍照找同款"完全可用——需要产品侧确认前端引导文案是否要对高棉语用户优先推荐拍照入口，以弥补文字搜图的缺失。
5. **§2.6 的两条备选路线（mSigLIP 第二索引 / MADLAD-400 翻译前置）是否纳入 v2**：需要先看 I-0 的高棉语实测证据与运营侧对高棉语文字搜图的真实需求强度，再决定。

---

## 11. 决定台账（可追溯）

| 日期 | 来源 | 决定 | 本文落点 |
| --- | --- | --- | --- |
| 2026-10-29 | 项目负责人（初始需求） | 搜索要有图片搜索，淘宝功能与交互照搬，视觉与品牌素材不照搬；试点必须 | 全文 |
| 2026-10-29 | 董事会审阅 | 方案通过（D1–D7 同意方向）；错误码 `2008` 与 A-09 冲突，改 `2009`；新错误码分配前须 grep 所有未合并分支 | §6.3 |
| 2026-10-29 | 项目负责人确认（董事会转达） | ①§8 第 1–4 项全批，先 I-0 且 top-5 ≥ 70% 才开发 | §8 审批状态 |
| 2026-10-29 | 同上 | ②PoC 先用公开可商用数据集，实拍日后复测 | §7.1 |
| 2026-10-29 | 同上 | ③imgvec 与 Ollama 同机共用 GPU，显存上限 2 GB + 保护措施 | §2.5.1、D10 |
| 2026-10-29 | 同上 | ④查询图保留 7 天 + 独立权限 + 审计 + 内网 + 隐私政策改版（N-07 上线前置） | §5.1、§5.5、D5、I-9 |
| 2026-10-29 | 同上 | ⑤文字搜图试点就做；I-0 实测中/英/高棉语；高棉语不达标回退普通文字搜索 | §1.3、§2.6、§6.4、D8/D9、I-8 |

### 本版相对上一版的三处结论修正（如实记录，不藏）

1. **高棉语文字搜图从"未核实"变为"已核实做不了"**：SigLIP2 的 Gemma 词表只有 88 个高棉文 token、1 个多字 token（§2.6 实测）。上一版"文本塔是一条覆盖高棉语的后路"的说法**不成立**，已更正。
2. **jina-clip-v2 的描述更正**：上一版称它是"唯一明确覆盖高棉语的"，核实后其模型卡的 89 语言列表**并未列出 `km`**，该说法改标未核实（它因 CC-BY-NC-4.0 被排除的结论不变）。
3. **"全部卡不属 IAM 路径"更正为 I-9 属 IAM 路径**：7 天留存带来的运营查看通道需要新增后台权限（§5.4）。
