package comfypartner

import (
	"context"
	"fmt"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/motiongen"
)

// MotionGenerator uses Luma Ray 3.2 through an allowlisted Comfy Partner Node
// workflow. The generated five-second loop is sampled by FFmpeg into the
// requested GIF frame count and playback speed.
type MotionGenerator struct {
	client *Generator
}

func NewMotion(options Options) (*MotionGenerator, error) {
	options.ValidationAttempts = 0
	client, err := New(options)
	if err != nil {
		return nil, err
	}
	return &MotionGenerator{client: client}, nil
}

func (g *MotionGenerator) Descriptor() motiongen.Descriptor {
	return motiongen.Descriptor{ID: "comfyui-partner-luma-ray-3.2", Label: "ComfyUI · Luma Ray 3.2", Hosted: true}
}

func (g *MotionGenerator) Generate(ctx context.Context, request motiongen.Request) (motiongen.Result, error) {
	if g == nil || g.client == nil {
		return motiongen.Result{}, ErrUnavailable
	}
	if err := request.Validate(); err != nil {
		return motiongen.Result{}, err
	}
	uploaded, err := g.client.uploadImage(ctx, request.Input.Data)
	if err != nil {
		return motiongen.Result{}, err
	}
	promptID, err := g.client.queue(ctx, lumaMotionWorkflow(request, uploaded))
	if err != nil {
		return motiongen.Result{}, err
	}
	output, err := g.client.waitForOutput(ctx, promptID)
	if err != nil {
		return motiongen.Result{}, err
	}
	data, err := g.client.fetchOutput(ctx, output)
	if err != nil {
		return motiongen.Result{}, fmt.Errorf("fetch motion video: %w", err)
	}
	if len(data) < 12 {
		return motiongen.Result{}, fmt.Errorf("comfy partner: motion video is empty")
	}
	return motiongen.Result{
		Data: data, ContentType: "video/mp4", Filename: output.Filename,
		Engine: g.Descriptor().ID, SourceDurationMS: 5000,
	}, nil
}

func lumaMotionWorkflow(request motiongen.Request, uploaded uploadedImage) map[string]any {
	name := uploaded.Name
	if uploaded.Subfolder != "" {
		name = strings.Trim(uploaded.Subfolder, "/") + "/" + name
	}
	resolution := "540p"
	if request.Width > 480 || request.Height > 480 {
		resolution = "720p"
	}
	seed := uint64(request.Seed)
	return map[string]any{
		"1": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": name}},
		"2": map[string]any{"class_type": "LumaRay32ImageToVideoNode", "inputs": map[string]any{
			"prompt": motionPrompt(request.Prompt), "resolution": resolution, "loop": true,
			"seed": seed, "start_frame": []any{"1", 0},
		}},
		"3": map[string]any{"class_type": "SaveVideo", "inputs": map[string]any{
			"video": []any{"2", 0}, "filename_prefix": "gogif/motion/luma-ray-3.2",
			"format": "mp4", "format.codec": "auto", "codec": "auto",
		}},
	}
}

func motionPrompt(prompt string) string {
	return fmt.Sprintf(`Animate this exact source image as a seamless five-second loop that fulfills: %s. Preserve the subjects, identity, setting, materials, composition, and aspect ratio. Add physically plausible subject and environmental motion plus restrained cinematic camera movement. Keep the first and last frame visually continuous. Do not morph identities, replace the setting, add objects, add text, add logos, add watermarks, add borders, or letterbox the image.`, strings.TrimSpace(prompt))
}
