package main

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app/server"

	"memory-os/internal/config"
	"memory-os/internal/db"
	"memory-os/internal/health"
	httpapi "memory-os/internal/http"
	"memory-os/internal/logger"
	"memory-os/internal/qdrant"
	"memory-os/internal/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	log, err := logger.New(logger.Options{Environment: "development", Service: "memory-api"})
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	h, err := buildServer(cfg)
	if err != nil {
		panic(err)
	}

	log.Info("memory-api starting")
	h.Spin()
}

func buildServer(cfg config.Config) (*server.Hertz, error) {
	if cfg.APIAddr == "" {
		return nil, errors.New("api addr is required")
	}

	checkers := map[string]health.Checker{}
	if cfg.PostgresDSN != "" {
		pool, err := db.NewPool(context.Background(), cfg.PostgresDSN)
		if err != nil {
			return nil, err
		}
		checkers["db"] = db.Checker{Pool: pool}
	}
	if cfg.RedisAddr != "" {
		client, err := redis.NewClient(cfg.RedisAddr)
		if err != nil {
			return nil, err
		}
		checkers["redis"] = redis.Checker{Client: client}
	}
	if cfg.QdrantURL != "" {
		client, err := qdrant.NewClient(cfg.QdrantURL)
		if err != nil {
			return nil, err
		}
		checkers["qdrant"] = qdrant.Checker{Client: client}
	}

	healthService := health.NewService(checkers)
	h := server.New(server.WithHostPorts(cfg.APIAddr))
	httpapi.RegisterRoutes(h.Engine, healthService, cfg.AppEnv)
	return h, nil
}
