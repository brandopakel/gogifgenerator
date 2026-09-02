package provider

import (
	"context"
	"log/slog"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/intent"
)

// Ranker reorders candidate documents by their similarity to a query. It is
// satisfied by semantic.Ranker; the interface keeps this package free of any
// embedding dependency.
type Ranker interface {
	Order(ctx context.Context, query string, candidates []string) []int
}

// minDistilledWords is the point at which a query stops looking like a
// catalog search and starts looking like a sentence. Short queries are
// forwarded exactly as typed because archive indexes match them well.
const minDistilledWords = 6

// Interpreted reads the query before searching and orders the results by
// meaning afterwards.
//
// Archive catalogs index concrete nouns, so "please make me a gif of a rocket
// launching" finds nothing while "rocket launching" finds the reel. Ranking
// then repairs the opposite problem: a provider's keyword relevance puts an
// unrelated title first because it happened to repeat a common word.
//
// Both steps are optional and fail open. A nil Ranker still distills the
// query; a ranking failure returns the provider's own order untouched.
type Interpreted struct {
	Next   Provider
	Ranker Ranker
	Logger *slog.Logger
}

func (i Interpreted) Descriptor() Descriptor {
	return i.Next.Descriptor()
}

func (i Interpreted) Search(ctx context.Context, query Query) (Page, error) {
	normalized, err := query.Normalize()
	if err != nil {
		return Page{}, err
	}
	original := normalized.Text
	brief := intent.Interpret(original)
	normalized.Text = distill(original, brief)

	page, err := i.Next.Search(ctx, normalized)
	if err != nil {
		return Page{}, err
	}
	if i.Ranker == nil || len(page.Results) < 2 {
		return page, nil
	}
	candidates := make([]string, len(page.Results))
	for index, result := range page.Results {
		candidates[index] = documentText(result)
	}
	order := i.Ranker.Order(ctx, original, candidates)
	if len(order) != len(page.Results) {
		if i.Logger != nil {
			i.Logger.Warn("semantic ranking returned an incomplete order",
				"provider", i.Descriptor().ID, "results", len(page.Results), "ordered", len(order))
		}
		return page, nil
	}
	ranked := make([]Result, 0, len(page.Results))
	seen := make(map[int]bool, len(order))
	for _, index := range order {
		if index < 0 || index >= len(page.Results) || seen[index] {
			// A malformed permutation must not drop or duplicate a result.
			return page, nil
		}
		seen[index] = true
		ranked = append(ranked, page.Results[index])
	}
	page.Results = ranked
	return page, nil
}

func (i Interpreted) Resolve(ctx context.Context, externalID, locale string) (Result, error) {
	resolver, ok := i.Next.(Resolver)
	if !ok {
		return Result{}, ErrUnsupported
	}
	return resolver.Resolve(ctx, externalID, locale)
}

func (i Interpreted) ResolveQuote(ctx context.Context, externalID, locale, quote string) (Result, error) {
	resolver, ok := i.Next.(QuoteResolver)
	if !ok {
		return Result{}, ErrUnsupported
	}
	// A quote is matched verbatim against captions, so it is never distilled.
	return resolver.ResolveQuote(ctx, externalID, locale, quote)
}

// distill replaces a sentence-shaped query with its concrete search terms and
// leaves anything shorter exactly as the user typed it.
func distill(original string, brief intent.Brief) string {
	if len(strings.Fields(original)) < minDistilledWords || brief.Empty() {
		return original
	}
	distilled := brief.SearchQuery(original)
	if strings.TrimSpace(distilled) == "" {
		return original
	}
	if len(distilled) > 200 {
		distilled = strings.TrimSpace(distilled[:200])
	}
	return distilled
}

// documentText is what a result "says" for ranking purposes. Rights metadata
// and URLs are excluded so licence boilerplate cannot dominate similarity.
func documentText(result Result) string {
	parts := make([]string, 0, 4)
	if result.Title != "" {
		parts = append(parts, result.Title)
	}
	if result.QuoteMatch != nil && result.QuoteMatch.Text != "" {
		parts = append(parts, result.QuoteMatch.Text)
	}
	if result.Description != "" {
		description := result.Description
		if len([]rune(description)) > 600 {
			description = string([]rune(description)[:600])
		}
		parts = append(parts, description)
	}
	if result.Author != "" {
		parts = append(parts, result.Author)
	}
	if len(parts) == 0 {
		return result.ExternalID
	}
	return strings.Join(parts, ". ")
}
