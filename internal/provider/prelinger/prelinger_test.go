package prelinger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

func TestSearchScopesQueryAndNormalizesItemRights(t *testing.T) {
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		query := r.URL.Query()
		if got := query.Get("q"); got != `collection:prelinger AND mediatype:movies AND text:"victory \"dance\""` {
			t.Errorf("q = %q", got)
		}
		if query.Get("rows") != "2" || query.Get("page") != "1" || query.Get("output") != "json" || len(query["fl[]"]) != 5 {
			t.Errorf("query = %#v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"responseHeader": map[string]any{"status": 0},
			"response": map[string]any{"numFound": 3, "start": 0, "docs": []any{
				map[string]any{
					"identifier": "VictoryFilm", "title": "<b>Victory</b> Dance", "creator": []string{"Archive Unit"},
					"licenseurl": "http://creativecommons.org/licenses/by/3.0/",
				},
				map[string]any{"identifier": "unsafe/id", "title": "Unsafe"},
			}},
		})
	}))
	defer server.Close()

	archive := newTestProvider(t, server)
	page, err := archive.Search(context.Background(), provider.Query{Text: `victory "dance"`, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if userAgent != "GoGIF-test/contact" {
		t.Fatalf("User-Agent = %q", userAgent)
	}
	if page.Provider != "prelinger" || page.Cursor != "2" || len(page.Results) != 1 {
		t.Fatalf("page = %#v", page)
	}
	result := page.Results[0]
	if result.Kind != media.KindVideo || result.Title != "Victory Dance" || result.Author != "Archive Unit" {
		t.Fatalf("result = %#v", result)
	}
	if result.PreviewURL != server.URL+"/services/img/VictoryFilm" || result.SourceURL != server.URL+"/details/VictoryFilm" {
		t.Fatalf("URLs = %#v", result)
	}
	if result.LicenseID != "cc-by-3.0" || result.LicenseName != "CC BY 3.0" || result.LicenseURL != "https://creativecommons.org/licenses/by/3.0/" {
		t.Fatalf("license = %#v", result)
	}
	if result.CommercialUse != media.PermissionAllowed || result.Derivatives != media.PermissionAllowed || result.TransformPolicy != provider.TransformAllowed {
		t.Fatalf("rights = %#v", result)
	}
	if len(result.AllowedHandling) != 2 || result.AllowedHandling[1] != provider.HandlingDisplay {
		t.Fatalf("allowed handling = %#v", result.AllowedHandling)
	}
}

func TestResolveReturnsStableVideoRenditionsAndCaptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata/AmosAlon1950" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{
				"identifier": "AmosAlon1950", "mediatype": "movies", "collection": []string{"prelinger", "newsreels"},
				"title": "Birthday Newsreel", "creator": "Universal Newsreel",
				"licenseurl": "http://creativecommons.org/licenses/publicdomain/", "runtime": "0:35", "sound": "Sd",
			},
			"files": []any{
				map[string]any{"name": "AmosAlon1950_edit.mp4", "format": "HiRes MPEG4", "size": "9648975", "length": "34.8", "width": "320", "height": "240", "source": "original"},
				map[string]any{"name": "AmosAlon1950_512kb.mp4", "format": "512Kb MPEG4", "size": 2476399, "length": 34.86, "width": 320, "height": 240, "source": "derivative"},
				map[string]any{"name": "AmosAlon1950.mp4", "format": "h.264", "size": "3570431", "length": "34.87", "width": "640", "height": "480", "source": "derivative"},
				map[string]any{"name": "AmosAlon1950.asr.srt", "format": "SubRip", "size": "697"},
				map[string]any{"name": "AmosAlon1950.fr.vtt", "format": "Web Video Text Tracks", "size": "777"},
				map[string]any{"name": "../unsafe.mp4", "format": "h.264"},
			},
		})
	}))
	defer server.Close()

	archive := newTestProvider(t, server)
	result, err := archive.Resolve(context.Background(), "AmosAlon1950", "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Renditions) != 3 || result.Renditions[0].Name != "h.264" {
		t.Fatalf("renditions = %#v", result.Renditions)
	}
	if result.OriginalURL != server.URL+"/download/AmosAlon1950/AmosAlon1950.mp4" || result.ContentType != "video/mp4" {
		t.Fatalf("primary rendition = %#v", result)
	}
	if result.DurationMS != 34870 || result.Width != 640 || result.Height != 480 || !result.HasAudio {
		t.Fatalf("video metadata = %#v", result)
	}
	if len(result.Captions) != 2 || result.Captions[0].Language != "en" || result.Captions[1].Language != "fr" {
		t.Fatalf("captions = %#v", result.Captions)
	}
	if result.LicenseID != "public-domain" || result.TransformPolicy != provider.TransformAllowed {
		t.Fatalf("rights = %#v", result)
	}
	if strings.Contains(result.OriginalURL, "../") || len(result.AllowedHandling) != 2 {
		t.Fatalf("handling = %#v", result)
	}
}

