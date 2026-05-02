package jobs

import (
	"context"
	"testing"
	"time"
)

func TestRunnerProcessesJobs(t *testing.T) {
	t.Parallel()

	runner := NewRunner(nil, 1)
	handled := make(chan Job, 1)

	if err := runner.RegisterHandler("notify", func(ctx context.Context, job Job) error {
		handled <- job
		return nil
	}); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner.Start(ctx)

	if err := runner.Submit(context.Background(), Job{Name: "notify", BotSlug: "sample-echo"}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case job := <-handled:
		if job.BotSlug != "sample-echo" {
			t.Fatalf("unexpected job: %+v", job)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for job")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	if err := runner.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRunnerRejectsSubmitWhenStopped(t *testing.T) {
	t.Parallel()

	runner := NewRunner(nil, 1)
	if err := runner.Submit(context.Background(), Job{Name: "notify"}); err != ErrNotRunning {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}

