package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryKVPutCopiesValues(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryKV()
	input := []byte("original")
	if err := kv.Put(ctx, "asset", input, 0); err != nil {
		t.Fatal(err)
	}
	input[0] = 'X'

	got, err := kv.Get(ctx, "asset")
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 'Y'
	again, err := kv.Get(ctx, "asset")
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != "original" {
		t.Fatalf("Get() = %q", again)
	}
}

func TestMemoryKVExpiresValues(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	kv := NewMemoryKV()
	kv.now = func() time.Time { return now }
	if err := kv.Put(ctx, "job", []byte("running"), time.Minute); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := kv.Get(ctx, "job"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryKVDeleteIsIdempotent(t *testing.T) {
	kv := NewMemoryKV()
	if err := kv.Delete(context.Background(), "missing"); err != nil {
		t.Fatal(err)
	}
}
