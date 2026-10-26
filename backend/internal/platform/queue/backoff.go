package queue

import (
	"errors"
	"math/rand/v2"
	"time"

	"github.com/hibiken/asynq"
)

type Backoff struct {
	Base time.Duration
	Max  time.Duration
}

func (b Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := b.Base
	if base <= 0 {
		base = time.Second
	}
	max := b.Max
	if max <= 0 {
		max = time.Hour
	}
	for i := 1; i < attempt && base < max/2; i++ {
		base *= 2
	}
	if base > max {
		base = max
	}
	// Full jitter prevents many failed tasks retrying in lockstep.
	return time.Duration(rand.Int64N(int64(base) + 1))
}

func RetryDelay(registry *Registry) asynq.RetryDelayFunc {
	return func(attempt int, _ error, task *asynq.Task) time.Duration {
		if spec, ok := registry.Lookup(task.Type()); ok {
			return spec.Backoff.Delay(attempt)
		}
		return (Backoff{}).Delay(attempt)
	}
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

func IsPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}
