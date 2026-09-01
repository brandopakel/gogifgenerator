package scene

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestArtifactRepositoryPutAndVerify(t *testing.T) {
	kv := store.NewMemoryKV()
	blobs, err := store.NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewArtifactRepository(kv, blobs, 1024)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := repository.Put(context.Background(), "scn_project", "video", "master.mp4", "video/mp4; codecs=avc1", bytes.NewBufferString("bounded-video"))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Kind != "video" || artifact.ContentType != "video/mp4" || artifact.SizeBytes != 13 || artifact.SHA256 == "" {
		t.Fatalf("artifact = %#v", artifact)
	}
	if err := repository.Verify(context.Background(), "scn_project", []Artifact{artifact}); err != nil {
		t.Fatal(err)
	}
	tampered := artifact
	tampered.SizeBytes++
	if err := repository.Verify(context.Background(), "scn_project", []Artifact{tampered}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered verify = %v", err)
	}
	recordData, err := kv.Get(context.Background(), artifactRecordKey(artifact.StorageKey))
	if err != nil {
		t.Fatal(err)
	}
	var record artifactRecord
	if err := json.Unmarshal(recordData, &record); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Delete(context.Background(), record.Blob.Key); err != nil {
		t.Fatal(err)
	}
	if err := repository.Verify(context.Background(), "scn_project", []Artifact{artifact}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing physical blob verify = %v", err)
	}
}

func TestArtifactRepositoryRejectsUnsafeAndOversizedUploads(t *testing.T) {
	kv := store.NewMemoryKV()
	blobs, err := store.NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewArtifactRepository(kv, blobs, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Put(context.Background(), "scn_project", "video", "../master.mp4", "video/mp4", bytes.NewBufferString("x")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe filename = %v", err)
	}
	if _, err := repository.Put(context.Background(), "scn_project", "video", "master.mp4", "text/html", bytes.NewBufferString("x")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe content type = %v", err)
	}
	if _, err := repository.Put(context.Background(), "scn_project", "video", "master.mp4", "video/mp4", bytes.NewBufferString("large")); !errors.Is(err, store.ErrTooLarge) {
		t.Fatalf("oversized upload = %v", err)
	}
}
