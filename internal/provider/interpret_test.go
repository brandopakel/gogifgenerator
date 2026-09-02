package provider

import (
	"context"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
)

type stubProvider struct {
	lastQuery Query
	page      Page
	err       error
}

func (s *stubProvider) Descriptor() Descriptor { return Descriptor{ID: "stub", Label: "Stub"} }

func (s *stubProvider) Search(_ context.Context, query Query) (Page, error) {
	s.lastQuery = query
	if s.err != nil {
		return Page{}, s.err
	}
	return s.page, nil
}

type reverseRanker struct{ calledWith string }

func (r *reverseRanker) Order(_ context.Context, query string, candidates []string) []int {
	r.calledWith = query
	order := make([]int, len(candidates))
	for index := range order {
		order[index] = len(candidates) - 1 - index
	}
	return order
}

type brokenRanker struct{}

func (brokenRanker) Order(_ context.Context, _ string, candidates []string) []int {
	return []int{0, 0}
}

func page(titles ...string) Page {
	results := make([]Result, len(titles))
	for index, title := range titles {
		results[index] = Result{Provider: "stub", ExternalID: title, Title: title, Kind: media.KindImage}
	}
	return Page{Provider: "stub", Results: results}
}

func TestInterpretedDistillsSentenceQueries(t *testing.T) {
	next := &stubProvider{page: page("one")}
	if _, err := (Interpreted{Next: next}).Search(context.Background(),
		Query{Text: "please make me a gif of a rocket launching from a desert pad"}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if next.lastQuery.Text == "please make me a gif of a rocket launching from a desert pad" {
		t.Fatal("Search() forwarded the sentence unchanged")
	}
	for _, unwanted := range []string{"please", "gif"} {
		if contains(next.lastQuery.Text, unwanted) {
			t.Fatalf("distilled query kept %q: %q", unwanted, next.lastQuery.Text)
		}
	}
	if !contains(next.lastQuery.Text, "rocket") {
		t.Fatalf("distilled query lost the subject: %q", next.lastQuery.Text)
	}
}

func TestInterpretedForwardsShortQueriesVerbatim(t *testing.T) {
	next := &stubProvider{page: page("one")}
	if _, err := (Interpreted{Next: next}).Search(context.Background(), Query{Text: "apollo 11 launch"}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if next.lastQuery.Text != "apollo 11 launch" {
		t.Fatalf("short query was rewritten: %q", next.lastQuery.Text)
	}
}

func TestInterpretedReordersResultsAgainstTheOriginalQuery(t *testing.T) {
	next := &stubProvider{page: page("first", "second", "third")}
	ranker := &reverseRanker{}
	result, err := (Interpreted{Next: next, Ranker: ranker}).Search(context.Background(),
		Query{Text: "please make me a gif of a rocket launching from a desert pad"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := []string{result.Results[0].Title, result.Results[2].Title}; got[0] != "third" || got[1] != "first" {
		t.Fatalf("results were not reordered: %v", got)
	}
	// Ranking compares against what the user actually meant, not the
	// keyword-shaped query sent upstream.
	if !contains(ranker.calledWith, "please") {
		t.Fatalf("ranker received the distilled query: %q", ranker.calledWith)
	}
}

func TestInterpretedKeepsProviderOrderOnMalformedRanking(t *testing.T) {
	next := &stubProvider{page: page("first", "second")}
	result, err := (Interpreted{Next: next, Ranker: brokenRanker{}}).Search(context.Background(), Query{Text: "rockets"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Results) != 2 || result.Results[0].Title != "first" {
		t.Fatalf("malformed ranking changed the page: %#v", result.Results)
	}
}

func TestInterpretedPropagatesProviderErrors(t *testing.T) {
	next := &stubProvider{err: ErrUnavailable}
	if _, err := (Interpreted{Next: next}).Search(context.Background(), Query{Text: "rockets"}); err == nil {
		t.Fatal("Search() expected the provider error")
	}
	if _, err := (Interpreted{Next: next}).Search(context.Background(), Query{Text: ""}); err == nil {
		t.Fatal("Search() expected a query validation error")
	}
}

func TestInterpretedResolveDelegatesAndRejectsUnsupported(t *testing.T) {
	if _, err := (Interpreted{Next: &stubProvider{}}).Resolve(context.Background(), "id", "en"); err != ErrUnsupported {
		t.Fatalf("Resolve() error = %v", err)
	}
	if _, err := (Interpreted{Next: &stubProvider{}}).ResolveQuote(context.Background(), "id", "en", "quote"); err != ErrUnsupported {
		t.Fatalf("ResolveQuote() error = %v", err)
	}
}

func TestDocumentTextExcludesRightsBoilerplate(t *testing.T) {
	document := documentText(Result{
		Title: "Moon landing", Description: "Apollo 11 footage", Author: "NASA",
		LicenseName: "Public Domain", LicenseURL: "https://example.com/license",
		SourceURL: "https://example.com/item",
	})
	for _, unwanted := range []string{"Public Domain", "example.com"} {
		if contains(document, unwanted) {
			t.Fatalf("documentText included %q: %q", unwanted, document)
		}
	}
	if !contains(document, "Moon landing") || !contains(document, "Apollo 11") {
		t.Fatalf("documentText dropped searchable text: %q", document)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}
