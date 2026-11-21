# B11 · V14 API design（4 组非资金接口）

> Design-only contract for #307/#311 follow-up, 2026-11-21. The OpenAPI additions in `backend/api/openapi.yaml` are intentionally unimplemented: no handlers, migrations, or UI changes are included here. The generated Go interface and TypeScript schema are refreshed contract artifacts only; they do not add endpoint behavior. KESS, wallet top-up/withdrawal, membership, financing, advertising, and delivery tracking are explicitly out of scope.

## Shared contract rules

- All JSON fields are `snake_case`; all new resource identifiers are canonical uppercase ULIDs in responses and lowercase ULIDs in paths.
- All list endpoints use one-based `page` and capped `page_size` (1–100), and return `items`, `total`, `page`, and `page_size`.
- Every mutation requires `Idempotency-Key`; the same key and same request may replay the stored response, while a different request with the same key is a conflict/pending response.
- Every mutation has an `x-audit` action. The eventual handler must record actor, role, target, request ID, result, and a redacted detail object. No audit schema or storage migration is added in this PR.
- Auth failures are `401`; scope/role/permission failures are `403`; malformed input is `400`; missing resources are `404`; idempotency/state conflicts are `409`; rate limiting is `429`; service faults are `500`/`503`.
- Phone values in the new address resource are masked on reads. The write request may carry the owner's full phone; no response in this design returns it. This is separate from the existing buyer-owned order snapshot and merchant-order masking contract.
- Coordinates are present as `latitude` and `longitude`; retention, precision, geocoding, and map-provider policy remain owner-tbd. No map or external adapter is implied.

## 1. Saved address book（buyer + merchant）

| Method | Path | Permission / scope | Audit | Design notes |
| --- | --- | --- | --- | --- |
| GET | `/v1/buyer/me/addresses` | buyer; own addresses only | — | Paginated; phone masked. |
| POST | `/v1/buyer/me/addresses` | buyer; own account | `address.create` | `recipient_name`, `address_text`, full write-only `phone`, latitude/longitude, optional default. |
| GET/PATCH/DELETE | `/v1/buyer/me/addresses/{public_id}` | buyer; own address only | update/delete | 404 for another/unknown owner without leaking existence. |
| GET | `/v1/merchant/me/addresses` | merchant owner; own merchant account | — | Staff do not gain access by having a merchant role. |
| POST | `/v1/merchant/me/addresses` | merchant owner | `address.create` | Same fields and masking as buyer. |
| GET/PATCH/DELETE | `/v1/merchant/me/addresses/{public_id}` | merchant owner; own address only | update/delete | Idempotent writes; default-address behavior is owner-tbd. |

`AddressRecord.phone` is `^\\*{4}([0-9]{4})?$`. The optional final four digits are a presentation decision already used by merchant order serialization; whether the address book should show final four digits is **owner-tbd**. Coordinates have no amount or funding semantics.

## 2. 找装修（public profile/cases, buyer inquiries, merchant quotes）

### Public read model

| Method | Path | Permission / privacy | Pagination |
| --- | --- | --- | --- |
| GET | `/v1/public/contractors` | anonymous; published profiles only | page/page_size/category |
| GET | `/v1/public/contractors/{public_id}` | anonymous; published profile only | — |
| GET | `/v1/public/cases` | anonymous; published cases only | page/page_size/category |
| GET | `/v1/public/cases/{public_id}` | anonymous; published case only | — |

`ContractorProfile` contains a public identifier, three-language name/description, categories, and `status: published`. `ContractorCase` contains a public identifier, contractor reference, three-language title/body, `image_file_public_ids`, status, and publication timestamp. Images reference the existing private-file flow by ID; this design does not grant download access.

### Buyer inquiry

