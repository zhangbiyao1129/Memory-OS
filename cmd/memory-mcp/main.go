package main

import (
	"encoding/json"
	"net/http"

	"memory-os/internal/config"
	"memory-os/internal/logger"
	"memory-os/internal/mcp"
)

type Server struct {
	Addr  string
	Tools []mcp.Tool
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log, err := logger.New(logger.Options{Environment: "development", Service: "memory-mcp"})
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	server := buildServer(cfg.MCPAddr)
	if server == nil {
		panic("mcp addr is required")
	}
	log.Info("memory-mcp starting")
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func buildServer(addr string) *Server {
	if addr == "" {
		return nil
	}
	return &Server{Addr: addr, Tools: mcp.Tools()}
}

func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Tools)
	})
	return http.ListenAndServe(s.Addr, mux)
}
