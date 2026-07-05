package jobs

import (
	"context"
	"errors"
	"time"

	"memory-os/internal/candidatememory"
)

var errCandidateQueueNotConfigured = errors.New("candidate memory queue not configured")

// CandidateMemoryQueue 候选提炼任务的 worker 侧队列接口(Lease/Complete/Fail),
// 同时其 Enqueue 满足 http.candidateEnqueuer,供 router 入队。
type CandidateMemoryQueue interface {
	Enqueue(ctx context.Context, job candidatememory.Job) error
	Lease(ctx context.Context) (candidatememory.Job, bool, error)
	Complete(ctx context.Context, job candidatememory.Job, result CandidateMemoryJobResult) error
	Fail(ctx context.Context, job candidatememory.Job, err error) error
}

type PGCandidateMemoryQueueOptions struct {
	WorkerID string
	LockTTL  time.Duration
}

// PGCandidateMemoryQueue 基于 candidatememory.PGRepository 的队列实现。
// 幂等由 candidate_memory_jobs.idempotency_key 唯一约束保证。
type PGCandidateMemoryQueue struct {
	repo     *candidatememory.PGRepository
	workerID string
	lockTTL  time.Duration
}

func NewPGCandidateMemoryQueue(repo *candidatememory.PGRepository, opts PGCandidateMemoryQueueOptions) *PGCandidateMemoryQueue {
	if opts.LockTTL <= 0 {
		opts.LockTTL = time.Minute
	}
	return &PGCandidateMemoryQueue{repo: repo, workerID: opts.WorkerID, lockTTL: opts.LockTTL}
}

func (q *PGCandidateMemoryQueue) Enqueue(ctx context.Context, job candidatememory.Job) error {
	if q.repo == nil {
		return errCandidateQueueNotConfigured
	}
	_, err := q.repo.UpsertJob(ctx, job)
	return err
}

func (q *PGCandidateMemoryQueue) Lease(ctx context.Context) (candidatememory.Job, bool, error) {
	if q.repo == nil {
		return candidatememory.Job{}, false, errCandidateQueueNotConfigured
	}
	j, err := q.repo.LeaseJob(ctx, time.Now().UTC(), q.workerID, q.lockTTL)
	if err != nil {
		return candidatememory.Job{}, false, err
	}
	if j == nil {
		return candidatememory.Job{}, false, nil
	}
	return *j, true, nil
}

func (q *PGCandidateMemoryQueue) Complete(ctx context.Context, job candidatememory.Job, result CandidateMemoryJobResult) error {
	if q.repo == nil {
		return errCandidateQueueNotConfigured
	}
	return q.repo.CompleteJob(ctx, job.ID, result.CandidateIDs)
}

func (q *PGCandidateMemoryQueue) Fail(ctx context.Context, job candidatememory.Job, err error) error {
	if q.repo == nil {
		return errCandidateQueueNotConfigured
	}
	return q.repo.FailJob(ctx, job.ID, err.Error())
}
