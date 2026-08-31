package media

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

const (
	assetKeyPrefix      = "media:v1:"
	ownerKeyPrefix      = "owner:v1:"
	collectionKeyPrefix = "collection:v1:"
	shareKeyPrefix      = "share:v1:"
	maxCollections      = 100
	maxCollectionAssets = 2500
)

// Repository stores provider-neutral asset documents in MemKV-compatible KV.
// Secondary set/sorted-set indexes are added separately so records remain
// readable and migratable JSON values.
type Repository struct {
	kv  store.KV
	mu  sync.Mutex
	now func() time.Time
}

func NewRepository(kv store.KV) *Repository {
	return &Repository{kv: kv, now: time.Now}
}

func (r *Repository) Put(ctx context.Context, asset Asset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.putUnlocked(ctx, asset, true)
}

func (r *Repository) putUnlocked(ctx context.Context, asset Asset, updateOwnerIndex bool) error {
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
	if updateOwnerIndex && asset.OwnerID != "" {
		index, err := r.readStringList(ctx, ownerAssetsKey(asset.OwnerID))
		if err != nil {
			return err
		}
		if !slices.Contains(index, asset.ID) {
			index = append(index, asset.ID)
			if err := r.writeStringList(ctx, ownerAssetsKey(asset.OwnerID), index); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getUnlocked(ctx, id)
}

func (r *Repository) getUnlocked(ctx context.Context, id string) (Asset, error) {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	asset, err := r.getUnlocked(ctx, id)
	if err != nil {
		return err
	}
	asset.State = StateDeleted
	asset.DeletedAt = &at
	asset.UpdatedAt = at
	return r.putUnlocked(ctx, asset, false)
}

type AssetPage struct {
	Items      []Asset `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

type AssetPatch struct {
	Title    *string   `json:"title,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
	Favorite *bool     `json:"favorite,omitempty"`
}

func (r *Repository) ListOwner(ctx context.Context, ownerID, kind, cursor string, limit int) (AssetPage, error) {
	if strings.TrimSpace(ownerID) == "" {
		return AssetPage{}, errors.New("owner ID is required")
	}
	if limit < 1 || limit > 50 {
		limit = 24
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ids, err := r.readStringList(ctx, ownerAssetsKey(ownerID))
	if err != nil {
		return AssetPage{}, err
	}
	start := len(ids) - 1
	if cursor != "" {
		position := slices.Index(ids, cursor)
		if position < 0 {
			return AssetPage{}, errors.New("invalid library cursor")
		}
		start = position - 1
	}
	page := AssetPage{Items: make([]Asset, 0, limit)}
	for index := start; index >= 0; index-- {
		asset, getErr := r.getUnlocked(ctx, ids[index])
		if errors.Is(getErr, store.ErrNotFound) {
			continue
		}
		if getErr != nil {
			return AssetPage{}, getErr
		}
		if asset.OwnerID != ownerID || asset.State != StateReady || (kind != "" && string(asset.Kind) != kind) {
			continue
		}
		page.Items = append(page.Items, asset)
		if len(page.Items) == limit {
			if index > 0 {
				page.NextCursor = ids[index]
			}
			break
		}
	}
	return page, nil
}

func (r *Repository) UpdateOwnerAsset(ctx context.Context, ownerID, id string, patch AssetPatch) (Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	asset, err := r.getUnlocked(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if asset.OwnerID != ownerID || asset.State != StateReady {
		return Asset{}, store.ErrNotFound
	}
	if patch.Title != nil {
		asset.Title = strings.TrimSpace(*patch.Title)
		if len(asset.Title) > 160 {
			return Asset{}, errors.New("title must not exceed 160 characters")
		}
	}
	if patch.Tags != nil {
		if len(*patch.Tags) > 12 {
			return Asset{}, errors.New("an asset can have at most 12 tags")
		}
		seen := make(map[string]bool)
		asset.Tags = asset.Tags[:0]
		for _, value := range *patch.Tags {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" || len(value) > 32 || seen[value] {
				continue
			}
			seen[value] = true
			asset.Tags = append(asset.Tags, value)
		}
	}
	if patch.Favorite != nil {
		asset.Favorite = *patch.Favorite
	}
	asset.UpdatedAt = r.now().UTC()
	return asset, r.putUnlocked(ctx, asset, false)
}

func (r *Repository) DeleteOwnerAsset(ctx context.Context, ownerID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	asset, err := r.getUnlocked(ctx, id)
	if err != nil {
		return err
	}
	if asset.OwnerID != ownerID || asset.State != StateReady {
		return store.ErrNotFound
	}
	now := r.now().UTC()
	asset.State = StateDeleted
	asset.DeletedAt = &now
	asset.UpdatedAt = now
	return r.putUnlocked(ctx, asset, false)
}

func (r *Repository) OwnerUsage(ctx context.Context, ownerID string) (int, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids, err := r.readStringList(ctx, ownerAssetsKey(ownerID))
	if err != nil {
		return 0, 0, err
	}
	count := 0
	var bytes int64
	for _, id := range ids {
		asset, getErr := r.getUnlocked(ctx, id)
		if errors.Is(getErr, store.ErrNotFound) {
			continue
		}
		if getErr != nil {
			return 0, 0, getErr
		}
		if asset.OwnerID != ownerID || asset.State != StateReady {
			continue
		}
		count++
		for _, rendition := range asset.Renditions {
			if rendition.Storage == StorageManaged {
				bytes += rendition.SizeBytes
			}
		}
	}
	return count, bytes, nil
}

type Collection struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	Name      string    `json:"name"`
	AssetIDs  []string  `json:"asset_ids"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *Repository) CreateCollection(ctx context.Context, ownerID, name string) (Collection, error) {
	name = strings.TrimSpace(name)
	if ownerID == "" || name == "" || len(name) > 80 {
		return Collection{}, errors.New("owner and a collection name of at most 80 characters are required")
	}
	id, err := randomMediaToken("col_", 12)
	if err != nil {
		return Collection{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ids, err := r.readStringList(ctx, ownerCollectionsKey(ownerID))
	if err != nil {
		return Collection{}, err
	}
	if len(ids) >= maxCollections {
		return Collection{}, fmt.Errorf("an account can have at most %d collections", maxCollections)
	}
	now := r.now().UTC()
	collection := Collection{ID: id, OwnerID: ownerID, Name: name, CreatedAt: now, UpdatedAt: now}
	if err := r.putCollection(ctx, collection); err != nil {
		return Collection{}, err
	}
	ids = append(ids, id)
	if err := r.writeStringList(ctx, ownerCollectionsKey(ownerID), ids); err != nil {
		_ = r.kv.Delete(ctx, collectionKey(id))
		return Collection{}, err
	}
	return collection, nil
}

func (r *Repository) ListCollections(ctx context.Context, ownerID string) ([]Collection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids, err := r.readStringList(ctx, ownerCollectionsKey(ownerID))
	if err != nil {
		return nil, err
	}
	collections := make([]Collection, 0, len(ids))
	for _, id := range ids {
		collection, getErr := r.getCollection(ctx, id)
		if errors.Is(getErr, store.ErrNotFound) {
			continue
		}
		if getErr != nil {
			return nil, getErr
		}
		if collection.OwnerID == ownerID {
			collections = append(collections, collection)
		}
	}
	return collections, nil
}

func (r *Repository) UpdateCollection(ctx context.Context, ownerID, id, name string, assetID *string, add bool) (Collection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	collection, err := r.getCollection(ctx, id)
	if err != nil {
		return Collection{}, err
	}
	if collection.OwnerID != ownerID {
		return Collection{}, store.ErrNotFound
	}
	if name != "" {
		name = strings.TrimSpace(name)
		if len(name) > 80 {
			return Collection{}, errors.New("collection name must not exceed 80 characters")
		}
		collection.Name = name
	}
	if assetID != nil {
		asset, getErr := r.getUnlocked(ctx, *assetID)
		if getErr != nil || asset.OwnerID != ownerID || asset.State != StateReady {
			return Collection{}, store.ErrNotFound
		}
		position := slices.Index(collection.AssetIDs, *assetID)
		if add && position < 0 {
			if len(collection.AssetIDs) >= maxCollectionAssets {
				return Collection{}, fmt.Errorf("a collection can contain at most %d creations", maxCollectionAssets)
			}
			collection.AssetIDs = append(collection.AssetIDs, *assetID)
		} else if !add && position >= 0 {
			collection.AssetIDs = slices.Delete(collection.AssetIDs, position, position+1)
		}
	}
	collection.UpdatedAt = r.now().UTC()
	return collection, r.putCollection(ctx, collection)
}

func (r *Repository) DeleteCollection(ctx context.Context, ownerID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	collection, err := r.getCollection(ctx, id)
	if err != nil {
		return err
	}
	if collection.OwnerID != ownerID {
		return store.ErrNotFound
	}
	if err := r.kv.Delete(ctx, collectionKey(id)); err != nil {
		return err
	}
	ids, err := r.readStringList(ctx, ownerCollectionsKey(ownerID))
	if err != nil {
		return err
	}
	if position := slices.Index(ids, id); position >= 0 {
		ids = slices.Delete(ids, position, position+1)
	}
	return r.writeStringList(ctx, ownerCollectionsKey(ownerID), ids)
}

type ShareGrant struct {
	Token     string    `json:"token,omitempty"`
	AssetID   string    `json:"asset_id"`
	OwnerID   string    `json:"owner_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (r *Repository) CreateShare(ctx context.Context, ownerID, assetID string, lifetime time.Duration) (ShareGrant, error) {
	if lifetime < time.Minute || lifetime > 30*24*time.Hour {
		return ShareGrant{}, errors.New("share lifetime must be between one minute and 30 days")
	}
	token, err := randomMediaToken("shr_", 24)
	if err != nil {
		return ShareGrant{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	asset, err := r.getUnlocked(ctx, assetID)
	if err != nil || asset.OwnerID != ownerID || asset.State != StateReady {
		return ShareGrant{}, store.ErrNotFound
	}
	expires := r.now().UTC().Add(lifetime)
	grant := ShareGrant{Token: token, AssetID: assetID, OwnerID: ownerID, ExpiresAt: expires}
	data, _ := json.Marshal(ShareGrant{AssetID: assetID, OwnerID: ownerID, ExpiresAt: expires})
	if previous, getErr := r.kv.Get(ctx, shareAssetKey(assetID)); getErr == nil {
		if err := r.kv.Delete(ctx, string(previous)); err != nil {
			return ShareGrant{}, err
		}
	} else if !errors.Is(getErr, store.ErrNotFound) {
		return ShareGrant{}, getErr
	}
	if err := r.kv.Put(ctx, shareKey(token), data, lifetime); err != nil {
		return ShareGrant{}, err
	}
	if err := r.kv.Put(ctx, shareAssetKey(assetID), []byte(shareKey(token)), lifetime); err != nil {
		return ShareGrant{}, err
	}
	asset.Shared = true
	asset.ShareExpiry = &expires
	asset.UpdatedAt = r.now().UTC()
	if err := r.putUnlocked(ctx, asset, false); err != nil {
		return ShareGrant{}, err
	}
	return grant, nil
}

func (r *Repository) ResolveShare(ctx context.Context, token string) (ShareGrant, Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := r.kv.Get(ctx, shareKey(token))
	if err != nil {
		return ShareGrant{}, Asset{}, err
	}
	var grant ShareGrant
	if err := json.Unmarshal(data, &grant); err != nil || !r.now().UTC().Before(grant.ExpiresAt) {
		return ShareGrant{}, Asset{}, store.ErrNotFound
	}
	asset, err := r.getUnlocked(ctx, grant.AssetID)
	if err != nil || asset.State != StateReady {
		return ShareGrant{}, Asset{}, store.ErrNotFound
	}
	return grant, asset, nil
}

func (r *Repository) RevokeShare(ctx context.Context, ownerID, assetID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	asset, err := r.getUnlocked(ctx, assetID)
	if err != nil || asset.OwnerID != ownerID || asset.State != StateReady {
		return store.ErrNotFound
	}
	if data, getErr := r.kv.Get(ctx, shareAssetKey(assetID)); getErr == nil {
		if err := r.kv.Delete(ctx, string(data)); err != nil {
			return err
		}
	} else if !errors.Is(getErr, store.ErrNotFound) {
		return getErr
	}
	_ = r.kv.Delete(ctx, shareAssetKey(assetID))
	asset.Shared = false
	asset.ShareExpiry = nil
	asset.UpdatedAt = r.now().UTC()
	return r.putUnlocked(ctx, asset, false)
}

func (r *Repository) readStringList(ctx context.Context, key string) ([]string, error) {
	data, err := r.kv.Get(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func (r *Repository) writeStringList(ctx context.Context, key string, values []string) error {
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return r.kv.Put(ctx, key, data, 0)
}

func (r *Repository) putCollection(ctx context.Context, collection Collection) error {
	data, err := json.Marshal(collection)
	if err != nil {
		return err
	}
	return r.kv.Put(ctx, collectionKey(collection.ID), data, 0)
}

func (r *Repository) getCollection(ctx context.Context, id string) (Collection, error) {
	data, err := r.kv.Get(ctx, collectionKey(id))
	if err != nil {
		return Collection{}, err
	}
	var collection Collection
	return collection, json.Unmarshal(data, &collection)
}

func ownerAssetsKey(ownerID string) string      { return ownerKeyPrefix + ownerID + ":assets" }
func ownerCollectionsKey(ownerID string) string { return ownerKeyPrefix + ownerID + ":collections" }
func collectionKey(id string) string            { return collectionKeyPrefix + id }

func shareKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return shareKeyPrefix + fmt.Sprintf("%x", digest[:])
}

func shareAssetKey(assetID string) string { return shareKeyPrefix + "asset:" + assetID }

func randomMediaToken(prefix string, size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func assetKey(id string) string {
	return assetKeyPrefix + id
}

func errorsForID() error {
	return fmt.Errorf("invalid asset ID")
}
