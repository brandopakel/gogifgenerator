// Package planner turns a natural-language idea into a validated GIF spec.
package planner

import (
	"context"
	"fmt"
	"strings"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
)

type Request struct {
	Prompt         string `json:"prompt"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	Frames         int    `json:"frames,omitempty"`
	DelayMS        int    `json:"delay_ms,omitempty"`
	Seed           int64  `json:"seed,omitempty"`
	GenerationMode string `json:"generation_mode,omitempty"`
}

func (r Request) Validate() error {
	if len(r.Prompt) < 1 || len(r.Prompt) > 500 {
		return fmt.Errorf("prompt must contain between 1 and 500 characters")
	}
	switch strings.ToLower(strings.TrimSpace(r.GenerationMode)) {
	case "", "fast", "semantic", "studio":
	default:
		return fmt.Errorf("generation_mode must be fast, semantic, or studio")
	}
	return nil
}

type Result struct {
	Spec   gifdomain.Spec
	Engine string
}

type Planner interface {
	Plan(context.Context, Request) (Result, error)
}

// WithFallback keeps generation available if a remote planner is unavailable.
type WithFallback struct {
	Primary  Planner
	Fallback Planner
	OnError  func(error)
}

func (p WithFallback) Plan(ctx context.Context, request Request) (Result, error) {
	result, err := p.Primary.Plan(ctx, request)
	if err == nil {
		return result, nil
	}
	if p.OnError != nil {
		p.OnError(err)
	}
	result, fallbackErr := p.Fallback.Plan(ctx, request)
	if fallbackErr != nil {
		return Result{}, fmt.Errorf("primary planner: %v; fallback planner: %w", err, fallbackErr)
	}
	result.Engine += "-fallback"
	return result, nil
}
