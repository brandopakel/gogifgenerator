package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image/gif"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
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
	query provider.Query
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
