package wikimedia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

func TestSearchNormalizesResultsAndRights(t *testing.T) {
	var receivedUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		query := r.URL.Query()
		for key, want := range map[string]string{
			"action": "query", "generator": "search", "gsrsearch": "victory dance",
			"gsrnamespace": "6", "gsrlimit": "2", "prop": "imageinfo",
			"format": "json", "formatversion": "2", "iiextmetadatalanguage": "en",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("query[%q] = %q, want %q", key, got, want)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"continue": map[string]any{"gsroffset": 2},
			"query": map[string]any{"pages": []any{
				map[string]any{
					"pageid": 42,
					"title":  "File:Victory.gif",
					"imageinfo": []any{map[string]any{
						"url": "https://upload.wikimedia.org/wikipedia/commons/a/a1/Victory.gif", "descriptionurl": "https://commons.wikimedia.org/wiki/File:Victory.gif",
						"thumburl": "https://upload.wikimedia.org/wikipedia/commons/thumb/a/a1/Victory.gif/480px-Victory.gif", "mime": "image/gif", "thumbmime": "image/gif",
						"mediatype": "BITMAP", "width": 640, "height": 360, "size": 12345,
						"extmetadata": map[string]any{
							"ImageDescription": map[string]any{"value": "<b>A happy</b> dance &amp; cheer"},
							"Artist":           map[string]any{"value": "<a href='x'>Ada</a>"},
							"Credit":           map[string]any{"value": "Ada"},
							"License":          map[string]any{"value": "cc-by-sa-4.0"},
							"LicenseShortName": map[string]any{"value": "CC BY-SA 4.0"},
							"LicenseUrl":       map[string]any{"value": "https://creativecommons.org/licenses/by-sa/4.0/"},
							"Restrictions":     map[string]any{"value": "trademarked|personality rights"},
						},
					}},
				},
				map[string]any{
					"pageid": 43, "title": "File:Audio.ogg",
					"imageinfo": []any{map[string]any{
						"url": "https://upload.wikimedia.org/audio.ogg", "descriptionurl": "https://commons.wikimedia.org/wiki/File:Audio.ogg",
						"mime": "audio/ogg", "mediatype": "AUDIO",
					}},
				},
			}},
		})
	}))
	defer server.Close()

	commons, err := New(Options{Endpoint: server.URL, UserAgent: "GoGIF-test/contact"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := commons.Search(context.Background(), provider.Query{Text: "victory dance", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if receivedUserAgent != "GoGIF-test/contact" {
		t.Fatalf("User-Agent = %q", receivedUserAgent)
	}
	if page.Provider != "wikimedia" || page.Cursor != "2" || len(page.Results) != 1 {
		t.Fatalf("page = %#v", page)
	}
	result := page.Results[0]
	if result.Kind != media.KindGIF || result.Title != "Victory.gif" || result.Description != "A happy dance & cheer" {
		t.Fatalf("normalized result = %#v", result)
	}
	if result.Author != "Ada" || result.Attribution != "Victory.gif · Ada · CC BY-SA 4.0" {
		t.Fatalf("attribution = %q; author = %q", result.Attribution, result.Author)
	}
	if result.CommercialUse != media.PermissionAllowed || result.Derivatives != media.PermissionAllowed || !result.ShareAlike || result.TransformPolicy != provider.TransformAllowed {
		t.Fatalf("rights = %#v", result)
	}
	if len(result.Renditions) != 2 || len(result.AllowedHandling) != 3 || result.AllowedHandling[2] != provider.HandlingTemporaryTransform {
		t.Fatalf("media handling = %#v; renditions = %#v", result.AllowedHandling, result.Renditions)
	}
	if len(result.Restrictions) != 2 {
		t.Fatalf("restrictions = %#v", result.Restrictions)
	}
}

func TestSearchRejectsInvalidCursor(t *testing.T) {
	commons, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = commons.Search(context.Background(), provider.Query{Text: "dance", Cursor: "not-a-number"})
	if err == nil {
		t.Fatal("Search() error = nil")
	}
}

func TestResolveRevalidatesProviderItemByPageID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("pageids"); got != "42" {
			t.Errorf("pageids = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"pages": []any{map[string]any{
			"pageid": 42, "title": "File:Reusable.png",
			"imageinfo": []any{map[string]any{
				"url": "https://upload.wikimedia.org/reusable.png", "descriptionurl": "https://commons.wikimedia.org/wiki/File:Reusable.png",
				"mime": "image/png", "mediatype": "BITMAP",
				"extmetadata": map[string]any{
					"License": map[string]any{"value": "cc0"}, "LicenseShortName": map[string]any{"value": "CC0 1.0"},
				},
			}},
		}}}})
	}))
	defer server.Close()
	commons, err := New(Options{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := commons.Resolve(context.Background(), "42", "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExternalID != "42" || result.TransformPolicy != provider.TransformAllowed || result.OriginalURL != "https://upload.wikimedia.org/reusable.png" {
		t.Fatalf("Resolve() = %#v", result)
	}
}

func TestNormalizeUsesRasterThumbnailAsDrawingReference(t *testing.T) {
	result, ok := normalize(apiPage{
		PageID: 51,
		Title:  "File:Reusable.svg",
		ImageInfo: []imageInfo{{
			URL:         "https://upload.wikimedia.org/reusable.svg",
			Description: "https://commons.wikimedia.org/wiki/File:Reusable.svg",
			ThumbURL:    "https://upload.wikimedia.org/reusable-480px.png",
			MIME:        "image/svg+xml",
			ThumbMIME:   "image/png",
			MediaType:   "DRAWING",
			ExtMetadata: map[string]metadataValue{
				"License": {Value: "cc0"}, "LicenseShortName": {Value: "CC0 1.0"},
			},
		}},
	})
	if !ok {
		t.Fatal("normalize() rejected reusable SVG")
	}
	if result.OriginalURL != "https://upload.wikimedia.org/reusable.svg" || result.ReferenceURL != "https://upload.wikimedia.org/reusable-480px.png" {
		t.Fatalf("URLs = original %q, reference %q", result.OriginalURL, result.ReferenceURL)
	}
}

func TestClassifyLicense(t *testing.T) {
	tests := []struct {
		license     string
		commercial  media.Permission
		derivatives media.Permission
		shareAlike  bool
		policy      provider.TransformPolicy
	}{
		{"Public domain", media.PermissionAllowed, media.PermissionAllowed, false, provider.TransformAllowed},
		{"CC0 1.0", media.PermissionAllowed, media.PermissionAllowed, false, provider.TransformAllowed},
		{"CC BY 4.0", media.PermissionAllowed, media.PermissionAllowed, false, provider.TransformAllowed},
		{"CC BY-SA 4.0", media.PermissionAllowed, media.PermissionAllowed, true, provider.TransformAllowed},
		{"CC BY-NC 4.0", media.PermissionProhibited, media.PermissionAllowed, false, provider.TransformReview},
		{"CC BY-ND 4.0", media.PermissionAllowed, media.PermissionProhibited, false, provider.TransformReference},
		{"CC BY-NC-ND 4.0", media.PermissionProhibited, media.PermissionProhibited, false, provider.TransformReference},
		{"Unknown license", media.PermissionUnknown, media.PermissionUnknown, false, provider.TransformReview},
	}
	for _, test := range tests {
		t.Run(test.license, func(t *testing.T) {
			commercial, derivatives, shareAlike, policy := classifyLicense("", test.license)
			if commercial != test.commercial || derivatives != test.derivatives || shareAlike != test.shareAlike || policy != test.policy {
				t.Fatalf("classifyLicense() = %q, %q, %v, %q", commercial, derivatives, shareAlike, policy)
			}
		})
	}
}
