package comfyui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

func validGGUF() GGUF {
	return GGUF{
		UNet: "flux1-schnell-Q4_K_S.gguf", ClipL: "clip_l.safetensors",
		ClipT5: "t5xxl_fp8.safetensors", VAE: "ae.safetensors",
	}
}

func TestNewRejectsInvalidGGUFConfiguration(t *testing.T) {
	cases := map[string]GGUF{
		"missing unet":  {ClipL: "clip_l.safetensors", ClipT5: "t5.safetensors", VAE: "ae.safetensors"},
		"path in unet":  {UNet: "../unet/flux.gguf", ClipL: "clip_l.safetensors", ClipT5: "t5.safetensors", VAE: "ae.safetensors"},
		"not quantized": {UNet: "flux1-dev.safetensors", ClipL: "clip_l.safetensors", ClipT5: "t5.safetensors", VAE: "ae.safetensors"},
		"missing clip":  {UNet: "flux.gguf", ClipT5: "t5.safetensors", VAE: "ae.safetensors"},
		"bad guidance":  {UNet: "flux.gguf", ClipL: "clip_l.safetensors", ClipT5: "t5.safetensors", VAE: "ae.safetensors", Guidance: 99},
	}
	for name, gguf := range cases {
		if _, err := New(Options{Endpoint: "http://127.0.0.1:8188", Recipe: RecipeFluxGGUF, GGUF: gguf}); err == nil {
			t.Fatalf("New(%s) expected an error", name)
		}
	}
}

func TestNewRejectsUnknownRecipe(t *testing.T) {
	if _, err := New(Options{Endpoint: "http://127.0.0.1:8188", Recipe: "midjourney"}); err == nil {
		t.Fatal("New() accepted an unknown recipe")
	}
}

func TestPrivateEndpointPolicy(t *testing.T) {
	base := Options{Recipe: RecipeFluxGGUF, GGUF: validGGUF()}

	remote := base
	remote.Endpoint = "http://100.101.102.103:8188"
	if _, err := New(remote); err == nil {
		t.Fatal("New() allowed a remote endpoint without an explicit opt-in")
	}

	remote.PrivateEndpoint = true
	generator, err := New(remote)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if generator.Descriptor().Local {
		t.Fatal("a private worker was reported as local")
	}
	if !strings.Contains(generator.Descriptor().ID, "private") {
		t.Fatalf("Descriptor().ID = %q", generator.Descriptor().ID)
	}

	public := base
	public.Endpoint = "https://comfy.example.com"
	public.PrivateEndpoint = true
	if _, err := New(public); err == nil {
		t.Fatal("New() allowed a public host as a private worker")
	}

	tailnet := base
	tailnet.Endpoint = "http://gpu-box.tail1234.ts.net:8188"
	tailnet.PrivateEndpoint = true
	if _, err := New(tailnet); err != nil {
		t.Fatalf("New() rejected a tailnet host: %v", err)
	}
}

func TestFluxWorkflowLoadsQuantizedNodes(t *testing.T) {
	generator, err := New(Options{Endpoint: "http://127.0.0.1:8188", Recipe: RecipeFluxGGUF, GGUF: validGGUF(), Steps: 4})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workflow := generator.workflow(imagegen.Request{Prompt: "a rocket", Width: 512, Height: 512, Seed: 7}, "")
	encoded, err := json.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, expected := range []string{"UnetLoaderGGUF", "DualCLIPLoader", "VAELoader", "FluxGuidance", "flux1-schnell-Q4_K_S.gguf"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("flux workflow is missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "CheckpointLoaderSimple") {
		t.Fatalf("flux workflow still loads a checkpoint: %s", body)
	}
	sampler, ok := workflow["3"].(map[string]any)["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("workflow has no sampler inputs: %s", body)
	}
	if sampler["cfg"] != 1.0 {
		t.Fatalf("distilled model received CFG %v", sampler["cfg"])
	}
	if sampler["steps"] != 4 {
		t.Fatalf("steps = %v", sampler["steps"])
	}
}

func TestGGUFRecipeRefusesReferenceImages(t *testing.T) {
	generator, err := New(Options{
		Endpoint: "http://127.0.0.1:8188", Recipe: RecipeFluxGGUF, GGUF: validGGUF(),
		InputDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if generator.Descriptor().SupportsReferences {
		t.Fatal("the quantized recipe advertised reference support")
	}
	_, err = generator.Generate(context.Background(), imagegen.Request{
		Prompt: "a rocket", Width: 512, Height: 512,
		Inputs: []imagegen.Input{{Data: []byte{1, 2, 3}, ContentType: "image/png"}},
	})
	if err == nil {
		t.Fatal("Generate() silently ignored a reference image")
	}
}

func TestCheckpointRecipeStillDefaults(t *testing.T) {
	generator, err := New(Options{Endpoint: "http://127.0.0.1:8188", Checkpoint: "v1-5-pruned.safetensors"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if generator.Descriptor().ID != "comfyui-local" {
		t.Fatalf("Descriptor().ID = %q", generator.Descriptor().ID)
	}
	workflow := generator.workflow(imagegen.Request{Prompt: "a rocket", Width: 512, Height: 512}, "")
	if _, ok := workflow["4"].(map[string]any)["inputs"].(map[string]any)["ckpt_name"]; !ok {
		t.Fatalf("checkpoint recipe changed shape: %#v", workflow["4"])
	}
}
