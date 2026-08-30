package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/reference"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestSecurityPolicyAllowsConfiguredCatalogMedia(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	policy := response.Header().Get("Content-Security-Policy")
	for _, host := range []string{
		"https://upload.wikimedia.org", "https://blob.gifcities.org", "https://archive.org", "https://*.archive.org",
	} {
		if !strings.Contains(policy, host) {
			t.Fatalf("Content-Security-Policy does not allow %s: %q", host, policy)
		}
	}
}

func TestGenerate(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "tests are green",
      "width": 128,
      "height": 128,
      "frames": 4
    }`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/gif" {
		t.Fatalf("Content-Type = %q", got)
	}
	if _, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
}

func TestGenerateAnimatesLocalImageGeneratorOutput(t *testing.T) {
	generator := &recordingImageGenerator{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "a tiny local robot",
      "width": 128,
      "height": 128,
      "frames": 4
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, ImageGenerator: generator}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-GoGIF-Engine"); got != "test-local+local" {
		t.Fatalf("X-GoGIF-Engine = %q", got)
	}
	if generator.request.Prompt != "a tiny local robot" || generator.request.Width != 128 || generator.request.Height != 128 {
		t.Fatalf("Generate() request = %#v", generator.request)
	}
	if _, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
}

func TestGenerateFromReferenceRevalidatesFetchesAndDeletesSource(t *testing.T) {
	localGenerator := &recordingImageGenerator{}
	saver := &recordingSaver{}
	sourcePNG := generatedTestPNG(t)
	sourceServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(sourcePNG)
	}))
	defer sourceServer.Close()
	sourceURL, _ := url.Parse(sourceServer.URL)
	temporaryDirectory := t.TempDir()
	fetcher, err := reference.New(reference.Options{
		Client: sourceServer.Client(), TempDir: temporaryDirectory,
		AllowedHosts: map[string][]string{"test": {sourceURL.Hostname()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mediaProvider := &recordingProvider{result: provider.Result{
		Provider: "test", ExternalID: "42", Kind: media.KindImage, OriginalURL: sourceServer.URL,
		Derivatives: media.PermissionAllowed, TransformPolicy: provider.TransformAllowed,
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate-from-reference", bytes.NewBufferString(`{
      "provider":"test", "external_id":"42", "prompt":"make this dance",
      "width":128, "height":128, "frames":4
    }`))
	response := httptest.NewRecorder()
	New(Options{
		Planner: planner.Local{}, Providers: []provider.Provider{mediaProvider},
		ImageGenerator: localGenerator, ReferenceFetcher: fetcher, GeneratedSaver: saver,
	}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if mediaProvider.resolvedID != "42" || len(localGenerator.request.Inputs) != 1 || localGenerator.request.Inputs[0].SourceID != "test:42" {
		t.Fatalf("resolved ID = %q; generator request = %#v", mediaProvider.resolvedID, localGenerator.request)
	}
	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary references = %#v, %v", entries, err)
	}
	if _, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
	if saver.generated.Source == nil || saver.generated.Source.Provider != "test" || saver.generated.Source.ExternalID != "42" {
		t.Fatalf("persisted source = %#v", saver.generated.Source)
	}
}

func TestGeneratePersistsAssetWhenConfigured(t *testing.T) {
	saver := &recordingSaver{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "save this one",
      "width": 128,
      "height": 128,
      "frames": 4
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, GeneratedSaver: saver}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-GoGIF-Asset-ID"); got != "gif_saved" {
		t.Fatalf("X-GoGIF-Asset-ID = %q", got)
	}
	if got := response.Header().Get("Location"); got != "/api/v1/gifs/gif_saved" {
		t.Fatalf("Location = %q", got)
	}
	if saver.generated.Prompt != "save this one" || len(saver.generated.Data) == 0 {
		t.Fatalf("SaveGenerated() input = %#v", saver.generated)
	}
}

func TestGenerateRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{"prompt":"hi","surprise":true}`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestGeneratedServesOnlyConfiguredLibraryContent(t *testing.T) {
	reader := &recordingGeneratedReader{data: []byte("GIF89a-generated")}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/gifs/gif_owned", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, GeneratedReader: reader}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "GIF89a-generated" {
		t.Fatalf("status = %d; body = %q", response.Code, response.Body.String())
	}
	if reader.id != "gif_owned" || response.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("id = %q; Content-Type = %q", reader.id, response.Header().Get("Content-Type"))
	}
}

