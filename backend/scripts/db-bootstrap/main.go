package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

type role struct {
	login       string
	permission  string
	passwordEnv string
}

var roles = []role{
	{"app_iam", "iam_rw", "IAM_DB_PASSWORD"},
	{"app_catalog", "catalog_rw", "CATALOG_DB_PASSWORD"},
	{"app_trade", "trade_rw", "TRADE_DB_PASSWORD"},
	{"app_wms", "wms_rw", "WMS_DB_PASSWORD"},
	{"app_logistics", "logistics_rw", "LOGISTICS_DB_PASSWORD"},
	{"app_ledger", "ledger_rw", "LEDGER_DB_PASSWORD"},
	{"app_comms", "comms_rw", "COMMS_DB_PASSWORD"},
	{"app_ai", "ai_rw", "AI_DB_PASSWORD"},
	{"app_ops", "ops_rw", "OPS_DB_PASSWORD"},
	{"app_audit", "audit_writer", "AUDIT_DB_PASSWORD"},
	{"app_report", "report_ro", "REPORT_DB_PASSWORD"},
}

func main() {
	ctx := context.Background()
	databaseURL := os.Getenv("MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		fatal("MIGRATION_DATABASE_URL is required")
	}
	for _, role := range roles {
		if os.Getenv(role.passwordEnv) == "" {
			fatal(role.passwordEnv + " is required")
		}
	}

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer conn.Close(ctx)

	for _, role := range roles {
		if err := ensureRole(ctx, conn, role, os.Getenv(role.passwordEnv)); err != nil {
			fatal(err.Error())
		}
	}
}

func ensureRole(ctx context.Context, conn *pgx.Conn, role role, password string) error {
	var statement string
	err := conn.QueryRow(ctx, `
		SELECT format(
			CASE WHEN EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1::text)
				THEN 'ALTER ROLE %I LOGIN INHERIT PASSWORD %L'
				ELSE 'CREATE ROLE %I LOGIN INHERIT PASSWORD %L'
			END,
			$1::text, $2::text
		)`, role.login, password).Scan(&statement)
	if err != nil {
		return fmt.Errorf("prepare %s: %w", role.login, err)
	}
	if _, err := conn.Exec(ctx, statement); err != nil {
		return fmt.Errorf("create or update %s: %w", role.login, err)
	}
	grant := fmt.Sprintf("GRANT %s TO %s", pgx.Identifier{role.permission}.Sanitize(), pgx.Identifier{role.login}.Sanitize())
	if _, err := conn.Exec(ctx, grant); err != nil {
		return fmt.Errorf("grant %s: %w", role.login, err)
	}
	return nil
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "db-bootstrap:", message)
	os.Exit(1)
}
