// Package cinematic defines the contract for multi-engine, frame-sequence
// rendering. It is intentionally separate from imagegen: still-image engines
// create references, while cinematic renderers own animation and final encoding.
package cinematic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

var ErrUnavailable = errors.New("cinematic: renderer unavailable")

const (
	ManifestVersion = 1
	MaxOutputBytes  = 64 << 20
)

type Request struct {
	Prompt string
	Inputs []imagegen.Input
	Spec   gifdomain.Spec
}

func (r Request) Validate() (Request, error) {
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Prompt == "" || len(r.Prompt) > 2000 {
		return Request{}, errors.New("cinematic: prompt must contain between 1 and 2000 characters")
	}
	spec, err := r.Spec.Normalize()
	if err != nil {
		return Request{}, fmt.Errorf("cinematic: invalid animation spec: %w", err)
	}
	r.Spec = spec
	if len(r.Inputs) > imagegen.MaxInputs {
		return Request{}, fmt.Errorf("cinematic: at most %d reference inputs are allowed", imagegen.MaxInputs)
	}
	for index, input := range r.Inputs {
		if len(input.Data) == 0 || len(input.Data) > imagegen.MaxInputBytes || !strings.HasPrefix(input.ContentType, "image/") {
			return Request{}, fmt.Errorf("cinematic: input %d must be an image no larger than %d bytes", index, imagegen.MaxInputBytes)
		}
	}
	return r, nil
}

type StageDescriptor struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Role      string `json:"role"`
	Local     bool   `json:"local"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type Descriptor struct {
	ID                 string            `json:"id"`
	Label              string            `json:"label"`
	Local              bool              `json:"local"`
	Enabled            bool              `json:"enabled"`
	SupportsReferences bool              `json:"supports_references"`
	Stages             []StageDescriptor `json:"stages"`
}

type Result struct {
	Data        []byte
	ContentType string
	Engine      string
}

type Renderer interface {
	Descriptor() Descriptor
	Render(context.Context, Request) (Result, error)
}

type Job struct {
	Workspace    string
	ManifestPath string
	Manifest     Manifest
}

type Stage interface {
	Descriptor() StageDescriptor
	Run(context.Context, Job) error
}

type FrameSequence struct {
	Directory string
	Pattern   string
	Width     int
	Height    int
	Frames    int
	DelayMS   int
}

type Encoder interface {
	Descriptor() StageDescriptor
	Encode(context.Context, FrameSequence) ([]byte, error)
}

type Manifest struct {
	Version     int           `json:"version"`
	Prompt      string        `json:"prompt"`
	Width       int           `json:"width"`
	Height      int           `json:"height"`
	Frames      int           `json:"frames"`
	DelayMS     int           `json:"delay_ms"`
	Seed        int64         `json:"seed"`
	Motion      string        `json:"motion"`
	Palette     []string      `json:"palette"`
	Caption     string        `json:"caption"`
	ShowCaption bool          `json:"show_caption"`
	Paths       ManifestPaths `json:"paths"`
}

type ManifestPaths struct {
	ReferenceImage  string `json:"reference_image,omitempty"`
	BlenderAsset    string `json:"blender_asset"`
	BlenderPreview  string `json:"blender_preview"`
	UnityFrames     string `json:"unity_frames"`
	UnityMotion     string `json:"unity_motion"`
	UnrealFrames    string `json:"unreal_frames"`
	CompositeFrames string `json:"composite_frames"`
	OutputGIF       string `json:"output_gif"`
}