| Method | Path | Permission | Audit / privacy |
| --- | --- | --- | --- |
| GET | `/v1/buyer/contractor-inquiries` | buyer-owned rows | Buyer contact and address references stay private. |
| POST | `/v1/buyer/contractor-inquiries` | buyer | `contractor_inquiry.create`; accepts title, description, optional contractor/address/file references. |
| GET | `/v1/buyer/contractor-inquiries/{public_id}` | buyer-owned row | Status is `submitted`, `matching`, `quoted`, `accepted`, `closed`, or `cancelled`. |

The inquiry status is a state label only. No quote acceptance, payment, financing, or money rule is defined here.

### Merchant demand and quote

| Method | Path | Permission / scope | Audit / privacy |
| --- | --- | --- | --- |
| GET | `/v1/merchant/contractor-demands` | merchant service-category scope | Buyer contact remains masked until an owner-approved disclosure state. |
| GET | `/v1/merchant/contractor-demands/{public_id}` | merchant scope | No sibling merchant data. |
| GET/POST | `/v1/merchant/contractor-demands/{public_id}/quotes` | scoped merchant | POST is `contractor_quote.create`; quote data is participant-scoped. |
| GET | `/v1/merchant/contractor-demands/{public_id}/quotes/{quote_id}` | scoped merchant | 404/403 must not reveal an out-of-scope quote. |

`ContractorQuote.amount_e4` reuses the existing integer `OrderMoneyE4` schema. This defines the field and serialization only; tax, validity, rounding, acceptance, refund, and business limits are **owner-tbd** and must not be inferred by an implementation.

## 3. Merchant staff and roles

| Method | Path | Permission / scope | Audit |
| --- | --- | --- | --- |
| GET/POST | `/v1/merchant/staff` | merchant owner; own merchant | POST `merchant_staff.create` |
| POST | `/v1/merchant/staff/invitations` | merchant owner | `merchant_staff.invite`; target is Telegram username or phone |
| GET/PATCH/DELETE | `/v1/merchant/staff/{public_id}` | merchant owner; own staff | update/delete |

The four sample roles are `store_manager`, `warehouse_manager`, `customer_service`, and `finance`. They are examples requested by B11, not a frozen role catalog. `permission_scope` is an array of opaque permission/data-scope identifiers until owner approves the RBAC matrix. All role, invitation, scope, and data-disclosure behavior is **owner 亲审**. Staff contact fields are private; responses contain no invitation secret or credential.

## 4. Admin policies and agreements

| Method | Path | Permission | Audit / step-up |
| --- | --- | --- | --- |
| GET | `/v1/admin/policies` | `admin.agreement.publish` | Paginated current documents. |
| GET | `/v1/admin/policies/{public_id}` | `admin.agreement.publish` | Draft or published document. |
| GET | `/v1/admin/policies/{public_id}/versions` | `admin.agreement.publish` | Immutable publication history, paginated; read audit is specified. |
| PUT | `/v1/admin/policies/{public_id}/draft` | `admin.agreement.publish` | `policy.draft.save`; three-language draft. |
| POST | `/v1/admin/policies/{public_id}/publish` | `admin.agreement.publish` | `policy.publish`; requires `X-Step-Up-Token` and idempotency key. |

`PolicyDocument.content` is required in `zh`, `en`, and `km`; `kind` is `privacy`, `terms`, or `help`; `state` is `draft` or `published`. Publishing takes `change_note`, `effective_date`, and optional expected version for optimistic concurrency. The contract does not decide translation quality, effective-date conflict policy, consent migration, or which role can approve; these are **owner/合规亲审**. Published content is administrative data and must be included in audit redaction rules.

## Owner-tbd / acceptance gate

1. Owner must approve address phone presentation and coordinate retention/precision.
2. Owner must approve contractor visibility, buyer-contact disclosure state, case image access, and the quote amount rules (the field is defined, business rules are not).
3. Owner must provide the authoritative merchant RBAC role/permission/data-scope matrix and invitation verification policy.
4. Owner/compliance must approve policy versioning, three-language publication rules, consent migration, and step-up scope.
5. Implementation is a separate follow-up: add handlers/tests/migrations as separate scoped work after this design is approved. KESS and all external adapters remain “待规范”.

Refs #307
Refs #311
