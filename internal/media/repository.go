package media

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

const assetKeyPrefix = "media:v1:"

// Repository stores provider-neutral asset documents in MemKV-compatible KV.
// Secondary set/sorted-set indexes are added separately so records remain
// readable and migratable JSON values.
type Repository struct {
	kv store.KV
}

func NewRepository(kv store.KV) *Repository {
	return &Repository{kv: kv}
}

func (r *Repository) Put(ctx context.Context, asset Asset) error {
	if err := asset.Validate(); err != nil {
		return fmt.Errorf("validate asset: %w", err)
	}
	data, err := json.Marshal(asset)
	if err != nil {
		return fmt.Errorf("encode asset: %w", err)
	}
	if err := r.kv.Put(ctx, assetKey(asset.ID), data, 0); err != nil {
		return fmt.Errorf("store asset: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (Asset, error) {
	if !validID(id) {
		return Asset{}, errorsForID()
	}
	data, err := r.kv.Get(ctx, assetKey(id))
	if err != nil {
		return Asset{}, err
	}
	var asset Asset
	if err := json.Unmarshal(data, &asset); err != nil {
		return Asset{}, fmt.Errorf("decode asset: %w", err)
	}
	if err := asset.Validate(); err != nil {
		return Asset{}, fmt.Errorf("stored asset is invalid: %w", err)
	}
	return asset, nil
}

func (r *Repository) Delete(ctx context.Context, id string, at time.Time) error {
	asset, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	asset.State = StateDeleted
	asset.DeletedAt = &at
	asset.UpdatedAt = at
	return r.Put(ctx, asset)
}

func assetKey(id string) string {
	return assetKeyPrefix + id
}

func errorsForID() error {
	return fmt.Errorf("invalid asset ID")
}
