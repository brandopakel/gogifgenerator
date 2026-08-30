package provider

import (
	"context"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestCachedSearchReusesPage(t *testing.T) {
	fake := &countingProvider{}
	cached := Cached{Next: fake, KV: store.NewMemoryKV(), TTL: time.Minute}
	query := Query{Text: "victory"}
	for range 2 {
		page, err := cached.Search(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Results) != 1 {
			t.Fatalf("results = %d", len(page.Results))
		}
	}
	if fake.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", fake.calls)
	}
}

func TestCachedReportsUnsupportedResolveOperations(t *testing.T) {
	cached := Cached{Next: &countingProvider{}, KV: store.NewMemoryKV(), TTL: time.Minute}
	if _, err := cached.Resolve(context.Background(), "1", "en"); err != ErrUnsupported {
		t.Fatalf("Resolve() error = %v", err)
	}
	if _, err := cached.ResolveQuote(context.Background(), "1", "en", "hello"); err != ErrUnsupported {
		t.Fatalf("ResolveQuote() error = %v", err)
	}
}

type countingProvider struct{ calls int }

func (p *countingProvider) Descriptor() Descriptor {
	return Descriptor{ID: "counting", Label: "Counting"}
}

func (p *countingProvider) Search(_ context.Context, _ Query) (Page, error) {
	p.calls++
	return Page{Provider: "counting", Results: []Result{{Provider: "counting", ExternalID: "1"}}}, nil
}
