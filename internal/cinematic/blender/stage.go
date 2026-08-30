// Package blender creates portable scene geometry for the cinematic pipeline.
package blender

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
)

//go:embed asset.py
var assetScript []byte

type Options struct {
	Executable string
	Timeout    time.Duration
}

type Stage struct {
	executable string
	timeout    time.Duration
	version    string
}

func New(options Options) (*Stage, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "blender"
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("%w: find Blender: %v", cinematic.ErrUnavailable, err)
	}
	if options.Timeout <= 0 {
		options.Timeout = 3 * time.Minute
	}
	return &Stage{executable: resolved, timeout: options.Timeout, version: executableVersion(resolved, "--version")}, nil
}

func (s *Stage) Descriptor() cinematic.StageDescriptor {
	return cinematic.StageDescriptor{
		ID: "blender", Label: "Blender", Role: "assets and geometry", Local: true,
		Available: s != nil && s.executable != "", Version: s.version,
	}
}

func (s *Stage) Run(ctx context.Context, job cinematic.Job) error {
	if s == nil || s.executable == "" {
		return errors.New("Blender is not configured")
	}
	scriptPath := filepath.Join(job.Workspace, "blender-stage.py")
	if err := os.WriteFile(scriptPath, assetScript, 0o600); err != nil {
		return fmt.Errorf("write Blender script: %w", err)
	}
	renderContext, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	command := exec.CommandContext(renderContext, s.executable,
		"--background", "--factory-startup", "--python", scriptPath, "--", "--manifest", job.ManifestPath,
	)
	var diagnostics cappedBuffer
	command.Stdout = &diagnostics
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		if renderContext.Err() != nil {
			return fmt.Errorf("render timed out: %w", renderContext.Err())
		}
		return fmt.Errorf("render failed: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	assetInfo, err := os.Stat(job.Manifest.Paths.BlenderAsset)
	if err != nil || assetInfo.IsDir() || assetInfo.Size() == 0 || assetInfo.Size() > 256<<20 {
		return errors.New("Blender did not create a bounded FBX asset")
	}
	preview, err := os.Open(job.Manifest.Paths.BlenderPreview)
	if err != nil {
		return errors.New("Blender did not create a preview PNG")
	}
	configuration, decodeErr := png.DecodeConfig(preview)
	closeErr := preview.Close()
	if decodeErr != nil || closeErr != nil || configuration.Width != job.Manifest.Width || configuration.Height != job.Manifest.Height {
		return errors.New("Blender preview dimensions do not match the manifest")
	}
	return nil
}

func executableVersion(executable string, argument string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, executable, argument).Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	return strings.TrimSpace(line)
}

type cappedBuffer struct{ data []byte }

func (b *cappedBuffer) Write(data []byte) (int, error) {
	const maximum = 64 << 10
	if remaining := maximum - len(b.data); remaining > 0 {
		b.data = append(b.data, data[:min(remaining, len(data))]...)
	}
	return len(data), nil
}

func (b *cappedBuffer) String() string { return string(bytes.TrimSpace(b.data)) }
