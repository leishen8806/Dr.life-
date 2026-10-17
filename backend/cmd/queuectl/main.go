package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const (
	exitOK      = 0
	exitRuntime = 1
	exitUsage   = 2

	listPageSize = 100
)

// cliConfig keeps run testable without reading process environment.
type cliConfig struct {
	queueRedisURL string
	databaseURL   string
}

type queueStats struct {
	Queue             string  `json:"queue"`
	Pending           int     `json:"pending"`
	Active            int     `json:"active"`
	Scheduled         int     `json:"scheduled"`
	Retry             int     `json:"retry"`
	Dead              int     `json:"dead_letters"`
	ProcessedTotal    int     `json:"processed_total"`
	FailedTotal       int     `json:"failed_total"`
	Paused            bool    `json:"paused"`
	OldestWaitSeconds float64 `json:"oldest_wait_seconds"`
}

type deadTask struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Queue        string     `json:"queue"`
	Retried      int        `json:"retried"`
	MaxRetry     int        `json:"max_retry"`
	LastFailedAt *time.Time `json:"last_failed_at,omitempty"`
}

type deadActionResult struct {
	Queue    string `json:"queue"`
	Replayed int    `json:"replayed"`
	Deleted  int    `json:"deleted"`
	Failed   int    `json:"failed"`
}

type outboxLag struct {
	Pending          int64   `json:"pending"`
	OldestAgeSeconds float64 `json:"oldest_age_seconds"`
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(exitRuntime)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, cliConfig{
		queueRedisURL: cfg.QueueRedisURL,
		databaseURL:   cfg.DatabaseURL,
	}))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, cfg cliConfig) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "stats":
		return runStats(args[1:], stdout, stderr, cfg)
	case "pause":
		return runPauseResume(args[1:], stdout, stderr, cfg, true)
	case "resume":
		return runPauseResume(args[1:], stdout, stderr, cfg, false)
	case "dead":
		return runDead(ctx, args[1:], stdout, stderr, cfg)
	case "outbox":
		return runOutbox(ctx, args[1:], stdout, stderr, cfg)
	case "help", "-h", "--help":
		printUsage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `usage: queuectl <command> [flags]

commands:
  stats                          print queue depth, wait time, and counters as JSON
  pause --queue <name>           stop consuming a queue
  resume --queue <name>          resume consuming a queue
  dead list --queue <name>       list archived (dead-letter) tasks as JSON
  dead replay --queue <name> --yes
                                 replay archived tasks (requires --yes)
  dead delete --queue <name> --yes
                                 delete archived tasks (requires --yes)
  outbox lag                     print outbox pending count and oldest age as JSON

exit codes: 0 success, 2 usage/refusal, 1 runtime error
`)
}

func runStats(args []string, stdout, stderr io.Writer, cfg cliConfig) int {
	fs := newFlagSet("queuectl stats", stderr)
	if code := parseFlags(fs, args, stdout, stderr); code != exitOK {
		return code
	}
	inspector, err := newInspector(cfg.queueRedisURL)
	if err != nil {
		return runtimeError(stderr, err)
	}
	defer inspector.Close()

	existing, err := inspector.Queues()
	if err != nil {
		return runtimeError(stderr, fmt.Errorf("list queues: %w", err))
	}
	known := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		known[name] = struct{}{}
	}
	rows := make([]queueStats, 0, len(queue.AllQueues))
	for _, name := range queue.AllQueues {
		if _, ok := known[name]; !ok {
			rows = append(rows, queueStats{Queue: name})
			continue
		}
		info, err := inspector.GetQueueInfo(name)
		if err != nil {
			return runtimeError(stderr, fmt.Errorf("read queue %s: %w", name, err))
		}
		rows = append(rows, queueStats{
			Queue:             name,
			Pending:           info.Pending,
			Active:            info.Active,
			Scheduled:         info.Scheduled,
			Retry:             info.Retry,
			Dead:              info.Archived,
			ProcessedTotal:    info.ProcessedTotal,
			FailedTotal:       info.FailedTotal,
			Paused:            info.Paused,
			OldestWaitSeconds: info.Latency.Seconds(),
		})
	}
	return writeJSON(stdout, stderr, rows)
}

