package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/config"
)

// NewPool 创建 PostgreSQL pgx pool。
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if dsn == "" {
		return nil, errors.New("postgres dsn is required")
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn %s failed", config.RedactDSN(dsn))
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("connect postgres %s: %w", config.RedactDSN(dsn), err)
	}

	return pool, nil
}

// Checker 用于 health service 检查 PostgreSQL。
type Checker struct {
	Pool *pgxpool.Pool
}

func (c Checker) Check(ctx context.Context) error {
	if c.Pool == nil {
		return errors.New("postgres pool is not configured")
	}
	if err := c.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping failed: %w", err)
	}
	return nil
}
