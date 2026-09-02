package comfyui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

// Recipes are the audited workflow graphs this adapter can queue. A browser
// request can select nothing here: the recipe and every model filename are
// server configuration, and the request supplies only prompt, size, and seed.
const (
	// RecipeCheckpoint is the original single-file Stable Diffusion graph.
	RecipeCheckpoint = "sd-checkpoint"
	// RecipeFluxGGUF runs a quantized FLUX transformer through the
	// ComfyUI-GGUF custom nodes, with CLIP and VAE loaded separately. This is
	// what makes a large model fit a small GPU.
	RecipeFluxGGUF = "flux-gguf"
)

// GGUF names the files a quantized FLUX graph loads. They are filenames
// inside ComfyUI's own model directories, never paths.
type GGUF struct {
	// UNet is the quantized transformer, for example
	// flux1-schnell-Q4_K_S.gguf in models/unet.
	UNet string
	// ClipL and ClipT5 are the two text encoders FLUX requires, in
	// models/clip. The T5 encoder may itself be a GGUF quantization.
	ClipL  string
	ClipT5 string
	// VAE is the autoencoder in models/vae, normally ae.safetensors.
	VAE string
	// Guidance is FLUX's distilled guidance value. It defaults to 3.5, and
	// schnell-class models expect a low value with very few steps.
	Guidance float64
	// NonCommercial acknowledges that the configured weights are licensed for
	// non-commercial use only. FLUX.1-dev and its quantizations are;
	// FLUX.1-schnell is Apache-2.0. Nothing here can determine the licence of
	// a local file, so the operator states it and GoGIF records it.
	NonCommercial bool
}

func (g GGUF) validate() (GGUF, error) {
	for label, name := range map[string]string{"unet": g.UNet, "clip_l": g.ClipL, "clip_t5": g.ClipT5, "vae": g.VAE} {
		if name == "" {
			return GGUF{}, fmt.Errorf("comfyui: gguf %s filename is required", label)
		}
		if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
			return GGUF{}, fmt.Errorf("comfyui: gguf %s must be a filename from its ComfyUI model directory", label)
		}
	}
	if !strings.HasSuffix(strings.ToLower(g.UNet), ".gguf") {
		return GGUF{}, errors.New("comfyui: gguf unet must be a .gguf quantization")
	}
	if g.Guidance == 0 {
		g.Guidance = 3.5
	}
	if g.Guidance < 0 || g.Guidance > 30 {
		return GGUF{}, errors.New("comfyui: gguf guidance must be between 0 and 30")
	}
	return g, nil
}

// fluxWorkflow builds the quantized FLUX graph. It mirrors the checkpoint
// graph's contract — same node ids for the sampler, latent, and decode — so
// the queue, poll, and output-normalization paths stay shared and audited
// once.
//
// Reference inputs are not wired here. FLUX image-to-image needs a different
// conditioning path, and quietly ignoring a licensed reference would be worse
// than refusing it, so Generate rejects references under this recipe.
func (g *Generator) fluxWorkflow(request imagegen.Request) map[string]any {
	seed := uint64(request.Seed) & uint64(^uint64(0)>>1)
	return map[string]any{
		"3": map[string]any{
			"class_type": "KSampler",
			"inputs": map[string]any{
				// FLUX is distilled: classifier-free guidance is applied by the
				// FluxGuidance node, so the sampler's own CFG stays at 1.
				"cfg": 1.0, "denoise": 1.0, "latent_image": []any{"5", 0},
				"model": []any{"4", 0}, "negative": []any{"7", 0}, "positive": []any{"13", 0},
				"sampler_name": "euler", "scheduler": "simple", "seed": seed, "steps": g.steps,
			},
		},
		"4": map[string]any{"class_type": "UnetLoaderGGUF", "inputs": map[string]any{"unet_name": g.gguf.UNet}},
		"5": map[string]any{"class_type": "EmptySD3LatentImage", "inputs": map[string]any{
			"batch_size": 1, "height": request.Height, "width": request.Width,
		}},
		"6": map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{
			"clip": []any{"11", 0}, "text": imagegen.CompactDiffusionPrompt(request),
		}},
		// FLUX ignores a negative prompt, but the sampler still requires the
		// conditioning input, so it receives an empty encode.
		"7": map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{
			"clip": []any{"11", 0}, "text": "",
		}},
		"8": map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"3", 0}, "vae": []any{"12", 0}}},
		"9": map[string]any{"class_type": "PreviewImage", "inputs": map[string]any{"images": []any{"8", 0}}},
		"11": map[string]any{"class_type": "DualCLIPLoader", "inputs": map[string]any{
			"clip_name1": g.gguf.ClipT5, "clip_name2": g.gguf.ClipL, "type": "flux",
		}},
		"12": map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": g.gguf.VAE}},
		"13": map[string]any{"class_type": "FluxGuidance", "inputs": map[string]any{
			"conditioning": []any{"6", 0}, "guidance": g.gguf.Guidance,
		}},
	}
}
