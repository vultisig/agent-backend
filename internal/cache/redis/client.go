package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps the Redis client.
type Client struct {
	rdb *redis.Client
}

// New creates a new Redis client from a URI.
func New(uri string) (*Client, error) {
	opt, err := redis.ParseURL(uri)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(opt)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &Client{rdb: rdb}, nil
}

// Get retrieves a value by key.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.rdb.Get(ctx, key).Result()
}

// Set stores a value with an optional TTL.
func (c *Client) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// Delete removes a key.
func (c *Client) Delete(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}
