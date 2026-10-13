# Envelope encryption

## Threat model

The package protects database backups, copied ciphertext columns, and configuration exposure from revealing Bot tokens, AI keys, and integration credentials without the injected master key. AES-256-GCM also detects ciphertext, header, and AAD tampering.

It does not protect a running process after host compromise, an administrator who can access both shares, plaintext used by a downstream provider, or values already copied before encryption. Go garbage collection may retain copies; temporary byte slices are cleared on a best-effort basis, but complete memory erasure cannot be guaranteed.

## Format

One binary value can be stored directly in `bytea`. Version 1 contains the `DLSE` marker, format version, KEK id, wrapping nonce, wrapped DEK, data nonce, and encrypted data. Every encryption creates an independent random 32-byte DEK and independent nonces. The DEK is wrapped by the active KEK; data is never encrypted directly with a long-lived KEK.

Both encryption and decryption require AAD. Use a stable purpose and immutable record identifier:

- `bot_token:<id>`
- `bot_webhook_secret:<id>`
- `integration_key:<id>`
- `ai_provider_key:<id>`

Never reuse ciphertext under another record id. Changing the AAD intentionally makes decryption fail.

## Master keys and rotation

Inject either `MASTER_KEYS="id1:<base64>,id2:<base64>"` with `ACTIVE_MASTER_KEY_ID=id2`, or two equal-length base64 shares in `MASTER_KEY_SHARE_A` and `MASTER_KEY_SHARE_B`. Every decoded key must be exactly 32 bytes. Environment values are read into memory and are never written by this package.

Rotation order:

1. Add the new KEK and make it active while retaining the old KEK.
2. Rewrap every stored value with its original AAD. `Rewrap` decrypts only the DEK and does not re-encrypt application data.
3. Verify every value with the new-only provider and reconcile the processed count.
4. Remove the old KEK only after verification and backup approval.

If a KEK is lost, values not rewrapped to another available KEK cannot be recovered. Restore the offline backup or rotate the affected external credentials. If a KEK leaks, disable affected external credentials, add a new KEK, rewrap values, remove the compromised KEK, and audit all reads.

## Never do this

- Print plaintext, KEKs, DEKs, shares, tokens, or API keys in logs or errors.
- Put a master key or age private identity in Git, a Docker image, URL, or command-line argument.
- Use `DisplayHint` as authentication evidence; it is display-only.
- Remove an old KEK before every corresponding value has been rewrapped and verified.
