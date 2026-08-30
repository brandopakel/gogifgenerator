package nasa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

func TestSearchNormalizesNASAResultsAndRights(t *testing.T) {
	var requestQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestQuery = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"collection": map[string]any{
			"metadata": map[string]any{"total_hits": 5},
			"links":    []map[string]any{{"rel": "next", "href": "https://images-api.nasa.gov/search?page=3"}},
			"items": []map[string]any{
				{
					"data": []map[string]any{{
						"nasa_id": "AS11-40-5874", "title": "  Apollo   11  ", "media_type": "image",
						"description": "On the Moon", "center": "JSC", "photographer": "Neil Armstrong",
					}},
					"links": []map[string]any{{"rel": "preview", "render": "image", "href": "https://images-assets.nasa.gov/image/AS11-40-5874/preview.jpg"}},
				},
				{
					"data":  []map[string]any{{"nasa_id": "unsafe", "title": "Unsafe", "media_type": "image"}},
					"links": []map[string]any{{"rel": "preview", "render": "image", "href": "https://example.com/preview.jpg"}},
				},
			},
		}})
	}))
	defer server.Close()
	library := testLibrary(t, server)
	page, err := library.Search(context.Background(), provider.Query{Text: "moon", Limit: 2, Cursor: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if requestQuery.Get("q") != "moon" || requestQuery.Get("media_type") != "image,video" || requestQuery.Get("page_size") != "2" || requestQuery.Get("page") != "2" {
		t.Fatalf("query = %v", requestQuery)
	}
	if page.Provider != "nasa" || page.Cursor != "3" || len(page.Results) != 1 {
		t.Fatalf("page = %#v", page)
	}
	result := page.Results[0]
	if result.ExternalID != "AS11-40-5874" || result.Title != "Apollo 11" || result.Kind != media.KindImage {
		t.Fatalf("result = %#v", result)
	}
	if result.SourceURL != "https://images.nasa.gov/details/AS11-40-5874" || result.Attribution != "NASA · JSC · Neil Armstrong" {
		t.Fatalf("source/attribution = %q / %q", result.SourceURL, result.Attribution)
	}
	if result.CommercialUse != media.PermissionUnknown || result.Derivatives != media.PermissionUnknown || result.TransformPolicy != provider.TransformReview {
		t.Fatalf("rights = %#v", result)
	}
	if len(result.AllowedHandling) != 2 || len(result.Restrictions) < 4 || result.LicenseURL != usageGuidelinesURL {
		t.Fatalf("policy metadata = %#v", result)
	}
}

func TestResolveAddsBrowserVideoRenditions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search":
			if r.URL.Query().Get("nasa_id") != "demo_video" {
				t.Fatalf("nasa_id = %q", r.URL.Query().Get("nasa_id"))
			}
			_, _ = w.Write([]byte(`{"collection":{"items":[{"data":[{"nasa_id":"demo_video","title":"Demo video","media_type":"video","center":"HQ"}],"links":[{"rel":"preview","render":"image","href":"https://images-assets.nasa.gov/video/demo_video/preview.jpg"}]}]}}`))
		case r.URL.Path == "/asset/demo_video":
			_, _ = w.Write([]byte(`{"collection":{"items":[
				{"href":"https://images-assets.nasa.gov/video/demo_video/demo_video~small.mp4"},
				{"href":"https://images-assets.nasa.gov/video/demo_video/demo_video~orig.mov"},
				{"href":"http://images-assets.nasa.gov/video/demo_video/demo_video~orig.mp4"},
				{"href":"https://example.com/unsafe.mp4"},
				{"href":"https://images-assets.nasa.gov/video/demo_video/metadata.json"}
			]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	library := testLibrary(t, server)
	result, err := library.Resolve(context.Background(), "demo_video", "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != media.KindVideo || len(result.Renditions) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if result.Renditions[0].ContentType != "video/mp4" || result.Renditions[0].Name != "original" || result.OriginalURL != result.Renditions[0].URL {
		t.Fatalf("renditions = %#v", result.Renditions)
	}
	if !strings.HasPrefix(result.Renditions[0].URL, "https://images-assets.nasa.gov/") {
		t.Fatalf("insecure rendition URL = %q", result.Renditions[0].URL)
	}
}

func TestRejectsInvalidCursorAndID(t *testing.T) {
	library, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Search(context.Background(), provider.Query{Text: "moon", Cursor: "zero"}); !errors.Is(err, provider.ErrInvalidQuery) {
		t.Fatalf("Search() error = %v", err)
	}
	if _, err := library.Resolve(context.Background(), "../escape", "en"); !errors.Is(err, provider.ErrInvalidQuery) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestSearchMapsUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	library := testLibrary(t, server)
	_, err := library.Search(context.Background(), provider.Query{Text: "moon"})
	if !errors.Is(err, provider.ErrUnavailable) || !strings.Contains(err.Error(), "503") {
		t.Fatalf("Search() error = %v", err)
	}
}

func testLibrary(t *testing.T, server *httptest.Server) *Library {
	t.Helper()
	library, err := New(Options{
		SearchEndpoint: server.URL + "/search",
		AssetBase:      server.URL + "/asset/",
		DetailsBase:    "https://images.nasa.gov/details/",
		Client:         server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return library
}
