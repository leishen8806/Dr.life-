-- Placeholder-only bootstrap. Run migrations and db-bootstrap separately.
CREATE ROLE migration NOLOGIN;
CREATE ROLE app_iam NOLOGIN;
CREATE ROLE app_report NOLOGIN;
CREATE ROLE app_audit NOLOGIN;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT CONNECT ON DATABASE drlife_staging TO migration, app_iam, app_report, app_audit;
