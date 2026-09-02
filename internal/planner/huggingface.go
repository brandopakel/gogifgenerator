package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HuggingFace interprets a prompt with an open-weights model through an
// OpenAI-compatible chat completions endpoint.
//
// Two very different deployments share this one adapter, because they speak
// the same protocol:
//
//   - Hugging Face's Inference Providers router, which serves hosted open
//     models under a single token and bills at the provider's own rate.
//   - Any loopback server exposing the same route — llama.cpp's server,
//     Ollama, vLLM — which is how a locally quantized GGUF model plans
//     prompts with no vendor, no key, and no bill.
//
// Only the prompt text is sent. It produces the same validated brief and spec
// as every other planner, so switching between them changes cost and latency
// rather than the contract the renderer receives.
type HuggingFace struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

const (
	defaultHuggingFaceBaseURL = "https://router.huggingface.co/v1"
	defaultHuggingFaceModel   = "openai/gpt-oss-120b:cheapest"
	maxHuggingFaceResponse    = 1 << 20
)

// huggingFaceHosts is the allowlist for non-loopback planning endpoints. The
// prompt is user text, so its destination is pinned rather than accepted from
// configuration.
var huggingFaceHosts = map[string]bool{
	"router.huggingface.co":        true,
	"api-inference.huggingface.co": true,
}

func (p HuggingFace) Plan(ctx context.Context, request Request) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	if p.BaseURL == "" {
		p.BaseURL = defaultHuggingFaceBaseURL
	}
	endpoint, local, err := huggingFaceEndpoint(p.BaseURL)
	if err != nil {
		return Result{}, err
	}
	if !local && strings.TrimSpace(p.APIKey) == "" {
		return Result{}, errors.New("Hugging Face API token is not configured")
	}
	if p.Model == "" {
		p.Model = defaultHuggingFaceModel
	}
	if p.Client == nil {
		p.Client = &http.Client{Timeout: 30 * time.Second}
	}

	payload := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": planInstructions},
			{"role": "user", "content": "Interpret this idea for an animated GIF: " + request.Prompt},
		},
		"max_tokens":  700,
		"temperature": 0.4,
		"stream":      false,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "gif_plan",
				"strict": true,
				"schema": planSchema(),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(p.APIKey) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.APIKey))
	}

	response, err := p.Client.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("request Hugging Face plan: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxHuggingFaceResponse))
	if err != nil {
		return Result{}, fmt.Errorf("read Hugging Face response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("Hugging Face returned %s: %s", response.Status, compactError(responseBody))
	}

	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return Result{}, fmt.Errorf("decode Hugging Face response: %w", err)
	}
	if len(envelope.Choices) == 0 || strings.TrimSpace(envelope.Choices[0].Message.Content) == "" {
		return Result{}, errors.New("Hugging Face response did not contain a plan")
	}

	var artPlan modelPlan
	if err := json.Unmarshal([]byte(unfence(envelope.Choices[0].Message.Content)), &artPlan); err != nil {
		return Result{}, fmt.Errorf("decode Hugging Face art plan: %w", err)
	}
	brief, err := artPlan.brief(request.Prompt, "huggingface")
	if err != nil {
		return Result{}, fmt.Errorf("invalid Hugging Face art plan: %w", err)
	}
	spec, err := artPlan.spec(request, brief, request.Seed)
	if err != nil {
		return Result{}, fmt.Errorf("invalid Hugging Face art plan: %w", err)
	}
	return Result{Spec: spec, Brief: brief, Engine: "huggingface"}, nil
}

func huggingFaceEndpoint(base string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || parsed.Host == "" {
		return "", false, errors.New("Hugging Face base URL must be absolute")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false, errors.New("Hugging Face base URL cannot contain credentials, query, or fragment")
	}
	host := strings.ToLower(parsed.Hostname())
	local := strings.EqualFold(host, "localhost")
	if address := net.ParseIP(host); address != nil && address.IsLoopback() {
		local = true
	}
	switch {
	case local:
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", false, errors.New("Hugging Face loopback base URL must be HTTP(S)")
		}
	case parsed.Scheme != "https":
		return "", false, errors.New("Hugging Face base URL must use HTTPS")
	case !huggingFaceHosts[host]:
		return "", false, fmt.Errorf("%q is not an allowlisted planning host", host)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/chat/completions"
	return parsed.String(), local, nil
}

// unfence removes the Markdown code fence some open models wrap around JSON
// even when a strict schema was requested.
func unfence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	content = strings.TrimPrefix(content, "```")
	if index := strings.IndexByte(content, '\n'); index >= 0 {
		// Drop an optional language tag on the opening fence.
		if !strings.Contains(content[:index], "{") {
			content = content[index+1:]
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(content), "```"))
}

// IsLocalEndpoint reports whether a base URL points at a loopback server.
// Startup uses it to tell a free local model from a billable hosted one.
func IsLocalEndpoint(baseURL string) bool {
	if strings.TrimSpace(baseURL) == "" {
		return false
	}
	_, local, err := huggingFaceEndpoint(baseURL)
	return err == nil && local
}
