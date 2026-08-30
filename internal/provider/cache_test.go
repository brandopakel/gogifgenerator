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

type countingProvider struct{ calls int }

func (p *countingProvider) Descriptor() Descriptor {
	return Descriptor{ID: "counting", Label: "Counting"}
}

func (p *countingProvider) Search(_ context.Context, _ Query) (Page, error) {
	p.calls++
	return Page{Provider: "counting", Results: []Result{{Provider: "counting", ExternalID: "1"}}}, nil
}
