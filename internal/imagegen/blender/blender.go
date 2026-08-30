// Package blender uses the local Blender executable as a free procedural
// still-image engine. Blender does not require an account, network, or API key.
package blender

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

//go:embed scene.py
var sceneScript []byte

var ErrUnavailable = errors.New("blender: local executable unavailable")

type Options struct {
	Executable string
	Timeout    time.Duration
}

type Generator struct {
	executable string
	timeout    time.Duration
}

func New(options Options) (*Generator, error) {
	if options.Executable == "" {
		options.Executable = "blender"
	}
	executable, err := exec.LookPath(options.Executable)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if options.Timeout <= 0 {
		options.Timeout = 90 * time.Second
	}
	return &Generator{executable: executable, timeout: options.Timeout}, nil
}

func (g *Generator) Descriptor() imagegen.Descriptor {
	return imagegen.Descriptor{ID: "blender-local", Label: "Blender (local)", Local: true}
}

func (g *Generator) Generate(ctx context.Context, request imagegen.Request) (imagegen.Result, error) {
	if err := request.Validate(); err != nil {
		return imagegen.Result{}, err
	}
	if len(request.Inputs) != 0 {
		return imagegen.Result{}, errors.New("blender: the procedural workflow does not accept reference images")
	}
	directory, err := os.MkdirTemp("", "gogif-blender-*")
	if err != nil {
		return imagegen.Result{}, fmt.Errorf("blender: create temporary directory: %w", err)
	}
	defer os.RemoveAll(directory)
	requestPath := filepath.Join(directory, "request.json")
	scriptPath := filepath.Join(directory, "scene.py")
	outputPath := filepath.Join(directory, "output.png")
	requestData, _ := json.Marshal(map[string]any{
		"width": request.Width, "height": request.Height, "seed": request.Seed,
	})
	if err := os.WriteFile(requestPath, requestData, 0o600); err != nil {
		return imagegen.Result{}, fmt.Errorf("blender: write request: %w", err)
	}
	if err := os.WriteFile(scriptPath, sceneScript, 0o600); err != nil {
		return imagegen.Result{}, fmt.Errorf("blender: write scene script: %w", err)
	}

	renderContext, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	command := exec.CommandContext(renderContext, g.executable,
		"--background", "--factory-startup", "--python", scriptPath, "--",
		"--request", requestPath, "--output", outputPath,
	)
	var commandOutput cappedBuffer
	command.Stdout = &commandOutput
	command.Stderr = &commandOutput
	if err := command.Run(); err != nil {
		if renderContext.Err() != nil {
			return imagegen.Result{}, fmt.Errorf("blender: render timed out: %w", renderContext.Err())
		}
		return imagegen.Result{}, fmt.Errorf("blender: render failed: %w: %s", err, commandOutput.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return imagegen.Result{}, fmt.Errorf("blender: read output: %w: %s", err, commandOutput.String())
	}
	if len(data) == 0 || len(data) > imagegen.MaxInputBytes || http.DetectContentType(data) != "image/png" {
		return imagegen.Result{}, errors.New("blender: renderer did not produce a valid bounded PNG")
	}
	configuration, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || configuration.Width != request.Width || configuration.Height != request.Height {
		return imagegen.Result{}, errors.New("blender: output dimensions do not match the request")
	}
	return imagegen.Result{Data: data, ContentType: "image/png", Engine: g.Descriptor().ID}, nil
}

type cappedBuffer struct {
	data []byte
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	const maximum = 64 << 10
	remaining := maximum - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, data[:min(remaining, len(data))]...)
	}
	return len(data), nil
}

func (b *cappedBuffer) String() string { return string(b.data) }
