package jobs

import (
	"context"
	"errors"
	"time"

	"memory-os/internal/archive"
)

const defaultPollInterval = time.Second

type ArchiveQueue interface {
	Lease(ctx context.Context) (ArchiveJob, bool, error)
	Complete(ctx context.Context, job ArchiveJob, result archive.Result) error
	Fail(ctx context.Context, job ArchiveJob, err error) error
}

type RAGIndexQueue interface {
	Enqueue(ctx context.Context, job RAGIndexJob) error
	Lease(ctx context.Context) (RAGIndexJob, bool, error)
	Complete(ctx context.Context, job RAGIndexJob, result RAGIndexResult) error
	Fail(ctx context.Context, job RAGIndexJob, err error) error
}

// Options 保存 worker 运行参数。
type Options struct {
	Concurrency    int
	ArchiveWorker  *ArchiveWorker
	ArchiveQueue   ArchiveQueue
	RAGIndexWorker *RAGIndexWorker
	RAGIndexQueue  RAGIndexQueue
	PollInterval   time.Duration
	Cleanup        func()
}

// Runner 是 Phase 1 的后台任务骨架，后续承载 archive/index/hotmemory jobs。
type Runner struct {
	options Options
}

func NewRunner(options Options) Runner {
	if options.Concurrency <= 0 {
		options.Concurrency = 1
	}
	if options.PollInterval <= 0 {
		options.PollInterval = defaultPollInterval
	}
	return Runner{options: options}
}

func NewRunnerChecked(options Options) (Runner, error) {
	if options.Concurrency <= 0 {
		return Runner{}, errors.New("worker concurrency must be positive")
	}
	if options.PollInterval <= 0 {
		options.PollInterval = defaultPollInterval
	}
	return Runner{options: options}, nil
}

func (r Runner) Run(ctx context.Context) error {
	if r.options.Cleanup != nil {
		defer r.options.Cleanup()
	}
	loops := []func(context.Context) error{}
	if r.options.ArchiveWorker != nil && r.options.ArchiveQueue != nil {
		loops = append(loops, r.runArchiveLoop)
	}
	if r.options.RAGIndexWorker != nil && r.options.RAGIndexQueue != nil {
		loops = append(loops, r.runRAGIndexLoop)
	}
	if len(loops) > 0 {
		return runLoops(ctx, loops)
	}
	<-ctx.Done()
	return nil
}

func (r Runner) ArchiveWorkerConfigured() bool {
	return r.options.ArchiveWorker != nil
}

func (r Runner) ArchiveQueueConfigured() bool {
	return r.options.ArchiveQueue != nil
}

func (r Runner) RAGIndexWorkerConfigured() bool {
	return r.options.RAGIndexWorker != nil
}

func (r Runner) RAGIndexQueueConfigured() bool {
	return r.options.RAGIndexQueue != nil
}

func (r Runner) runArchiveLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		job, ok, err := r.options.ArchiveQueue.Lease(ctx)
		if err != nil {
			return err
		}
		if !ok {
			if err := waitForNextPoll(ctx, r.options.PollInterval); err != nil {
				return nil
			}
			continue
		}

		result, err := r.options.ArchiveWorker.Handle(job)
		if err != nil {
			if failErr := r.options.ArchiveQueue.Fail(ctx, job, err); failErr != nil {
				return failErr
			}
			continue
		}
		if err := r.options.ArchiveQueue.Complete(ctx, job, result); err != nil {
			return err
		}
	}
}

func (r Runner) runRAGIndexLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		job, ok, err := r.options.RAGIndexQueue.Lease(ctx)
		if err != nil {
			return err
		}
		if !ok {
			if err := waitForNextPoll(ctx, r.options.PollInterval); err != nil {
				return nil
			}
			continue
		}

		result, err := r.options.RAGIndexWorker.Handle(job)
		if err != nil {
			if failErr := r.options.RAGIndexQueue.Fail(ctx, job, err); failErr != nil {
				return failErr
			}
			continue
		}
		if err := r.options.RAGIndexQueue.Complete(ctx, job, result); err != nil {
			return err
		}
	}
}

func runLoops(ctx context.Context, loops []func(context.Context) error) error {
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(loops))
	for _, loop := range loops {
		go func(loop func(context.Context) error) {
			errCh <- loop(loopCtx)
		}(loop)
	}
	for range loops {
		err := <-errCh
		if err != nil {
			cancel()
			return err
		}
	}
	return nil
}

func waitForNextPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
