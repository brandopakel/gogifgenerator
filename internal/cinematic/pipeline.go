package cinematic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	stdgif "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/render"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type PipelineOptions struct {
	ReferenceGenerator imagegen.Generator
	Stages             []Stage
	Encoder            Encoder
	TempDir            string
}

type Pipeline struct {
	referenceGenerator imagegen.Generator
	stages             []Stage
	encoder            Encoder
	tempDir            string
	slot               chan struct{}
}

func New(options PipelineOptions) (*Pipeline, error) {
	if len(options.Stages) == 0 || options.Encoder == nil {
		return nil, errors.New("cinematic: at least one render stage and an encoder are required")
	}
	seen := make(map[string]bool, len(options.Stages)+1)
	for _, stage := range options.Stages {
		if stage == nil || strings.TrimSpace(stage.Descriptor().ID) == "" || seen[stage.Descriptor().ID] {
			return nil, errors.New("cinematic: stages must have unique non-empty IDs")
		}
		seen[stage.Descriptor().ID] = true
	}
	if descriptor := options.Encoder.Descriptor(); descriptor.ID == "" || seen[descriptor.ID] {
		return nil, errors.New("cinematic: encoder must have a unique non-empty ID")
	}
	pipeline := &Pipeline{
		referenceGenerator: options.ReferenceGenerator,
		stages:             append([]Stage(nil), options.Stages...),
		encoder:            options.Encoder,
		tempDir:            options.TempDir,
		slot:               make(chan struct{}, 1),
	}
	pipeline.slot <- struct{}{}
	return pipeline, nil
}

func (p *Pipeline) Descriptor() Descriptor {
	stages := make([]StageDescriptor, 0, len(p.stages)+2)
	if p.referenceGenerator != nil {
		reference := p.referenceGenerator.Descriptor()
		stages = append(stages, StageDescriptor{
			ID: reference.ID, Label: reference.Label, Role: "reference imagery", Local: reference.Local, Available: true,
		})
	}
	for _, stage := range p.stages {
		stages = append(stages, stage.Descriptor())
	}
	stages = append(stages, p.encoder.Descriptor())
	return Descriptor{
		ID: "cinematic-local", Label: "Cinematic local pipeline", Local: true, Enabled: true,
		SupportsReferences: true, Stages: stages,
	}
}

