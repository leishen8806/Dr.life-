// Command migrate applies the repository's forward-only goose migrations.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "migration configuration error: DATABASE_URL is required")
		os.Exit(1)
	}
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "/migrations"
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration database error:", err)
		os.Exit(1)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "migration database error:", err)
		os.Exit(1)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintln(os.Stderr, "migration configuration error:", err)
		os.Exit(1)
	}
	if err := goose.UpContext(ctx, database, migrationsDir); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		os.Exit(1)
	}
}
