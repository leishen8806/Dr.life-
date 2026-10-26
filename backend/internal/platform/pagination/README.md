# pagination

Shared validation for list endpoints.

- `ParsePage` defaults to `page=1`, `page_size=20`; `page_size` is capped at 100.
- `ParseSort("name,-created_at", allowed...)` accepts comma-separated fields with a leading `-`
  for descending order and rejects unknown, empty, or duplicate fields.
- `ParseFilter(values, allowed...)` reads `filter[field]=value` and rejects unknown or malformed
  fields.
- `EncodeCursor` / `DecodeCursor` provide a URL-safe keyset cursor. The cursor is opaque to
  clients but not encrypted: never put secrets or personal data in it.

Invalid input returns an error; handlers should map it to the documented 400 `invalid_argument`
response rather than guessing at a default.