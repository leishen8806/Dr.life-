# Storage adapters

`storage` creates direct object-store URLs; application handlers must return these URLs and must not proxy file bytes. Object keys are generated from cryptographically random bytes as `objects/<hex>` and therefore contain no account identifier, phone number, or original filename.

## Adapter choice

This package uses `github.com/aws/aws-sdk-go-v2/service/s3`. The AWS SDK's SigV4 presigner supports both AWS S3 and MinIO when configured with an explicit MinIO endpoint and path-style addressing, so a second S3 client dependency would add no capability.

`NewS3` is for AWS or another S3-compatible endpoint. `NewMinIO` applies MinIO's path-style configuration. `NewLocal` is only an adapter contract/test option: it creates HMAC-protected URLs for an external local file server; it does not stream files through this service.

## Policy and expiry

- PUT expiry is fixed at 10 minutes; signed PUTs include `Content-Type` and `Content-Length`.
- GET expiry is fixed at 5 minutes.
- `Policy.AllowedContentTypes` must be non-empty and rejects any unlisted type.
- `Policy.MaxContentLength` must be positive and rejects larger objects.
- Invalid content types, oversized content, and non-positive length return distinct sentinel errors.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `STORAGE_DRIVER` | `s3` | `s3`, `minio`, or `local`; wiring belongs to a later task. |
| `STORAGE_BUCKET` | none | Object-storage bucket. |
| `STORAGE_REGION` | `us-east-1` | S3 signing region. |
| `STORAGE_ENDPOINT` | none | Explicit S3-compatible or MinIO endpoint. |
| `STORAGE_ACCESS_KEY` | none | Injected object-store access key; never log it. |
| `STORAGE_SECRET_KEY` | none | Injected object-store secret; never log it. |
| `STORAGE_ALLOWED_CONTENT_TYPES` | none | Comma-separated allow-list, configured by future wiring. |
| `STORAGE_MAX_CONTENT_LENGTH` | none | Positive byte limit, configured by future wiring. |
| `STORAGE_CDN_PREFIX` | empty/off | Reserved CDN prefix; disabled by default. |
| `STORAGE_SCAN_HOOK_ENABLED` | `false` | Reserved malware-scan hook; disabled by default. |

No presigned URL, access key, or secret key may be written to logs. ScanHook is intentionally only documented here: wiring and a provider decision are outside this package's scope.

`VerifyLocalURL` validates the signature and expiry only. Its caller must separately verify that the actual HTTP request method matches the signed URL's `method` query parameter.
