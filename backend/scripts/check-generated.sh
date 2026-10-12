#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
make generate-all
changes="$(git status --short -- internal/api/gen ../packages/api-client/src/schema.d.ts)"
if [ -n "$changes" ]; then
	printf '%s\n' "$changes"
	exit 1
fi
