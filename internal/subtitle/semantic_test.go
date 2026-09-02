package subtitle

import (
	"context"
	"strings"
	"testing"
)

type stubMatcher struct {
	query      string
	candidates []string
	index      int
	score      float64
	ok         bool
}

func (s *stubMatcher) Best(_ context.Context, query string, candidates []string) (int, float64, bool) {
	s.query = query
	s.candidates = candidates
	return s.index, s.score, s.ok
}

func sampleCues() []Cue {
	return []Cue{
		{StartMS: 0, EndMS: 2000, Text: "Good morning everyone"},
		{StartMS: 2000, EndMS: 4000, Text: "the vehicle has cleared the tower"},
		{StartMS: 4000, EndMS: 6000, Text: "and is accelerating downrange"},
		{StartMS: 6000, EndMS: 8000, Text: "we will now stand by for staging"},
	}
}

func TestFindSemanticPrefersExactMatching(t *testing.T) {
	matcher := &stubMatcher{index: 0, score: 0.99, ok: true}
	match, ok := FindSemantic(context.Background(), sampleCues(), "the vehicle has cleared the tower", matcher)
	if !ok {
		t.Fatal("FindSemantic() did not find an exact quote")
	}
	if !match.Exact {
		t.Fatalf("exact quote was reported as a paraphrase: %#v", match)
	}
	if matcher.candidates != nil {
		t.Fatal("FindSemantic() called the matcher despite an exact hit")
	}
}

func TestFindSemanticRecoversAParaphrase(t *testing.T) {
	matcher := &stubMatcher{index: 1, score: 0.81, ok: true}
	match, ok := FindSemantic(context.Background(), sampleCues(), "the rocket left the launch pad", matcher)
	if !ok {
		t.Fatal("FindSemantic() did not fall back to the matcher")
	}
	if match.Exact {
		t.Fatal("a paraphrase was reported as exact")
	}
	if match.Confidence != 0.81 {
		t.Fatalf("Confidence = %v", match.Confidence)
	}
	if match.StartMS != 2000 {
		t.Fatalf("StartMS = %d", match.StartMS)
	}
	if matcher.query != "the rocket left the launch pad" {
		t.Fatalf("matcher received %q", matcher.query)
	}
	for _, window := range matcher.candidates {
		if strings.TrimSpace(window) == "" {
			t.Fatal("matcher received an empty window")
		}
	}
}

func TestFindSemanticRejectsLowConfidence(t *testing.T) {
	matcher := &stubMatcher{index: 1, score: 0.2, ok: true}
	if _, ok := FindSemantic(context.Background(), sampleCues(), "a completely different topic", matcher); ok {
		t.Fatal("FindSemantic() accepted an unrelated window")
	}
}

func TestFindSemanticWithoutAMatcherFallsBackToFind(t *testing.T) {
	if _, ok := FindSemantic(context.Background(), sampleCues(), "the rocket left the launch pad", nil); ok {
		t.Fatal("FindSemantic() matched a paraphrase with no matcher configured")
	}
	if _, ok := FindSemantic(context.Background(), sampleCues(), "accelerating downrange", nil); !ok {
		t.Fatal("FindSemantic() lost exact matching when no matcher is configured")
	}
}

func TestFindSemanticIgnoresUnusableCues(t *testing.T) {
	cues := []Cue{
		{StartMS: -1, EndMS: 10, Text: "broken"},
		{StartMS: 100, EndMS: 100, Text: "zero length"},
		{StartMS: 200, EndMS: 400, Text: "   "},
	}
	matcher := &stubMatcher{index: 0, score: 0.99, ok: true}
	if _, ok := FindSemantic(context.Background(), cues, "anything at all", matcher); ok {
		t.Fatal("FindSemantic() matched against unusable cues")
	}
	if matcher.candidates != nil {
		t.Fatalf("matcher was called with %v", matcher.candidates)
	}
}
