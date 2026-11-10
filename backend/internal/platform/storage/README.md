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

## File retention and cleanup

Every unreferenced upload receives a 24-hour deadline. A business module must
attach a reference before that deadline and release it only when the business
lifecycle ends. The final release starts the purpose-specific retention period:
product images 90 days after delisting, inbound and delivery photos two years
after order completion, and merchant KYC material five years after merchant
cancellation.

- Pending or stuck verifying uploads older than 24 hours become `expired`.
- Active `ops.file_reference` rows prevent deletion regardless of age.
- Ready and rejected files become `deleted` only after the last reference is
  released and `retention_until` has passed.
- Cleanup claims a row first, then performs object deletion with a 30-second
  timeout without holding a database row lock. Failures clear the claim so the
  next run can retry.

The cleanup worker preserves `ops.file` rows for audit and is safe to rerun.
Image uploads use a clean object key after re-encoding. WebP is not accepted
until a metadata sanitizer is available. With S3 versioning enabled, the raw
upload prefix must have a lifecycle rule that permanently removes noncurrent
versions; otherwise deleted uploads can retain metadata in old versions.
