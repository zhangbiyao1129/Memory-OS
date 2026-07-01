package main

import "testing"

func TestBuildServer(t *testing.T) {
	server := buildServer(":18082")
	if server == nil {
		t.Fatal("buildServer() returned nil")
	}
}

func TestBuildServerRejectsMissingAddr(t *testing.T) {
	server := buildServer("")
	if server != nil {
		t.Fatal("buildServer() returned server, want nil")
	}
}
