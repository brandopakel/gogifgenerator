package store

import (
	"context"
	"io"
)

// Blob identifies immutable bytes by their SHA-256 digest.
type Blob struct {
	Key    string `json:"key"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type PutBlobOptions struct {
	// MaxBytes rejects an upload after this many bytes. Zero means unlimited.
	MaxBytes int64
}

// BlobStore owns large binary objects. KV implementations deliberately do not.
type BlobStore interface {
	Put(context.Context, io.Reader, PutBlobOptions) (Blob, error)
	Open(context.Context, string) (io.ReadCloser, Blob, error)
	Delete(context.Context, string) error
}
