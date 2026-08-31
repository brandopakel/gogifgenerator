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

func TestRepositoryOwnerLibraryCollectionsAndShares(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(store.NewMemoryKV())
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return now }
	asset := validAsset(now)
	asset.OwnerID = "usr_123"
	if err := repository.Put(ctx, asset); err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListOwner(ctx, asset.OwnerID, "gif", "", 24)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != asset.ID {
		t.Fatalf("ListOwner() = %#v, %v", page, err)
	}
	favorite := true
	updated, err := repository.UpdateOwnerAsset(ctx, asset.OwnerID, asset.ID, AssetPatch{Favorite: &favorite})
	if err != nil || !updated.Favorite {
		t.Fatalf("UpdateOwnerAsset() = %#v, %v", updated, err)
	}
	collection, err := repository.CreateCollection(ctx, asset.OwnerID, "Favorites")
	if err != nil {
		t.Fatal(err)
	}
	collection, err = repository.UpdateCollection(ctx, asset.OwnerID, collection.ID, "", &asset.ID, true)
	if err != nil || len(collection.AssetIDs) != 1 {
		t.Fatalf("UpdateCollection() = %#v, %v", collection, err)
	}
	grant, err := repository.CreateShare(ctx, asset.OwnerID, asset.ID, time.Hour)
	if err != nil || grant.Token == "" {
		t.Fatalf("CreateShare() = %#v, %v", grant, err)
	}
	_, shared, err := repository.ResolveShare(ctx, grant.Token)
	if err != nil || shared.ID != asset.ID {
		t.Fatalf("ResolveShare() = %#v, %v", shared, err)
	}
	replacement, err := repository.CreateShare(ctx, asset.OwnerID, asset.ID, 2*time.Hour)
	if err != nil || replacement.Token == grant.Token {
		t.Fatalf("replacement CreateShare() = %#v, %v", replacement, err)
	}
	if _, _, err := repository.ResolveShare(ctx, grant.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("superseded ResolveShare error = %v", err)
	}
	if err := repository.RevokeShare(ctx, asset.OwnerID, asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ResolveShare(ctx, replacement.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked ResolveShare error = %v", err)
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
