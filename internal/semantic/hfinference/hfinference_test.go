package hfinference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/semantic"
)

func TestNewRejectsUnapprovedHostsAndModels(t *testing.T) {
	for name, options := range map[string]Options{
		"unapproved host": {Endpoint: "https://evil.example.com", APIKey: "token"},
		"plain http":      {Endpoint: "http://router.huggingface.co", APIKey: "token"},
		"credentials":     {Endpoint: "https://user:pass@router.huggingface.co", APIKey: "token"},
		"missing token":   {Endpoint: "https://router.huggingface.co"},
		"bad model":       {Endpoint: "https://router.huggingface.co", APIKey: "token", Model: "../../etc/passwd"},
		"bare model":      {Endpoint: "https://router.huggingface.co", APIKey: "token", Model: "bge-small"},
		"path traversal":  {Endpoint: "https://router.huggingface.co", APIKey: "token", Path: "/../admin"},
	} {
		if _, err := New(options); err == nil {
			t.Fatalf("New(%s) expected an error", name)
		}
	}
}

func TestNewAllowsLoopbackWithoutAToken(t *testing.T) {
	embedder, err := New(Options{Endpoint: "http://127.0.0.1:8080"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	descriptor := embedder.Descriptor()
	if !descriptor.Local {
		t.Fatalf("loopback endpoint reported as hosted: %#v", descriptor)
	}
	if embedder.path != "/embed" {
		t.Fatalf("loopback path = %q", embedder.path)
	}
}

func TestHostedRouteAndAuthorization(t *testing.T) {
	var path, authorization string
	var payload map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorization = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[3,4],[0,1]]`))
	}))
	defer server.Close()

	embedder, err := New(Options{
		Endpoint: server.URL, APIKey: "hf-token", Model: "BAAI/bge-small-en-v1.5",
		Path:   "/hf-inference/models/BAAI/bge-small-en-v1.5/pipeline/feature-extraction",
		Client: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// httptest serves on loopback, so the adapter treats it as a local
	// server. Force the hosted route and token to assert both are sent.
	embedder.local = false
	embedder.apiKey = "hf-token"

	vectors, err := embedder.Embed(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if path != "/hf-inference/models/BAAI/bge-small-en-v1.5/pipeline/feature-extraction" {
		t.Fatalf("path = %q", path)
	}
	if authorization != "Bearer hf-token" {
		t.Fatalf("Authorization = %q", authorization)
	}
	if _, ok := payload["options"]; !ok {
		t.Fatalf("hosted request did not wait for a cold model: %#v", payload)
	}
	if len(vectors) != 2 {
		t.Fatalf("Embed() returned %d vectors", len(vectors))
	}
	if got := semantic.Cosine(vectors[0], []float32{3, 4}); got < 0.999 {
		t.Fatalf("vector direction changed: %v", vectors[0])
	}
	var norm float32
	for _, value := range vectors[0] {
		norm += value * value
	}
	if norm < 0.999 || norm > 1.001 {
		t.Fatalf("vector was not normalized: %v", vectors[0])
	}
}

func TestEmbedMeanPoolsTokenMatrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[[1,0],[0,1]]]`))
	}))
	defer server.Close()

	embedder, err := New(Options{Endpoint: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	vectors, err := embedder.Embed(context.Background(), []string{"one"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if got := semantic.Cosine(vectors[0], []float32{1, 1}); got < 0.999 {
		t.Fatalf("token matrix was not mean pooled: %v", vectors[0])
	}
}

func TestEmbedReportsProviderFailuresAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model is loading", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	embedder, _ := New(Options{Endpoint: server.URL, Client: server.Client()})
	_, err := embedder.Embed(context.Background(), []string{"one"})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("Embed() error = %v", err)
	}
}

func TestEmbedRejectsVectorCountMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[1,0]]`))
	}))
	defer server.Close()

	embedder, _ := New(Options{Endpoint: server.URL, Client: server.Client()})
	if _, err := embedder.Embed(context.Background(), []string{"one", "two"}); err == nil {
		t.Fatal("Embed() expected a count mismatch error")
	}
}
