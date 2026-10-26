package queue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

var (
	ErrPayloadTooLarge = errors.New("queue task payload exceeds limit")
	ErrQueueFull       = errors.New("queue is at capacity")
)

type Client interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

type queueInspector interface {
	GetQueueInfo(string) (*asynq.QueueInfo, error)
}

type EnqueueOptions struct {
	PayloadMaxBytes int
	DepthLimits     map[string]int
	DepthCacheTTL   time.Duration
	MemoryBlocked   func(context.Context, TypeSpec) (bool, error)
	OnDropped       func(TypeSpec)
}

type Enqueuer struct {
	client    Client
	registry  *Registry
	inspector queueInspector
	options   EnqueueOptions
	mu        sync.Mutex
	depth     map[string]depthCache
}

type depthCache struct {
	value int
	at    time.Time
}

func NewEnqueuer(client Client, registry *Registry, options EnqueueOptions) *Enqueuer {
	if options.PayloadMaxBytes <= 0 {
		options.PayloadMaxBytes = 4 << 10
	}
	if options.DepthCacheTTL <= 0 {
		options.DepthCacheTTL = time.Second
	}
	if options.DepthLimits == nil {
		options.DepthLimits = map[string]int{QueueNotify: 50000, QueueAI: 2000, QueueBatch: 10000}
	}
	return &Enqueuer{client: client, registry: registry, options: options, depth: make(map[string]depthCache)}
}

func (e *Enqueuer) WithInspector(inspector queueInspector) *Enqueuer {
	e.inspector = inspector
	return e
}

func (e *Enqueuer) Enqueue(ctx context.Context, name string, payload []byte, options ...asynq.Option) (*asynq.TaskInfo, error) {
	spec, ok := e.registry.Lookup(name)
	if !ok {
		return nil, ErrUnregisteredType
	}
	if len(payload) > e.options.PayloadMaxBytes {
		return nil, ErrPayloadTooLarge
	}
	if spec.Queue != QueueCritical {
		blocked, err := e.blocked(ctx, spec)
		if err != nil {
			return nil, err
		}
		if blocked {
			return e.full(spec)
		}
		full, err := e.fullQueue(spec.Queue)
		if err != nil {
			return nil, err
		}
		if full {
			return e.full(spec)
		}
	}
	task := asynq.NewTask(name, payload)
	options = append(options, asynq.Queue(spec.Queue), asynq.Timeout(spec.Timeout), asynq.MaxRetry(spec.MaxRetry))
	return e.client.EnqueueContext(ctx, task, options...)
}

func (e *Enqueuer) full(spec TypeSpec) (*asynq.TaskInfo, error) {
	if spec.QueueFullPolicy == DropWhenFull {
		if e.options.OnDropped != nil {
			e.options.OnDropped(spec)
		}
		return nil, nil
	}
	return nil, ErrQueueFull
}

func (e *Enqueuer) blocked(ctx context.Context, spec TypeSpec) (bool, error) {
	if e.options.MemoryBlocked == nil || (spec.Queue != QueueBatch && spec.Queue != QueueNotify) {
		return false, nil
	}
	return e.options.MemoryBlocked(ctx, spec)
}

func (e *Enqueuer) fullQueue(queue string) (bool, error) {
	limit := e.options.DepthLimits[queue]
	if limit <= 0 || e.inspector == nil {
		return false, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if cached, ok := e.depth[queue]; ok && time.Since(cached.at) < e.options.DepthCacheTTL {
		return cached.value >= limit, nil
	}
	info, err := e.inspector.GetQueueInfo(queue)
	if err != nil {
		return false, err
	}
	depth := info.Pending + info.Active + info.Scheduled + info.Retry
	e.depth[queue] = depthCache{value: depth, at: time.Now()}
	return depth >= limit, nil
}

type MemoryClient struct{}

func NewMemoryClient() *MemoryClient { return &MemoryClient{} }
func (*MemoryClient) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{}, nil
}
