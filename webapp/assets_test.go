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
		`id="trim-start-control"`, `id="target-size-control"`, `id="search-scope"`, `<option value="stickers">Stickers</option>`, `<option value="clips">Clips</option>`, `role="slider"`, `aria-live="assertive"`,
	} {
		if !strings.Contains(page, marker) {
			t.Fatalf("index.html does not contain %q", marker)
		}
	}
}

func TestYarnPhraseSearchUsesTheGoScraperAndOfficialEmbeds(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, marker := range []string{
		"searchScope === 'clips'", "searchYarn(query, cursor)", "/api/v1/providers/yarn/search",
		"YARN · OFFICIAL EMBEDS", "description: item.description", "toggleEmbeddedClip(item, media, preview)",
		"frame.src = item.embedURL", "frame.loading = 'lazy'", "frame.allow = 'fullscreen'", "Yarn phrase results are unavailable",
	} {
		if !strings.Contains(script, marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
	for _, forbidden := range []string{"y.yarn.co", "curl_cffi", "cloudflare", "Search on Yarn"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("browser Yarn integration contains unsupported media/scraping marker %q", forbidden)
		}
	}
}

func TestSearchUsesOnlyTheSharedStatusSurface(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := fs.ReadFile(Files(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(index), `id="search-message"`); count != 1 {
		t.Fatalf("search status surfaces = %d; want 1", count)
	}
	for name, content := range map[string]string{"app.js": string(script), "app.css": string(styles)} {
		for _, duplicate := range []string{"provider-status", "renderProviderFailure", "providerStatus"} {
			if strings.Contains(content, duplicate) {
				t.Fatalf("%s contains duplicate search status mechanism %q", name, duplicate)
			}
		}
	}
}

func TestClipCardsHydrateQuotesAndNavigateRelatedResults(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="clip-trail"`) {
		t.Fatal("index.html does not expose the related clip path")
	}
	for _, marker := range []string{
		"Finding the closest timed quote", "resolveClipItem(item)", "hydrateClipCard", "payload.quote_match",
		"Related clips", "exploreRelatedClips(item)", "state.clipTrail", "preserveClipTrail: true",
		"clipCardObserver", "clipHydrationActive < 3", "loadMoreSearchResults",
	} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("app.js does not contain %q", marker)
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

func TestCreateFineTuneToolbarAndBrandUseOneCanonicalMark(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(index)
	toolbar := strings.Index(page, `id="create-toolbar"`)
	workspace := strings.Index(page, `id="create-panel"`)
	if toolbar < 0 || workspace < 0 || toolbar > workspace {
		t.Fatal("create toolbar is not positioned above the Create workspace")
	}
	if strings.Count(page, `id="generation-controls"`) != 1 || strings.Count(page, `id="create-options"`) != 1 {
		t.Fatal("create toolbar controls were duplicated")
	}
	for _, marker := range []string{`class="brand-logo"`, `src="/icon.svg?v=2"`, `class="search-options create-output"`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("index.html does not contain %q", marker)
		}
	}
	if strings.Contains(page, "brand-mark") {
		t.Fatal("legacy three-bar brand mark is still present")
	}
	styles, err := fs.ReadFile(Files(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		".create-controls > summary::-webkit-details-marker",
		".create-controls > summary::after",
		".create-controls[open] > summary::after",
		".create-controls .control-grid { width: 100%; grid-template-columns: repeat(3, minmax(0, 1fr))",
		"grid-template-columns: repeat(2, minmax(0, 1fr))",
	} {
		if !strings.Contains(string(styles), marker) {
			t.Fatalf("app.css does not contain balanced create toolbar rule %q", marker)
		}
	}
}

func TestAccountIsThePermanentFarRightHeaderAction(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(index)
	install := strings.Index(page, `id="install-button"`)
	accountButton := strings.Index(page, `id="account-button"`)
	accountDialog := strings.Index(page, `id="account-dialog"`)
	if install < 0 || accountButton < install || accountDialog < 0 {
		t.Fatal("Account is not the far-right header action with a dedicated dialog")
	}
	for _, marker := range []string{`id="account-auth-link"`, `id="account-library-button"`, `id="account-plans-button"`} {
		if !strings.Contains(page, marker) {
			t.Fatalf("account UI does not contain %q", marker)
		}
	}
}

func TestCollectionCancelAndGlobalInteractionCursors(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := fs.ReadFile(Files(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="collection-cancel-button" class="secondary-button" type="button"`) {
		t.Fatal("collection Cancel remains a validating form submission")
	}
	for _, marker := range []string{"collectionCancel: document.querySelector", "elements.collectionDialog.close()"} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("collection Cancel behavior does not contain %q", marker)
		}
	}
	for _, marker := range []string{"button:not(:disabled), a[href], summary, select:not(:disabled)", ".library-tools button { white-space: nowrap; }", "min-width: max-content"} {
		if !strings.Contains(string(styles), marker) {
			t.Fatalf("interaction styling does not contain %q", marker)
		}
	}
}

