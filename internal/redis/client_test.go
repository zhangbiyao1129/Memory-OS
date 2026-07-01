package redis

import (
	"context"
	"testing"
)

func TestNewClientRejectsEmptyAddr(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("NewClient() error = nil, want missing addr error")
	}
}

func TestNewClientAcceptsAddr(t *testing.T) {
	client, err := NewClient("localhost:6379")
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
