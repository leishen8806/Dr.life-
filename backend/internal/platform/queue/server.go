package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

var ErrQueueRedisEviction = errors.New("queue Redis maxmemory-policy must be noeviction")

type Handler func(context.Context, *asynq.Task) error

type WorkerOptions struct {
	Concurrency     map[string]int
	ShutdownTimeout time.Duration
	Logger          *slog.Logger
}

type Worker struct {
	registry *Registry
	servers  map[string]*asynq.Server
	handlers map[string]Handler
	logger   *slog.Logger
	mu       sync.RWMutex
}

func NewWorker(redisURL string, registry *Registry, options WorkerOptions) (*Worker, error) {
	opt, err := asynqRedisOptions(redisURL)
	if err != nil {
		return nil, err
	}
	if registry == nil {
		return nil, errors.New("queue registry is required")
	}
	defaults := map[string]int{QueueCritical: 16, QueueDefault: 16, QueueNotify: 8, QueueAI: 4, QueueBatch: 4, QueueScheduled: 4}
	if options.Concurrency != nil {
		for name, value := range options.Concurrency {
			if validQueue(name) && value > 0 {
				defaults[name] = value
			}
		}
	}
	worker := &Worker{registry: registry, servers: make(map[string]*asynq.Server, len(defaults)), handlers: make(map[string]Handler), logger: options.Logger}
	for name, concurrency := range defaults {
		worker.servers[name] = asynq.NewServer(opt, asynq.Config{Concurrency: concurrency, Queues: map[string]int{name: 1}, RetryDelayFunc: RetryDelay(registry), ShutdownTimeout: options.ShutdownTimeout, ErrorHandler: asynq.ErrorHandlerFunc(worker.logTaskError)})
	}
	return worker, nil
}

func (w *Worker) Register(name string, handler Handler) error {
	spec, ok := w.registry.Lookup(name)
	if !ok {
		return ErrUnregisteredType
	}
	if handler == nil {
		return errors.New("queue handler is required")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.handlers[name]; exists {
		return fmt.Errorf("queue handler already registered: %s", name)
	}
	if w.servers[spec.Queue] == nil {
		return fmt.Errorf("queue server missing: %s", spec.Queue)
	}
	w.handlers[name] = handler
	return nil
}

func (w *Worker) Start(ctx context.Context) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for queueName, server := range w.servers {
		queueName, server := queueName, server
		if err := server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error { return w.process(ctx, queueName, task) })); err != nil {
			w.shutdown()
			return err
		}
	}
	go func() { <-ctx.Done(); w.shutdown() }()
	return nil
}

func (w *Worker) process(ctx context.Context, queueName string, task *asynq.Task) (err error) {
	spec, ok := w.registry.Lookup(task.Type())
	if !ok || spec.Queue != queueName {
		return fmt.Errorf("%w: %s", asynq.SkipRetry, task.Type())
	}
	w.mu.RLock()
	handler := w.handlers[task.Type()]
	w.mu.RUnlock()
	if handler == nil {
		return fmt.Errorf("%w: handler for %s", asynq.SkipRetry, task.Type())
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if w.logger != nil {
				w.logger.Error("queue task panic", "type", task.Type(), "task_id", task.ResultWriter().TaskID())
			}
			err = fmt.Errorf("task panic: %v", recovered)
		}
	}()
	err = handler(ctx, task)
	if IsPermanent(err) || (spec.SkipRetryOn != nil && spec.SkipRetryOn(err)) {
		return fmt.Errorf("%w: %w", asynq.SkipRetry, err)
	}
	return err
}

func (w *Worker) logTaskError(_ context.Context, task *asynq.Task, err error) {
	if w.logger != nil {
		w.logger.Error("queue task failed", "type", task.Type(), "task_id", task.ResultWriter().TaskID(), "error", err)
	}
}

func (w *Worker) shutdown() {
	for _, server := range w.servers {
		server.Shutdown()
	}
}

func QueueRedisPolicy(ctx context.Context, rawURL string, allowEviction bool) error {
	if allowEviction {
		return nil
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return errors.New("invalid queue Redis URL")
	}
	client := redis.NewClient(options)
	defer client.Close()
	policy, err := client.ConfigGet(ctx, "maxmemory-policy").Result()
	if err != nil {
		return fmt.Errorf("read queue Redis maxmemory-policy: %w", err)
	}
	if policy["maxmemory-policy"] != "noeviction" {
		return ErrQueueRedisEviction
	}
	return nil
}

func asynqRedisOptions(raw string) (asynq.RedisClientOpt, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "redis" || parsed.Host == "" {
		return asynq.RedisClientOpt{}, errors.New("invalid queue Redis URL")
	}
	db := 0
	if parsed.Path != "" && parsed.Path != "/" {
		_, err = fmt.Sscanf(parsed.Path, "/%d", &db)
		if err != nil {
			return asynq.RedisClientOpt{}, errors.New("invalid queue Redis URL")
		}
	}
	password, _ := parsed.User.Password()
	return asynq.RedisClientOpt{Addr: parsed.Host, Password: password, DB: db}, nil
}
