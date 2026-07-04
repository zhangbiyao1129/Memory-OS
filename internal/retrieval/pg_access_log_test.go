package retrieval

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"memory-os/internal/hotmemory"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGAccessLogRequiresPool(t *testing.T) {
	log := NewPGAccessLog(nil)
	request := retrievalPGRequest("request_missing_pool")

	if err := log.LogRequest(request, false); err == nil {
		t.Fatal("LogRequest() error = nil, want missing pool error")
	}
	if err := log.LogResult(request.RequestID, 1, SearchResult{Source: SourceRef{Kind: SourceHotMemory, MemoryID: "hm_1"}}); err == nil {
		t.Fatal("LogResult() error = nil, want missing pool error")
	}
	if _, err := log.ListRequests(AccessLogListFilter{OrgID: "org_1", ProjectID: "project_1"}); err == nil {
		t.Fatal("ListRequests() error = nil, want missing pool error")
	}
	if _, err := log.ListResults(AccessLogListFilter{OrgID: "org_1", ProjectID: "project_1"}); err == nil {
		t.Fatal("ListResults() error = nil, want missing pool error")
	}
}

func TestPGAccessLogPersistsRequestAndDedupesRequestID(t *testing.T) {
	pool := retrievalTestPool(t)
	log := NewPGAccessLog(pool)
	request := retrievalPGRequest("retrieval_request_" + retrievalSuffix())

	if err := log.LogRequest(request, true); err != nil {
		t.Fatalf("LogRequest() error = %v", err)
	}
	if err := log.LogRequest(request, false); err != nil {
		t.Fatalf("LogRequest() duplicate error = %v", err)
	}

	var count int
	var degraded bool
	if err := pool.QueryRow(context.Background(), `
SELECT count(*), bool_or(rerank_degraded)
FROM retrieval_requests
WHERE request_id = $1`, request.RequestID).Scan(&count, &degraded); err != nil {
		t.Fatalf("select retrieval request: %v", err)
	}
	if count != 1 {
		t.Fatalf("retrieval_requests rows = %d, want 1", count)
	}
	if !degraded {
		t.Fatal("rerank_degraded = false, want true preserved after duplicate")
	}

	requests, err := log.ListRequests(AccessLogListFilter{OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}
	if len(requests) != 1 || requests[0].RequestID != request.RequestID || requests[0].QueryHash == "" || !requests[0].RerankDegraded {
		t.Fatalf("ListRequests() = %#v, want saved degraded request", requests)
	}
}

func TestPGAccessLogPersistsResultAndAccessLogIdempotently(t *testing.T) {
	pool := retrievalTestPool(t)
	log := NewPGAccessLog(pool)
	request := retrievalPGRequest("retrieval_result_" + retrievalSuffix())
	result := SearchResult{
		Text:  "Project deploy note",
		Score: 0.88,
		Source: SourceRef{
			Kind:           SourceArchiveChunk,
			ArchiveID:      "archive_" + retrievalSuffix(),
			ChunkID:        "chunk_" + retrievalSuffix(),
			SourceEventIDs: []string{"event_" + retrievalSuffix()},
		},
	}

	if err := log.LogRequest(request, false); err != nil {
		t.Fatalf("LogRequest() error = %v", err)
	}
	if err := log.LogResult(request.RequestID, 1, result); err != nil {
		t.Fatalf("LogResult() error = %v", err)
	}
	if err := log.LogResult(request.RequestID, 1, result); err != nil {
		t.Fatalf("LogResult() duplicate error = %v", err)
	}

	var resultCount int
	var sourceKind string
	var sourceRefBytes []byte
	if err := pool.QueryRow(context.Background(), `
SELECT count(*), max(source_kind), max(source_ref::text)::bytea
FROM retrieval_results
WHERE request_id = $1`, request.RequestID).Scan(&resultCount, &sourceKind, &sourceRefBytes); err != nil {
		t.Fatalf("select retrieval result: %v", err)
	}
	if resultCount != 1 {
		t.Fatalf("retrieval_results rows = %d, want 1", resultCount)
	}
	if sourceKind != string(SourceArchiveChunk) {
		t.Fatalf("source_kind = %q, want archive_chunk", sourceKind)
	}
	var sourceRef map[string]any
	if err := json.Unmarshal(sourceRefBytes, &sourceRef); err != nil {
		t.Fatalf("unmarshal source_ref: %v", err)
	}
	if sourceRef["archive_id"] != result.Source.ArchiveID || sourceRef["chunk_id"] != result.Source.ChunkID {
		t.Fatalf("source_ref = %#v, want archive/chunk refs", sourceRef)
	}

	var accessCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM memory_access_logs WHERE request_id = $1`, request.RequestID).Scan(&accessCount); err != nil {
		t.Fatalf("select access log count: %v", err)
	}
	if accessCount != 1 {
		t.Fatalf("memory_access_logs rows = %d, want 1", accessCount)
	}

	results, err := log.ListResults(AccessLogListFilter{OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListResults() error = %v", err)
	}
	if len(results) != 1 || results[0].RequestID != request.RequestID || results[0].SourceKind != string(SourceArchiveChunk) {
		t.Fatalf("ListResults() = %#v, want saved result", results)
	}
	if results[0].SourceRef["archive_id"] != result.Source.ArchiveID || results[0].SourceRef["chunk_id"] != result.Source.ChunkID {
		t.Fatalf("ListResults() source ref = %#v, want archive/chunk refs", results[0].SourceRef)
	}
}

func retrievalTestPool(t *testing.T) *pgxpool.Pool {
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

func retrievalPGRequest(requestID string) SearchRequest {
	suffix := retrievalSuffix()
	return SearchRequest{
		RequestID:              requestID,
		Query:                  "deploy api " + suffix,
		Actor:                  Actor{UserID: "user_" + suffix, OrgID: "org_" + suffix, ProjectID: "project_" + suffix, AgentID: "codex"},
		Scope:                  hotmemory.ScopeProject,
		Visibility:             "project",
		PermissionLabels:       []string{"project:project_" + suffix + ":read"},
		ArchiveIndexGeneration: 1,
		MaxContextBytes:        512,
	}
}

func retrievalSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
