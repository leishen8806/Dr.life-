const zh = {
  home: {
    intro: "DR.LIFE+ 是面向柬埔寨的新一代综合商业平台。从建材家居出发，把中国供应链、本地仓配与线上交易连成一体，让每一次购买更简单、更可信。",
    visionIntro: "先把一个行业做深、做透，再把同一套供应链、仓配、交易与结算能力，延展到更多品类与更多生活场景。",
    ecosystemNodes: [["线上商城", "Telegram 三语商城", "open"], ["本地仓配", "金边仓 · 出库配送", "open"], ["支付结算", "在线付款 · 签收后结算", "open"], ["生活服务", "规划中", "planned"], ["商家服务", "规划中", "planned"], ["跨境供应链", "中国 → 柬埔寨", "open"]],
    categoriesLabel: "WHERE WE START", categoriesTitle: "从建材家居开始", categoriesCta: "进入商城浏览全部商品 →",
    categories: [["TILES · FLOORING", "瓷砖与地板", "[场景大片]"], ["", "卫浴", ""], ["", "门窗", ""], ["", "家具", ""], ["", "涂料", ""]],
    chainLabel: "ONE-STOP · CHINA TO CAMBODIA", chainTitle: "从中国工厂，到柬埔寨买家手中", chainIntro: "采购、集运、清关、仓储、销售、配送，一个平台对接。商家不必自建仓库，也不必分别对接多家服务商。",
    chain: ["中国工厂", "广州集货", "跨境清关", "金边入仓", "DR.LIFE+ 销售", "本地配送"], chainBodies: ["对接厂家与供应商", "短期集货、合并装柜", "路线按商品确认", "分区上架，库存到库位", "三语商城，免佣金", "进度实时可查"],
    chainNote: "跨境线路、清关责任、费用与时效按商品和订单逐项确认，以报价单为准。", partnersLabel: "GROW WITH US", partnerCtas: ["申请入驻 →", "洽谈合作 →", "联系我们 →"]
  },
  merchant: {
    intro: "适合在金边有现货或稳定补货的建材、家居商户，也适合想把货卖进柬埔寨、又不想自建仓的中国厂家与代理：广州集货、跨境运输、金边入仓、线上销售与本地配送一条龙。",
    steps: [["打开商家 Bot", "在 Telegram 打开 @DrLifeMerchantBot，选择语言并同意商户协议。"], ["提交资料", "营业执照、负责人身份证件、收款账户。资料仅用于核验。"], ["平台审核", "运营核验资料，结果通过 Bot 通知；需要补件会写明原因。"], ["上架商品", "按类目模板填写规格与价格，商品审核通过后对买家可见。"], ["送货入仓", "按入仓预约把货送到合作仓，可售数量以入仓后的库存为准。"]],
    feesIntro: "所有费用都在商家 Mini App 的账单里逐项列出。下表金额以正式报价单与费用公告为准。", feeHeaders: ["项目", "收费", "说明"],
    fees: [["平台销售佣金", "0%", "不按成交额抽成。"], ["结算服务费", "0", "货款结算到商户钱包不另收服务费。"], ["仓储费", "15 美元/立方米/月起", "按体积计费、按天计提、每月出账；承诺期与存量优惠按公开规则执行，以入仓报价单为准。"], ["跨境集运与清关", "按线路报价", "广州集货中转到金边入仓，按商品、体积与线路逐单报价。"]]
  },
  legal: { history: "历史版本", historyCurrent: "当前版本；以前的版本发布后在此保留，可按版本号查看原文。" }
} as const;

const en = {
  home: {
    intro: "DR.LIFE+ is Cambodia's next-generation integrated commerce platform. Starting with building materials and home furnishings, it connects China's supply chain, local warehousing and fulfilment, and online transactions, making every purchase simpler and more reliable.",
    visionIntro: "We first go deep in one industry, then extend the same supply-chain, warehousing, fulfilment, transaction and settlement capabilities to more categories and everyday scenarios.",
    ecosystemNodes: [["Online marketplace", "Telegram trilingual marketplace", "open"], ["Local warehousing and fulfilment", "Phnom Penh warehouse and delivery", "open"], ["Payment and settlement", "Online payment and settlement after receipt", "open"], ["Lifestyle services", "Planned", "planned"], ["Merchant services", "Planned", "planned"], ["Cross-border supply chain", "China → Cambodia", "open"]],
    categoriesLabel: "WHERE WE START", categoriesTitle: "Starting with building materials and home", categoriesCta: "Browse all products in the marketplace →", categories: [["TILES · FLOORING", "Tiles and flooring", "[Scene image]"], ["", "Bathroom", ""], ["", "Doors and windows", ""], ["", "Furniture", ""], ["", "Paint", ""]],
    chainLabel: "ONE-STOP · CHINA TO CAMBODIA", chainTitle: "From Chinese factories to Cambodian buyers", chainIntro: "Sourcing, consolidation, customs clearance, warehousing, sales and delivery through one platform. Merchants need not build their own warehouses or coordinate multiple providers.", chain: ["Chinese factories", "Guangzhou consolidation", "Cross-border customs clearance", "Phnom Penh warehousing", "DR.LIFE+ sales", "Local delivery"], chainBodies: ["Connect factories and suppliers", "Short-term consolidation and container loading", "Routes confirmed by product", "Zone storage and bin locations", "Trilingual marketplace, zero commission", "Track progress in real time"], chainNote: "Cross-border routes, customs responsibility, fees and timing are confirmed item by item for each product and order; the quotation prevails.", partnersLabel: "GROW WITH US", partnerCtas: ["Apply to join →", "Discuss cooperation →", "Contact us →"]
  },
  merchant: {
    intro: "For Phnom Penh building-material and home merchants with stock or stable replenishment, and for Chinese manufacturers and agents that want to sell in Cambodia without building their own warehouses: Guangzhou consolidation, cross-border transport, Phnom Penh warehousing, online sales and local delivery.",
    steps: [["Open the merchant Bot", "Open @DrLifeMerchantBot in Telegram, choose a language and accept the merchant agreement."], ["Submit documents", "Business licence, responsible person's identification and settlement account. Documents are used only for verification."], ["Platform review", "Operations verifies the documents and sends the result through the Bot; requests for additional documents state the reason."], ["List products", "Complete specifications and prices from the category template. Products become visible after review."], ["Deliver to the warehouse", "Deliver goods to the partner warehouse through a warehouse appointment. Sellable quantity follows received inventory."]],
    feesIntro: "Every charge is itemised in the merchant Mini App bill. Table amounts are subject to the formal quotation and fee notice.", feeHeaders: ["Item", "Charge", "Details"], fees: [["Platform sales commission", "0%", "No percentage is charged on transaction value."], ["Settlement service fee", "0", "No extra service fee is charged when sales proceeds settle to the merchant wallet."], ["Storage", "US$15/m³/month from", "Charged by volume, accrued daily and billed monthly; commitments and existing-stock discounts follow published rules and the inbound quotation."], ["Cross-border consolidation and customs clearance", "Quoted by route", "Guangzhou consolidation through Phnom Penh warehousing is quoted per order by product, volume and route."]]
  },
  legal: { history: "Version history", historyCurrent: "Current version; earlier versions will remain here after publication and can be viewed by version number." }
} as const;

// The task permits Chinese fallback when Localizer's corresponding Khmer copy is absent.
const km = zh;

export const pageCopy = { zh, en, km } as const;
