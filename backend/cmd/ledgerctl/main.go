package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/db"
)

type ledgerService interface {
	RunDailyReconciliation(context.Context, time.Time, ledger.ReconciliationOptions) (ledger.ReconciliationRun, error)
	SnapshotDaily(context.Context, time.Time) (ledger.SnapshotResult, error)
	ListPayoutFreezes(context.Context, bool) ([]ledger.PayoutFreeze, error)
	ReleasePayoutFreeze(context.Context, int64, int64, string) (ledger.PayoutFreeze, error)
}

type command struct {
	name       string
	day        time.Time
	force      bool
	reason     string
	freezeID   int64
	releasedBy int64
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cmd, err := parseCommand(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "configuration error:", err)
		return 1
	}
	location, err := time.LoadLocation(cfg.LedgerReconTimezone)
	if err != nil {
		fmt.Fprintln(stderr, "reconciliation timezone error:", err)
		return 1
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "database connection error:", err)
		return 1
	}
	defer pool.Close()
	return execute(ctx, cmd, ledger.New(pool, ledger.Options{ReconciliationLocation: location}), stdout, stderr)
}

func execute(ctx context.Context, cmd command, service ledgerService, stdout, stderr io.Writer) int {
	var value any
	var err error
	switch cmd.name {
	case "recon":
		value, err = service.RunDailyReconciliation(ctx, cmd.day, ledger.ReconciliationOptions{Force: cmd.force, Reason: cmd.reason})
	case "snapshot":
		value, err = service.SnapshotDaily(ctx, cmd.day)
	case "freeze-list":
		value, err = service.ListPayoutFreezes(ctx, true)
	case "freeze-release":
		value, err = service.ReleasePayoutFreeze(ctx, cmd.freezeID, cmd.releasedBy, cmd.reason)
	default:
		err = errors.New("unsupported command")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if run, ok := value.(ledger.ReconciliationRun); ok && run.Status != "ok" {
		_ = json.NewEncoder(stdout).Encode(run)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return command{}, errors.New("usage: ledgerctl recon|snapshot|freeze")
	}
	switch args[0] {
	case "recon", "snapshot":
		set := flag.NewFlagSet(args[0], flag.ContinueOnError)
		set.SetOutput(io.Discard)
		dayText := set.String("day", "", "day in YYYY-MM-DD")
		force := set.Bool("force", false, "force a new reconciliation attempt")
		reason := set.String("reason", "", "force reason")
		if err := set.Parse(args[1:]); err != nil || set.NArg() != 0 {
			return command{}, errors.New("invalid command arguments")
		}
		day, err := time.Parse("2006-01-02", *dayText)
		if err != nil {
			return command{}, errors.New("--day must use YYYY-MM-DD")
		}
		if args[0] == "snapshot" && (*force || *reason != "") {
			return command{}, errors.New("snapshot does not accept --force or --reason")
		}
		if *force && *reason == "" {
			return command{}, ledger.ErrReconciliationForceReason
		}
		if !*force && *reason != "" {
			return command{}, errors.New("--reason requires --force")
		}
		return command{name: args[0], day: day, force: *force, reason: *reason}, nil
	case "freeze":
		if len(args) < 2 {
			return command{}, errors.New("usage: ledgerctl freeze list|release")
		}
		switch args[1] {
		case "list":
			if len(args) != 2 {
				return command{}, errors.New("freeze list accepts no arguments")
			}
			return command{name: "freeze-list"}, nil
		case "release":
			set := flag.NewFlagSet("freeze release", flag.ContinueOnError)
			set.SetOutput(io.Discard)
			id := set.String("id", "", "freeze id")
			by := set.String("by", "", "releaser actor id")
			reason := set.String("reason", "", "release reason")
			if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
				return command{}, errors.New("invalid freeze release arguments")
			}
			freezeID, errID := strconv.ParseInt(*id, 10, 64)
			releasedBy, errBy := strconv.ParseInt(*by, 10, 64)
			if errID != nil || errBy != nil || freezeID <= 0 || releasedBy <= 0 || *reason == "" {
				return command{}, errors.New("freeze release requires positive --id, positive --by, and non-empty --reason")
			}
			return command{name: "freeze-release", freezeID: freezeID, releasedBy: releasedBy, reason: *reason}, nil
		}
	}
	return command{}, errors.New("unknown command")
}
