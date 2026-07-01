package qdrant

import (
	"context"
	"testing"
)

func TestNewClientRejectsEmptyBaseURL(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("NewClient() error = nil, want missing url error")
	}
}

func TestNewClientAcceptsBaseURL(t *testing.T) {
	client, err := NewClient("http://localhost:18083")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil client")
	}
}

func TestCheckerReturnsErrorWhenClientIsNil(t *testing.T) {
	checker := Checker{}

	err := checker.Check(context.Background())

	if err == nil {
		t.Fatal("Checker.Check() error = nil, want missing client error")
	}
}
