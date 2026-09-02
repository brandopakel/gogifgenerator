package semantic

import (
	"context"
	"sort"
	"time"
)

// Ranker turns an optional embedder into a safe ordering function. Ranking is
// an enhancement, never a dependency: any failure falls back to the offline
// lexical embedder, and a failure there keeps the original order.
type Ranker struct {
	// Embedder is the preferred, possibly hosted, model. It may be nil.
	Embedder Embedder
	// Fallback runs when Embedder is nil or fails. It defaults to Lexical.
	Fallback Embedder
	// Weight is how much the semantic score counts against the upstream
	// provider's own ordering, in the inclusive range 0..1. It defaults to
	// 0.6, which reorders confidently without discarding provider relevance.
	Weight float64
	// Timeout bounds one ranking pass. It defaults to five seconds so a slow
	// embedder delays a search page rather than hanging it.
	Timeout time.Duration
	// OnError observes degraded ranking. It is optional.
	OnError func(error)
}

// Order returns the candidate indexes in their new order. The returned slice
// is always a complete permutation of the input, so a caller can apply it
// without checking for dropped results.
func (r Ranker) Order(ctx context.Context, query string, candidates []string) []int {
	order := make([]int, len(candidates))
	for index := range order {
		order[index] = index
	}
	if len(candidates) < 2 {
		return order
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	scored, err := r.score(ctx, query, candidates)
	if err != nil {
		if r.OnError != nil {
			r.OnError(err)
		}
		return order
	}

	weight := r.Weight
	if weight <= 0 {
		weight = 0.6
	}
	if weight > 1 {
		weight = 1
	}
	// Blend the semantic score with the provider's ordering so a confident
	// upstream match is not displaced by a marginal vector difference.
	blended := make([]Scored, len(candidates))
	positionWeight := 1 - weight
	for _, candidate := range scored {
		position := 1 - float64(candidate.Index)/float64(len(candidates))
		blended[candidate.Index] = Scored{
			Index: candidate.Index,
			Score: weight*candidate.Score + positionWeight*position,
		}
	}
	sort.SliceStable(blended, func(i, j int) bool { return blended[i].Score > blended[j].Score })
	for position, candidate := range blended {
		order[position] = candidate.Index
	}
	return order
}

func (r Ranker) score(ctx context.Context, query string, candidates []string) ([]Scored, error) {
	if r.Embedder != nil {
		scored, err := Rank(ctx, r.Embedder, query, candidates)
		if err == nil {
			return scored, nil
		}
		if r.OnError != nil {
			r.OnError(err)
		}
	}
	fallback := r.Fallback
	if fallback == nil {
		fallback = Lexical{}
	}
	return Rank(ctx, fallback, query, candidates)
}

// Descriptor reports which embedder ranking will prefer.
func (r Ranker) Descriptor() Descriptor {
	if r.Embedder != nil {
		return r.Embedder.Descriptor()
	}
	if r.Fallback != nil {
		return r.Fallback.Descriptor()
	}
	return Lexical{}.Descriptor()
}

// Best returns the single closest candidate and its raw similarity, without
// the positional blending Order applies. Callers that have no upstream
// ordering to preserve — matching a quote against transcript windows, for
// example — use this and apply their own confidence threshold.
func (r Ranker) Best(ctx context.Context, query string, candidates []string) (int, float64, bool) {
	if len(candidates) == 0 {
		return 0, 0, false
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	scored, err := r.score(ctx, query, candidates)
	if err != nil {
		if r.OnError != nil {
			r.OnError(err)
		}
		return 0, 0, false
	}
	if len(scored) == 0 {
		return 0, 0, false
	}
	return scored[0].Index, scored[0].Score, true
}
