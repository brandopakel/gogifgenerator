// Package hfinference embeds text with a hosted or self-hosted feature
// extraction endpoint. It targets Hugging Face's Inference Providers router,
// which exposes CPU embedding models under a single token, and it accepts a
// loopback endpoint so the identical code path can drive a locally served
// quantized model with no vendor and no bill.
//
// Only the prompt text and candidate titles are sent. No user media, no
// account identifiers, and no library contents leave the process here.
package hfinference

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

	"github.com/brandopakel/gogifgenerator/internal/semantic"
)

var ErrUnavailable = fmt.Errorf("%w: hugging face feature extraction unavailable", semantic.ErrUnavailable)

const (
	defaultEndpoint     = "https://router.huggingface.co"
	defaultModel        = "BAAI/bge-small-en-v1.5"
	maxResponseBytes    = 8 << 20
	defaultRequestLimit = 20 * time.Second
)

// hostedHosts is the allowlist for non-loopback endpoints. Embedding requests
// carry user prompt text, so the destination is pinned rather than accepted
// from configuration.
var hostedHosts = map[string]bool{
	"router.huggingface.co":        true,
	"api-inference.huggingface.co": true,
}

type Options struct {
	Endpoint string
	APIKey   string
	Model    string
	// Path overrides the request path. It is normally derived from the
	// endpoint: the hosted router uses its feature-extraction route, and a
	// loopback server uses the text-embeddings-inference /embed route.
	Path       string
	Dimensions int
	Client     *http.Client
}

type Embedder struct {
	endpoint   *url.URL
	apiKey     string
	model      string
	path       string
	dimensions int
	local      bool
	client     *http.Client
}

func New(options Options) (*Embedder, error) {
	if options.Endpoint == "" {
		options.Endpoint = defaultEndpoint
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || endpoint.Host == "" {
		return nil, errors.New("hfinference: endpoint must be an absolute URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("hfinference: endpoint cannot contain credentials, query, or fragment")
	}
	local := isLoopback(endpoint.Hostname())
	switch {
	case local:
		if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			return nil, errors.New("hfinference: loopback endpoint must be HTTP(S)")
		}
	case endpoint.Scheme != "https":
		return nil, errors.New("hfinference: hosted endpoint must use HTTPS")
	case !hostedHosts[strings.ToLower(endpoint.Hostname())]:
		return nil, fmt.Errorf("hfinference: %q is not an allowlisted embedding host", endpoint.Hostname())
	}
	if options.Model == "" {
		options.Model = defaultModel
	}
	if !validModel(options.Model) {
		return nil, errors.New("hfinference: model must be an owner/name identifier")
	}
	if !local && strings.TrimSpace(options.APIKey) == "" {
		return nil, errors.New("hfinference: an API token is required for hosted embedding")
	}
	path := options.Path
	if path == "" {
		if local {
			path = "/embed"
		} else {
			path = "/hf-inference/models/" + options.Model + "/pipeline/feature-extraction"
		}
	}
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return nil, errors.New("hfinference: path must be an absolute route without traversal")
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: defaultRequestLimit}
	}
	return &Embedder{
		endpoint:   endpoint,
		apiKey:     strings.TrimSpace(options.APIKey),
		model:      options.Model,
		path:       path,
		dimensions: options.Dimensions,
		local:      local,
		client:     options.Client,
	}, nil
}

func (e *Embedder) Descriptor() semantic.Descriptor {
	label := "Hugging Face " + e.model
	if e.local {
		label = "Local embedding server " + e.model
	}
	return semantic.Descriptor{ID: "hf-inference", Label: label, Local: e.local, Dimensions: e.dimensions}
}

func (e *Embedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if err := semantic.ValidateInputs(inputs); err != nil {
		return nil, err
	}
	payload := map[string]any{"inputs": inputs}
	if !e.local {
		// The hosted router cold-starts CPU models; waiting is cheaper than a
		// user-visible failure on the first search of the day.
		payload["options"] = map[string]any{"wait_for_model": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	target := *e.endpoint
	target.Path = strings.TrimSuffix(target.Path, "/") + e.path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	response, err := e.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: %s: %s", ErrUnavailable, response.Status, compact(responseBody))
	}
	vectors, err := decodeVectors(responseBody, len(inputs))
	if err != nil {
		return nil, err
	}
	for index := range vectors {
		vectors[index] = semantic.Unit(vectors[index])
	}
	return vectors, nil
}

// decodeVectors accepts both shapes these endpoints return: one vector per
// input, or one vector per token which is mean-pooled into a sentence vector.
func decodeVectors(body []byte, expected int) ([][]float32, error) {
	var flat [][]float32
	if err := json.Unmarshal(body, &flat); err == nil {
		if len(flat) != expected {
			return nil, fmt.Errorf("%w: expected %d vectors, received %d", ErrUnavailable, expected, len(flat))
		}
		return flat, nil
	}
	var tokenized [][][]float32
	if err := json.Unmarshal(body, &tokenized); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}
	if len(tokenized) != expected {
		return nil, fmt.Errorf("%w: expected %d vectors, received %d", ErrUnavailable, expected, len(tokenized))
	}
	pooled := make([][]float32, len(tokenized))
	for index, tokens := range tokenized {
		if len(tokens) == 0 {
			return nil, fmt.Errorf("%w: empty token matrix", ErrUnavailable)
		}
		vector := make([]float32, len(tokens[0]))
		for _, token := range tokens {
			if len(token) != len(vector) {
				return nil, fmt.Errorf("%w: ragged token matrix", ErrUnavailable)
			}
			for position, value := range token {
				vector[position] += value
			}
		}
		for position := range vector {
			vector[position] /= float32(len(tokens))
		}
		pooled[index] = vector
	}
	return pooled, nil
}

func validModel(model string) bool {
	if model == "" || len(model) > 128 || strings.Contains(model, "..") {
		return false
	}
	owner, name, found := strings.Cut(model, "/")
	if !found || owner == "" || name == "" || strings.Contains(name, "/") {
		return false
	}
	for _, symbol := range model {
		switch {
		case symbol >= 'a' && symbol <= 'z', symbol >= 'A' && symbol <= 'Z',
			symbol >= '0' && symbol <= '9', symbol == '-', symbol == '_',
			symbol == '.', symbol == '/':
		default:
			return false
		}
	}
	return true
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func compact(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) > 240 {
		value = value[:240]
	}
	return value
}
