package yarn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

const testClipID = "bbdb6c42-1fa4-44a5-8728-07529eafb138"

func TestSearchBuildsLinkOnlyOfficialSearch(t *testing.T) {
	page, err := (Yarn{}).Search(context.Background(), provider.Query{Text: "we shipped it", Limit: 12})
	if err != nil {
		t.Fatal(err)
	}
	if page.Provider != "yarn" || page.Cursor != "" || len(page.Results) != 1 {
		t.Fatalf("page = %#v", page)
	}
	result := page.Results[0]
	if result.SourceURL != "https://getyarn.io/yarn-find?text=we+shipped+it" {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	if result.PreviewURL != "" || result.OriginalURL != "" || result.ContentType != "text/html" {
		t.Fatalf("link result unexpectedly exposes provider media: %#v", result)
	}
	if result.Kind != media.KindClip || result.CommercialUse != media.PermissionUnknown || result.Derivatives != media.PermissionUnknown {
		t.Fatalf("rights = %#v", result)
	}
	if result.TransformPolicy != provider.TransformReference || len(result.AllowedHandling) != 1 || result.AllowedHandling[0] != provider.HandlingLink {
		t.Fatalf("handling = %#v", result)
	}
	if !strings.HasPrefix(result.ExternalID, "search-") {
		t.Fatalf("ExternalID = %q", result.ExternalID)
	}
}

func TestSearchRecognizesExactClipURLWithoutDerivingMediaURL(t *testing.T) {
	page, err := (Yarn{}).Search(context.Background(), provider.Query{Text: "https://getyarn.io/yarn-clip/" + strings.ToUpper(testClipID)})
	if err != nil {
		t.Fatal(err)
	}
	result := page.Results[0]
	if result.ExternalID != testClipID || result.SourceURL != "https://getyarn.io/yarn-clip/"+testClipID {
		t.Fatalf("result = %#v", result)
	}
	if result.PreviewURL != "" || result.OriginalURL != "" || len(result.Renditions) != 0 {
		t.Fatalf("exact clip leaked an unsupported direct rendition: %#v", result)
	}
}

func TestResolveAcceptsOnlyCanonicalClipIDs(t *testing.T) {
	result, err := (Yarn{}).Resolve(context.Background(), testClipID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceURL != "https://getyarn.io/yarn-clip/"+testClipID {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	for _, input := range []string{"../escape", "search-anything", "not-a-uuid"} {
		if _, err := (Yarn{}).Resolve(context.Background(), input, "en"); !errors.Is(err, provider.ErrInvalidQuery) {
			t.Fatalf("Resolve(%q) error = %v", input, err)
		}
	}
}

func TestSearchRejectsCursorAndInvalidQuery(t *testing.T) {
	for _, query := range []provider.Query{{Text: ""}, {Text: "hello", Cursor: "2"}} {
		if _, err := (Yarn{}).Search(context.Background(), query); !errors.Is(err, provider.ErrInvalidQuery) {
			t.Fatalf("Search(%#v) error = %v", query, err)
		}
	}
}
