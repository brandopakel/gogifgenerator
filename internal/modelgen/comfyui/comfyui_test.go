package comfyui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/modelgen"
)

func TestGenerateQueuesAllowlistedWorkflowAndFetchesGLB(t *testing.T) {
	glb := append([]byte("glTF"), make([]byte, 16)...)
	var queued map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/prompt":
			if err := json.NewDecoder(r.Body).Decode(&queued); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"prompt_id":"model-job"}`))
		case "/history/model-job":
			_, _ = w.Write([]byte(`{"model-job":{"outputs":{"2":{"3d":[{"filename":"tripo31_00001_.glb","subfolder":"gogif/models","type":"output"}]}},"status":{"completed":true}}}`))
		case "/view":
			if r.URL.Query().Get("filename") != "tripo31_00001_.glb" || r.URL.Query().Get("subfolder") != "gogif/models" {
				t.Errorf("unexpected output query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write(glb)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	generator, err := New(Options{Endpoint: server.URL, APIKey: "paid-partner-key", PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), modelgen.Request{Prompt: "a highly detailed clockwork fox", Recipe: "tripo-3.1", Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	if result.ContentType != "model/gltf-binary" || result.Extension != "glb" || !bytes.Equal(result.Data, glb) {
		t.Fatalf("Generate() = %#v", result)
	}
	prompt := queued["prompt"].(map[string]any)
	tripo := prompt["1"].(map[string]any)
	if tripo["class_type"] != "TripoTextToModelNode" {
		t.Fatalf("queued prompt = %#v", queued)
	}
	inputs := tripo["inputs"].(map[string]any)
	if inputs["prompt"] != "a highly detailed clockwork fox" || inputs["pbr"] != true || inputs["geometry_quality"] != "detailed" {
		t.Fatalf("Tripo inputs = %#v", inputs)
	}
	extra := queued["extra_data"].(map[string]any)
	if extra["api_key_comfy_org"] != "paid-partner-key" {
		t.Fatalf("extra_data = %#v", extra)
	}
}

func TestGenerateRejectsUnknownRecipeBeforeNetwork(t *testing.T) {
	generator, err := New(Options{Endpoint: "http://127.0.0.1:8188"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(context.Background(), modelgen.Request{Prompt: "fox", Recipe: "arbitrary-workflow"}); err == nil {
		t.Fatal("Generate() accepted an arbitrary workflow")
	}
}

func TestGenerateUsesCloudJobsAPIAndDoesNotForwardKeyToOutputStorage(t *testing.T) {
	glb := append([]byte("glTF"), make([]byte, 16)...)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stored-model.glb" && r.Header.Get("X-API-Key") != "paid-partner-key" {
			t.Errorf("missing Cloud API key on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/prompt":
			_, _ = w.Write([]byte(`{"prompt_id":"cloud-model-job"}`))
		case "/api/jobs/cloud-model-job":
			_, _ = w.Write([]byte(`{"status":"completed","outputs":{"2":{"3d":[{"filename":"model.glb","subfolder":"gogif/models","type":"output"}]}}}`))
		case "/api/view":
			http.Redirect(w, r, "https://storage.example/stored-model.glb", http.StatusFound)
		case "/stored-model.glb":
			if r.Header.Get("X-API-Key") != "" {
				t.Error("Comfy API key was forwarded to output storage")
			}
			_, _ = w.Write(glb)
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
	generator, err := New(Options{Endpoint: "https://cloud.example/api", APIKey: "paid-partner-key", Client: client, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), modelgen.Request{Prompt: "clockwork fox", Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Data, glb) {
		t.Fatal("Generate() returned the wrong GLB")
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

func TestRemoteEndpointRequiresHTTPSAndAPIKey(t *testing.T) {
	if _, err := New(Options{Endpoint: "http://example.com/api", APIKey: "key"}); err == nil {
		t.Fatal("New() accepted insecure remote HTTP")
	}
	if _, err := New(Options{Endpoint: "https://cloud.comfy.org/api"}); err == nil {
		t.Fatal("New() accepted remote endpoint without an API key")
	}
}

func TestHunyuanWorkflowIsPBRAndUsesGLBOutput(t *testing.T) {
	workflow := hunyuanWorkflow("ceramic robot", 12)
	inputs := workflow["1"].(map[string]any)["inputs"].(map[string]any)
	generateType := inputs["generate_type"].(map[string]any)
	if inputs["model"] != "3.1" || generateType["pbr"] != true {
		t.Fatalf("workflow = %#v", workflow)
	}
	mesh := workflow["2"].(map[string]any)["inputs"].(map[string]any)["mesh"].([]any)
	if mesh[0] != "1" || mesh[1] != 1 {
		t.Fatalf("GLB mesh input = %#v", mesh)
	}
}
