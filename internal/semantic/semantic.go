// Package semantic adds meaning-aware ranking to search. GoGIF's catalogs
// match keywords, so a phrasing the archivist did not use returns nothing
// useful even when the footage exists. An embedder turns the query and each
// candidate into vectors whose angle measures similarity, which recovers
// those results without asking any provider to change.
//
// Every embedder is optional. When none is configured, or a remote one fails,
// ranking falls back to the offline lexical embedder so search keeps working
// with no vendor, no key, and no network.
package semantic

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

var (
	ErrInvalidRequest = errors.New("semantic: invalid request")
	ErrUnavailable    = errors.New("semantic: embedder unavailable")
)

const (
	MaxBatch     = 32
	MaxInputRune = 2000
)

type Descriptor struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Local      bool   `json:"local"`
	Dimensions int    `json:"dimensions"`
}

// Embedder maps text to unit-length vectors. Implementations must return one
// vector per input, in order, and must normalize to unit length so callers
// can treat a dot product as cosine similarity.
type Embedder interface {
	Descriptor() Descriptor
	Embed(context.Context, []string) ([][]float32, error)
}

// ValidateInputs bounds a batch before it reaches a vendor or a local model.
func ValidateInputs(inputs []string) error {
	if len(inputs) == 0 {
		return fmt.Errorf("%w: at least one input is required", ErrInvalidRequest)
	}
	if len(inputs) > MaxBatch {
		return fmt.Errorf("%w: at most %d inputs per batch", ErrInvalidRequest, MaxBatch)
	}
	for index, input := range inputs {
		if strings.TrimSpace(input) == "" {
			return fmt.Errorf("%w: input %d is empty", ErrInvalidRequest, index)
		}
		if len([]rune(input)) > MaxInputRune {
			return fmt.Errorf("%w: input %d exceeds %d characters", ErrInvalidRequest, index, MaxInputRune)
		}
	}
	return nil
}

// Cosine returns the similarity of two vectors in the inclusive range -1..1.
// It tolerates non-normalized input so callers can score raw vectors.
func Cosine(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		l, r := float64(left[index]), float64(right[index])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

// Unit scales a vector to length one in place and returns it. Embedders call
// it so downstream comparisons stay cheap.
func Unit(vector []float32) []float32 {
	var norm float64
	for _, value := range vector {
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return vector
	}
	scale := 1 / math.Sqrt(norm)
	for index := range vector {
		vector[index] = float32(float64(vector[index]) * scale)
	}
	return vector
}

// Scored is one candidate with its similarity to the query.
type Scored struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

// Rank orders candidates by similarity to the query, most similar first.
// Ties keep their original order so a provider's own relevance survives where
// the embedder has no opinion.
func Rank(ctx context.Context, embedder Embedder, query string, candidates []string) ([]Scored, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: query is empty", ErrInvalidRequest)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if embedder == nil {
		return nil, ErrUnavailable
	}
	inputs := make([]string, 0, len(candidates)+1)
	inputs = append(inputs, query)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			// An empty candidate cannot be embedded, but dropping it would
			// break index alignment, so it is scored against a placeholder.
			candidate = "untitled"
		}
		inputs = append(inputs, clampRunes(candidate, MaxInputRune))
	}
	vectors, err := embedBatched(ctx, embedder, inputs)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(inputs) {
		return nil, fmt.Errorf("%w: embedder returned %d vectors for %d inputs", ErrUnavailable, len(vectors), len(inputs))
	}
	scored := make([]Scored, len(candidates))
	for index := range candidates {
		scored[index] = Scored{Index: index, Score: Cosine(vectors[0], vectors[index+1])}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	return scored, nil
}

// embedBatched respects MaxBatch so a long result page cannot turn into one
// oversized vendor request.
func embedBatched(ctx context.Context, embedder Embedder, inputs []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(inputs))
	for start := 0; start < len(inputs); start += MaxBatch {
		end := min(start+MaxBatch, len(inputs))
		batch, err := embedder.Embed(ctx, inputs[start:end])
		if err != nil {
			return nil, err
		}
		if len(batch) != end-start {
			return nil, fmt.Errorf("%w: batch returned %d vectors for %d inputs", ErrUnavailable, len(batch), end-start)
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func clampRunes(value string, limit int) string {
	if runes := []rune(value); len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
