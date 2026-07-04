package eventlog

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGRepositoryRequiresPool(t *testing.T) {
	repo := NewPGRepository(nil)
	event := validEvent()
	sanitized, err := Sanitize(event, SanitizerOptions{})
	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}

	if _, err := repo.Save(sanitized, "request_missing_pool"); err == nil {
		t.Fatal("Save() error = nil, want missing pool error")
	}
	if repo.Count() != 0 {
		t.Fatal("Count() with missing pool should return 0")
	}
}

func TestPGRepositorySavesSafePayloadAndDedupesRequest(t *testing.T) {
	pool := eventlogTestPool(t)
	repo := NewPGRepository(pool)
	event := validEvent()
	event.EventID = "event_pg_" + eventlogSuffix()
	event.Payload = map[string]any{"text": "token sk-test-redacted-example"}
	sanitized, err := Sanitize(event, SanitizerOptions{})
	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}

	result, err := repo.Save(sanitized, "request_"+event.EventID)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if result.Deduped {
		t.Fatal("Save() deduped = true, want false")
	}

	duplicate, err := repo.Save(sanitized, "request_"+event.EventID)
	if err != nil {
		t.Fatalf("Save() duplicate error = %v", err)
	}
	if !duplicate.Deduped || duplicate.EventID != event.EventID {
		t.Fatalf("duplicate result = %#v, want same event deduped", duplicate)
	}

	var payload string
	var hash string
	var warnings []string
	if err := pool.QueryRow(context.Background(), `SELECT payload::text, safe_payload_hash, warnings FROM turn_event_payloads WHERE event_id = $1`, event.EventID).Scan(&payload, &hash, &warnings); err != nil {
		t.Fatalf("select payload: %v", err)
	}
	if containsPlainSecret(payload) {
		t.Fatalf("payload leaked secret: %s", payload)
	}
	if hash != sanitized.PayloadHash {
		t.Fatalf("safe_payload_hash = %q, want %q", hash, sanitized.PayloadHash)
	}
	if len(warnings) == 0 {
		t.Fatal("warnings empty, want sanitizer warnings persisted")
	}
	assertEventLogRowCount(t, pool, "event_ingest_requests", event.EventID, 1)
}

func TestPGRepositoryStoresEmptyWarningsArray(t *testing.T) {
	pool := eventlogTestPool(t)
	repo := NewPGRepository(pool)
	event := validEvent()
	event.EventID = "event_pg_empty_warnings_" + eventlogSuffix()
	event.Payload = map[string]any{"text": "plain production event"}
	sanitized, err := Sanitize(event, SanitizerOptions{})
	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}
	if len(sanitized.Event.Warnings) != 0 {
		t.Fatalf("sanitized warnings = %v, want empty", sanitized.Event.Warnings)
	}

	if _, err := repo.Save(sanitized, "request_"+event.EventID); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var warnings []string
	if err := pool.QueryRow(context.Background(), `SELECT warnings FROM turn_event_payloads WHERE event_id = $1`, event.EventID).Scan(&warnings); err != nil {
		t.Fatalf("select warnings: %v", err)
	}
	if warnings == nil {
		t.Fatal("warnings = nil, want empty array")
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings length = %d, want 0", len(warnings))
	}
}

func eventlogTestPool(t *testing.T) *pgxpool.Pool {
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

func assertEventLogRowCount(t *testing.T, pool *pgxpool.Pool, tableName, eventID string, want int) {
	t.Helper()
	var count int
	query := "SELECT count(*) FROM " + tableName + " WHERE event_id = $1"
	if err := pool.QueryRow(context.Background(), query, eventID).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if count != want {
		t.Fatalf("%s rows = %d, want %d", tableName, count, want)
	}
}

func containsPlainSecret(text string) bool {
	return strings.Contains(text, "sk-test-redacted-example")
}

func eventlogSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
