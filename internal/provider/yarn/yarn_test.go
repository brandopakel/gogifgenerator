package yarn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

const testClipID = "bbdb6c42-1fa4-44a5-8728-07529eafb138"

func TestSearchScrapesPhraseResultsAndMetadata(t *testing.T) {
	var receivedQuery, receivedPage, receivedAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query().Get("text")
		receivedPage = r.URL.Query().Get("page")
		receivedAgent = r.UserAgent()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<article class="clip-wrap" data-show="Cheers">
  <div class="movie-title">Cheers (1982) · S04E02 Woody Goes Belly Up</div>
  <a class="yarn-link" href="/yarn-clip/` + testClipID + `">
    <img src="https://y.yarn.co/` + testClipID + `_screenshot.jpg" alt="Great idea!">
  </a>
  <div class="clip-transcript">Great idea, Woody. I like your thinkin'.</div>
  <span class="duration">2.8s</span>
</article>
<article class="clip-wrap">
  <a href="https://attacker.invalid/yarn-clip/11111111-1111-1111-1111-111111111111">ignore me</a>
</article>
<a rel="next" href="/yarn-find?text=great+idea&amp;page=2">Next</a>
</body></html>`))
	}))
	defer server.Close()

	client := newTestYarn(t, server)
	page, err := client.Search(context.Background(), provider.Query{Text: "great idea", Limit: 12})
	if err != nil {
		t.Fatal(err)
	}
	if receivedQuery != "great idea" || receivedPage != "" || receivedAgent != "GoGIF-test" {
		t.Fatalf("request = query %q, page %q, agent %q", receivedQuery, receivedPage, receivedAgent)
	}
	if page.Provider != "yarn" || page.Cursor != "page=2" || len(page.Results) != 1 {
		t.Fatalf("page = %#v", page)
	}
	result := page.Results[0]
	if result.Provider != "yarn" || result.ExternalID != testClipID || result.Kind != media.KindClip {
		t.Fatalf("identity = %#v", result)
	}
	if result.Title != "Cheers (1982) · S04E02 Woody Goes Belly Up" || result.Description != "Great idea, Woody. I like your thinkin'." {
		t.Fatalf("metadata = %#v", result)
	}
	if result.PreviewURL != "https://y.yarn.co/"+testClipID+"_screenshot.jpg" || result.DurationMS != 2800 {
		t.Fatalf("preview metadata = %#v", result)
	}
	wantSource := server.URL + "/yarn-clip/" + testClipID
	if result.SourceURL != wantSource || result.EmbedURL != wantSource+"/embed?autoplay=false&responsive=true" {
		t.Fatalf("official destinations = %#v", result)
	}
	if result.OriginalURL != "" || len(result.Renditions) != 0 {
		t.Fatalf("scraper exposed unsupported movie bytes: %#v", result)
	}
	if result.QuoteMatch == nil || result.QuoteMatch.Text != result.Description || !result.QuoteMatch.Exact {
		t.Fatalf("quote match = %#v", result.QuoteMatch)
	}
	if result.CommercialUse != media.PermissionUnknown || result.Derivatives != media.PermissionUnknown || result.TransformPolicy != provider.TransformReference {
		t.Fatalf("rights = %#v", result)
	}
	if !handlingEquals(result.AllowedHandling, provider.HandlingLink, provider.HandlingDisplay) {
		t.Fatalf("handling = %#v", result.AllowedHandling)
	}
}

func TestSearchAppliesCursorLimitAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("page"); got != "7" {
			t.Errorf("page = %q", got)
		}
		_, _ = w.Write([]byte(`<div class="clip-card"><a href="/yarn-clip/` + testClipID + `">First</a></div>
<div class="clip-card"><a href="/yarn-clip/` + testClipID + `">Duplicate</a></div>
<div class="clip-card"><a href="/yarn-clip/11111111-1111-1111-1111-111111111111">Second</a></div>`))
	}))
	defer server.Close()

	page, err := newTestYarn(t, server).Search(context.Background(), provider.Query{Text: "hello", Limit: 1, Cursor: "page=7"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].ExternalID != testClipID {
		t.Fatalf("results = %#v", page.Results)
	}
}

func TestSearchReportsCloudflareChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("browser challenge"))
	}))
	defer server.Close()

	_, err := newTestYarn(t, server).Search(context.Background(), provider.Query{Text: "hello"})
	if !errors.Is(err, provider.ErrUnavailable) || !strings.Contains(err.Error(), "browser challenge") {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchRejectsOversizedHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxSearchBytes+1)))
	}))
	defer server.Close()

	_, err := newTestYarn(t, server).Search(context.Background(), provider.Query{Text: "hello"})
	if !errors.Is(err, provider.ErrUnavailable) || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveConstructsEmbedWithoutMediaDownload(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := newTestYarn(t, server)

	result, err := client.Resolve(context.Background(), strings.ToUpper(testClipID), "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExternalID != testClipID || result.SourceURL != server.URL+"/yarn-clip/"+testClipID {
		t.Fatalf("result = %#v", result)
	}
	if result.EmbedURL != result.SourceURL+"/embed?autoplay=false&responsive=true" || result.OriginalURL != "" || len(result.Renditions) != 0 {
		t.Fatalf("embed result = %#v", result)
	}
	for _, input := range []string{"../escape", "search-anything", "not-a-uuid"} {
		if _, err := client.Resolve(context.Background(), input, "en"); !errors.Is(err, provider.ErrInvalidQuery) {
			t.Fatalf("Resolve(%q) error = %v", input, err)
		}
	}
}

func TestSearchRejectsInvalidCursorAndQuery(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := newTestYarn(t, server)
	for _, query := range []provider.Query{
		{Text: ""},
		{Text: "hello", Cursor: "2"},
		{Text: "hello", Cursor: "page=-1"},
		{Text: "hello", Cursor: "redirect=https://attacker.invalid"},
	} {
		if _, err := client.Search(context.Background(), query); !errors.Is(err, provider.ErrInvalidQuery) {
			t.Fatalf("Search(%#v) error = %v", query, err)
		}
	}
}

func TestNewRejectsUnsafeEndpoints(t *testing.T) {
	for _, options := range []Options{
		{SearchEndpoint: "/relative"},
		{SearchEndpoint: "file:///tmp/search"},
		{ClipBase: "https://user:password@example.com/yarn-clip/"},
	} {
		if _, err := New(options); err == nil {
			t.Fatalf("New(%#v) unexpectedly succeeded", options)
		}
	}
}

func newTestYarn(t *testing.T, server *httptest.Server) *Yarn {
	t.Helper()
	client, err := New(Options{
		SearchEndpoint: server.URL + "/yarn-find",
		ClipBase:       server.URL + "/yarn-clip/",
		UserAgent:      "GoGIF-test",
		Client:         server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func handlingEquals(got []provider.HandlingMode, want ...provider.HandlingMode) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
