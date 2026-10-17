// Package queue owns the shared asynq task contract. Producers can enqueue
// only registered task types; consumers receive only the same registered types.
package queue

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

const (
	QueueCritical  = "critical"
	QueueDefault   = "default"
	QueueNotify    = "notify"
	QueueAI        = "ai"
	QueueBatch     = "batch"
	QueueScheduled = "scheduled"
)

var (
	ErrUnregisteredType = errors.New("queue task type is not registered")
	ErrInvalidTypeSpec  = errors.New("invalid queue task type specification")
	taskTypePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*:v[1-9][0-9]*$`)
)

type QueueFullPolicy uint8

const (
	RejectWhenFull QueueFullPolicy = iota
	DropWhenFull
)

type TypeSpec struct {
	Name            string
	Queue           string
	Timeout         time.Duration
	MaxRetry        int
	Backoff         Backoff
	SkipRetryOn     func(error) bool
	QueueFullPolicy QueueFullPolicy
}

type Registry struct {
	mu    sync.RWMutex
	specs map[string]TypeSpec
}

func NewRegistry() *Registry { return &Registry{specs: make(map[string]TypeSpec)} }

func (r *Registry) Register(spec TypeSpec) error {
	if !taskTypePattern.MatchString(spec.Name) || !validQueue(spec.Queue) || spec.Timeout <= 0 || spec.MaxRetry < 0 {
		return fmt.Errorf("%w: name, queue, timeout, or retry", ErrInvalidTypeSpec)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.specs[spec.Name]; exists {
		return fmt.Errorf("%w: duplicate %q", ErrInvalidTypeSpec, spec.Name)
	}
	r.specs[spec.Name] = spec
	return nil
}

func (r *Registry) Lookup(name string) (TypeSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[name]
	return spec, ok
}

func validQueue(queue string) bool {
	switch queue {
	case QueueCritical, QueueDefault, QueueNotify, QueueAI, QueueBatch, QueueScheduled:
		return true
	default:
		return false
	}
}
