package store

import (
	"context"
	"sync"
	"time"
)

type memoryValue struct {
	data      []byte
	expiresAt time.Time
}

// MemoryKV implements KV for tests and zero-configuration local use.
type MemoryKV struct {
	mu     sync.Mutex
	values map[string]memoryValue
	now    func() time.Time
}

func NewMemoryKV() *MemoryKV {
	return &MemoryKV{
		values: make(map[string]memoryValue),
		now:    time.Now,
	}
}

func (m *MemoryKV) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (m *MemoryKV) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.values[key]
	if !ok {
		return nil, ErrNotFound
	}
	if !value.expiresAt.IsZero() && !m.now().Before(value.expiresAt) {
		delete(m.values, key)
		return nil, ErrNotFound
	}
	return append([]byte(nil), value.data...), nil
}

func (m *MemoryKV) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry := memoryValue{data: append([]byte(nil), value...)}
	if ttl > 0 {
		entry.expiresAt = m.now().Add(ttl)
	}
	m.mu.Lock()
	m.values[key] = entry
	m.mu.Unlock()
	return nil
}

func (m *MemoryKV) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.values, key)
	m.mu.Unlock()
	return nil
}
