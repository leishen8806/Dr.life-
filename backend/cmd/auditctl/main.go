package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: auditctl verify|anchor")
	}
	dsn := os.Getenv("AUDIT_DATABASE_URL")
	if dsn == "" {
		fatal("AUDIT_DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fatal("connect audit database: " + err.Error())
	}
	defer pool.Close()
	store := &platformaudit.Store{DB: pool}
	switch os.Args[1] {
	case "verify":
		set := flag.NewFlagSet("verify", flag.ExitOnError)
		from := set.Int64("from", 0, "first audit id, zero means genesis")
		to := set.Int64("to", 0, "last audit id, zero means current tail")
		_ = set.Parse(os.Args[2:])
		if err := store.Verify(ctx, *from, *to); err != nil {
			fatal(err.Error())
		}
		fmt.Println("audit chain verified", "from="+strconv.FormatInt(*from, 10), "to="+strconv.FormatInt(*to, 10))
	case "anchor":
		set := flag.NewFlagSet("anchor", flag.ExitOnError)
		day := set.String("day", time.Now().UTC().AddDate(0, 0, -1).Format(time.DateOnly), "UTC day (YYYY-MM-DD)")
		path := set.String("file", "audit-anchors.jsonl", "append-only local anchor file")
		_ = set.Parse(os.Args[2:])
		parsed, err := time.Parse(time.DateOnly, *day)
		if err != nil {
			fatal("invalid day")
		}
		record, err := store.AnchorDay(ctx, parsed, platformaudit.FileAnchorSink{Path: *path})
		if err != nil {
			fatal(err.Error())
		}
		fmt.Printf("anchored day=%s last_id=%d hash=%s\n", record.Day, record.LastID, record.LastHash)
	default:
		fatal("usage: auditctl verify|anchor")
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
