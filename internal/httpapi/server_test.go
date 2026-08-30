package httpapi

import (
	"bytes"
	"context"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/planner"
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

type recordingSaver struct {
	generated media.GeneratedAsset
}

func (s *recordingSaver) SaveGenerated(_ context.Context, generated media.GeneratedAsset) (media.Asset, error) {
	s.generated = generated
	return media.Asset{ID: "gif_saved"}, nil
}
