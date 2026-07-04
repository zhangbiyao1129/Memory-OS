package main

import (
	"context"
	"fmt"
	"os"

	"memory-os/internal/mcpproxy"
	"memory-os/internal/mcpstdio"
)

func main() {
	server := mcpstdio.NewServer(mcpproxy.New(mcpproxy.Config{
		MCPURL:  envOrDefault("MEMORY_OS_MCP_URL", "http://127.0.0.1:18082"),
		Token:   os.Getenv("MEMORY_OS_TOKEN"),
		AgentID: envOrDefault("MEMORY_OS_AGENT_ID", "mcp"),
	}))
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
