package comfypartner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

func TestCloudGenerateQueuesAllowlistedFluxWorkflowAndNormalizesImage(t *testing.T) {
	var queued map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "comfy-test-key" {
			t.Errorf("X-API-Key missing from %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/prompt":
			if err := json.NewDecoder(r.Body).Decode(&queued); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"prompt_id":"image-job"}`))
		case "/api/jobs/image-job":
			_, _ = w.Write([]byte(`{"status":"completed","outputs":{"2":{"images":[{"filename":"flux.png","subfolder":"gogif/semantic","type":"output"}]}}}`))
		case "/api/view":
			if r.URL.Query().Get("filename") != "flux.png" || r.URL.Query().Get("subfolder") != "gogif/semantic" {
				t.Errorf("unexpected output query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "image/png")
			canvas := image.NewNRGBA(image.Rect(0, 0, 96, 64))
			for y := 0; y < 64; y++ {
				for x := 0; x < 96; x++ {
					canvas.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 160, A: 255})
				}
			}
			if err := png.Encode(w, canvas); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Transport = rewriteTransport{target: target, next: client.Transport}
	generator, err := New(Options{
		Endpoint: "https://cloud.example/api", APIKey: "comfy-test-key", Client: client,
		PollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), imagegen.Request{
		Prompt: "a clockwork fox running across a moonlit rooftop", Width: 64, Height: 64, Seed: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil || format != "png" || configuration.Width != 64 || configuration.Height != 64 {
		t.Fatalf("output = %s %dx%d, err=%v", format, configuration.Width, configuration.Height, err)
	}
	if result.Engine != "comfyui-partner-flux-ultra" || result.ContentType != "image/png" {
		t.Fatalf("result = %#v", result)
	}
	workflow := queued["prompt"].(map[string]any)
	flux := workflow["1"].(map[string]any)
	if flux["class_type"] != "FluxProUltraImageNode" {
		t.Fatalf("workflow = %#v", workflow)
	}
	inputs := flux["inputs"].(map[string]any)
	if inputs["raw"] != true || inputs["aspect_ratio"] != "1:1" {
		t.Fatalf("Flux inputs = %#v", inputs)
	}
	extra := queued["extra_data"].(map[string]any)
	if extra["api_key_comfy_org"] != "comfy-test-key" {
		t.Fatalf("extra_data = %#v", extra)
	}
}

func TestGeneratorRejectsUnsafeConfigurationAndUnsupportedInputs(t *testing.T) {
	if _, err := New(Options{Endpoint: "http://example.com/api", APIKey: "key"}); err == nil {
		t.Fatal("New accepted remote HTTP")
	}
	if _, err := New(Options{Endpoint: "https://cloud.comfy.org/api"}); err == nil {
		t.Fatal("New accepted a missing API key")
	}
	if _, err := New(Options{Endpoint: "https://cloud.comfy.org/api", APIKey: "key", Recipe: "arbitrary"}); err == nil {
		t.Fatal("New accepted an arbitrary recipe")
	}
	generator, err := New(Options{Endpoint: "http://127.0.0.1:8188", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(context.Background(), imagegen.Request{
		Prompt: "fox", Width: 64, Height: 64,
		Inputs: []imagegen.Input{{Data: []byte("image"), ContentType: "image/png"}},
	})
	if !errors.Is(err, ErrUnsupportedInput) {
		t.Fatalf("Generate error = %v", err)
	}
}

func TestAspectRatioIsReducedAndBounded(t *testing.T) {
	for _, test := range []struct {
		width, height int
		want          string
	}{{1920, 1080, "16:9"}, {1024, 1024, "1:1"}, {2048, 64, "4:1"}, {64, 2048, "1:4"}} {
		if got := aspectRatio(test.width, test.height); got != test.want {
			t.Fatalf("aspectRatio(%d, %d) = %q, want %q", test.width, test.height, got, test.want)
		}
	}
}

func TestLocalExecutionErrorReturnsOnlySafeMessage(t *testing.T) {
	messages := []json.RawMessage{
		json.RawMessage(`["execution_start",{"prompt_id":"job"}]`),
		json.RawMessage(`["execution_error",{"exception_message":"Payment Required: Please add credits.\n","traceback":["private internals"]}]`),
	}
	if got := localExecutionError(messages); got != "Payment Required: Please add credits." {
		t.Fatalf("localExecutionError() = %q", got)
	}
}

type rewriteTransport struct {
	target *url.URL
	next   http.RoundTripper
}

func (r rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	clone.Host = r.target.Host
	return r.next.RoundTrip(clone)
}
