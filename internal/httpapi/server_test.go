package httpapi

import (
	"bytes"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestGenerateRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{"prompt":"hi","surprise":true}`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}
