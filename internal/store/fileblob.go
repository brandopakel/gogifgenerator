package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileBlobStore is the local-development implementation of BlobStore. Its key
// layout matches the content-addressing scheme the S3/R2 implementation uses.
type FileBlobStore struct {
	root string
}

func NewFileBlobStore(root string) (*FileBlobStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("blob root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve blob root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create blob root: %w", err)
	}
	return &FileBlobStore{root: absolute}, nil
}

func (s *FileBlobStore) Put(ctx context.Context, source io.Reader, options PutBlobOptions) (Blob, error) {
	if err := ctx.Err(); err != nil {
		return Blob{}, err
	}
	temporary, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return Blob{}, fmt.Errorf("create temporary blob: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		_ = temporary.Close()
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	hash := sha256.New()
	reader := io.Reader(&contextReader{ctx: ctx, source: source})
	if options.MaxBytes > 0 {
		reader = io.LimitReader(reader, options.MaxBytes+1)
	}
	size, err := io.Copy(io.MultiWriter(temporary, hash), reader)
	if err != nil {
		return Blob{}, fmt.Errorf("write blob: %w", err)
	}
	if options.MaxBytes > 0 && size > options.MaxBytes {
		return Blob{}, ErrTooLarge
	}
	if err := temporary.Sync(); err != nil {
		return Blob{}, fmt.Errorf("sync blob: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Blob{}, fmt.Errorf("close blob: %w", err)
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	key := blobKey(digest)
	target, err := s.path(key)
	if err != nil {
		return Blob{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return Blob{}, fmt.Errorf("create blob shard: %w", err)
	}
	if info, err := os.Stat(target); err == nil {
		return Blob{Key: key, Digest: digest, Size: info.Size()}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Blob{}, fmt.Errorf("inspect existing blob: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Blob{}, fmt.Errorf("commit blob: %w", err)
	}
	keepTemporary = true
	return Blob{Key: key, Digest: digest, Size: size}, nil
}

func (s *FileBlobStore) Open(ctx context.Context, key string) (io.ReadCloser, Blob, error) {
	if err := ctx.Err(); err != nil {
		return nil, Blob{}, err
	}
	path, err := s.path(key)
	if err != nil {
		return nil, Blob{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Blob{}, ErrNotFound
	}
	if err != nil {
		return nil, Blob{}, fmt.Errorf("open blob: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, Blob{}, fmt.Errorf("inspect blob: %w", err)
	}
	digest := filepath.Base(path)
	return file, Blob{Key: key, Digest: digest, Size: info.Size()}, nil
}

func (s *FileBlobStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}

func (s *FileBlobStore) path(key string) (string, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "sha256" || len(parts[1]) != 2 || len(parts[2]) != sha256.Size*2 || parts[1] != parts[2][:2] {
		return "", fmt.Errorf("invalid blob key %q", key)
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", fmt.Errorf("invalid blob key %q", key)
	}
	return filepath.Join(s.root, parts[0], parts[1], parts[2]), nil
}

func blobKey(digest string) string {
	return "sha256/" + digest[:2] + "/" + digest
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(buffer)
}
