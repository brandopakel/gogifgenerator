package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Run with GOGIF_TEST_MEMKV_ADDR=127.0.0.1:8081 against the develop branch of
// brandopakel/memkv. The regular test suite remains self-contained.
func TestMemKVIntegration(t *testing.T) {
	address := os.Getenv("GOGIF_TEST_MEMKV_ADDR")
	if address == "" {
		t.Skip("GOGIF_TEST_MEMKV_ADDR is not set")
	}
	kv, err := NewMemKV(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := kv.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	key := "gogif:test:memkv:roundtrip"
	if err := kv.Put(ctx, key, []byte("connected"), time.Second); err != nil {
		t.Fatal(err)
	}
	got, err := kv.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "connected" {
		t.Fatalf("Get() = %q", got)
	}
	if err := kv.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete error = %v, want ErrNotFound", err)
	}
}
