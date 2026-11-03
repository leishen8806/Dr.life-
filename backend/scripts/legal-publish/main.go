// legal-publish publishes an existing, legally-approved document draft.
// It intentionally has no create/edit capability and must use a privileged ops URL.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/legalpublish"
)

type connection struct{ conn *pgx.Conn }

func (c connection) Exec(ctx context.Context, statement string, args ...any) (int64, error) {
	tag, err := c.conn.Exec(ctx, statement, args...)
	return tag.RowsAffected(), err
}

func main() {
	var request legalpublish.Request
	var databaseURL string
	flag.StringVar(&databaseURL, "database-url", os.Getenv("LEGAL_PUBLISH_DATABASE_URL"), "privileged PostgreSQL URL")
	flag.StringVar(&request.Kind, "kind", "", "legal document kind: privacy or terms")
	flag.StringVar(&request.Version, "version", "", "existing draft version, for example v1.0")
	flag.Int64Var(&request.ApprovedBy, "approved-by", 0, "approving active super or ops administrator ID")
	flag.StringVar(&request.ApprovalRef, "approval-ref", "", "approved change or board reference")
	flag.Parse()
	if databaseURL == "" {
		fatal("LEGAL_PUBLISH_DATABASE_URL or -database-url is required")
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer conn.Close(ctx)
	if err := legalpublish.PublishDraft(ctx, connection{conn: conn}, request); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("published legal document kind=%s version=%s approval_ref=%s\n", request.Kind, request.Version, request.ApprovalRef)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "legal-publish:", message)
	os.Exit(1)
}
