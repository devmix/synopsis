package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_RegisterAndRunNow(t *testing.T) {
	t.Parallel()

	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var calls atomic.Int32
	if err := s.Register("test_job", 3600, func(ctx context.Context) {
		calls.Add(1)
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	s.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()

	if err := s.RunNow("test_job"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	waitForCalls(t, &calls, 1)
}

// waitForCalls blocks until the atomic counter reaches want, failing the test
// after a timeout. RunNow returns before the job function finishes, so tests
// must poll for side effects.
func waitForCalls(t *testing.T, calls *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("calls = %d after timeout, want %d", calls.Load(), want)
}

func TestScheduler_RegisterValidation(t *testing.T) {
	t.Parallel()

	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name    string
		jobName string
		seconds int
		fn      JobFunc
		wantErr bool
	}{
		{name: "empty name", jobName: "", seconds: 60, fn: func(ctx context.Context) {}, wantErr: true},
		{name: "zero interval", jobName: "job", seconds: 0, fn: func(ctx context.Context) {}, wantErr: true},
		{name: "negative interval", jobName: "job", seconds: -5, fn: func(ctx context.Context) {}, wantErr: true},
		{name: "nil function", jobName: "job", seconds: 60, fn: nil, wantErr: true},
		{name: "valid", jobName: "job", seconds: 60, fn: func(ctx context.Context) {}, wantErr: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := s.Register(tt.jobName, tt.seconds, tt.fn)
			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestScheduler_RunNowUnknownJob(t *testing.T) {
	t.Parallel()

	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.RunNow("missing"); err == nil {
		t.Error("expected error for unknown job, got nil")
	}
}

func TestScheduler_RegisterReplacesExisting(t *testing.T) {
	t.Parallel()

	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var calls atomic.Int32
	if err := s.Register("job", 3600, func(ctx context.Context) { calls.Add(1) }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Re-register with a different function; the old one must be replaced.
	if err := s.Register("job", 3600, func(ctx context.Context) { calls.Add(10) }); err != nil {
		t.Fatalf("re-Register: %v", err)
	}

	s.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()

	if err := s.RunNow("job"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	waitForCalls(t, &calls, 10)
}

func TestScheduler_ScheduledExecution(t *testing.T) {
	t.Parallel()

	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var calls atomic.Int32
	if err := s.Register("tick", 1, func(ctx context.Context) { calls.Add(1) }); err != nil {
		t.Fatalf("Register: %v", err)
	}

	s.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()

	// The job runs every second; wait for at least one scheduled execution.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("expected at least one scheduled execution within 5s")
}