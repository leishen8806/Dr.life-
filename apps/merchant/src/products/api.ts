import { createApiClient, withIdempotencyKey } from "@drlife/api-client";
import type {
  Category,
  Product,
  ProductCreateRequest,
  ProductUpdateRequest
} from "./form";
import type { ProductStatus } from "./status";

type ApiClient = ReturnType<typeof createApiClient>;

/** 分页上限，与 openapi.yaml 的 PageSize 一致；品类表远小于这个数。 */
const maxPageSize = 100;

export type ProductStatusFilter = ProductStatus | "all";

export interface MerchantProductApi {
  list(filter: ProductStatusFilter): Promise<Product[]>;
  listCategories(): Promise<Category[]>;
  create(request: ProductCreateRequest): Promise<Product>;
  update(publicId: string, patch: ProductUpdateRequest): Promise<Product>;
  publish(publicId: string): Promise<Product>;
}

/** 网关拒绝大写路径段；响应里回来的仍是规范的大写 ULID。 */
function pathId(publicId: string): string {
  return publicId.toLowerCase();
}

/** 成功响应一定带信封；缺了说明不是我们的接口在应答，不编数据出来。 */
function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new Error("Merchant product request returned no payload");
  }
  return data;
}

export function createMerchantProductApi(client: ApiClient): MerchantProductApi {
  return {
    async list(filter) {
      const { data } = await client.GET("/v1/merchant/products", {
        params: { query: filter === "all" ? {} : { status: filter } }
      });
      return required(data).data.items;
    },
    async listCategories() {
      // 接口已按 sort 再 code 排好序，而且只返回启用的品类，前端不再重排。
      const { data } = await client.GET("/v1/public/categories", {
        params: { query: { page_size: maxPageSize } }
      });
      return required(data).data.items;
    },
    async create(request) {
      const { data } = await client.POST("/v1/merchant/products", {
        params: { header: withIdempotencyKey() },
        body: request
      });
      return required(data).data;
    },
    async update(publicId, patch) {
      const { data } = await client.PATCH("/v1/merchant/products/{public_id}", {
        params: {
          header: withIdempotencyKey(),
          path: { public_id: pathId(publicId) }
        },
        body: patch
      });
      return required(data).data;
    },
    async publish(publicId) {
      const { data } = await client.POST(
        "/v1/merchant/products/{public_id}/publish",
        {
          params: {
            header: withIdempotencyKey(),
            path: { public_id: pathId(publicId) }
          }
        }
      );
      return required(data).data;
    }
  };
}
