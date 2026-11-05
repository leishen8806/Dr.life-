# Legal-document publication runbook

## Preconditions

- A board-approved legal and Khmer-language review identifies the exact `terms` or `privacy` draft version, approval reference, and an existing active `super` or `ops` administrator ID.
- The document already exists as an unpublished draft in `ops.legal_doc`; this process never creates, edits, or changes migration seeds.
- Before opening buyer, merchant, or driver traffic, publish at least one effective version of **both** `terms` and `privacy`. Publishing only one kind leaves the missing kind fail-open, but the published kind remains enforced for accounts that have not accepted it.

## Publish one approved draft

Use a privileged, one-time operations connection; do not use `IAM_DATABASE_URL` or an application role. Keep the approval reference in the change record and run:

```powershell
$env:LEGAL_PUBLISH_DATABASE_URL = '<approved privileged PostgreSQL URL>'
go run ./scripts/legal-publish --kind terms --version v1.0 --approved-by <super-or-ops-admin-id> --approval-ref <approved-change-reference>
```

Run the same command for `privacy`. The command publishes only an existing unpublished draft, requires a non-empty approval reference, and fails if the selected document is already published or absent. The database persists the approval reference and enforces that the approving `iam.admin_user` is active with role `super` or `ops`. It does not expose legal-text bodies.

## Verify before traffic

Query with the privileged connection that each kind has one or more `published_at IS NOT NULL` rows whose `effective_at` is in force. Then exercise the public legal endpoint and the consent flow. If a kind has no published effective document, gateway logs event `consent_no_published_document` at WARN with `kind`; current board policy allows that route through, while genuine storage failures remain 503.
