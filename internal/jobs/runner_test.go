package jobs

import (
	"context"
	"testing"
	"time"
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
