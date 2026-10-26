package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"
)

// ScheduledTask declares a recurring registered task. Payload receives the UTC window.
type ScheduledTask struct {
	Name    string
	Cron    string
	Type    string
	Payload func(time.Time) []byte
	Daily   bool
}

type Scheduler struct {
	enqueuer  *Enqueuer
	tasks     map[string]ScheduledTask
	schedules map[string]cron.Schedule
}

func NewScheduler(enqueuer *Enqueuer, tasks []ScheduledTask) (*Scheduler, error) {
	if enqueuer == nil {
		return nil, errors.New("queue enqueuer is required")
	}
	scheduler := &Scheduler{enqueuer: enqueuer, tasks: make(map[string]ScheduledTask, len(tasks)), schedules: make(map[string]cron.Schedule, len(tasks))}
	for _, task := range tasks {
		if task.Name == "" || task.Type == "" || task.Payload == nil {
			return nil, errors.New("scheduled task name, type, and payload are required")
		}
		if _, ok := enqueuer.registry.Lookup(task.Type); !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnregisteredType, task.Type)
		}
		schedule, err := cron.ParseStandard(task.Cron)
		if err != nil {
			return nil, fmt.Errorf("invalid cron for %s: %w", task.Name, err)
		}
		if _, exists := scheduler.tasks[task.Name]; exists {
			return nil, fmt.Errorf("duplicate scheduled task: %s", task.Name)
		}
		scheduler.tasks[task.Name], scheduler.schedules[task.Name] = task, schedule
	}
	return scheduler, nil
}

// EnqueueWindow is safe to call from every application instance: Redis TaskID
// deduplication leaves exactly one queued task for a name/window pair.
func (s *Scheduler) EnqueueWindow(ctx context.Context, name string, window time.Time) error {
	task, ok := s.tasks[name]
	if !ok {
		return ErrUnregisteredType
	}
	window = window.UTC().Truncate(time.Minute)
	_, err := s.enqueuer.Enqueue(ctx, task.Type, task.Payload(window), asynq.TaskID(windowTaskID(task.Name, window)))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// Run checks cron windows once a minute. Callers run it on every instance;
// EnqueueWindow supplies the cross-instance guard.
func (s *Scheduler) Run(ctx context.Context) {
	s.runDue(ctx, time.Now())
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.runDue(ctx, now)
		}
	}
}

func (s *Scheduler) runDue(ctx context.Context, now time.Time) {
	window := now.UTC().Truncate(time.Minute)
	previous := window.Add(-time.Minute)
	for name, schedule := range s.schedules {
		if schedule.Next(previous).Equal(window) {
			_ = s.EnqueueWindow(ctx, name, window)
		}
	}
}

// Backfill queues the previous days only; today's business date is never processed.
func (s *Scheduler) Backfill(ctx context.Context, now time.Time, days int) error {
	if days <= 0 {
		return nil
	}
	end := now.UTC().Truncate(24 * time.Hour)
	for _, task := range s.tasks {
		if !task.Daily {
			continue
		}
		for day := end.AddDate(0, 0, -days); day.Before(end); day = day.AddDate(0, 0, 1) {
			if err := s.EnqueueWindow(ctx, task.Name, day); err != nil {
				return err
			}
		}
	}
	return nil
}

func windowTaskID(name string, window time.Time) string {
	return name + ":" + window.UTC().Format("20060102T1504")
}
