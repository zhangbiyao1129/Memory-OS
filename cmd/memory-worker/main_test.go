package main

import (
	"testing"

	"memory-os/internal/config"
)

func TestBuildWorker(t *testing.T) {
	worker, err := buildWorker(config.Config{})
	if err != nil {
		t.Fatalf("buildWorker() error = %v", err)
	}
	if worker == nil {
		t.Fatal("buildWorker() returned nil worker")
	}
}
