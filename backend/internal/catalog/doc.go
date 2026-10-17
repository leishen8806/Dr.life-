// Package catalog 实现商品目录模块：分类、商品、阶梯价与三语校验。
//
// 依据《Dr.life+数据库表结构与账务详细设计.md》第三节 catalog 表，
// 迁移文件 backend/migrations/00012_catalog_tables.sql。
//
// 边界（AGENTS.md 第 4 节规则 8）：本模块只读写自己的 schema catalog。
// product.merchant_id 指向 iam.merchant.id，但按规则不建跨 schema 外键，
// 商户归属与状态由 MerchantDirectory 端口注入，适配层负责跨模块查询。
package catalog
