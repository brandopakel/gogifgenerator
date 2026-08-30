package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
)

type OpenAI struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

func (p OpenAI) Plan(ctx context.Context, request Request) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return Result{}, errors.New("OpenAI API key is not configured")
	}
	if p.Model == "" {
		p.Model = "gpt-5-mini"
	}
	if p.BaseURL == "" {
		p.BaseURL = "https://api.openai.com/v1"
	}
	if p.Client == nil {
		p.Client = &http.Client{Timeout: 20 * time.Second}
	}

	input := fmt.Sprintf("Create a bold, legible animation plan for this idea: %s", request.Prompt)
	payload := map[string]any{
		"model":             p.Model,
		"instructions":      "You are the art director for a tiny GIF renderer. Pick a short caption, harmonious high-contrast colors, and the motion that best communicates the user's emotion. Return only the structured plan.",
		"input":             input,
		"store":             false,
		"max_output_tokens": 350,
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "gif_plan",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"caption": map[string]any{"type": "string", "maxLength": 32},
						"palette": map[string]any{
							"type":     "array",
							"minItems": 5,
							"maxItems": 5,
							"items":    map[string]any{"type": "string", "pattern": "^#[0-9A-Fa-f]{6}$"},
						},
						"motion": map[string]any{"type": "string", "enum": []string{"orbit", "pulse", "waves", "confetti"}},
					},
					"required": []string{"caption", "palette", "motion"},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := p.Client.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("request OpenAI plan: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read OpenAI response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("OpenAI returned %s: %s", response.Status, compactError(responseBody))
	}

	var envelope struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return Result{}, fmt.Errorf("decode OpenAI response: %w", err)
	}
	var outputText string
	for _, item := range envelope.Output {
		for _, content := range item.Content {
			if content.Type == "output_text" {
				outputText = content.Text
				break
			}
		}
	}
	if outputText == "" {
		return Result{}, errors.New("OpenAI response did not contain output text")
	}

	var artPlan struct {
		Caption string   `json:"caption"`
		Palette []string `json:"palette"`
		Motion  string   `json:"motion"`
	}
	if err := json.Unmarshal([]byte(outputText), &artPlan); err != nil {
		return Result{}, fmt.Errorf("decode OpenAI art plan: %w", err)
	}
	spec := gifdomain.Defaults()
	spec.Caption = artPlan.Caption
	spec.Palette = artPlan.Palette
	spec.Motion = artPlan.Motion
	spec.Seed = request.Seed
	if request.Width != 0 {
		spec.Width = request.Width
	}
	if request.Height != 0 {
		spec.Height = request.Height
	}
	if request.Frames != 0 {
		spec.Frames = request.Frames
	}
	if request.DelayMS != 0 {
		spec.DelayMS = request.DelayMS
	}
	normalized, err := spec.Normalize()
	if err != nil {
		return Result{}, fmt.Errorf("invalid OpenAI art plan: %w", err)
	}
	return Result{Spec: normalized, Engine: "openai"}, nil
}

func compactError(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) > 300 {
		value = value[:300]
	}
	return value
}
