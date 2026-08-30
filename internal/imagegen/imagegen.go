// Package imagegen defines a vendor-neutral boundary for still-image engines.
// Animation and GIF encoding remain separate concerns in internal/render.
package imagegen

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidRequest = errors.New("imagegen: invalid request")

const (
	MaxInputs     = 4
	MaxInputBytes = 20 << 20
	MaxDimension  = 2048
)

// Input contains already-validated bytes. Provider URLs must be fetched by a
// controlled importer first so generator adapters cannot become SSRF clients.
type Input struct {
	Data        []byte `json:"-"`
	ContentType string `json:"content_type"`
	SourceID    string `json:"source_id,omitempty"`
}

type Request struct {
	Prompt string  `json:"prompt"`
	Inputs []Input `json:"inputs,omitempty"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Seed   int64   `json:"seed,omitempty"`
}

func (r Request) Validate() error {
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Prompt == "" || len(r.Prompt) > 2000 {
		return fmt.Errorf("%w: prompt must contain between 1 and 2000 characters", ErrInvalidRequest)
	}
	if r.Width < 64 || r.Width > MaxDimension || r.Height < 64 || r.Height > MaxDimension {
		return fmt.Errorf("%w: dimensions must be between 64 and %d pixels", ErrInvalidRequest, MaxDimension)
	}
	if len(r.Inputs) > MaxInputs {
		return fmt.Errorf("%w: at most %d reference inputs are allowed", ErrInvalidRequest, MaxInputs)
	}
	for index, input := range r.Inputs {
		if len(input.Data) == 0 || len(input.Data) > MaxInputBytes || !strings.HasPrefix(input.ContentType, "image/") {
			return fmt.Errorf("%w: input %d must be an image no larger than %d bytes", ErrInvalidRequest, index, MaxInputBytes)
		}
	}
	return nil
}

type Result struct {
	Data          []byte `json:"-"`
	ContentType   string `json:"content_type"`
	Engine        string `json:"engine"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type Descriptor struct {
	ID                 string `json:"id"`
	Label              string `json:"label"`
	Local              bool   `json:"local"`
	SupportsReferences bool   `json:"supports_references"`
}

type Generator interface {
	Descriptor() Descriptor
	Generate(context.Context, Request) (Result, error)
}
