package gifcities

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

func TestSearchNormalizesResultsConservatively(t *testing.T) {
	var receivedUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		if got := r.URL.Query().Get("q"); got != "celebration" {
			t.Errorf("q = %q", got)
		}
		_ = json.NewEncoder(w).Encode([]apiItem{
			{
				URLText: "a celebration of life earth globe", Checksum: "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567",
				Width: 50, Height: 50,
				Page: "https://web.archive.org/web/20090723104817/http://example.com/page.html",
			},
			{
				URLText: "unsafe source", Checksum: "BCDEFGHIJKLMNOPQRSTUVWXY234567A",
				Page: "https://example.com/not-wayback.html",
			},
		})
	}))
	defer server.Close()

	cities, err := New(Options{Endpoint: server.URL, UserAgent: "GoGIF-test/contact"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := cities.Search(context.Background(), provider.Query{Text: "celebration", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if receivedUserAgent != "GoGIF-test/contact" {
		t.Fatalf("User-Agent = %q", receivedUserAgent)
	}
	if page.Provider != "gifcities" || len(page.Results) != 1 {
		t.Fatalf("page = %#v", page)
	}
	result := page.Results[0]
	if result.ExternalID != "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" || result.Kind != media.KindGIF {
		t.Fatalf("result identity = %#v", result)
	}
	if result.PreviewURL != "https://blob.gifcities.org/gifcities/ABCDEFGHIJKLMNOPQRSTUVWXYZ234567.gif" || result.SourceURL == "" {
		t.Fatalf("result URLs = %#v", result)
	}
	if result.CommercialUse != media.PermissionUnknown || result.Derivatives != media.PermissionUnknown || result.TransformPolicy != provider.TransformReview {
		t.Fatalf("result rights = %#v", result)
	}
	if len(result.Renditions) != 1 || result.Renditions[0].ContentType != "image/gif" {
		t.Fatalf("renditions = %#v", result.Renditions)
	}
	if len(result.AllowedHandling) != 2 || result.AllowedHandling[1] != provider.HandlingDisplay {
		t.Fatalf("allowed handling = %#v", result.AllowedHandling)
	}
}

func TestSearchAppliesLimitAfterDiscardingInvalidResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]apiItem{
			{Checksum: "invalid", Page: "https://web.archive.org/web/1/http://example.com", URLText: "invalid"},
			{Checksum: "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", Page: "https://web.archive.org/web/1/http://example.com", URLText: "one"},
			{Checksum: "BCDEFGHIJKLMNOPQRSTUVWXY234567A", Page: "https://web.archive.org/web/2/http://example.com", URLText: "two"},
		})
	}))
	defer server.Close()
	cities, err := New(Options{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	page, err := cities.Search(context.Background(), provider.Query{Text: "anything", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].Title != "one" {
		t.Fatalf("results = %#v", page.Results)
	}
}

func TestSearchRejectsCursor(t *testing.T) {
	cities, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cities.Search(context.Background(), provider.Query{Text: "dance", Cursor: "12"})
	if !errors.Is(err, provider.ErrInvalidQuery) {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestSearchMapsUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cities, err := New(Options{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cities.Search(context.Background(), provider.Query{Text: "dance"})
	if !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestNewRejectsRelativeURLs(t *testing.T) {
	if _, err := New(Options{Endpoint: "/search"}); err == nil {
		t.Fatal("New() endpoint error = nil")
	}
	if _, err := New(Options{BlobBase: "/gifs/"}); err == nil {
		t.Fatal("New() blob base error = nil")
	}
}
