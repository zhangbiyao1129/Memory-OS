package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"memory-os/internal/archive"
)

func TestRunnerStopsWhenContextIsCanceled(t *testing.T) {
	runner := NewRunner(Options{Concurrency: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.Run(ctx)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerWaitsForContext(t *testing.T) {
	runner := NewRunner(Options{Concurrency: 1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- runner.Run(ctx)
	}()

	select {
	case <-done:
		t.Fatal("Run() returned before context was canceled")
	case <-time.After(10 * time.Millisecond):
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRejectsInvalidConcurrency(t *testing.T) {
	_, err := NewRunnerChecked(Options{Concurrency: 0})
	if err == nil {
		t.Fatal("NewRunnerChecked() error = nil, want invalid concurrency error")
	}
}

func TestRunnerReportsArchiveWorkerConfigured(t *testing.T) {
	archiveWorker := ArchiveWorker{}
	runner := NewRunner(Options{Concurrency: 1, ArchiveWorker: &archiveWorker})

	if !runner.ArchiveWorkerConfigured() {
		t.Fatal("ArchiveWorkerConfigured() = false, want true")
	}
}

func TestRunnerReportsAutoMaintenanceConfigured(t *testing.T) {
	maintenance := &fakeAutoMaintenance{}
	runner := NewRunner(Options{Concurrency: 1, AutoMaintenance: maintenance})

	if !runner.AutoMaintenanceConfigured() {
		t.Fatal("AutoMaintenanceConfigured() = false, want true")
	}
}

func TestRunnerReportsHotMemoryMaintenanceConfigured(t *testing.T) {
	maintenance := &fakeHotMemoryMaintenance{}
	runner := NewRunner(Options{Concurrency: 1, HotMemoryMaintenance: maintenance})

	if !runner.HotMemoryMaintenanceConfigured() {
		t.Fatal("HotMemoryMaintenanceConfigured() = false, want true")
	}
}

func TestRunnerRunsCleanupOnExit(t *testing.T) {
	cleaned := false
	runner := NewRunner(Options{Concurrency: 1, Cleanup: func() { cleaned = true }})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !cleaned {
		t.Fatal("Run() did not call cleanup")
	}
}

func TestRunnerRunsAutoMaintenanceLoop(t *testing.T) {
	maintenance := &fakeAutoMaintenance{}
	runner := NewRunner(Options{Concurrency: 1, AutoMaintenance: maintenance, AutoMaintenanceInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	waitFor(t, func() bool { return maintenance.runCount() >= 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRunsHotMemoryMaintenanceLoop(t *testing.T) {
	maintenance := &fakeHotMemoryMaintenance{}
	runner := NewRunner(Options{Concurrency: 1, HotMemoryMaintenance: maintenance, HotMemoryMaintenanceInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	waitFor(t, func() bool { return maintenance.runCount() >= 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerKeepsHotMemoryMaintenanceLoopAfterError(t *testing.T) {
	maintenance := &fakeHotMemoryMaintenance{errOnFirstRun: errors.New("model provider status: 429")}
	runner := NewRunner(Options{Concurrency: 1, HotMemoryMaintenance: maintenance, HotMemoryMaintenanceInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	waitFor(t, func() bool { return maintenance.runCount() >= 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerProcessesArchiveJob(t *testing.T) {
	worker := ArchiveWorker{handle: func(job ArchiveJob) (archive.Result, error) {
		if job.RequestID != "request_1" {
			t.Fatalf("job RequestID = %q, want request_1", job.RequestID)
		}
		return archive.Result{Metadata: archive.Metadata{ArchiveID: job.ArchiveID}}, nil
	}}
	queue := &fakeArchiveQueue{jobs: []ArchiveJob{{RequestID: "request_1", ArchiveID: "archive_1"}}}
	runner := NewRunner(Options{Concurrency: 1, ArchiveWorker: &worker, ArchiveQueue: queue, PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	waitFor(t, func() bool { return queue.completedCount() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if failed := queue.failedCount(); failed != 0 {
		t.Fatalf("failed jobs = %d, want 0", failed)
	}
}

func TestRunnerFailsArchiveJobWhenWorkerReturnsError(t *testing.T) {
	workerErr := errors.New("archive failed")
	worker := ArchiveWorker{handle: func(job ArchiveJob) (archive.Result, error) {
		return archive.Result{}, workerErr
	}}
	queue := &fakeArchiveQueue{jobs: []ArchiveJob{{RequestID: "request_1", ArchiveID: "archive_1"}}}
	runner := NewRunner(Options{Concurrency: 1, ArchiveWorker: &worker, ArchiveQueue: queue, PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	waitFor(t, func() bool { return queue.failedCount() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if completed := queue.completedCount(); completed != 0 {
		t.Fatalf("completed jobs = %d, want 0", completed)
	}
	if err := queue.failedErr(0); !errors.Is(err, workerErr) {
		t.Fatalf("failed err = %v, want %v", err, workerErr)
	}
}

func TestRunnerPollsArchiveQueueUntilContextCanceled(t *testing.T) {
	worker := ArchiveWorker{handle: func(job ArchiveJob) (archive.Result, error) {
		t.Fatal("worker should not be called when queue has no job")
		return archive.Result{}, nil
	}}
	queue := &fakeArchiveQueue{}
	runner := NewRunner(Options{Concurrency: 1, ArchiveWorker: &worker, ArchiveQueue: queue, PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- runner.Run(ctx)
	}()

	waitFor(t, func() bool { return queue.leaseCount() >= 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerProcessesRAGIndexJob(t *testing.T) {
	worker := &RAGIndexWorker{handle: func(job RAGIndexJob) (RAGIndexResult, error) {
		if job.IdempotencyKey != "rag_1" {
			t.Fatalf("job IdempotencyKey = %q, want rag_1", job.IdempotencyKey)
		}
		return RAGIndexResult{}, nil
	}}
	queue := &fakeRAGIndexQueueForRunner{jobs: []RAGIndexJob{{IdempotencyKey: "rag_1"}}}
	runner := NewRunner(Options{Concurrency: 1, RAGIndexWorker: worker, RAGIndexQueue: queue, PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	waitFor(t, func() bool { return queue.completedCount() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if failed := queue.failedCount(); failed != 0 {
		t.Fatalf("failed jobs = %d, want 0", failed)
	}
}

type fakeArchiveQueue struct {
	mu        sync.Mutex
	jobs      []ArchiveJob
	leases    int
	completed []ArchiveJob
	failed    []failedArchiveJob
}

type failedArchiveJob struct {
	job ArchiveJob
	err error
}

type fakeAutoMaintenance struct {
	mu   sync.Mutex
	runs int
}

type fakeHotMemoryMaintenance struct {
	mu            sync.Mutex
	runs          int
	errOnFirstRun error
}

func (m *fakeAutoMaintenance) RunAutoClean(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs++
	return 1, nil
}

func (m *fakeAutoMaintenance) runCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs
}

func (m *fakeHotMemoryMaintenance) RunAutoOrganize(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs++
	if m.runs == 1 && m.errOnFirstRun != nil {
		return 0, m.errOnFirstRun
	}
	return 1, nil
}

func (m *fakeHotMemoryMaintenance) runCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs
}

func (q *fakeArchiveQueue) Lease(ctx context.Context) (ArchiveJob, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.leases++
	if len(q.jobs) == 0 {
		return ArchiveJob{}, false, nil
	}
	job := q.jobs[0]
	q.jobs = q.jobs[1:]
	return job, true, nil
}

func (q *fakeArchiveQueue) Complete(ctx context.Context, job ArchiveJob, result archive.Result) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, job)
	return nil
}

func (q *fakeArchiveQueue) Fail(ctx context.Context, job ArchiveJob, err error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, failedArchiveJob{job: job, err: err})
	return nil
}

func (q *fakeArchiveQueue) leaseCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.leases
}

func (q *fakeArchiveQueue) completedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.completed)
}

func (q *fakeArchiveQueue) failedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.failed)
}

func (q *fakeArchiveQueue) failedErr(index int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.failed[index].err
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(200 * time.Millisecond)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("condition was not met before timeout")
		case <-ticker.C:
		}
	}
}

type fakeRAGIndexQueueForRunner struct {
	mu        sync.Mutex
	jobs      []RAGIndexJob
	completed []RAGIndexJob
	failed    []RAGIndexJob
}

func (q *fakeRAGIndexQueueForRunner) Enqueue(ctx context.Context, job RAGIndexJob) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, job)
	return nil
}

func (q *fakeRAGIndexQueueForRunner) Lease(ctx context.Context) (RAGIndexJob, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.jobs) == 0 {
		return RAGIndexJob{}, false, nil
	}
	job := q.jobs[0]
	q.jobs = q.jobs[1:]
	return job, true, nil
}

func (q *fakeRAGIndexQueueForRunner) Complete(ctx context.Context, job RAGIndexJob, result RAGIndexResult) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, job)
	return nil
}

func (q *fakeRAGIndexQueueForRunner) Fail(ctx context.Context, job RAGIndexJob, err error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, job)
	return nil
}

func (q *fakeRAGIndexQueueForRunner) completedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.completed)
}

func (q *fakeRAGIndexQueueForRunner) failedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.failed)
}
