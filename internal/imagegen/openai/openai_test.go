package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

func TestGenerateCreatesSemanticImageAndNormalizesOutput(t *testing.T) {
	output := encodedPNG(t, 1024, 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing server-side authorization")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload["model"] != "gpt-image-2" || payload["quality"] != "high" || payload["size"] != "1024x1024" {
			t.Errorf("payload = %#v", payload)
		}
		prompt, _ := payload["prompt"].(string)
		if !strings.Contains(prompt, "hero swinging through the city") || !strings.Contains(prompt, "show it already in progress") {
			t.Errorf("prompt = %q", prompt)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
			"b64_json": base64.StdEncoding.EncodeToString(output), "revised_prompt": "cinematic hero",
		}}})
	}))
	defer server.Close()

	generator, err := New(Options{APIKey: "test-key", BaseURL: server.URL + "/v1", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), imagegen.Request{
		Prompt: "hero swinging through the city", Width: 480, Height: 480, Seed: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, _, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil || configuration.Width != 480 || configuration.Height != 480 {
		t.Fatalf("generated image = %#v, %v", configuration, err)
	}
	if result.Engine != "openai-gpt-image-2" || result.ContentType != "image/png" || result.RevisedPrompt != "cinematic hero" {
		t.Fatalf("result = %#v", result)
	}
	if descriptor := generator.Descriptor(); !descriptor.Semantic || descriptor.Local || !descriptor.SupportsReferences {
		t.Fatalf("Descriptor() = %#v", descriptor)
	}
}

func TestGenerateWithReferencesUsesEditEndpoint(t *testing.T) {
	input := encodedPNG(t, 128, 128)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Error(err)
			return
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) != 1 || r.FormValue("model") != "gpt-image-2" || r.FormValue("size") != "1536x1024" {
			t.Errorf("form = %#v; files = %#v", r.MultipartForm.Value, files)
		}
		file, err := files[0].Open()
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if !bytes.Equal(data, input) {
			t.Error("reference bytes changed")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
			"b64_json": base64.StdEncoding.EncodeToString(encodedPNG(t, 1536, 1024)),
		}}})
	}))
	defer server.Close()
	generator, err := New(Options{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(context.Background(), imagegen.Request{
		Prompt: "move the subject into a city", Width: 720, Height: 480,
		Inputs: []imagegen.Input{{Data: input, ContentType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsInsecureRemoteBaseURL(t *testing.T) {
	if _, err := New(Options{APIKey: "test", BaseURL: "http://example.com/v1"}); err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestGenerateReturnsBoundedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"prompt was rejected"}}`))
	}))
	defer server.Close()
	generator, err := New(Options{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(context.Background(), imagegen.Request{Prompt: "test", Width: 128, Height: 128})
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "prompt was rejected") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func encodedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 80, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
