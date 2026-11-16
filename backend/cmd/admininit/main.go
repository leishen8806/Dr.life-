package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	platformsecrets "github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil { fmt.Fprintln(os.Stderr, "admininit: "+err.Error()); os.Exit(1) }
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) < 1 { return errors.New("usage: admininit super|bot") }
	dsn := strings.TrimSpace(os.Getenv("IAM_DATABASE_URL")); if dsn == "" { dsn = strings.TrimSpace(os.Getenv("DATABASE_URL")) }
	if dsn == "" { return errors.New("IAM_DATABASE_URL or DATABASE_URL is required") }
	pool, err := pgxpool.New(ctx, dsn); if err != nil { return fmt.Errorf("connect database: %w", err) }; defer pool.Close()
	ring, err := platformsecrets.LoadFromEnvironment(); if err != nil { return errors.New("master key configuration is invalid") }
	envelope, err := platformsecrets.NewEnvelope(ring); if err != nil { return err }
	service := &iam.BootstrapService{DB: pool, Cipher: envelope, Audit: &platformaudit.Store{DB: pool}}
	switch args[0] {
	case "super":
		if len(args) != 3 || args[1] != "--username" { return errors.New("usage: admininit super --username <name>") }
		result, err := service.CreateFirstSuper(ctx, args[2]); if err != nil { return err }
		fmt.Fprintf(stdout, "username=%s\npassword=%s\ntotp=%s\n", result.Admin.Name, result.InitialPassword, result.TOTPProvisioningURI)
		return nil
	case "bot":
		if len(args) != 7 || args[1] != "--end" || args[3] != "--username" || args[5] != "--actor" { return errors.New("usage: admininit bot --end buyer|merchant --username <name> --actor <super public id>") }
		scanner := bufio.NewScanner(stdin); if !scanner.Scan() { return errors.New("bot token is required on standard input") }
		if err := service.RegisterBot(ctx, iam.BootstrapBotInput{End: args[2], Username: args[4], Actor: args[6], Token: scanner.Text()}); err != nil { return err }
		fmt.Fprintln(stdout, "bot registered")
		return nil
	default: return errors.New("usage: admininit super|bot")
	}
}
