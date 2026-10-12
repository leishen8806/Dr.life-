# SOPS deployment secrets

This directory stores deployment configuration encrypted by SOPS with age. Application database values such as Bot tokens and integration keys use the envelope encryption package instead.

## Setup

1. On an offline administrator machine, generate an age identity with `age-keygen -o keys.txt`.
2. Keep `keys.txt` offline and split custody between the designated administrators. Never commit it.
3. Replace the obvious placeholder public recipient in the repository root `.sops.yaml` with the deployment public key through the reviewed deployment configuration process.
4. Encrypt a configuration file with `sops --encrypt --in-place backend/secrets/<environment>.yaml`.
5. Decrypt only on an authorized deployment host with `sops --decrypt backend/secrets/<environment>.yaml` and inject values directly into the process environment.

Bot tokens, API credentials, bank credentials, master keys, and private age identities must be encrypted. Only `*.example.yaml` files containing obvious fake placeholders may remain plaintext. Never commit decrypted YAML, `.env` files, age identities, master-key shares, or output copied from `keytool`.

Run `backend/scripts/check-sops-encrypted.sh` from the repository root before committing. It rejects non-example YAML without a `sops:` metadata section. The script is intentionally not connected to CI in F-10; a later CI review can add it to `light-checks`.

Losing the age private identity makes the configuration unrecoverable unless another authorized backup exists. Exposure requires replacing the identity and re-encrypting every affected file.

SOPS protects deployment files in Git. Application envelope encryption protects each database value with its own DEK and supports KEK rotation without rewriting plaintext data. They solve different storage paths and do not replace each other.
