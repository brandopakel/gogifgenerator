package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubEmbedder struct {
	calls   int
	batches [][]string
	err     error
	vectors map[string][]float32
}

func (s *stubEmbedder) Descriptor() Descriptor {
	return Descriptor{ID: "stub", Label: "Stub", Dimensions: 3}
}

func (s *stubEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	s.calls++
	s.batches = append(s.batches, append([]string(nil), inputs...))
	if s.err != nil {
		return nil, s.err
	}
	vectors := make([][]float32, len(inputs))
	for index, input := range inputs {
		vector, ok := s.vectors[input]
		if !ok {
			vector = []float32{0, 0, 1}
		}
		vectors[index] = append([]float32(nil), vector...)
	}
	return vectors, nil
}

func TestCosineMatchesKnownAngles(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{1, 0}); got < 0.999 {
		t.Fatalf("identical vectors scored %v", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); got > 0.001 || got < -0.001 {
		t.Fatalf("orthogonal vectors scored %v", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{-1, 0}); got > -0.999 {
		t.Fatalf("opposite vectors scored %v", got)
	}
	if got := Cosine(nil, []float32{1}); got != 0 {
		t.Fatalf("mismatched vectors scored %v", got)
	}
}

func TestUnitScalesToLengthOne(t *testing.T) {
	vector := Unit([]float32{3, 4})
	if got := Cosine(vector, []float32{3, 4}); got < 0.999 {
		t.Fatalf("Unit changed direction: %v", vector)
	}
	var norm float32
	for _, value := range vector {
		norm += value * value
	}
	if norm < 0.999 || norm > 1.001 {
		t.Fatalf("Unit norm = %v", norm)
	}
}

func TestRankOrdersBySimilarity(t *testing.T) {
	embedder := &stubEmbedder{vectors: map[string][]float32{
		"rocket launch":   {1, 0, 0},
		"a rocket lifts":  {0.9, 0.1, 0},
		"a bowl of soup":  {0, 1, 0},
		"launch sequence": {0.7, 0, 0.2},
	}}
	scored, err := Rank(context.Background(), embedder, "rocket launch",
		[]string{"a bowl of soup", "a rocket lifts", "launch sequence"})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}
	if len(scored) != 3 {
		t.Fatalf("Rank() returned %d results", len(scored))
	}
	if scored[0].Index != 1 {
		t.Fatalf("Rank() put candidate %d first", scored[0].Index)
	}
	if scored[len(scored)-1].Index != 0 {
		t.Fatalf("Rank() did not rank the unrelated candidate last: %#v", scored)
	}
}

