package cinematic

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
)

func TestPipelineComposesStagesAndCleansWorkspace(t *testing.T) {
	var workspace string
	stages := []Stage{
		fakeStage{descriptor: StageDescriptor{ID: "blender", Label: "Blender", Role: "assets", Available: true}, run: func(job Job) error {
			workspace = job.Workspace
			if err := os.WriteFile(job.Manifest.Paths.BlenderAsset, []byte("fbx"), 0o600); err != nil {
				return err
			}
			return writeSolidPNG(job.Manifest.Paths.BlenderPreview, job.Manifest.Width, job.Manifest.Height, color.NRGBA{R: 30, A: 255})
		}},
		fakeStage{descriptor: StageDescriptor{ID: "unity-6.3", Label: "Unity", Role: "VFX", Available: true}, run: func(job Job) error {
			if _, err := os.Stat(job.Manifest.Paths.BlenderAsset); err != nil {
				return err
			}
			for index := range job.Manifest.Frames {
				if err := writeSolidPNG(filepath.Join(job.Manifest.Paths.UnityFrames, fmt.Sprintf("frame-%04d.png", index)), job.Manifest.Width, job.Manifest.Height, color.NRGBA{B: 255, A: 96}); err != nil {
					return err
				}
			}
			return os.WriteFile(job.Manifest.Paths.UnityMotion, []byte(`{"version":1,"frames":[{},{},{},{}]}`), 0o600)
		}},
		fakeStage{descriptor: StageDescriptor{ID: "unreal-5", Label: "Unreal", Role: "beauty", Available: true}, run: func(job Job) error {
			if _, err := os.Stat(job.Manifest.Paths.UnityMotion); err != nil {
				return err
			}
			for index := range job.Manifest.Frames {
				shade := uint8(80 + index*20)
				if err := writeSolidPNG(filepath.Join(job.Manifest.Paths.UnrealFrames, fmt.Sprintf("frame-%04d.png", index)), job.Manifest.Width, job.Manifest.Height, color.NRGBA{R: shade, G: 40, A: 255}); err != nil {
					return err
				}
			}
			return nil
		}},
	}
	pipeline, err := New(PipelineOptions{Stages: stages, Encoder: fakeEncoder{}})
	if err != nil {
		t.Fatal(err)
	}
	spec := gifdomain.Defaults()
	spec.Width, spec.Height, spec.Frames = 128, 128, 4
	result, err := pipeline.Render(context.Background(), Request{Prompt: "cinematic robot", Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != "blender+unity-6.3+unreal-5+test-encoder" || result.ContentType != "image/gif" {
		t.Fatalf("result = %#v", result)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(result.Data))
	if err != nil || len(animation.Image) != 4 || animation.Config.Width != 128 {
		t.Fatalf("animation = %#v, %v", animation, err)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists: %s (%v)", workspace, err)
	}
}

func TestPipelineRejectsMissingEngineFrames(t *testing.T) {
	pipeline, err := New(PipelineOptions{
		Stages: []Stage{fakeStage{
			descriptor: StageDescriptor{ID: "broken", Label: "Broken", Role: "test", Available: true},
			run:        func(Job) error { return nil },
		}},
		Encoder: fakeEncoder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := gifdomain.Defaults()
	spec.Width, spec.Height, spec.Frames = 128, 128, 4
	if _, err := pipeline.Render(context.Background(), Request{Prompt: "missing frames", Spec: spec}); err == nil {
		t.Fatal("Render() error = nil")
	}
}

func TestCompositeSequencesPreservesReferenceAndUnrealBeauty(t *testing.T) {
	workspace := t.TempDir()
	manifest := Manifest{
		Width: 4, Height: 4, Frames: 1,
		Paths: ManifestPaths{
			ReferenceImage:  filepath.Join(workspace, "reference.png"),
			UnrealFrames:    filepath.Join(workspace, "unreal"),
			UnityFrames:     filepath.Join(workspace, "unity"),
			CompositeFrames: filepath.Join(workspace, "composite"),
		},
	}
	for _, directory := range []string{manifest.Paths.UnrealFrames, manifest.Paths.UnityFrames, manifest.Paths.CompositeFrames} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeSolidPNG(manifest.Paths.ReferenceImage, 4, 4, color.NRGBA{R: 255, A: 255}); err != nil {
		t.Fatal(err)
	}
	if err := writeSolidPNG(filepath.Join(manifest.Paths.UnrealFrames, "frame-0000.png"), 4, 4, color.NRGBA{B: 255, A: 255}); err != nil {
		t.Fatal(err)
	}
	if err := writeSolidPNG(filepath.Join(manifest.Paths.UnityFrames, "frame-0000.png"), 4, 4, color.NRGBA{}); err != nil {
		t.Fatal(err)
	}
	if err := compositeSequences(manifest); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(manifest.Paths.CompositeFrames, "frame-0000.png"))
	if err != nil {
		t.Fatal(err)
	}
	frame, decodeErr := png.Decode(file)
	closeErr := file.Close()
	if decodeErr != nil || closeErr != nil {
		t.Fatalf("decode composite: %v; close: %v", decodeErr, closeErr)
	}
	red, _, blue, _ := frame.At(2, 2).RGBA()
	if red == 0 || blue == 0 {
		t.Fatalf("composite pixel = %#v; reference or Unreal beauty was discarded", frame.At(2, 2))
	}
}

func TestAnimateReferenceCreatesMotionWithoutDiscardingSource(t *testing.T) {
	reference := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			reference.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: 80, A: 255})
		}
	}
	first := animateReference(reference, 64, 64, 0, 8, "waves")
	second := animateReference(reference, 64, 64, 2, 8, "waves")
	if first.At(32, 32) == second.At(32, 32) {
		t.Fatal("animated reference frames are identical at the focal point")
	}
	_, _, _, alpha := second.At(32, 32).RGBA()
	if alpha == 0 {
		t.Fatal("animated reference discarded the semantic source")
	}
}

