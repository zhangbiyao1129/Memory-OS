package db

import (
	"context"
	"strings"
	"testing"
)

func TestNewPoolRejectsEmptyDSN(t *testing.T) {
	_, err := NewPool(context.Background(), "")
	if err == nil {
		t.Fatal("NewPool() error = nil, want missing dsn error")
	}
}

func TestNewPoolRedactsPasswordInParseError(t *testing.T) {
	dsn := "postgres://memory_user:secret-password@[invalid-host/memory_os"

	_, err := NewPool(context.Background(), dsn)

	if err == nil {
		t.Fatal("NewPool() error = nil, want parse error")
	}
	if strings.Contains(err.Error(), "secret-password") {
		t.Fatalf("error leaked password: %v", err)
	}
}

func TestCheckerReturnsErrorWhenPoolIsNil(t *testing.T) {
	checker := Checker{}

	err := checker.Check(context.Background())

	if err == nil {
		t.Fatal("Checker.Check() error = nil, want missing pool error")
	}
}
