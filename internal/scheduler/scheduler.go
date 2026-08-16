// Package scheduler provides a universal job scheduler built on gocron
// (github.com/go-co-op/gocron/v2). Jobs are registered by name so new tasks
// can be added without modifying the scheduler itself. Every job runs in
// singleton mode (LimitModeReschedule): overlapping executions are dropped,
// which protects SQLite from concurrent writers.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// JobFunc is the function executed by a scheduled job. It receives the
// context the job was started with (cancelled on scheduler shutdown).
type JobFunc func(ctx context.Context)

// Scheduler wraps a gocron scheduler with a name-based job registry.
type Scheduler struct {
	mu   sync.Mutex
	s    gocron.Scheduler
	jobs map[string]gocron.Job
}

// New creates a scheduler. The underlying gocron scheduler is started on the
// first Start() call; jobs registered before that are scheduled immediately
// once the scheduler starts.
func New() (*Scheduler, error) {
	s, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}
	return &Scheduler{
		s:    s,
		jobs: make(map[string]gocron.Job),
	}, nil
}

// Register adds a job to the registry under the given name. Registering an
// existing name replaces the previous job with the new definition.
// intervalSeconds must be positive. fn receives the job context and should
// respect its cancellation.
func (s *Scheduler) Register(name string, intervalSeconds int, fn JobFunc) error {
	if name == "" {
		return fmt.Errorf("job name is required")
	}
	if intervalSeconds <= 0 {
		return fmt.Errorf("job %q: interval_seconds must be positive", name)
	}
	if fn == nil {
		return fmt.Errorf("job %q: function is required", name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.s.NewJob(
		gocron.DurationJob(time.Duration(intervalSeconds)*time.Second),
		gocron.NewTask(fn),
		gocron.WithName(name),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		return fmt.Errorf("register job %q: %w", name, err)
	}

	if old, ok := s.jobs[name]; ok {
		_ = s.s.RemoveJob(old.ID()) //nolint:errcheck // replacing an existing job
	}
	s.jobs[name] = job
	return nil
}

// RunNow executes the named job once immediately without altering its
// schedule. Returns an error if the job is not registered.
func (s *Scheduler) RunNow(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[name]
	if !ok {
		return fmt.Errorf("job %q not registered", name)
	}
	if err := job.RunNow(); err != nil {
		return fmt.Errorf("run job %q: %w", name, err)
	}
	return nil
}

// Start begins scheduling all registered jobs. Non-blocking.
func (s *Scheduler) Start() {
	s.s.Start()
}

// Shutdown stops the scheduler and waits for running jobs to finish, respecting
// the given context deadline.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if err := s.s.ShutdownWithContext(ctx); err != nil {
		return fmt.Errorf("shutdown scheduler: %w", err)
	}
	return nil
}
