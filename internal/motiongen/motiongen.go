// Package motiongen defines hosted image-to-video generation independently
// from still-image generation and GIF encoding.
package motiongen

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
)

var ErrInvalidRequest = errors.New("motiongen: invalid request")

type Request struct {
	Prompt string
	Input  imagegen.Input
	Width  int
	Height int
	Seed   int64
}

func (r Request) Validate() error {
	if strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 2000 {
		return fmt.Errorf("%w: prompt is required", ErrInvalidRequest)
	}
	if len(r.Input.Data) == 0 || len(r.Input.Data) > imagegen.MaxInputBytes || !strings.HasPrefix(r.Input.ContentType, "image/") {
		return fmt.Errorf("%w: a bounded source image is required", ErrInvalidRequest)
	}
	if r.Width < 128 || r.Width > 1024 || r.Height < 128 || r.Height > 1024 {
		return fmt.Errorf("%w: dimensions must be between 128 and 1024", ErrInvalidRequest)
	}
	return nil
}

type Result struct {
	Data             []byte
	ContentType      string
	Filename         string
	Engine           string
	SourceDurationMS int
}

type Descriptor struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Hosted bool   `json:"hosted"`
}

type Generator interface {
	Descriptor() Descriptor
	Generate(context.Context, Request) (Result, error)
}