func TestLibraryIsOnlyOpenedFromAccount(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(index)
	if strings.Contains(page, `id="library-tab"`) || strings.Contains(page, `data-mode="library"`) {
		t.Fatal("Library remains exposed as a primary mode tab")
	}
	if !strings.Contains(page, `id="account-library-button"`) || !strings.Contains(string(script), "setMode('library')") {
		t.Fatal("Account no longer provides the Library entry point")
	}
}

func TestResultActionsNeverWrap(t *testing.T) {
	styles, err := fs.ReadFile(Files(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		".result-actions { display: flex; flex: 0 0 auto; flex-wrap: nowrap;",
		"grid-template-columns: repeat(4, minmax(0, 1fr))",
		".result-actions > * { min-width: 0; padding-inline: 4px; font-size: .62rem; }",
	} {
		if !strings.Contains(string(styles), marker) {
			t.Fatalf("result action layout does not contain %q", marker)
		}
	}
}

func TestCreateResetAndFreshVariationSeeds(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`id="reset-create-button"`, "function randomSeed()", "state.seed = randomSeed()", "function resetCreateWorkspace()", "clearResult(false)", "Your creation remains saved in your library"} {
		if !strings.Contains(string(index), marker) && !strings.Contains(string(script), marker) {
			t.Fatalf("Create reset and variation behavior does not contain %q", marker)
		}
	}
	if strings.Contains(string(script), "if (reroll) state.seed = Date.now()") {
		t.Fatal("ordinary Create submissions still reuse a seed until Reroll")
	}
}

func TestCreateDoesNotExposeProceduralOrLocalEditorRenderers(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`id="generation-mode-control"`, "Studio Local", "Abstract only"} {
		if strings.Contains(string(index), marker) {
			t.Fatalf("create UI still exposes %q", marker)
		}
	}
	for _, marker := range []string{"generationModeForPrompt", "Animating abstract shapes locally", "Rendering locally in Blender, Unity, and Unreal"} {
		if strings.Contains(string(script), marker) {
			t.Fatalf("create client still contains renderer selection path %q", marker)
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

func TestReferenceRemixUsesSemanticGeneratorOnly(t *testing.T) {
	data, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !strings.Contains(script, "const canRemix = state.config?.image_generator?.supports_references") {
		t.Fatal("reference remix is not gated on the semantic generator")
	}
	if strings.Contains(script, "quality_pipeline?.supports_references") {
		t.Fatal("reference remix can still launch the local editor pipeline")
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
	if strings.Contains(string(index), `id="generation-mode-control"`) {
		t.Fatal("GIF create still exposes an engine selector")
	}
	for _, marker := range []string{"generation_mode: 'semantic'", "Generating the scene in Comfy Cloud…"} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
	for _, marker := range []string{"generation_mode: generationMode", "Animating abstract shapes locally", "Rendering locally in Blender, Unity, and Unreal"} {
		if strings.Contains(string(script), marker) {
			t.Fatalf("app.js still contains user-facing renderer branch %q", marker)
		}
	}
}

func TestGIFFineTuneContainsOnlyOutputControls(t *testing.T) {
	index, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(index)
	for _, label := range []string{"Canvas", "Tempo", "Animation quality"} {
		if !strings.Contains(page, label) {
			t.Fatalf("GIF fine tune is missing %q", label)
		}
	}
	for _, label := range []string{"Visual source", "Studio Local", "Abstract only"} {
		if strings.Contains(page, label) {
			t.Fatalf("GIF fine tune still contains %q", label)
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
	for _, marker := range []string{`id="create-kind"`, `<option value="model">3D model</option>`, `id="model-recipe"`, `id="model-preview"`, `model-viewer/4.3.1/model-viewer.min.js`} {
		if !strings.Contains(string(index), marker) {
			t.Fatalf("index.html does not contain %q", marker)
		}
	}
	if strings.Contains(string(index), `data-mode="model"`) {
		t.Fatal("3D remains a top-level workspace instead of a Create output")
	}
	for _, marker := range []string{"/api/v1/models/generate", "presentModelResult", "model/gltf-binary", "gogif-model.glb", "Save .GLB"} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
	for _, marker := range []string{"elements.createKind.addEventListener('change'", "setMode(elements.createKind.value === 'model' ? 'model' : 'create')", "selectedTopLevelMode = modeling ? 'create' : mode"} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("app.js does not contain %q", marker)
		}
	}
}

func TestSourceSearchExplainsMediaMatches(t *testing.T) {
	script, err := fs.ReadFile(Files(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := fs.ReadFile(Files(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"description: item.description", "showMetadata: true", "source-card-title", "source-card-meta"} {
		if !strings.Contains(string(script), marker) && !strings.Contains(string(styles), marker) {
			t.Fatalf("source search does not contain %q", marker)
		}
	}
}
