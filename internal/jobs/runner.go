package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var (
	ErrNotRunning      = errors.New("job runner is not running")
	ErrHandlerNotFound = errors.New("job handler not found")
)

type Job struct {
	Name       string          `json:"name"`
	BotSlug    string          `json:"bot_slug,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	EnqueuedAt time.Time       `json:"enqueued_at"`
}

type Handler func(context.Context, Job) error

type Runner struct {
	logger   *slog.Logger
	queue    chan Job
	handlers map[string]Handler

	mu      sync.RWMutex
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
}

func NewRunner(logger *slog.Logger, buffer int) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	if buffer <= 0 {
		buffer = 32
	}

	return &Runner{
		logger:   logger,
		queue:    make(chan Job, buffer),
		handlers: make(map[string]Handler),
	}
}

func (r *Runner) Start(parent context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return
	}

	r.ctx, r.cancel = context.WithCancel(parent)
	r.running = true
	r.wg.Add(1)

	go r.loop()
	r.logger.Info("worker loop started")
}

func (r *Runner) RegisterHandler(name string, handler Handler) error {
	if name == "" {
		return errors.New("handler name is required")
	}
	if handler == nil {
		return errors.New("handler is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("handler %q already registered", name)
	}

	r.handlers[name] = handler
	return nil
}

func (r *Runner) Submit(ctx context.Context, job Job) error {
	r.mu.RLock()
	running := r.running
	r.mu.RUnlock()

	if !running {
		return ErrNotRunning
	}

	if job.EnqueuedAt.IsZero() {
		job.EnqueuedAt = time.Now().UTC()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case r.queue <- job:
		return nil
	}
}

func (r *Runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}

	cancel := r.cancel
	r.cancel = nil
	r.running = false
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.wg.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		r.logger.Info("worker loop stopped")
		return nil
	}
}

func (r *Runner) loop() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return
		case job := <-r.queue:
			r.handleJob(job)
		}
	}
}

func (r *Runner) handleJob(job Job) {
	r.mu.RLock()
	handler, ok := r.handlers[job.Name]
	r.mu.RUnlock()

	if !ok {
		r.logger.Error("job handler is not registered", "job_name", job.Name, "bot_slug", job.BotSlug)
		return
	}

	if err := handler(r.ctx, job); err != nil {
		r.logger.Error("job handler failed", "job_name", job.Name, "bot_slug", job.BotSlug, "error", err)
	}
}

