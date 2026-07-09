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

type AutoMaintenance interface {
	RunAutoClean(ctx context.Context) (int, error)
}

type HotMemoryMaintenance interface {
	RunAutoOrganize(ctx context.Context) (int, error)
}

// MemoryKernelMaintenance Memory Kernel 自动治理接口。
type MemoryKernelMaintenance interface {
	RunAutoGovernance(ctx context.Context) (int, error)
}

// Options 保存 worker 运行参数。
type Options struct {
	Concurrency                  int
	ArchiveWorker                *ArchiveWorker
	ArchiveQueue                 ArchiveQueue
	RAGIndexWorker               *RAGIndexWorker
	RAGIndexQueue                RAGIndexQueue
	CandidateWorker              *CandidateMemoryWorker
	CandidateQueue               CandidateMemoryQueue
	AutoMaintenance                   AutoMaintenance
	HotMemoryMaintenance              HotMemoryMaintenance
	MemoryKernelMaintenance           MemoryKernelMaintenance
	PollInterval                      time.Duration
	AutoMaintenanceInterval           time.Duration
	HotMemoryMaintenanceInterval      time.Duration
	MemoryKernelMaintenanceInterval   time.Duration
	Cleanup                           func()
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
	if r.options.CandidateWorker != nil && r.options.CandidateQueue != nil {
		loops = append(loops, r.runCandidateLoop)
	}
	if r.options.AutoMaintenance != nil {
		loops = append(loops, r.runAutoMaintenanceLoop)
	}
	if r.options.HotMemoryMaintenance != nil {
		loops = append(loops, r.runHotMemoryMaintenanceLoop)
	}
	if r.options.MemoryKernelMaintenance != nil {
		loops = append(loops, r.runMemoryKernelMaintenanceLoop)
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

func (r Runner) CandidateWorkerConfigured() bool {
	return r.options.CandidateWorker != nil
}

func (r Runner) CandidateQueueConfigured() bool {
	return r.options.CandidateQueue != nil
}

func (r Runner) AutoMaintenanceConfigured() bool {
	return r.options.AutoMaintenance != nil
}

func (r Runner) HotMemoryMaintenanceConfigured() bool {
	return r.options.HotMemoryMaintenance != nil
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

func (r Runner) runCandidateLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		job, ok, err := r.options.CandidateQueue.Lease(ctx)
		if err != nil {
			return err
		}
		if !ok {
			if err := waitForNextPoll(ctx, r.options.PollInterval); err != nil {
				return nil
			}
			continue
		}

		result, err := r.options.CandidateWorker.Handle(job)
		if err != nil {
			if failErr := r.options.CandidateQueue.Fail(ctx, job, err); failErr != nil {
				return failErr
			}
			continue
		}
		if err := r.options.CandidateQueue.Complete(ctx, job, result); err != nil {
			return err
		}
	}
}

func (r Runner) runAutoMaintenanceLoop(ctx context.Context) error {
	interval := r.options.AutoMaintenanceInterval
	if interval <= 0 {
		interval = r.options.PollInterval
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if _, err := r.options.AutoMaintenance.RunAutoClean(ctx); err != nil {
			return err
		}
		if err := waitForNextPoll(ctx, interval); err != nil {
			return nil
		}
	}
}

func (r Runner) runHotMemoryMaintenanceLoop(ctx context.Context) error {
	interval := r.options.HotMemoryMaintenanceInterval
	if interval <= 0 {
		interval = r.options.AutoMaintenanceInterval
	}
	if interval <= 0 {
		interval = r.options.PollInterval
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, _ = r.options.HotMemoryMaintenance.RunAutoOrganize(ctx)
		if err := waitForNextPoll(ctx, interval); err != nil {
			return nil
		}
	}
}

func (r Runner) runMemoryKernelMaintenanceLoop(ctx context.Context) error {
	interval := r.options.MemoryKernelMaintenanceInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, _ = r.options.MemoryKernelMaintenance.RunAutoGovernance(ctx)
		if err := waitForNextPoll(ctx, interval); err != nil {
			return nil
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
