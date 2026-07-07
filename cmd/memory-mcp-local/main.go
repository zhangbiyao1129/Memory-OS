package main

import (
	"context"
	"fmt"
	"os"

	"memory-os/internal/mcpproxy"
	"memory-os/internal/mcpstdio"
	"memory-os/internal/secretlocal"
)

func main() {
	token := os.Getenv("MEMORY_OS_TOKEN")
	server := mcpstdio.NewServer(mcpproxy.New(mcpproxy.Config{
		MCPURL:  envOrDefault("MEMORY_OS_MCP_URL", "http://127.0.0.1:18082"),
		Token:   token,
		AgentID: envOrDefault("MEMORY_OS_AGENT_ID", "mcp"),
	}))

	// 本机 secret 工具：明文只在本机加解密，密钥文件默认 ~/.config/memory-os/secret-device-key.json。
	keyPath := os.Getenv("MEMORY_OS_SECRET_KEY_PATH")
	if keyPath == "" {
		if resolved, err := secretlocal.DefaultKeyPath(); err == nil {
			keyPath = resolved
		}
	}
	secretTools := secretlocal.NewToolHandler(secretlocal.ToolHandlerConfig{
		KeyPath: keyPath,
		Client:  secretlocal.NewClient(envOrDefault("MEMORY_OS_API_URL", "http://127.0.0.1:18081"), token, nil),
	})
	server = server.WithSecretTools(secretTools)

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
