package semantic

import (
	"context"
	"strings"
	"sync"
)

// Cached memoizes embeddings for repeated text. Search pages the same query
// and re-shows the same catalog titles, so without this a hosted embedder
// would be billed for identical inputs on every scroll.
//
// It is bounded and evicts everything at once when full. A precise LRU is not
// worth the contention here: entries are small, and a cold rebuild costs one
// extra request.
type Cached struct {
	Embedder Embedder
	// MaxEntries defaults to 2048 when zero.
	MaxEntries int

	mu      sync.Mutex
	entries map[string][]float32
}

func (c *Cached) Descriptor() Descriptor {
	return c.Embedder.Descriptor()
}

func (c *Cached) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if err := ValidateInputs(inputs); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(inputs))
	missingIndexes := make([]int, 0, len(inputs))
	missingInputs := make([]string, 0, len(inputs))

	c.mu.Lock()
	for index, input := range inputs {
		if vector, ok := c.entries[cacheKey(input)]; ok {
			vectors[index] = vector
			continue
		}
		missingIndexes = append(missingIndexes, index)
		missingInputs = append(missingInputs, input)
	}
	c.mu.Unlock()

	if len(missingInputs) == 0 {
		return vectors, nil
	}
	computed, err := c.Embedder.Embed(ctx, missingInputs)
	if err != nil {
		return nil, err
	}
	if len(computed) != len(missingInputs) {
		return nil, ErrUnavailable
	}

	c.mu.Lock()
	limit := c.MaxEntries
	if limit <= 0 {
		limit = 2048
	}
	if c.entries == nil || len(c.entries) >= limit {
		c.entries = make(map[string][]float32, limit)
	}
	for offset, index := range missingIndexes {
		vectors[index] = computed[offset]
		c.entries[cacheKey(missingInputs[offset])] = computed[offset]
	}
	c.mu.Unlock()
	return vectors, nil
}

func cacheKey(input string) string {
	return strings.ToLower(strings.Join(strings.Fields(input), " "))
}
