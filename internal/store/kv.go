// Package store defines the persistence boundaries used by GoGIF.
package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("store: value not found")
	ErrTooLarge = errors.New("store: value is too large")
)

// KV is the small string-value contract the media catalog needs. MemKV is the
// production-facing implementation; MemoryKV keeps tests and local development
// independent from an external process.
type KV interface {
	Ping(context.Context) error
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}
