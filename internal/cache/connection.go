package cache

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"archio/internal/config"
	"github.com/redis/go-redis/v9"
)

// Connection owns the Redis client lifecycle.
type Connection struct {
	client *redis.Client

	closeOnce sync.Once
	closeErr  error
}

// Open creates a Redis client and verifies it with PING before returning.
func Open(ctx context.Context, cfg config.RedisConfig) (*Connection, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.Database,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	return &Connection{client: client}, nil
}

// Client returns the Redis client for cache modules.
func (c *Connection) Client() *redis.Client {
	return c.client
}

// Ping reports whether Redis is reachable within the caller's deadline.
func (c *Connection) Ping(ctx context.Context) error {
	if c == nil || c.client == nil {
		return errors.New("redis connection is not initialized")
	}
	return c.client.Ping(ctx).Err()
}

// Close releases the Redis client once.
func (c *Connection) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closeErr = c.client.Close()
	})
	return c.closeErr
}
