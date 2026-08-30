package media

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestLibrarySavesGeneratedBytesAndCatalogRecord(t *testing.T) {
	ctx := context.Background()
	kv := store.NewMemoryKV()
	repository := NewRepository(kv)
	blobs, err := store.NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library := NewLibrary(repository, blobs)
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	library.now = func() time.Time { return now }
	library.newID = func() (string, error) { return "gif_generated", nil }
	spec := gifdomain.Defaults()

	asset, err := library.SaveGenerated(ctx, GeneratedAsset{
		Prompt: "we shipped it",
		Engine: "local",
		Spec:   spec,
		Data:   []byte("GIF89a-test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if asset.ID != "gif_generated" || asset.Fingerprint == nil || asset.Fingerprint.SHA256 == "" {
		t.Fatalf("SaveGenerated() = %#v", asset)
	}
	stored, err := repository.Get(ctx, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Prompt != "we shipped it" || stored.Renditions[0].SizeBytes != int64(len("GIF89a-test")) {
		t.Fatalf("stored asset = %#v", stored)
	}
	reader, _, err := blobs.Open(ctx, stored.Renditions[0].BlobKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()

	assetFromLibrary, generatedReader, err := library.OpenGenerated(ctx, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	generatedBytes, err := io.ReadAll(generatedReader)
	_ = generatedReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if assetFromLibrary.ID != asset.ID || string(generatedBytes) != "GIF89a-test" {
		t.Fatalf("OpenGenerated() = %#v, %q", assetFromLibrary, generatedBytes)
	}

	external := asset
	external.ID = "gif_external"
	external.Provenance.Provider = "wikimedia"
	if err := repository.Put(ctx, external); err != nil {
		t.Fatal(err)
	}
	if _, _, err := library.OpenGenerated(ctx, external.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("OpenGenerated(external) error = %v, want ErrNotFound", err)
	}
}

func TestLibraryPreservesTemporaryReferenceProvenance(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(store.NewMemoryKV())
	blobs, err := store.NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library := NewLibrary(repository, blobs)
	library.newID = func() (string, error) { return "gif_reference", nil }

	asset, err := library.SaveGenerated(ctx, GeneratedAsset{
		Prompt: "remix this",
		Engine: "comfyui-local+local",
		Spec:   gifdomain.Defaults(),
		Data:   []byte("GIF89a-test"),
		Source: &GeneratedSource{
			Provider: "wikimedia", ExternalID: "42", SourceURL: "https://commons.wikimedia.org/wiki/File:Example.png",
			Author: "Example Author", LicenseID: "cc-by-sa-4.0", LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/",
			Attribution: "Example Author / CC BY-SA 4.0", CommercialUse: PermissionAllowed,
			Derivatives: PermissionAllowed, ShareAlike: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asset.Provenance.Provider != "gogif" || asset.Provenance.SourceProvider != "wikimedia" || asset.Provenance.SourceExternalID != "42" {
		t.Fatalf("provenance = %#v", asset.Provenance)
	}
	if asset.Rights.LicenseID != "cc-by-sa-4.0" || !asset.Rights.ShareAlike || asset.Rights.Derivatives != PermissionAllowed {
		t.Fatalf("rights = %#v", asset.Rights)
	}
}
