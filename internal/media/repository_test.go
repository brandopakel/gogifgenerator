package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestRepositoryRoundTripAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(store.NewMemoryKV())
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	asset := validAsset(now)
	if err := repository.Put(ctx, asset); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != asset.ID || got.Renditions[0].BlobKey != asset.Renditions[0].BlobKey {
		t.Fatalf("Get() = %#v", got)
	}

	deletedAt := now.Add(time.Hour)
	if err := repository.Delete(ctx, asset.ID, deletedAt); err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.Get(ctx, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.State != StateDeleted || deleted.DeletedAt == nil || !deleted.DeletedAt.Equal(deletedAt) {
		t.Fatalf("deleted asset = %#v", deleted)
	}
}

func TestRepositoryRejectsInvalidAndMissingAssets(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(store.NewMemoryKV())
	if err := repository.Put(ctx, Asset{}); err == nil {
		t.Fatal("Put() accepted an invalid asset")
	}
	if _, err := repository.Get(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Get(ctx, "../unsafe"); err == nil {
		t.Fatal("Get() accepted an invalid ID")
	}
}

func validAsset(now time.Time) Asset {
	return Asset{
		ID:        "gif_123",
		Kind:      KindGIF,
		State:     StateReady,
		Title:     "Shipped it",
		CreatedAt: now,
		UpdatedAt: now,
		Provenance: Provenance{
			Provider:   "gogif",
			ExternalID: "generation_123",
		},
		Rights: Rights{
			Status:        "owned",
			CommercialUse: PermissionUnknown,
			Derivatives:   PermissionAllowed,
		},
		Renditions: []Rendition{{
			Name:        "original",
			Format:      "gif",
			ContentType: "image/gif",
			Storage:     StorageManaged,
			BlobKey:     "sha256/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Width:       480,
			Height:      480,
		}},
	}
}
