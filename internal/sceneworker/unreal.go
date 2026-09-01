package sceneworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/scene"
)

const maxSceneFrames = 1800

type UnrealRendererOptions struct {
	Blender            cinematic.Stage
	Unreal             cinematic.Stage
	ReferenceGenerator imagegen.Generator
	FFmpegExecutable   string
	FFmpegTimeout      time.Duration
}

type UnrealRenderer struct {
	blender            cinematic.Stage
	unreal             cinematic.Stage
	referenceGenerator imagegen.Generator
	ffmpeg             string
	ffmpegTimeout      time.Duration
}

func NewUnrealRenderer(options UnrealRendererOptions) (*UnrealRenderer, error) {
	if options.Blender == nil || options.Unreal == nil || options.ReferenceGenerator == nil {
		return nil, errors.New("scene worker: Blender, Unreal, and a semantic reference generator are required")
	}
	executable := strings.TrimSpace(options.FFmpegExecutable)
	if executable == "" {
		executable = "ffmpeg"
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("scene worker: find FFmpeg: %w", err)
	}
	if options.FFmpegTimeout <= 0 {
		options.FFmpegTimeout = 20 * time.Minute
	}
	return &UnrealRenderer{
		blender: options.Blender, unreal: options.Unreal, referenceGenerator: options.ReferenceGenerator,
		ffmpeg: resolved, ffmpegTimeout: options.FFmpegTimeout,
	}, nil
}

func (r *UnrealRenderer) Render(ctx context.Context, project scene.Project, workspace string, progress ProgressFunc) ([]LocalArtifact, error) {
	if project.Target != scene.TargetUnreal {
		return nil, fmt.Errorf("scene worker: Unreal renderer cannot process target %q", project.Target)
	}
	frames := int(math.Round(float64(project.DurationMS) * float64(project.FPS) / 1000))
	if frames < 1 || frames > maxSceneFrames || project.Width%2 != 0 || project.Height%2 != 0 {
		return nil, errors.New("scene worker: project frame count or dimensions are outside render bounds")
	}
	paths := cinematic.ManifestPaths{
		ReferenceImage:  filepath.Join(workspace, "input", "reference.png"),
		BlenderAsset:    filepath.Join(workspace, "blender", "asset.fbx"),
		BlenderPreview:  filepath.Join(workspace, "blender", "preview.png"),
		UnityMotion:     filepath.Join(workspace, "motion", "motion.json"),
		UnrealFrames:    filepath.Join(workspace, "unreal", "frames"),
		CompositeFrames: filepath.Join(workspace, "unused-composite"),
		OutputGIF:       filepath.Join(workspace, "unused.gif"),
	}
	for _, directory := range []string{
		filepath.Dir(paths.ReferenceImage), filepath.Dir(paths.BlenderAsset), filepath.Dir(paths.UnityMotion), paths.UnrealFrames, filepath.Join(workspace, "output"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("scene worker: create stage directory: %w", err)
		}
	}

	progress("acquiring-reference", 5)
	referenceWidth, referenceHeight := boundedReferenceDimensions(project.Width, project.Height)
	reference, err := r.referenceGenerator.Generate(ctx, imagegen.Request{
		Prompt: project.Prompt, Width: referenceWidth, Height: referenceHeight, Seed: project.Seed,
	})
	if err != nil {
		return nil, fmt.Errorf("scene worker: acquire semantic reference: %w", err)
	}
	if reference.ContentType != "image/png" || len(reference.Data) == 0 || len(reference.Data) > imagegen.MaxInputBytes {
		return nil, errors.New("scene worker: semantic reference is not a bounded PNG")
	}
	if err := os.WriteFile(paths.ReferenceImage, reference.Data, 0o600); err != nil {
		return nil, fmt.Errorf("scene worker: write semantic reference: %w", err)
	}
	if err := writeMotion(paths.UnityMotion, frames, project.FPS); err != nil {
		return nil, err
	}

	manifest := cinematic.Manifest{
		Version: cinematic.ManifestVersion, Prompt: project.Prompt, Width: project.Width, Height: project.Height,
		Frames: frames, DelayMS: max(1, int(math.Round(1000/float64(project.FPS)))), Seed: project.Seed,
		Motion: "orbit", Paths: paths,
	}
	manifestPath := filepath.Join(workspace, "manifest.json")
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		return nil, fmt.Errorf("scene worker: write manifest: %w", err)
	}
	job := cinematic.Job{Workspace: workspace, ManifestPath: manifestPath, Manifest: manifest}
	progress("blender-assets", 20)
	if err := r.blender.Run(ctx, job); err != nil {
		return nil, fmt.Errorf("scene worker: Blender stage: %w", err)
	}
	progress("unreal-render", 45)
	if err := r.unreal.Run(ctx, job); err != nil {
		return nil, fmt.Errorf("scene worker: Unreal stage: %w", err)
	}
	progress("encoding-master", 80)
	masterPath, contentType, err := r.encode(ctx, project, paths.UnrealFrames, frames, filepath.Join(workspace, "output"))
	if err != nil {
		return nil, err
	}
	posterPath := filepath.Join(workspace, "output", "poster.png")
	if err := copyBounded(filepath.Join(paths.UnrealFrames, "frame-0000.png"), posterPath, 32<<20); err != nil {
		return nil, fmt.Errorf("scene worker: create poster: %w", err)
	}
	return []LocalArtifact{
		{Kind: "video", Path: masterPath, Filename: filepath.Base(masterPath), ContentType: contentType},
		{Kind: "poster", Path: posterPath, Filename: "poster.png", ContentType: "image/png"},
		{Kind: "asset", Path: paths.BlenderAsset, Filename: "asset.fbx", ContentType: "application/octet-stream"},
	}, nil
}

