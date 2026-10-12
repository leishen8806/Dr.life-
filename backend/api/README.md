# OpenAPI 规范

`openapi.yaml` 是前后端接口的唯一来源。规范使用 OpenAPI 3.0.3，因为当前固定版本的 `oapi-codegen` 和 `openapi-typescript` 均稳定支持该版本。

## 组织与命名

- 路径按 `/v1/public|buyer|merchant|driver|wms|admin` 分组，tag 上的 `x-roles` 记录允许的角色。
- `operationId` 使用“动词 + 受众 + 资源”的 lowerCamelCase 形式，例如 `getPublicPing`。
- JSON 字段使用 `snake_case`。
- 通用信封、参数、响应与安全方案放在 `components`，业务路径只引用它们。

权限、数据范围和脱敏必须由服务端执行；客户端隐藏按钮不构成权限控制。除 public 外，令牌角色与路径不匹配必须返回 403，这对应 `AGENTS.md` 规则 9、10。实际鉴权由 F-05 实现。

## 新增接口

1. 修改 `api/openapi.yaml`，先定义路径、组件和错误响应。
2. 在 `backend` 目录运行 `make generate-all`。
3. 实现生成的 Go 服务端接口，不要手改 `internal/api/gen/` 或 `packages/api-client/src/schema.d.ts`。
4. 增加规范与处理器测试，运行 `make vet test`。
5. 运行 `sh scripts/check-generated.sh`，确认生成产物已提交且可重复生成。

`make generate-ts` 需要 Node.js。没有本机 Node 时使用 `make generate-ts-docker`（需要 Docker）；Go 生成器通过固定版本的 `go run` 执行。

## 兼容规则

新增可选字段、路径和响应码应保持向后兼容。删除字段、收紧字段含义或改变既有响应结构属于破坏性变更，必须发布 `/v2` 并保留 `/v1` 迁移期。
