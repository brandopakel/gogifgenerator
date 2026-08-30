package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestFileBlobStoreRoundTripAndDeduplication(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(ctx, bytes.NewBufferString("animated bytes"), PutBlobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(ctx, bytes.NewBufferString("animated bytes"), PutBlobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("duplicate blobs differ: %#v != %#v", first, second)
	}

	reader, opened, err := store.Open(ctx, first.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "animated bytes" || opened != first {
		t.Fatalf("Open() = %q, %#v", data, opened)
	}
}

func TestFileBlobStoreRejectsLargeAndInvalidObjects(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, bytes.NewBufferString("too large"), PutBlobOptions{MaxBytes: 3}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put() error = %v, want ErrTooLarge", err)
	}
	if _, _, err := store.Open(ctx, "../../etc/passwd"); err == nil {
		t.Fatal("Open() accepted an unsafe key")
	}
}

func TestFileBlobStoreDeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blob, err := store.Put(ctx, bytes.NewBufferString("delete me"), PutBlobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, blob.Key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, blob.Key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(ctx, blob.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() error = %v, want ErrNotFound", err)
	}
}
