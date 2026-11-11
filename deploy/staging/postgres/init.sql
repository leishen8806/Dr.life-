-- Placeholder-only bootstrap. Run migrations and backend/scripts/db-bootstrap
-- separately with owner-managed passwords; it is the single role/grant source.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
