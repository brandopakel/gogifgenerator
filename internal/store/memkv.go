package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// MemKV connects GoGIF to brandopakel/memkv over its RESP service boundary.
// Protocol 2 and disabled client identity avoid commands MemKV does not expose.
type MemKV struct {
	client *redis.Client
}

func NewMemKV(address string) (*MemKV, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("memkv address is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:                  address,
		Protocol:              2,
		DisableIdentity:       true,
		ContextTimeoutEnabled: true,
		MaxRetries:            2,
		PoolSize:              16,
	})
	return &MemKV{client: client}, nil
}

func (m *MemKV) Ping(ctx context.Context) error {
	if err := m.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping memkv: %w", err)
	}
	return nil
}

func (m *MemKV) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := m.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get memkv value: %w", err)
	}
	return value, nil
}

func (m *MemKV) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := m.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("put memkv value: %w", err)
	}
	return nil
}

func (m *MemKV) Delete(ctx context.Context, key string) error {
	if err := m.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("delete memkv value: %w", err)
	}
	return nil
}

func (m *MemKV) Close() error {
	return m.client.Close()
}