func TestRankRejectsEmptyQueryAndNilEmbedder(t *testing.T) {
	if _, err := Rank(context.Background(), Lexical{}, "  ", []string{"a"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty query error = %v", err)
	}
	if _, err := Rank(context.Background(), nil, "query", []string{"a"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil embedder error = %v", err)
	}
}

func TestRankBatchesLargeInputs(t *testing.T) {
	embedder := &stubEmbedder{}
	candidates := make([]string, MaxBatch*2)
	for index := range candidates {
		candidates[index] = strings.Repeat("x", index+1)
	}
	if _, err := Rank(context.Background(), embedder, "query", candidates); err != nil {
		t.Fatalf("Rank() error = %v", err)
	}
	if embedder.calls < 3 {
		t.Fatalf("Rank() made %d calls; expected batching", embedder.calls)
	}
	for _, batch := range embedder.batches {
		if len(batch) > MaxBatch {
			t.Fatalf("batch of %d exceeds MaxBatch", len(batch))
		}
	}
}

func TestValidateInputsBoundsBatches(t *testing.T) {
	if err := ValidateInputs(nil); err == nil {
		t.Fatal("ValidateInputs(nil) expected an error")
	}
	if err := ValidateInputs([]string{"  "}); err == nil {
		t.Fatal("ValidateInputs(blank) expected an error")
	}
	if err := ValidateInputs([]string{strings.Repeat("x", MaxInputRune+1)}); err == nil {
		t.Fatal("ValidateInputs(long) expected an error")
	}
	oversized := make([]string, MaxBatch+1)
	for index := range oversized {
		oversized[index] = "text"
	}
	if err := ValidateInputs(oversized); err == nil {
		t.Fatal("ValidateInputs(oversized batch) expected an error")
	}
}

func TestLexicalRelatesWordForms(t *testing.T) {
	related := Cosine(vectorOf(t, "a rocket launching"), vectorOf(t, "rocket launch"))
	unrelated := Cosine(vectorOf(t, "a rocket launching"), vectorOf(t, "a bowl of tomato soup"))
	if related <= unrelated {
		t.Fatalf("lexical vectors did not separate related text: related=%v unrelated=%v", related, unrelated)
	}
	if related < 0.3 {
		t.Fatalf("related text scored only %v", related)
	}
}

func TestLexicalIsDeterministic(t *testing.T) {
	if Cosine(vectorOf(t, "puppy in a sprinkler"), vectorOf(t, "puppy in a sprinkler")) < 0.999 {
		t.Fatal("lexical embedding is not deterministic")
	}
}

func vectorOf(t *testing.T, text string) []float32 {
	t.Helper()
	vectors, err := (Lexical{}).Embed(context.Background(), []string{text})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	return vectors[0]
}

func TestRankerFallsBackWhenEmbedderFails(t *testing.T) {
	var observed error
	embedder := &stubEmbedder{err: errors.New("provider down")}
	ranker := Ranker{Embedder: embedder, OnError: func(err error) { observed = err }}
	order := ranker.Order(context.Background(), "rocket launching",
		[]string{"tomato soup recipe", "rocket launching from a pad"})
	if observed == nil {
		t.Fatal("Ranker did not report the failure")
	}
	if len(order) != 2 {
		t.Fatalf("Order() = %v", order)
	}
	if order[0] != 1 {
		t.Fatalf("fallback ranking put %d first: %v", order[0], order)
	}
}

func TestRankerAlwaysReturnsACompletePermutation(t *testing.T) {
	ranker := Ranker{Embedder: &stubEmbedder{err: errors.New("down")}, Fallback: &stubEmbedder{err: errors.New("also down")}}
	candidates := []string{"one", "two", "three"}
	order := ranker.Order(context.Background(), "query", candidates)
	seen := make(map[int]bool, len(order))
	for _, index := range order {
		if index < 0 || index >= len(candidates) || seen[index] {
			t.Fatalf("Order() is not a permutation: %v", order)
		}
		seen[index] = true
	}
	if len(seen) != len(candidates) {
		t.Fatalf("Order() dropped candidates: %v", order)
	}
}

func TestRankerWeightKeepsProviderOrderWhenScoresAreClose(t *testing.T) {
	embedder := &stubEmbedder{vectors: map[string][]float32{
		"query": {1, 0, 0}, "first": {0.9, 0.1, 0}, "second": {0.91, 0.1, 0},
	}}
	// A marginal semantic edge must not displace a confident upstream first
	// result when the provider's own order carries most of the weight.
	ranker := Ranker{Embedder: embedder, Weight: 0.1}
	order := ranker.Order(context.Background(), "query", []string{"first", "second"})
	if order[0] != 0 {
		t.Fatalf("low weight still reordered: %v", order)
	}
}

func TestBestReportsRawScore(t *testing.T) {
	embedder := &stubEmbedder{vectors: map[string][]float32{
		"needle": {1, 0, 0}, "hay": {0, 1, 0}, "needle in a haystack": {0.99, 0.01, 0},
	}}
	index, score, ok := Ranker{Embedder: embedder}.Best(context.Background(), "needle", []string{"hay", "needle in a haystack"})
	if !ok || index != 1 {
		t.Fatalf("Best() = %d, %v, %v", index, score, ok)
	}
	if score < 0.9 {
		t.Fatalf("Best() score = %v", score)
	}
}

func TestCachedReusesVectors(t *testing.T) {
	embedder := &stubEmbedder{}
	cached := &Cached{Embedder: embedder}
	if _, err := cached.Embed(context.Background(), []string{"one", "two"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if _, err := cached.Embed(context.Background(), []string{"two", "One", "three"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if embedder.calls != 2 {
		t.Fatalf("embedder called %d times", embedder.calls)
	}
	if got := embedder.batches[1]; len(got) != 1 || got[0] != "three" {
		t.Fatalf("second batch = %v; only the new input should be embedded", got)
	}
}

func TestCachedEvictsWhenFull(t *testing.T) {
	embedder := &stubEmbedder{}
	cached := &Cached{Embedder: embedder, MaxEntries: 2}
	for _, input := range []string{"a", "b", "c", "a"} {
		if _, err := cached.Embed(context.Background(), []string{input}); err != nil {
			t.Fatalf("Embed() error = %v", err)
		}
	}
	if embedder.calls != 4 {
		t.Fatalf("embedder called %d times; expected an eviction to force a refetch", embedder.calls)
	}
}
