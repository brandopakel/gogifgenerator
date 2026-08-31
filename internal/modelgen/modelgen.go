// Package modelgen defines the provider-neutral boundary for generated 3D
// assets. Model generators return a portable GLB; preview, storage, and sharing
// remain GoGIF responsibilities.
package modelgen

import (
	"context"
	"errors"
	"strings"
)

const MaxOutputBytes = 256 << 20

var (
	ErrInvalidRequest = errors.New("modelgen: invalid request")
	ErrUnavailable    = errors.New("modelgen: generator unavailable")
)

type Recipe struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Hosted  bool   `json:"hosted"`
	PBR     bool   `json:"pbr"`
	Rigging bool   `json:"rigging"`
}

type Descriptor struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Local   bool     `json:"local"`
	Recipes []Recipe `json:"recipes"`
}

type Request struct {
	Prompt string `json:"prompt"`
	Recipe string `json:"recipe,omitempty"`
	Seed   int64  `json:"seed,omitempty"`
}

func (r Request) Validate() error {
	prompt := strings.TrimSpace(r.Prompt)
	if prompt == "" {
		return errors.Join(ErrInvalidRequest, errors.New("prompt is required"))
	}
	if len(prompt) > 1024 {
		return errors.Join(ErrInvalidRequest, errors.New("prompt must be at most 1024 characters"))
	}
	if len(r.Recipe) > 64 {
		return errors.Join(ErrInvalidRequest, errors.New("recipe must be at most 64 characters"))
	}
	return nil
}

type Result struct {
	Data        []byte
	ContentType string
	Extension   string
	Engine      string
}

type Generator interface {
	Descriptor() Descriptor
	Generate(context.Context, Request) (Result, error)
}
