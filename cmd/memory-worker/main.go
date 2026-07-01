package main

import (
	"context"
	"os/signal"
	"syscall"

	"memory-os/internal/config"
	"memory-os/internal/jobs"
	"memory-os/internal/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	log, err := logger.New(logger.Options{Environment: "development", Service: "memory-worker"})
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	worker, err := buildWorker(cfg)
	if err != nil {
		panic(err)
	}

	log.Info("memory-worker starting")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := worker.Run(ctx); err != nil {
		panic(err)
	}
}

func buildWorker(cfg config.Config) (*jobs.Runner, error) {
	runner := jobs.NewRunner(jobs.Options{Concurrency: 1})
	return &runner, nil
}