func TestResolveQuoteFetchesSelectedCaptionAndAddsTimecode(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/metadata/QuoteFilm":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{
					"identifier": "QuoteFilm", "mediatype": "movies", "collection": "prelinger",
					"title": "Quote Film", "licenseurl": "https://creativecommons.org/publicdomain/mark/1.0/",
				},
				"files": []any{
					map[string]any{"name": "QuoteFilm.mp4", "format": "h.264", "length": "20", "source": "derivative"},
					map[string]any{"name": "QuoteFilm.en.vtt", "format": "Web Video Text Tracks", "size": "180"},
				},
			})
		case "/download/QuoteFilm/QuoteFilm.en.vtt":
			if !strings.Contains(r.Header.Get("Accept"), "text/vtt") {
				t.Errorf("Accept = %q", r.Header.Get("Accept"))
			}
			_, _ = w.Write([]byte("WEBVTT\n\n00:00:07.000 --> 00:00:09.500\nWe actually shipped it.\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	archive := newTestProvider(t, server)
	result, err := archive.ResolveQuote(context.Background(), "QuoteFilm", "en-US", "actually shipped it")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0] != "/metadata/QuoteFilm" || requests[1] != "/download/QuoteFilm/QuoteFilm.en.vtt" {
		t.Fatalf("requests = %#v", requests)
	}
	if result.QuoteMatch == nil || !result.QuoteMatch.Exact || result.QuoteMatch.StartMS != 7000 || result.QuoteMatch.EndMS != 9500 {
		t.Fatalf("quote match = %#v", result.QuoteMatch)
	}
	if result.OriginalURL != server.URL+"/download/QuoteFilm/QuoteFilm.mp4" {
		t.Fatalf("original URL = %q", result.OriginalURL)
	}
}

func TestResolveQuoteKeepsPlayableItemWhenCaptionsAreMalformed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/metadata/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"identifier": "BrokenCaptions", "mediatype": "movies", "collection": "prelinger"},
				"files": []any{
					map[string]any{"name": "film.mp4", "format": "h.264"},
					map[string]any{"name": "film.vtt", "format": "Web Video Text Tracks"},
				},
			})
			return
		}
		_, _ = w.Write([]byte("not webvtt"))
	}))
	defer server.Close()

	archive := newTestProvider(t, server)
	result, err := archive.ResolveQuote(context.Background(), "BrokenCaptions", "en", "some quote")
	if err != nil || result.QuoteMatch != nil || len(result.Renditions) != 1 {
		t.Fatalf("result = %#v; error = %v", result, err)
	}
}

func TestResolveRejectsItemsOutsidePrelinger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{"identifier": "OtherFilm", "mediatype": "movies", "collection": []string{"opensource_movies"}},
		})
	}))
	defer server.Close()
	archive := newTestProvider(t, server)
	_, err := archive.Resolve(context.Background(), "OtherFilm", "en")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestResolveRejectsInvalidIdentifierBeforeNetwork(t *testing.T) {
	archive, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = archive.Resolve(context.Background(), "../metadata", "en")
	if !errors.Is(err, provider.ErrInvalidQuery) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestClassifyLicense(t *testing.T) {
	tests := []struct {
		url         string
		commercial  media.Permission
		derivatives media.Permission
		shareAlike  bool
		policy      provider.TransformPolicy
	}{
		{"http://creativecommons.org/licenses/publicdomain/", media.PermissionAllowed, media.PermissionAllowed, false, provider.TransformAllowed},
		{"https://creativecommons.org/licenses/by/4.0/", media.PermissionAllowed, media.PermissionAllowed, false, provider.TransformAllowed},
		{"https://creativecommons.org/licenses/by-sa/4.0/", media.PermissionAllowed, media.PermissionAllowed, true, provider.TransformAllowed},
		{"https://creativecommons.org/licenses/by-nc/4.0/", media.PermissionProhibited, media.PermissionAllowed, false, provider.TransformReview},
		{"https://creativecommons.org/licenses/by-nd/4.0/", media.PermissionAllowed, media.PermissionProhibited, false, provider.TransformReference},
		{"", media.PermissionUnknown, media.PermissionUnknown, false, provider.TransformReview},
	}
	for _, test := range tests {
		classification := classifyLicense(test.url)
		if classification.Commercial != test.commercial || classification.Derivatives != test.derivatives || classification.ShareAlike != test.shareAlike || classification.TransformPolicy != test.policy {
			t.Fatalf("classifyLicense(%q) = %#v", test.url, classification)
		}
	}
}

func TestSearchRejectsInvalidCursor(t *testing.T) {
	archive, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = archive.Search(context.Background(), provider.Query{Text: "dance", Cursor: "page-two"})
	if !errors.Is(err, provider.ErrInvalidQuery) {
		t.Fatalf("Search() error = %v", err)
	}
}

func newTestProvider(t *testing.T, server *httptest.Server) *Prelinger {
	t.Helper()
	archive, err := New(Options{
		SearchEndpoint: server.URL + "/advancedsearch.php", MetadataBase: server.URL + "/metadata/",
		DetailsBase: server.URL + "/details/", DownloadBase: server.URL + "/download/",
		ImageBase: server.URL + "/services/img/", UserAgent: "GoGIF-test/contact", Client: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return archive
}