func runPauseResume(args []string, stdout, stderr io.Writer, cfg cliConfig, pause bool) int {
	name := "queuectl resume"
	if pause {
		name = "queuectl pause"
	}
	fs := newFlagSet(name, stderr)
	queueName := fs.String("queue", "", "queue name")
	if code := parseFlags(fs, args, stdout, stderr); code != exitOK {
		return code
	}
	if code := validateQueueFlag(*queueName, stderr); code != exitOK {
		return code
	}
	inspector, err := newInspector(cfg.queueRedisURL)
	if err != nil {
		return runtimeError(stderr, err)
	}
	defer inspector.Close()

	action := "resumed"
	if pause {
		err = inspector.PauseQueue(*queueName)
		action = "paused"
	} else {
		err = inspector.UnpauseQueue(*queueName)
	}
	if err != nil {
		return runtimeError(stderr, fmt.Errorf("%s queue %s: %w", action, *queueName, err))
	}
	return writeJSON(stdout, stderr, struct {
		Queue  string `json:"queue"`
		Paused bool   `json:"paused"`
	}{Queue: *queueName, Paused: pause})
}

func runDead(ctx context.Context, args []string, stdout, stderr io.Writer, cfg cliConfig) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "dead requires one of: list, replay, delete")
		printUsage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "list":
		return runDeadList(args[1:], stdout, stderr, cfg)
	case "replay":
		return runDeadAction(ctx, args[1:], stdout, stderr, cfg, false)
	case "delete":
		return runDeadAction(ctx, args[1:], stdout, stderr, cfg, true)
	default:
		fmt.Fprintf(stderr, "unknown dead subcommand %q\n", args[0])
		return exitUsage
	}
}

func runDeadList(args []string, stdout, stderr io.Writer, cfg cliConfig) int {
	fs := newFlagSet("queuectl dead list", stderr)
	queueName := fs.String("queue", "", "queue name")
	if code := parseFlags(fs, args, stdout, stderr); code != exitOK {
		return code
	}
	if code := validateQueueFlag(*queueName, stderr); code != exitOK {
		return code
	}
	inspector, err := newInspector(cfg.queueRedisURL)
	if err != nil {
		return runtimeError(stderr, err)
	}
	defer inspector.Close()

	tasks, err := listArchived(inspector, *queueName)
	if err != nil {
		return runtimeError(stderr, err)
	}
	rows := make([]deadTask, 0, len(tasks))
	for _, task := range tasks {
		row := deadTask{ID: task.ID, Type: task.Type, Queue: task.Queue, Retried: task.Retried, MaxRetry: task.MaxRetry}
		if !task.LastFailedAt.IsZero() {
			lastFailed := task.LastFailedAt
			row.LastFailedAt = &lastFailed
		}
		rows = append(rows, row)
	}
	return writeJSON(stdout, stderr, rows)
}

func runDeadAction(_ context.Context, args []string, stdout, stderr io.Writer, cfg cliConfig, deleteTasks bool) int {
	name := "queuectl dead replay"
	action := "replay"
	if deleteTasks {
		name = "queuectl dead delete"
		action = "delete"
	}
	fs := newFlagSet(name, stderr)
	queueName := fs.String("queue", "", "queue name")
	confirmed := fs.Bool("yes", false, "confirm the destructive operation")
	if code := parseFlags(fs, args, stdout, stderr); code != exitOK {
		return code
	}
	if code := validateQueueFlag(*queueName, stderr); code != exitOK {
		return code
	}
	if !*confirmed {
		fmt.Fprintf(stderr, "refusing to %s dead letters in queue %q: pass --yes to confirm\n", action, *queueName)
		return exitUsage
	}
	if !deleteTasks {
		fmt.Fprintf(stderr, "warning: replaying dead letters re-runs the original task payloads in queue %q\n", *queueName)
	} else {
		fmt.Fprintf(stderr, "warning: deleting dead letters is permanent for queue %q\n", *queueName)
	}

	inspector, err := newInspector(cfg.queueRedisURL)
	if err != nil {
		return runtimeError(stderr, err)
	}
	defer inspector.Close()

	tasks, err := listArchived(inspector, *queueName)
	if err != nil {
		return runtimeError(stderr, err)
	}
	result := deadActionResult{Queue: *queueName}
	replayedIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		var actionErr error
		if deleteTasks {
			actionErr = inspector.DeleteTask(*queueName, task.ID)
			if actionErr == nil {
				result.Deleted++
			}
		} else {
			actionErr = inspector.RunTask(*queueName, task.ID)
			if actionErr == nil {
				result.Replayed++
				replayedIDs = append(replayedIDs, task.ID)
			}
		}
		if actionErr != nil {
			result.Failed++
		}
	}
	if !deleteTasks && len(replayedIDs) > 0 {
		waitForPickup(inspector, *queueName, replayedIDs, 5*time.Second)
	}
	if code := writeJSON(stdout, stderr, result); code != exitOK {
		return code
	}
	if result.Failed > 0 {
		return runtimeError(stderr, fmt.Errorf("%d dead letters could not be processed", result.Failed))
	}
	return exitOK
}

