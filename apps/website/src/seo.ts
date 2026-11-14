import type { Locale } from "./i18n";

export type SeoPage = "home" | "merchant";

const home = {
  zh: {
    title: "DR.LIFE+ 生活家｜金边建材家居与中柬跨境物流平台 - DR集团",
    description: "DR集团立足柬埔寨金边。DR.LIFE+ 连接建材家居选购、DR智慧建材城看样、中国到柬埔寨跨境物流与云仓配送，从选材到交付一站完成。",
    keywords: "DR集团,DR.LIFE+,DR生活家,金边建材,柬埔寨建材,金边建材市场,DR智慧建材城,金边家居,柬埔寨家具家电,金边装修,柬埔寨装修材料,中国到柬埔寨物流,中柬跨境物流,柬埔寨清关,一仓入柬,金边仓库,柬埔寨云仓,金边仓储配送,金边同城配送,中国品牌进入柬埔寨,柬埔寨电商平台"
  },
  en: {
    title: "DR.LIFE+ | Building Materials & Home Platform in Phnom Penh, Cambodia - DR Group",
    description: "DR Group in Phnom Penh. DR.LIFE+ brings together building materials and home shopping, the DR smart building-materials showroom, China–Cambodia cross-border logistics, and cloud warehousing with local delivery.",
    keywords: "DR Group Cambodia,DR.LIFE+,building materials Phnom Penh,building materials Cambodia,home furniture Phnom Penh,home renovation Cambodia,tiles Phnom Penh,building materials showroom Phnom Penh,China to Cambodia shipping,China Cambodia logistics,Cambodia customs clearance,warehouse Phnom Penh,fulfillment Cambodia,delivery Phnom Penh,Chinese brands Cambodia,Cambodia e-commerce platform"
  },
  km: {
    title: "DR.LIFE+ | វេទិកាសម្ភារៈសំណង់ និងគ្រឿងសង្ហារិមនៅភ្នំពេញ - DR Group",
    description: "DR Group នៅភ្នំពេញ។ DR.LIFE+ ភ្ជាប់ការទិញសម្ភារៈសំណង់ និងគ្រឿងប្រើប្រាស់ក្នុងផ្ទះ ផ្សារតាំងបង្ហាញសម្ភារៈសំណង់ DR ការដឹកជញ្ជូនឆ្លងដែនពីចិនមកកម្ពុជា និងឃ្លាំងទំនិញ ព្រមទាំងការដឹកជញ្ជូនក្នុងស្រុក។",
    keywords: "DR Group,DR.LIFE+,សម្ភារៈសំណង់ភ្នំពេញ,សម្ភារៈសំណង់កម្ពុជា,គ្រឿងសង្ហារិមភ្នំពេញ,ជួសជុលផ្ទះ,ការ៉ូ,ដឹកជញ្ជូនពីចិនមកកម្ពុជា,ឃ្លាំងទំនិញភ្នំពេញ,ដឹកជញ្ជូនក្នុងភ្នំពេញ"
  }
} satisfies Record<Locale, Record<string, string>>;

const merchant = {
  zh: { title: "商家入驻 DR.LIFE+｜金边线上建材店 0% 平台佣金 - DR集团", description: "在金边开一家线上建材店：商品上架、DR智慧建材城展示、仓配与安装一站对接，平台交易零佣金。", keywords: "金边建材商家入驻,柬埔寨开店,柬埔寨建材批发,金边建材店,DR.LIFE+商家" },
  en: { title: "Sell on DR.LIFE+ | Online Building-Materials Store in Phnom Penh - DR Group", description: "Open an online building-materials store in Phnom Penh with zero platform commission: listings, showroom display, warehousing, delivery and installation in one place.", keywords: "sell building materials Cambodia,open store Phnom Penh,building materials supplier Cambodia,DR.LIFE+ merchant" },
  // [TODO-KM-REVIEW] Native-speaker review remains required before publication.
  km: { title: "បើកហាងសំណង់អនឡាញនៅភ្នំពេញ ដោយគ្មានកម្រៃ", description: "បើកហាងសម្ភារៈសំណង់អនឡាញនៅភ្នំពេញ ដោយគ្មានកម្រៃជើងសារវេទិកា៖ ដាក់លក់ទំនិញ តាំងបង្ហាញក្នុងទីក្រុងសម្ភារៈ DR ស្តុក ដឹកជញ្ជូន និងដំឡើងនៅកន្លែងតែមួយ។", keywords: "លក់សម្ភារៈសំណង់កម្ពុជា,បើកហាងភ្នំពេញ,អ្នកផ្គត់ផ្គង់សម្ភារៈសំណង់កម្ពុជា,ហាងសម្ភារៈសំណង់ DR.LIFE+" }
} satisfies Record<Locale, Record<string, string>>;

export function seoFor(page: SeoPage, lang: Locale) {
  return (page === "merchant" ? merchant : home)[lang];
}

export const localeMeta = { zh: "zh_CN", en: "en_US", km: "km_KH" } as const;