func (r *UnrealRenderer) encode(ctx context.Context, project scene.Project, framesDirectory string, frames int, outputDirectory string) (string, string, error) {
	extension, contentType := string(project.Format), "video/"+string(project.Format)
	output := filepath.Join(outputDirectory, "master."+extension)
	arguments := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-framerate", strconv.Itoa(project.FPS), "-start_number", "0",
		"-i", filepath.Join(framesDirectory, "frame-%04d.png"), "-frames:v", strconv.Itoa(frames), "-an",
	}
	switch project.Format {
	case scene.FormatMP4:
		arguments = append(arguments, "-c:v", "libx264", "-preset", "slow", "-crf", "18", "-pix_fmt", "yuv420p", "-movflags", "+faststart")
	case scene.FormatWebM:
		arguments = append(arguments, "-c:v", "libvpx-vp9", "-crf", "28", "-b:v", "0", "-pix_fmt", "yuv420p")
	default:
		return "", "", errors.New("scene worker: unsupported master format")
	}
	arguments = append(arguments, output)
	encodeContext, cancel := context.WithTimeout(ctx, r.ffmpegTimeout)
	defer cancel()
	command := exec.CommandContext(encodeContext, r.ffmpeg, arguments...)
	var diagnostics boundedBuffer
	command.Stdout, command.Stderr = &diagnostics, &diagnostics
	if err := command.Run(); err != nil {
		if encodeContext.Err() != nil {
			return "", "", fmt.Errorf("scene worker: FFmpeg timed out: %w", encodeContext.Err())
		}
		return "", "", fmt.Errorf("scene worker: FFmpeg failed: %w: %s", err, diagnostics.String())
	}
	file, err := os.Open(output)
	if err != nil {
		return "", "", errors.New("scene worker: FFmpeg did not create a master")
	}
	header := make([]byte, 512)
	read, readErr := file.Read(header)
	info, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", "", readErr
	}
	if statErr != nil || closeErr != nil || info.Size() < 1 || info.Size() > scene.DefaultMaxArtifactBytes || http.DetectContentType(header[:read]) != contentType {
		return "", "", errors.New("scene worker: encoded master failed size or MIME validation")
	}
	return output, contentType, nil
}

type motionContract struct {
	Version int           `json:"version"`
	Frames  []motionFrame `json:"frames"`
}

type motionFrame struct {
	Yaw        float64 `json:"yaw"`
	Pitch      float64 `json:"pitch"`
	CameraX    float64 `json:"camera_x"`
	CameraY    float64 `json:"camera_y"`
	CameraZoom float64 `json:"camera_zoom"`
}

func writeMotion(filename string, frames, fps int) error {
	contract := motionContract{Version: cinematic.ManifestVersion, Frames: make([]motionFrame, frames)}
	for index := range frames {
		phase := float64(index) / float64(max(1, frames-1))
		angle := phase * 2 * math.Pi
		contract.Frames[index] = motionFrame{
			Yaw: phase * 360, Pitch: math.Sin(angle) * 4, CameraX: math.Sin(angle) * 0.3,
			CameraY: math.Cos(angle) * 0.12, CameraZoom: 1 + math.Sin(angle)*0.04,
		}
	}
	data, err := json.Marshal(contract)
	if err != nil {
		return err
	}
	if len(data) > 8<<20 || fps < 1 {
		return errors.New("scene worker: motion contract exceeds safe bounds")
	}
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		return fmt.Errorf("scene worker: write motion contract: %w", err)
	}
	return nil
}

func boundedReferenceDimensions(width, height int) (int, int) {
	if width <= imagegen.MaxDimension && height <= imagegen.MaxDimension {
		return width, height
	}
	scale := math.Min(float64(imagegen.MaxDimension)/float64(width), float64(imagegen.MaxDimension)/float64(height))
	return max(64, int(math.Round(float64(width)*scale))), max(64, int(math.Round(float64(height)*scale)))
}

func copyBounded(source, target string, limit int64) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || written < 1 || written > limit {
		_ = os.Remove(target)
		return errors.New("copied file exceeds safe bounds")
	}
	return nil
}

type boundedBuffer struct{ data []byte }

func (b *boundedBuffer) Write(data []byte) (int, error) {
	const limit = 64 << 10
	if remaining := limit - len(b.data); remaining > 0 {
		b.data = append(b.data, data[:min(remaining, len(data))]...)
	}
	return len(data), nil
}

func (b *boundedBuffer) String() string { return string(bytes.TrimSpace(b.data)) }