// waitForPickup gives a running worker a chance to claim replayed tasks before
// the command reports success. Replay itself only moves tasks back to pending.
func waitForPickup(inspector *asynq.Inspector, queueName string, ids []string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for len(ids) > 0 && time.Now().Before(deadline) {
		remaining := ids[:0]
		for _, id := range ids {
			info, err := inspector.GetTaskInfo(queueName, id)
			if err != nil {
				continue
			}
			if info.State == asynq.TaskStatePending {
				remaining = append(remaining, id)
			}
		}
		ids = remaining
		if len(ids) > 0 {
			time.Sleep(25 * time.Millisecond)
		}
	}
}

func runOutbox(ctx context.Context, args []string, stdout, stderr io.Writer, cfg cliConfig) int {
	if len(args) == 0 || args[0] != "lag" {
		fmt.Fprintln(stderr, "outbox requires the lag subcommand")
		return exitUsage
	}
	if len(args) > 1 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", args[1])
		return exitUsage
	}
	if cfg.databaseURL == "" {
		return runtimeError(stderr, errors.New("DATABASE_URL is required for outbox lag"))
	}
	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		return runtimeError(stderr, fmt.Errorf("connect database: %w", err))
	}
	defer pool.Close()
	pending, oldest, err := queue.OutboxLag(ctx, pool)
	if err != nil {
		return runtimeError(stderr, fmt.Errorf("read outbox lag: %w", err))
	}
	return writeJSON(stdout, stderr, outboxLag{Pending: pending, OldestAgeSeconds: oldest.Seconds()})
}

func listArchived(inspector *asynq.Inspector, queueName string) ([]*asynq.TaskInfo, error) {
	var tasks []*asynq.TaskInfo
	for page := 1; ; page++ {
		batch, err := inspector.ListArchivedTasks(queueName, asynq.PageSize(listPageSize), asynq.Page(page))
		if err != nil {
			if errors.Is(err, asynq.ErrQueueNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("list dead letters for %s: %w", queueName, err)
		}
		tasks = append(tasks, batch...)
		if len(batch) < listPageSize {
			return tasks, nil
		}
	}
}

func newInspector(rawURL string) (*asynq.Inspector, error) {
	if rawURL == "" {
		return nil, errors.New("QUEUE_REDIS_URL is required")
	}
	opt, err := asynq.ParseRedisURI(rawURL)
	if err != nil {
		return nil, errors.New("invalid QUEUE_REDIS_URL")
	}
	return asynq.NewInspector(opt), nil
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string, stdout, stderr io.Writer) int {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	return exitOK
}

func validateQueueFlag(name string, stderr io.Writer) int {
	if name == "" {
		fmt.Fprintln(stderr, "--queue is required")
		return exitUsage
	}
	for _, known := range queue.AllQueues {
		if name == known {
			return exitOK
		}
	}
	fmt.Fprintf(stderr, "unknown queue %q\n", name)
	return exitUsage
}

func writeJSON(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return runtimeError(stderr, fmt.Errorf("write output: %w", err))
	}
	return exitOK
}

func runtimeError(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "error:", err)
	return exitRuntime
}
