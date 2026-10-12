#!/usr/bin/env sh
set -eu

secrets_dir=${SECRETS_DIR:-backend/secrets}
failed=0
for file in "$secrets_dir"/*.yaml; do
	[ -e "$file" ] || continue
	case "$file" in *.example.yaml) continue ;; esac
	if ! grep -Eq '^[[:space:]]*sops:' "$file"; then
		printf 'plaintext secret configuration: %s\n' "$file" >&2
		failed=1
	fi
done

exit "$failed"