func TestSearchProvider(t *testing.T) {
	fake := &recordingProvider{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/test/search?q=victory&limit=3&cursor=12&locale=en", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Providers: []provider.Provider{fake}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if fake.query.Text != "victory" || fake.query.Limit != 3 || fake.query.Cursor != "12" || fake.query.Locale != "en" {
		t.Fatalf("Search() query = %#v", fake.query)
	}
	var page provider.Page
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Provider != "test" || len(page.Results) != 1 || page.Results[0].ExternalID != "result-1" {
		t.Fatalf("page = %#v", page)
	}
}

func TestSearchProviderRejectsBadLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/test/search?q=victory&limit=many", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Providers: []provider.Provider{&recordingProvider{}}}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestSearchProviderReturnsNotFound(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/missing/search?q=victory", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestResolveProviderItem(t *testing.T) {
	fake := &recordingProvider{result: provider.Result{
		Provider: "test", ExternalID: "clip-1", Title: "Resolved clip", Kind: media.KindVideo,
		Renditions: []provider.Rendition{{Name: "360p", ContentType: "video/mp4", URL: "https://example.com/clip.mp4"}},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/test/items/clip-1?locale=fr", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Providers: []provider.Provider{fake}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if fake.resolvedID != "clip-1" || fake.resolvedLocale != "fr" {
		t.Fatalf("resolved ID = %q; locale = %q", fake.resolvedID, fake.resolvedLocale)
	}
	var result provider.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != media.KindVideo || len(result.Renditions) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

type recordingSaver struct {
	generated media.GeneratedAsset
}

type recordingGeneratedReader struct {
	id   string
	data []byte
}

func (r *recordingGeneratedReader) OpenGenerated(_ context.Context, id string) (media.Asset, io.ReadCloser, error) {
	r.id = id
	return media.Asset{ID: id}, io.NopCloser(bytes.NewReader(r.data)), nil
}

type recordingProvider struct {
	query          provider.Query
	result         provider.Result
	resolvedID     string
	resolvedLocale string
}

func (p *recordingProvider) Resolve(_ context.Context, externalID, locale string) (provider.Result, error) {
	p.resolvedID = externalID
	p.resolvedLocale = locale
	return p.result, nil
}

type recordingImageGenerator struct {
	request imagegen.Request
}

func (g *recordingImageGenerator) Descriptor() imagegen.Descriptor {
	return imagegen.Descriptor{ID: "test-local", Label: "Test local", Local: true, SupportsReferences: true}
}

func (g *recordingImageGenerator) Generate(_ context.Context, request imagegen.Request) (imagegen.Result, error) {
	g.request = request
	return imagegen.Result{Data: generatedTestPNG(nil), ContentType: "image/png", Engine: "test-local"}, nil
}

func generatedTestPNG(t *testing.T) []byte {
	still := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			still.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 90, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, still); err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	return output.Bytes()
}

func (p *recordingProvider) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "test", Label: "Test provider"}
}

func (p *recordingProvider) Search(_ context.Context, query provider.Query) (provider.Page, error) {
	p.query = query
	return provider.Page{Provider: "test", Results: []provider.Result{{Provider: "test", ExternalID: "result-1", Title: "Victory"}}}, nil
}

func (s *recordingSaver) SaveGenerated(_ context.Context, generated media.GeneratedAsset) (media.Asset, error) {
	s.generated = generated
	return media.Asset{ID: "gif_saved"}, nil
}
