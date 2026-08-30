package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

// Cached keeps repeated public searches polite and fast. Cache failures never
// make the underlying provider unavailable.
type Cached struct {
	Next Provider
	KV   store.KV
	TTL  time.Duration
}

func (c Cached) Descriptor() Descriptor {
	return c.Next.Descriptor()
}

func (c Cached) Search(ctx context.Context, query Query) (Page, error) {
	normalized, err := query.Normalize()
	if err != nil {
		return Page{}, err
	}
	if c.KV == nil || c.TTL <= 0 {
		return c.Next.Search(ctx, normalized)
	}
	key := searchCacheKey(c.Descriptor().ID, normalized)
	if data, err := c.KV.Get(ctx, key); err == nil {
		var page Page
		if json.Unmarshal(data, &page) == nil {
			return page, nil
		}
	} else if !errors.Is(err, store.ErrNotFound) && ctx.Err() != nil {
		return Page{}, ctx.Err()
	}

	page, err := c.Next.Search(ctx, normalized)
	if err != nil {
		return Page{}, err
	}
	if data, err := json.Marshal(page); err == nil {
		_ = c.KV.Put(ctx, key, data, c.TTL)
	}
	return page, nil
}

func (c Cached) Resolve(ctx context.Context, externalID, locale string) (Result, error) {
	resolver, ok := c.Next.(Resolver)
	if !ok {
		return Result{}, ErrUnavailable
	}
	// A selected item is deliberately revalidated upstream instead of using
	// search cache data before a transformation fetch.
	return resolver.Resolve(ctx, externalID, locale)
}

func searchCacheKey(providerID string, query Query) string {
	data, _ := json.Marshal(query)
	digest := sha256.Sum256(data)
	return "search:v1:" + providerID + ":" + hex.EncodeToString(digest[:])
}
