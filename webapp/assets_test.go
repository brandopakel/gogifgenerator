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
	if strings.Contains(styles, "textarea:focus-visible") {
		t.Fatal("textarea has a second focus outline in addition to the prompt form")
	}
	if !strings.Contains(styles, "calc(100svh - 230px)") {
		t.Fatal("preview is not constrained to the visible viewport")
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

func TestCreateModeRequestsSemanticGenerationExplicitly(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`value="semantic"`, "Realistic AI · subject-aware", `value="studio"`, "Studio Local · Blender + Unity + Unreal"} {
		if !strings.Contains(string(index), marker) {
			t.Fatalf("index.html does not contain %q", marker)
		}
	}
	for _, marker := range []string{"generation_mode: generationMode", "image_generator?.semantic", "Realistic AI · setup required", "quality_pipeline?.enabled", "Rendering locally in Blender, Unity, and Unreal"} {
		if !strings.Contains(string(script), marker) {
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
		"response.headers.get('Location')", "function copyResultLink()", "navigator.clipboard.writeText(shareURL || state.resultURL)",
		"function currentResultShareURL()", "/share`,",
		"so its link was copied", "function copyGIFStill()", "canvas.toBlob(resolve, 'image/png')",
		"A still frame was copied. Use Share or Download to keep the animation.",
	} {
		if !strings.Contains(script, marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
}

func TestEscapeLeavesPromptInput(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, marker := range []string{"event.key === 'Escape'", "document.activeElement === elements.prompt", "elements.prompt.blur()"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
}

func TestFirstClass3DModelCreationAndActions(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`data-mode="model"`, `id="model-recipe"`, `id="model-preview"`, `model-viewer/4.3.1/model-viewer.min.js`} {
		if !strings.Contains(string(index), marker) {
			t.Fatalf("index.html does not contain %q", marker)
		}
	}
	for _, marker := range []string{"/api/v1/models/generate", "presentModelResult", "model/gltf-binary", "gogif-model.glb", "Save .GLB"} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
}
