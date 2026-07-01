package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewWritesJSON(t *testing.T) {
	var output bytes.Buffer

	log, err := New(Options{
		Environment: "test",
		Service:     "memory-api",
		Output:      &output,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	log.Info("health check", zap.String("component", "api"))

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not JSON: %v", err)
	}
	if entry["service"] != "memory-api" {
		t.Fatalf("service = %v, want memory-api", entry["service"])
	}
	if entry["component"] != "api" {
		t.Fatalf("component = %v, want api", entry["component"])
	}
}

func TestNewRejectsMissingService(t *testing.T) {
	_, err := New(Options{Environment: "test"})
	if err == nil {
		t.Fatal("New() error = nil, want missing service error")
	}
}

func TestRedactFieldMasksSensitiveValue(t *testing.T) {
	field := RedactField("api_key", "sk-test-redacted-example")

	if field.Key != "api_key" {
		t.Fatalf("field key = %q, want api_key", field.Key)
	}
	if strings.Contains(field.String, "sk-test-redacted-example") {
		t.Fatal("RedactField leaked the original value")
	}
	if field.String != "[REDACTED]" {
		t.Fatalf("redacted value = %q, want [REDACTED]", field.String)
	}
}
