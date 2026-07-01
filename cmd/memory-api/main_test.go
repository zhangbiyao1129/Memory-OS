package main

import (
	"testing"

	"memory-os/internal/config"
)

func TestBuildServer(t *testing.T) {
	cfg := config.Config{APIAddr: ":18081", RedisAddr: "", QdrantURL: ""}

	server, err := buildServer(cfg)
	if err != nil {
		t.Fatalf("buildServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("buildServer() returned nil server")
	}
}

func TestBuildServerRejectsMissingAPIAddr(t *testing.T) {
	_, err := buildServer(config.Config{})
	if err == nil {
		t.Fatal("buildServer() error = nil, want missing addr error")
	}
}