func (p *Pipeline) Render(ctx context.Context, request Request) (Result, error) {
	request, err := request.Validate()
	if err != nil {
		return Result{}, err
	}
	select {
	case <-ctx.Done():
		return Result{}, fmt.Errorf("cinematic: wait for local engines: %w", ctx.Err())
	case <-p.slot:
	}
	defer func() { p.slot <- struct{}{} }()
	workspace, err := os.MkdirTemp(p.tempDir, "gogif-cinematic-")
	if err != nil {
		return Result{}, fmt.Errorf("cinematic: create workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	paths := ManifestPaths{
		BlenderAsset:    filepath.Join(workspace, "blender", "asset.fbx"),
		BlenderPreview:  filepath.Join(workspace, "blender", "preview.png"),
		UnityFrames:     filepath.Join(workspace, "unity", "frames"),
		UnityMotion:     filepath.Join(workspace, "unity", "motion.json"),
		UnrealFrames:    filepath.Join(workspace, "unreal", "frames"),
		CompositeFrames: filepath.Join(workspace, "composite", "frames"),
		OutputGIF:       filepath.Join(workspace, "output", "gogif.gif"),
	}
	for _, directory := range []string{
		filepath.Dir(paths.BlenderAsset), paths.UnityFrames, paths.UnrealFrames, paths.CompositeFrames, filepath.Dir(paths.OutputGIF),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return Result{}, fmt.Errorf("cinematic: create stage directory: %w", err)
		}
	}

	reference, referenceEngine, err := p.reference(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if len(reference) != 0 {
		paths.ReferenceImage = filepath.Join(workspace, "reference.png")
		if err := os.WriteFile(paths.ReferenceImage, reference, 0o600); err != nil {
			return Result{}, fmt.Errorf("cinematic: write reference image: %w", err)
		}
	}
	manifest := Manifest{
		Version: ManifestVersion, Prompt: request.Prompt,
		Width: request.Spec.Width, Height: request.Spec.Height, Frames: request.Spec.Frames, DelayMS: request.Spec.DelayMS,
		Seed: request.Spec.Seed, Motion: request.Spec.Motion, Palette: append([]string(nil), request.Spec.Palette...),
		Caption: request.Spec.Caption, ShowCaption: request.Spec.ShowPrompt, Paths: paths,
	}
	manifestPath := filepath.Join(workspace, "manifest.json")
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("cinematic: encode manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		return Result{}, fmt.Errorf("cinematic: write manifest: %w", err)
	}
	job := Job{Workspace: workspace, ManifestPath: manifestPath, Manifest: manifest}
	engineParts := make([]string, 0, len(p.stages)+2)
	if referenceEngine != "" {
		engineParts = append(engineParts, referenceEngine)
	}
	for _, stage := range p.stages {
		if err := stage.Run(ctx, job); err != nil {
			return Result{}, fmt.Errorf("cinematic: %s stage: %w", stage.Descriptor().ID, err)
		}
		engineParts = append(engineParts, stage.Descriptor().ID)
	}
	if err := compositeSequences(manifest); err != nil {
		return Result{}, err
	}
	data, err := p.encoder.Encode(ctx, FrameSequence{
		Directory: paths.CompositeFrames, Pattern: "frame-%04d.png", Width: manifest.Width, Height: manifest.Height,
		Frames: manifest.Frames, DelayMS: manifest.DelayMS,
	})
	if err != nil {
		return Result{}, fmt.Errorf("cinematic: %s encoder: %w", p.encoder.Descriptor().ID, err)
	}
	if err := validateGIF(data, manifest); err != nil {
		return Result{}, err
	}
	engineParts = append(engineParts, p.encoder.Descriptor().ID)
	return Result{Data: data, ContentType: "image/gif", Engine: strings.Join(engineParts, "+")}, nil
}

func (p *Pipeline) reference(ctx context.Context, request Request) ([]byte, string, error) {
	if p.referenceGenerator != nil {
		result, err := p.referenceGenerator.Generate(ctx, imagegen.Request{
			Prompt: request.Prompt, Inputs: request.Inputs, Width: request.Spec.Width, Height: request.Spec.Height, Seed: request.Spec.Seed,
		})
		if err != nil {
			return nil, "", fmt.Errorf("cinematic: reference generator: %w", err)
		}
		data, err := normalizePNG(result.Data, request.Spec.Width, request.Spec.Height)
		if err != nil {
			return nil, "", fmt.Errorf("cinematic: reference generator output: %w", err)
		}
		return data, result.Engine, nil
	}
	if len(request.Inputs) == 0 {
		return nil, "", nil
	}
	data, err := normalizePNG(request.Inputs[0].Data, request.Spec.Width, request.Spec.Height)
	if err != nil {
		return nil, "", fmt.Errorf("cinematic: reference input: %w", err)
	}
	return data, "reference", nil
}

func normalizePNG(data []byte, width, height int) ([]byte, error) {
	if len(data) == 0 || len(data) > imagegen.MaxInputBytes {
		return nil, errors.New("image is empty or too large")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("image is not decodable")
	}
	normalized := image.NewNRGBA(image.Rect(0, 0, width, height))
	sourceBounds := decoded.Bounds()
	sourceAspect := float64(sourceBounds.Dx()) / float64(sourceBounds.Dy())
	targetAspect := float64(width) / float64(height)
	crop := sourceBounds
	if sourceAspect > targetAspect {
		cropWidth := max(1, int(math.Round(float64(sourceBounds.Dy())*targetAspect)))
		crop.Min.X += (sourceBounds.Dx() - cropWidth) / 2
		crop.Max.X = crop.Min.X + cropWidth
	} else if sourceAspect < targetAspect {
		cropHeight := max(1, int(math.Round(float64(sourceBounds.Dx())/targetAspect)))
		crop.Min.Y += (sourceBounds.Dy() - cropHeight) / 2
		crop.Max.Y = crop.Min.Y + cropHeight
	}
	xdraw.CatmullRom.Scale(normalized, normalized.Bounds(), decoded, crop, xdraw.Src, nil)
	var output bytes.Buffer
	if err := png.Encode(&output, normalized); err != nil {
		return nil, fmt.Errorf("encode PNG: %w", err)
	}
	if output.Len() > imagegen.MaxInputBytes || http.DetectContentType(output.Bytes()) != "image/png" {
		return nil, errors.New("normalized PNG exceeds safe bounds")
	}
	return output.Bytes(), nil
}

func compositeSequences(manifest Manifest) error {
	var reference image.Image
	if manifest.Paths.ReferenceImage != "" {
		var err error
		reference, err = decodeFrame(manifest.Paths.ReferenceImage, manifest.Width, manifest.Height)
		if err != nil {
			return fmt.Errorf("cinematic: semantic reference: %w", err)
		}
	}
	for index := range manifest.Frames {
		beautyPath := filepath.Join(manifest.Paths.UnrealFrames, fmt.Sprintf("frame-%04d.png", index))
		overlayPath := filepath.Join(manifest.Paths.UnityFrames, fmt.Sprintf("frame-%04d.png", index))
		beauty, err := decodeFrame(beautyPath, manifest.Width, manifest.Height)
		if err != nil {
			return fmt.Errorf("cinematic: Unreal beauty frame %d: %w", index, err)
		}
		overlay, err := decodeFrame(overlayPath, manifest.Width, manifest.Height)
		if err != nil {
			return fmt.Errorf("cinematic: Unity VFX frame %d: %w", index, err)
		}
		composite := image.NewNRGBA(image.Rect(0, 0, manifest.Width, manifest.Height))
		if reference == nil {
			draw.Draw(composite, composite.Bounds(), beauty, beauty.Bounds().Min, draw.Src)
		} else {
			draw.Draw(composite, composite.Bounds(), reference, reference.Bounds().Min, draw.Src)
			draw.DrawMask(composite, composite.Bounds(), beauty, beauty.Bounds().Min, image.NewUniform(color.Alpha{A: 96}), image.Point{}, draw.Over)
		}
		draw.Draw(composite, composite.Bounds(), overlay, overlay.Bounds().Min, draw.Over)
		if manifest.ShowCaption {
			render.CaptionImage(composite, manifest.Caption, index, manifest.Frames, "bottom")
		}
		path := filepath.Join(manifest.Paths.CompositeFrames, fmt.Sprintf("frame-%04d.png", index))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("cinematic: create composite frame: %w", err)
		}
		encodeErr := png.Encode(file, composite)
		closeErr := file.Close()
		if encodeErr != nil || closeErr != nil {
			return fmt.Errorf("cinematic: encode composite frame: %v %v", encodeErr, closeErr)
		}
	}
	return nil
}

func decodeFrame(path string, width, height int) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	frame, err := png.Decode(file)
	if err != nil {
		return nil, errors.New("frame is not a PNG")
	}
	if frame.Bounds().Dx() != width || frame.Bounds().Dy() != height {
		return nil, fmt.Errorf("frame dimensions must be %dx%d", width, height)
	}
	return frame, nil
}

func validateGIF(data []byte, manifest Manifest) error {
	if len(data) == 0 || len(data) > MaxOutputBytes || http.DetectContentType(data) != "image/gif" {
		return errors.New("cinematic: encoder did not produce a bounded GIF")
	}
	animation, err := stdgif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return errors.New("cinematic: encoder output is not a valid GIF")
	}
	if animation.Config.Width != manifest.Width || animation.Config.Height != manifest.Height || len(animation.Image) != manifest.Frames {
		return errors.New("cinematic: encoded GIF dimensions or frame count do not match the manifest")
	}
	return nil
}
