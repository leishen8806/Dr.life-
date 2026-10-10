-- +goose Up
CREATE EXTENSION pg_trgm WITH SCHEMA public;

CREATE SCHEMA iam;
CREATE SCHEMA catalog;
CREATE SCHEMA trade;
CREATE SCHEMA wms;
CREATE SCHEMA logistics;
CREATE SCHEMA ledger;
CREATE SCHEMA comms;
CREATE SCHEMA ai;
CREATE SCHEMA ops;
CREATE SCHEMA audit;

REVOKE ALL ON SCHEMA iam, catalog, trade, wms, logistics, ledger, comms, ai, ops, audit FROM PUBLIC;

-- +goose Down
DROP SCHEMA audit;
DROP SCHEMA ops;
DROP SCHEMA ai;
DROP SCHEMA comms;
DROP SCHEMA ledger;
DROP SCHEMA logistics;
DROP SCHEMA wms;
DROP SCHEMA trade;
DROP SCHEMA catalog;
DROP SCHEMA iam;

DROP EXTENSION pg_trgm;
