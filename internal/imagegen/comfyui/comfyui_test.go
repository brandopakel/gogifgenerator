package comfyui

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

func TestGenerateQueuesWorkflowFetchesOutputAndCleansReference(t *testing.T) {
	inputDirectory := t.TempDir()
	var queuedWorkflow map[string]any
	var uploadedPath string
	memoryReleased := false
	outputPNG := encodedPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/upload/image":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			file, header, err := r.FormFile("image")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			uploadedPath = filepath.Join(inputDirectory, "gogif", filepath.Base(header.Filename))
			if err := os.MkdirAll(filepath.Dir(uploadedPath), 0o750); err != nil {
				t.Error(err)
				return
			}
			data, _ := io.ReadAll(file)
			if err := os.WriteFile(uploadedPath, data, 0o600); err != nil {
				t.Error(err)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"name": filepath.Base(uploadedPath), "subfolder": "gogif", "type": "input"})
		case r.Method == http.MethodPost && r.URL.Path == "/prompt":
			var payload struct {
				Prompt map[string]any `json:"prompt"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			queuedWorkflow = payload.Prompt
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt_id": "prompt-1", "node_errors": map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/history/prompt-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt-1": map[string]any{
				"outputs": map[string]any{"9": map[string]any{"images": []any{map[string]any{"filename": "result.png", "subfolder": "", "type": "temp"}}}},
				"status":  map[string]any{"status_str": "success", "completed": true},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/view":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(outputPNG)
		case r.Method == http.MethodGet && r.URL.Path == "/system_stats":
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/free":
			memoryReleased = true
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	generator, err := New(Options{
		Endpoint: server.URL, Checkpoint: "small-model.safetensors", InputDirectory: inputDirectory,
		Client: server.Client(), PollInterval: time.Millisecond, MaxWait: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := generator.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), imagegen.Request{
		Prompt: "make this dance", Width: 128, Height: 128, Seed: 42,
		Inputs: []imagegen.Input{{Data: outputPNG, ContentType: "image/png", SourceID: "wikimedia:42"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != "comfyui-local" || result.ContentType != "image/png" || !bytes.Equal(result.Data, outputPNG) {
		t.Fatalf("Generate() = %#v", result)
	}
	if queuedWorkflow["10"] == nil || queuedWorkflow["12"] == nil {
		t.Fatalf("workflow does not contain reference-image nodes: %#v", queuedWorkflow)
	}
	if !memoryReleased {
		t.Fatal("ComfyUI model memory was not released before downstream rendering")
	}
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("temporary ComfyUI input still exists: %v", err)
	}
}

func TestNewRejectsNonLoopbackServer(t *testing.T) {
	if _, err := New(Options{Endpoint: "https://example.com", Checkpoint: "model.safetensors"}); err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestGenerateRequiresCleanupDirectoryForReference(t *testing.T) {
	generator, err := New(Options{Checkpoint: "model.safetensors"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(context.Background(), imagegen.Request{
		Prompt: "reference", Width: 128, Height: 128,
		Inputs: []imagegen.Input{{Data: encodedPNG(t), ContentType: "image/png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "INPUT_DIR") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestGenerateCleansExpectedUploadWhenComfyResponseIsUnsafe(t *testing.T) {
	inputDirectory := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("image")
		if err != nil {
			t.Error(err)
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		defer file.Close()
		target := filepath.Join(inputDirectory, "gogif", filepath.Base(header.Filename))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Error(err)
			return
		}
		data, _ := io.ReadAll(file)
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "different.png", "subfolder": "gogif", "type": "input"})
	}))
	defer server.Close()
	generator, err := New(Options{
		Endpoint: server.URL, Checkpoint: "model.safetensors", InputDirectory: inputDirectory, Client: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(context.Background(), imagegen.Request{
		Prompt: "reference", Width: 128, Height: 128,
		Inputs: []imagegen.Input{{Data: encodedPNG(t), ContentType: "image/png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe file location") {
		t.Fatalf("Generate() error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(inputDirectory, "gogif"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary ComfyUI inputs = %#v, %v", entries, err)
	}
}

func encodedPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	imageValue := image.NewRGBA(image.Rect(0, 0, 128, 128))
	imageValue.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&output, imageValue); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
