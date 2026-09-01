package scene

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

const (
	artifactRecordPrefix    = "scene:artifact:v1:"
	DefaultMaxArtifactBytes = int64(2 << 30)
)

var safeArtifactFilename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`)

type artifactRecord struct {
	Artifact Artifact   `json:"artifact"`
	Blob     store.Blob `json:"blob"`
}

// ArtifactRepository maps stable, project-scoped Scene artifact keys to the
// private content-addressed blob store used by the deployment.
type ArtifactRepository struct {
	kv       store.KV
	blobs    store.BlobStore
	maxBytes int64
}

func NewArtifactRepository(kv store.KV, blobs store.BlobStore, maxBytes int64) (*ArtifactRepository, error) {
	if kv == nil || blobs == nil {
		return nil, errors.New("scene: artifact KV and blob store are required")
	}
	if maxBytes <= 0 || maxBytes > 10<<30 {
		maxBytes = DefaultMaxArtifactBytes
	}
	return &ArtifactRepository{kv: kv, blobs: blobs, maxBytes: maxBytes}, nil
}

func (r *ArtifactRepository) MaxBytes() int64 { return r.maxBytes }

func (r *ArtifactRepository) Put(ctx context.Context, projectID, kind, filename, contentType string, source io.Reader) (Artifact, error) {
	filename = strings.TrimSpace(filename)
	if filename != filepath.Base(filename) || strings.Contains(filename, `\`) {
		return Artifact{}, fmt.Errorf("%w: artifact filename is invalid", ErrInvalid)
	}
	kind = strings.TrimSpace(kind)
	contentType, _, _ = strings.Cut(contentType, ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if !validID(projectID, "scn_") || !safeArtifactFilename.MatchString(filename) || !validArtifactContentType(kind, contentType) {
		return Artifact{}, fmt.Errorf("%w: artifact upload metadata is invalid", ErrInvalid)
	}
	blob, err := r.blobs.Put(ctx, source, store.PutBlobOptions{MaxBytes: r.maxBytes})
	if err != nil {
		return Artifact{}, err
	}
	if len(blob.Digest) != sha256.Size*2 || blob.Size < 1 || blob.Size > r.maxBytes {
		return Artifact{}, errors.New("scene: blob store returned invalid artifact metadata")
	}
	if _, err := hex.DecodeString(blob.Digest); err != nil {
		return Artifact{}, errors.New("scene: blob store returned an invalid artifact digest")
	}
	artifact := Artifact{
		Kind: kind, StorageKey: fmt.Sprintf("scenes/%s/%s/%s-%s", projectID, kind, blob.Digest[:16], filename),
		ContentType: contentType, SizeBytes: blob.Size, SHA256: blob.Digest,
	}
	if err := artifact.Validate(projectID); err != nil {
		return Artifact{}, err
	}
	record, err := json.Marshal(artifactRecord{Artifact: artifact, Blob: blob})
	if err != nil {
		return Artifact{}, err
	}
	if err := r.kv.Put(ctx, artifactRecordKey(artifact.StorageKey), record, 0); err != nil {
		return Artifact{}, fmt.Errorf("record scene artifact: %w", err)
	}
	return artifact, nil
}

func (r *ArtifactRepository) Verify(ctx context.Context, projectID string, artifacts []Artifact) error {
	for _, artifact := range artifacts {
		if err := artifact.Validate(projectID); err != nil {
			return err
		}
		data, err := r.kv.Get(ctx, artifactRecordKey(artifact.StorageKey))
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: artifact %q was not uploaded", ErrInvalid, artifact.StorageKey)
		}
		if err != nil {
			return err
		}
		var record artifactRecord
		if json.Unmarshal(data, &record) != nil || record.Artifact != artifact || record.Blob.Digest != artifact.SHA256 || record.Blob.Size != artifact.SizeBytes {
			return fmt.Errorf("%w: artifact %q metadata does not match storage", ErrInvalid, artifact.StorageKey)
		}
		reader, blob, err := r.blobs.Open(ctx, record.Blob.Key)
		if err != nil {
			return fmt.Errorf("%w: artifact %q is not present in private storage", ErrInvalid, artifact.StorageKey)
		}
		hash := sha256.New()
		size, readErr := io.Copy(hash, io.LimitReader(reader, artifact.SizeBytes+1))
		closeErr := reader.Close()
		digest := hex.EncodeToString(hash.Sum(nil))
		if readErr != nil || closeErr != nil || size != artifact.SizeBytes || digest != artifact.SHA256 || blob.Digest != artifact.SHA256 || blob.Size != artifact.SizeBytes {
			return fmt.Errorf("%w: artifact %q failed physical verification", ErrInvalid, artifact.StorageKey)
		}
	}
	return nil
}

func artifactRecordKey(storageKey string) string {
	return artifactRecordPrefix + base64.RawURLEncoding.EncodeToString([]byte(storageKey))
}

func validArtifactContentType(kind, contentType string) bool {
	allowed := map[string][]string{
		"blend":  {"application/x-blender", "application/octet-stream"},
		"asset":  {"application/octet-stream", "model/gltf-binary", "model/fbx"},
		"scene":  {"application/json", "application/octet-stream"},
		"video":  {"video/mp4", "video/webm"},
		"gif":    {"image/gif"},
		"poster": {"image/jpeg", "image/png", "image/webp"},
		"log":    {"application/json", "text/plain"},
	}
	return slicesContains(allowed[kind], contentType)
}

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
