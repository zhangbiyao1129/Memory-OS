package redis

import (
	"context"
	"errors"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

// NewClient 创建 Redis 客户端。
func NewClient(addr string) (*goredis.Client, error) {
	if addr == "" {
		return nil, errors.New("redis addr is required")
	}
	return goredis.NewClient(&goredis.Options{Addr: addr}), nil
}

// Checker 用于 health service 检查 Redis。
type Checker struct {
	Client *goredis.Client
}

func (c Checker) Check(ctx context.Context) error {
	if c.Client == nil {
		return errors.New("redis client is not configured")
	}
	if err := c.Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}
