import type { components } from "@drlife/api-client";

/** Catalogue shapes come from the generated OpenAPI schema; never hand-write them. */
export type Category = components["schemas"]["Category"];
export type CategoryPage = components["schemas"]["CategoryListData"];
export type Product = components["schemas"]["Product"];
export type ProductPage = components["schemas"]["ProductListData"];
export type ProductTier = components["schemas"]["ProductTier"];
export type LocalizedText = components["schemas"]["LocalizedText"];
