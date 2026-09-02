package subtitle

import (
	"context"
	"math"
	"strings"
)

// Matcher scores transcript windows against a quote. It is satisfied by
// semantic.Ranker; the interface keeps this package free of any embedding
// dependency.
type Matcher interface {
	Best(ctx context.Context, query string, candidates []string) (int, float64, bool)
}

const (
	// semanticWindowCues is how many neighbouring cues form one candidate.
	// Spoken lines are split arbitrarily across cues, so a single cue is
	// often half a sentence.
	semanticWindowCues = 3
	// minSemanticScore keeps a loose paraphrase from jumping the playhead to
	// an unrelated moment. Below it the caller keeps its own result.
	minSemanticScore = 0.55
	maxSemanticCues  = 600
)

// FindSemantic locates the moment a quote refers to when the words themselves
// do not appear in the transcript. Find stays the authority: it is tried
// first, and this runs only when exact and fuzzy token matching both fail.
//
// The returned match is never marked Exact, and its confidence is the
// similarity score, so callers and the UI can tell a paraphrase from a
// verbatim hit.
func FindSemantic(ctx context.Context, cues []Cue, quote string, matcher Matcher) (Match, bool) {
	if match, ok := Find(cues, quote); ok {
		return match, true
	}
	if matcher == nil || strings.TrimSpace(quote) == "" {
		return Match{}, false
	}
	usable := make([]int, 0, len(cues))
	for index, cue := range cues {
		if cue.StartMS < 0 || cue.EndMS <= cue.StartMS || strings.TrimSpace(cue.Text) == "" {
			continue
		}
		usable = append(usable, index)
		if len(usable) == maxSemanticCues {
			break
		}
	}
	if len(usable) == 0 {
		return Match{}, false
	}

	windows := make([]string, 0, len(usable))
	starts := make([]int, 0, len(usable))
	ends := make([]int, 0, len(usable))
	for position := range usable {
		last := min(position+semanticWindowCues, len(usable))
		parts := make([]string, 0, semanticWindowCues)
		for offset := position; offset < last; offset++ {
			parts = append(parts, strings.TrimSpace(cues[usable[offset]].Text))
		}
		window := strings.Join(parts, " ")
		if strings.TrimSpace(window) == "" {
			continue
		}
		windows = append(windows, window)
		starts = append(starts, usable[position])
		ends = append(ends, usable[last-1])
	}
	if len(windows) == 0 {
		return Match{}, false
	}

	index, score, ok := matcher.Best(ctx, quote, windows)
	if !ok || index < 0 || index >= len(windows) || score < minSemanticScore {
		return Match{}, false
	}
	return buildMatch(cues, starts[index], ends[index], false, math.Round(score*1000)/1000), true
}
