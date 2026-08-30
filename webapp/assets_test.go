package webapp

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEditorShellIncludesAccessibleMilestoneOneControls(t *testing.T) {
	data, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, marker := range []string{
		`id="copy-button"`, `id="undo-button"`, `id="redo-button"`, `id="save-draft-button"`,
		`id="trim-start-control"`, `id="target-size-control"`, `id="search-scope"`, `<option value="stickers">Stickers</option>`, `role="slider"`, `aria-live="assertive"`,
	} {
		if !strings.Contains(page, marker) {
			t.Fatalf("index.html does not contain %q", marker)
		}
	}
}

func TestStickerSearchUsesDedicatedGIPHYEndpoint(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, marker := range []string{
		"searchScope === 'stickers'", "searchGiphy(query, apiKey, cursor, 'stickers')", "'giphy-stickers'", "GIPHY Stickers",
		"stickers ? 'stickers' : 'gifs'",
	} {
		if !strings.Contains(script, marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
}

func TestSearchUsesActualGIFsAndReactiveInput(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, marker := range []string{
		"preview.url || item.images.original.url", "media.append(image)", "item.kind === 'sticker' ? 'Open Sticker' : 'Open GIF'",
		"function queueSearch()", "function clearSearchResults()", "if (state.mode === 'search') queueSearch()",
		"function loadMoreSearchResults", "new IntersectionObserver", "offset: String(offset)", "payload.cursor || ''", "clearTimeout(searchContinuationTimer)",
	} {
		if !strings.Contains(script, marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
	if strings.Contains(script, "preview.webp || preview.url") || strings.Contains(script, "link.append(image)") {
		t.Fatal("search preview falls back to a non-GIF rendition or wraps the touch target in a source link")
	}
}

func TestShellKeepsExplanatoryCopyOutOfTheInterface(t *testing.T) {
	data, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, marker := range []string{
		"hero-copy", "result-note", "result-touch-hint", "search-scope-note", "editor-help", "ONE PROMPT", "CURRENT CUT", "source discarded",
		`class="suggestions"`, "engine-badge", "hero-title", "Make a moment<br>",
	} {
		if strings.Contains(page, marker) {
			t.Fatalf("index.html still contains explanatory UI copy marker %q", marker)
		}
	}
}

func TestStylesRespectReducedMotionAndVisibleFocus(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(data)
	for _, marker := range []string{"prefers-reduced-motion: reduce", ":focus-visible", "outline:", "-webkit-touch-callout: default"} {
		if !strings.Contains(styles, marker) {
			t.Fatalf("app.css does not contain %q", marker)
		}
	}
}

func TestServiceWorkerDoesNotInterceptProviderMedia(t *testing.T) {
	data, err := fs.ReadFile(Files(), "service-worker.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "url.origin !== self.location.origin") {
		t.Fatal("service worker does not leave cross-origin provider media requests to the browser")
	}
}

func TestReferenceRemixRecognizesEnabledQualityPipeline(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, marker := range []string{"quality_pipeline?.enabled", "quality_pipeline?.supports_references"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
}

func TestResultActionsFallBackToShareableGIFLink(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, marker := range []string{
		"response.headers.get('Location')", "function copyResultLink()", "navigator.clipboard.writeText(state.resultURL)",
		"a shareable GIF link was copied", "its GIF link was copied",
	} {
		if !strings.Contains(script, marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
}