func TestUnityOverlayIsRestrainedExceptForExplicitConfetti(t *testing.T) {
	if got := unityOverlayOpacity("orbit"); got != 32 {
		t.Fatalf("orbit opacity = %d, want 32", got)
	}
	if got := unityOverlayOpacity("confetti"); got != 255 {
		t.Fatalf("confetti opacity = %d, want 255", got)
	}
}

func TestNewRejectsDuplicateStageIDs(t *testing.T) {
	stage := fakeStage{descriptor: StageDescriptor{ID: "same", Label: "Same", Role: "test"}}
	if _, err := New(PipelineOptions{Stages: []Stage{stage, stage}, Encoder: fakeEncoder{}}); err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestPipelineHonorsCancellationWhileWaitingForEngineSlot(t *testing.T) {
	pipeline, err := New(PipelineOptions{
		Stages:  []Stage{fakeStage{descriptor: StageDescriptor{ID: "stage", Label: "Stage", Role: "test"}}},
		Encoder: fakeEncoder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-pipeline.slot
	defer func() { pipeline.slot <- struct{}{} }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spec := gifdomain.Defaults()
	if _, err := pipeline.Render(ctx, Request{Prompt: "cancel me", Spec: spec}); err == nil {
		t.Fatal("Render() error = nil")
	}
}

type fakeStage struct {
	descriptor StageDescriptor
	run        func(Job) error
}

func (s fakeStage) Descriptor() StageDescriptor { return s.descriptor }

func (s fakeStage) Run(_ context.Context, job Job) error {
	if s.run == nil {
		return nil
	}
	return s.run(job)
}

type fakeEncoder struct{}

func (fakeEncoder) Descriptor() StageDescriptor {
	return StageDescriptor{ID: "test-encoder", Label: "Test encoder", Role: "encoding", Available: true}
}

func (fakeEncoder) Encode(_ context.Context, sequence FrameSequence) ([]byte, error) {
	animation := &gif.GIF{LoopCount: 0}
	palette := color.Palette{color.Black, color.White, color.RGBA{R: 80, G: 40, B: 96, A: 255}, color.RGBA{R: 140, G: 40, B: 96, A: 255}}
	for index := range sequence.Frames {
		file, err := os.Open(filepath.Join(sequence.Directory, fmt.Sprintf("frame-%04d.png", index)))
		if err != nil {
			return nil, err
		}
		frame, err := png.Decode(file)
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		paletted := image.NewPaletted(frame.Bounds(), palette)
		draw.FloydSteinberg.Draw(paletted, paletted.Bounds(), frame, frame.Bounds().Min)
		animation.Image = append(animation.Image, paletted)
		animation.Delay = append(animation.Delay, sequence.DelayMS/10)
	}
	var output bytes.Buffer
	if err := gif.EncodeAll(&output, animation); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeSolidPNG(path string, width, height int, value color.NRGBA) error {
	frame := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(frame, frame.Bounds(), &image.Uniform{C: value}, image.Point{}, draw.Src)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, frame)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}
