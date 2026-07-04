package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/eventlog"
)

func TestPGArchiveQueueRequiresPool(t *testing.T) {
	queue := NewPGArchiveQueue(nil, PGArchiveQueueOptions{})
	job := archiveQueueJob("archive_missing_pool")

	if err := queue.Enqueue(context.Background(), job); err == nil {
		t.Fatal("Enqueue() error = nil, want missing pool error")
	}
	if _, _, err := queue.Lease(context.Background()); err == nil {
		t.Fatal("Lease() error = nil, want missing pool error")
	}
	if err := queue.Complete(context.Background(), job, archiveQueueResult(job.ArchiveID)); err == nil {
		t.Fatal("Complete() error = nil, want missing pool error")
	}
	if err := queue.Fail(context.Background(), job, errors.New("boom")); err == nil {
		t.Fatal("Fail() error = nil, want missing pool error")
	}
}

func TestPGArchiveQueueEnqueueLeasesCompletesAndDedupesRequest(t *testing.T) {
	pool := jobsTestPool(t)
	queue := NewPGArchiveQueue(pool, PGArchiveQueueOptions{WorkerID: "worker_1", LeaseDuration: time.Minute})
	job := archiveQueueJob("archive_queue_" + jobsSuffix())
	insertTurnEvents(t, pool, job.Events)

	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() duplicate error = %v", err)
	}
	assertArchiveJobCount(t, pool, job.RequestID, 1)

	leased, ok, err := queue.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease() error = %v", err)
	}
	if !ok {
		t.Fatal("Lease() ok = false, want true")
	}
	if leased.RequestID != job.RequestID || leased.ArchiveID != job.ArchiveID || len(leased.Events) != len(job.Events) {
		t.Fatalf("leased job mismatch: %#v", leased)
	}
	if leased.Events[0].Payload["text"] != "deploy api" {
		t.Fatalf("leased payload = %#v, want safe event payload", leased.Events[0].Payload)
	}

	if err := queue.Complete(context.Background(), leased, archiveQueueResult(leased.ArchiveID)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	assertArchiveJobStatus(t, pool, job.RequestID, "completed")

	_, ok, err = queue.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease() after complete error = %v", err)
	}
	if ok {
		t.Fatal("Lease() after complete ok = true, want false")
	}
}

func TestPGArchiveQueueFailRequeuesUntilMaxAttempts(t *testing.T) {
	pool := jobsTestPool(t)
	queue := NewPGArchiveQueue(pool, PGArchiveQueueOptions{WorkerID: "worker_1", LeaseDuration: time.Minute, MaxAttempts: 2})
	job := archiveQueueJob("archive_retry_" + jobsSuffix())
	insertTurnEvents(t, pool, job.Events)
	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first, ok, err := queue.Lease(context.Background())
	if err != nil || !ok {
		t.Fatalf("first Lease() job=%#v ok=%v err=%v", first, ok, err)
	}
	if err := queue.Fail(context.Background(), first, errors.New("temporary")); err != nil {
		t.Fatalf("first Fail() error = %v", err)
	}
	assertArchiveJobStatus(t, pool, job.RequestID, "pending")

	second, ok, err := queue.Lease(context.Background())
	if err != nil || !ok {
		t.Fatalf("second Lease() job=%#v ok=%v err=%v", second, ok, err)
	}
	if err := queue.Fail(context.Background(), second, errors.New("permanent")); err != nil {
		t.Fatalf("second Fail() error = %v", err)
	}
	assertArchiveJobStatus(t, pool, job.RequestID, "failed")

	_, ok, err = queue.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease() after max attempts error = %v", err)
	}
	if ok {
		t.Fatal("Lease() after max attempts ok = true, want false")
	}
}

func jobsTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func archiveQueueJob(archiveID string) ArchiveJob {
	now := time.Now().UTC()
	userID := "user_" + jobsSuffix()
	orgID := "org_" + jobsSuffix()
	projectID := "project_" + jobsSuffix()
	return ArchiveJob{
		RequestID: "request_" + archiveID,
		ArchiveID: archiveID,
		Title:     "Archive " + archiveID,
		UserID:    userID,
		OrgID:     orgID,
		ProjectID: projectID,
		CreatedAt: now,
		Events: []eventlog.TurnEvent{{
			Version:   "v1",
			EventID:   "event_" + archiveID + "_" + jobsSuffix(),
			TurnID:    "turn_" + jobsSuffix(),
			ThreadID:  "thread_" + jobsSuffix(),
			SessionID: "session_" + jobsSuffix(),
			Type:      eventlog.EventUserMessage,
			CreatedAt: now,
			Actor:     eventlog.Actor{UserID: userID, OrgID: orgID, ProjectID: projectID, AgentID: "codex"},
			Source:    eventlog.Source{Platform: "codex", Host: "thinkpad"},
			Payload:   map[string]any{"text": "deploy api"},
			Warnings:  []string{"safe"},
		}},
	}
}

func insertTurnEvents(t *testing.T, pool *pgxpool.Pool, events []eventlog.TurnEvent) {
	t.Helper()
	for _, event := range events {
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		_, err = pool.Exec(context.Background(), `
INSERT INTO turn_events (event_id, turn_id, thread_id, session_id, event_type, user_id, org_id, project_id, agent_id, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (event_id) DO NOTHING`,
			event.EventID, event.TurnID, event.ThreadID, event.SessionID, string(event.Type), event.Actor.UserID, event.Actor.OrgID, event.Actor.ProjectID, event.Actor.AgentID, event.CreatedAt)
		if err != nil {
			t.Fatalf("insert turn event: %v", err)
		}
		_, err = pool.Exec(context.Background(), `
INSERT INTO turn_event_payloads (event_id, payload, safe_payload_hash, original_bytes, safe_bytes, truncated, warnings)
VALUES ($1,$2::jsonb,$3,$4,$5,false,$6)
ON CONFLICT (event_id) DO NOTHING`,
			event.EventID, string(payload), "hash_"+event.EventID, len(payload), len(payload), event.Warnings)
		if err != nil {
			t.Fatalf("insert turn event payload: %v", err)
		}
	}
}

func assertArchiveJobCount(t *testing.T, pool *pgxpool.Pool, requestID string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM archive_jobs WHERE request_id = $1`, requestID).Scan(&count); err != nil {
		t.Fatalf("count archive_jobs: %v", err)
	}
	if count != want {
		t.Fatalf("archive_jobs count = %d, want %d", count, want)
	}
}

func assertArchiveJobStatus(t *testing.T, pool *pgxpool.Pool, requestID, want string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM archive_jobs WHERE request_id = $1`, requestID).Scan(&status); err != nil {
		t.Fatalf("select archive job status: %v", err)
	}
	if status != want {
		t.Fatalf("archive job status = %q, want %q", status, want)
	}
}

func archiveQueueResult(archiveID string) archive.Result {
	return archive.Result{Metadata: archive.Metadata{ArchiveID: archiveID}}
}

func jobsSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
