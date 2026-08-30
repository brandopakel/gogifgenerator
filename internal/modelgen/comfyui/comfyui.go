// Package comfyui runs a small allowlisted set of ComfyUI 3D workflows. The
// browser never supplies workflow JSON or node names; it can only choose a
// server-owned recipe and prompt.
package comfyui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/modelgen"
)

var ErrUnavailable = errors.New("comfyui model generation unavailable")

const maxAPIResponseBytes = 4 << 20

type Options struct {
	Endpoint      string
	APIKey        string
	DefaultRecipe string
	Client        *http.Client
	PollInterval  time.Duration
	MaxWait       time.Duration
}

type Generator struct {
	endpoint      *url.URL
	apiKey        string
	defaultRecipe string
	client        *http.Client
	pollInterval  time.Duration
	maxWait       time.Duration
	cloud         bool
}

func New(options Options) (*Generator, error) {
	if options.Endpoint == "" {
		options.Endpoint = "http://127.0.0.1:8188"
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, errors.New("comfyui models: endpoint must be an absolute HTTP(S) URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("comfyui models: endpoint cannot contain credentials, query, or fragment")
	}
	local := isLoopback(endpoint.Hostname())
	if !local && endpoint.Scheme != "https" {
		return nil, errors.New("comfyui models: remote endpoints require HTTPS")
	}
	if !local && strings.TrimSpace(options.APIKey) == "" {
		return nil, errors.New("comfyui models: remote endpoints require an API key")
	}
	if options.DefaultRecipe == "" {
		options.DefaultRecipe = "tripo-3.1"
	}
	if _, ok := recipes()[options.DefaultRecipe]; !ok {
		return nil, fmt.Errorf("comfyui models: unsupported recipe %q", options.DefaultRecipe)
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 45 * time.Second}
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 750 * time.Millisecond
	}
	if options.MaxWait <= 0 {
		options.MaxWait = 12 * time.Minute
	}
	return &Generator{
		endpoint: endpoint, apiKey: strings.TrimSpace(options.APIKey), defaultRecipe: options.DefaultRecipe,
		client: options.Client, pollInterval: options.PollInterval, maxWait: options.MaxWait, cloud: !local,
	}, nil
}

func (g *Generator) Descriptor() modelgen.Descriptor {
	return modelgen.Descriptor{
		ID: "comfyui-3d", Label: "ComfyUI 3D", Local: !g.cloud,
		Recipes: []modelgen.Recipe{
			{ID: "tripo-3.1", Label: "Tripo 3.1 · detailed PBR", Hosted: true, PBR: true, Rigging: true},
			{ID: "hunyuan-3.1", Label: "Hunyuan 3D 3.1 · PBR", Hosted: true, PBR: true},
		},
	}
}

func (g *Generator) Generate(ctx context.Context, request modelgen.Request) (modelgen.Result, error) {
	if err := request.Validate(); err != nil {
		return modelgen.Result{}, err
	}
	recipeID := strings.TrimSpace(request.Recipe)
	if recipeID == "" {
		recipeID = g.defaultRecipe
	}
	recipe, ok := recipes()[recipeID]
	if !ok {
		return modelgen.Result{}, fmt.Errorf("%w: unknown recipe %q", modelgen.ErrInvalidRequest, recipeID)
	}
	seed := request.Seed
	if seed == 0 {
		seed = time.Now().UnixNano() & 0x7fffffff
	}
	promptID, err := g.queue(ctx, recipe.workflow(strings.TrimSpace(request.Prompt), seed))
	if err != nil {
		return modelgen.Result{}, err
	}
	output, err := g.waitForOutput(ctx, promptID)
	if err != nil {
		return modelgen.Result{}, err
	}
	data, err := g.fetchOutput(ctx, output)
	if err != nil {
		return modelgen.Result{}, err
	}
	if len(data) < 12 || string(data[:4]) != "glTF" {
		return modelgen.Result{}, errors.New("comfyui models: output is not a binary glTF file")
	}
	return modelgen.Result{Data: data, ContentType: "model/gltf-binary", Extension: "glb", Engine: "comfyui/" + recipeID}, nil
}

type recipe struct {
	workflow func(string, int64) map[string]any
}

func recipes() map[string]recipe {
	return map[string]recipe{
		"tripo-3.1":   {workflow: tripoWorkflow},
		"hunyuan-3.1": {workflow: hunyuanWorkflow},
	}
}

func tripoWorkflow(prompt string, seed int64) map[string]any {
	return map[string]any{
		"1": map[string]any{
			"class_type": "TripoTextToModelNode",
			"inputs": map[string]any{
				"prompt": prompt, "negative_prompt": "low quality, distorted geometry, broken topology, extra limbs",
				"model_version": "v3.1-20260211", "style": "None", "texture": true, "pbr": true,
				"image_seed": seed, "model_seed": seed, "texture_seed": seed,
				"texture_quality": "detailed", "geometry_quality": "detailed", "face_limit": -1, "quad": false,
			},
		},
		"2": map[string]any{
			"class_type": "SaveGLB",
			"inputs":     map[string]any{"mesh": []any{"1", 2}, "filename_prefix": "gogif/models/tripo31"},
		},
	}
}

func hunyuanWorkflow(prompt string, seed int64) map[string]any {
	return map[string]any{
		"1": map[string]any{
			"class_type": "TencentTextToModelNode",
			"inputs": map[string]any{
				"model": "3.1", "prompt": prompt, "face_count": 500000,
				"generate_type": map[string]any{"generate_type": "Normal", "pbr": true}, "seed": seed,
			},
		},
		"2": map[string]any{
			"class_type": "SaveGLB",
			"inputs":     map[string]any{"mesh": []any{"1", 1}, "filename_prefix": "gogif/models/hunyuan31"},
		},
	}
}

func (g *Generator) queue(ctx context.Context, workflow map[string]any) (string, error) {
	clientID, err := randomID()
	if err != nil {
		return "", err
	}
	payload := map[string]any{"prompt": workflow, "client_id": clientID}
	if g.apiKey != "" {
		payload["extra_data"] = map[string]any{"api_key_comfy_org": g.apiKey}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.route("/prompt"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	g.authorize(request)
	response, err := g.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: queue workflow: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("comfyui models: queue returned HTTP %d: %s", response.StatusCode, compactMessage(data))
	}
	var queued struct {
		PromptID string         `json:"prompt_id"`
		Error    string         `json:"error"`
		Nodes    map[string]any `json:"node_errors"`
	}
	if err := json.Unmarshal(data, &queued); err != nil {
		return "", fmt.Errorf("comfyui models: decode queue response: %w", err)
	}
	if queued.PromptID == "" {
		return "", fmt.Errorf("comfyui models: workflow rejected: %s", firstNonEmpty(queued.Error, compactMessage(data)))
	}
	return queued.PromptID, nil
}

type outputFile struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

type historyEntry struct {
	Outputs map[string]struct {
		ThreeD []outputFile `json:"3d"`
	} `json:"outputs"`
	Status struct {
		StatusStr string `json:"status_str"`
		Completed bool   `json:"completed"`
	} `json:"status"`
}

func (g *Generator) waitForOutput(ctx context.Context, promptID string) (outputFile, error) {
	waitContext, cancel := context.WithTimeout(ctx, g.maxWait)
	defer cancel()
	for {
		historyPath := "/history/" + url.PathEscape(promptID)
		if g.cloud {
			historyPath = "/history_v2/" + url.PathEscape(promptID)
		}
		request, err := http.NewRequestWithContext(waitContext, http.MethodGet, g.route(historyPath), nil)
		if err != nil {
			return outputFile{}, err
		}
		g.authorize(request)
		response, err := g.client.Do(request)
		if err != nil {
			if waitContext.Err() != nil {
				return outputFile{}, waitContext.Err()
			}
			return outputFile{}, fmt.Errorf("%w: read history: %v", ErrUnavailable, err)
		}
		data, readErr := readLimited(response.Body, maxAPIResponseBytes)
		_ = response.Body.Close()
		if readErr != nil {
			return outputFile{}, readErr
		}
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
			return outputFile{}, fmt.Errorf("comfyui models: history returned HTTP %d: %s", response.StatusCode, compactMessage(data))
		}
		if response.StatusCode == http.StatusOK {
			var history map[string]historyEntry
			if err := json.Unmarshal(data, &history); err != nil {
				return outputFile{}, fmt.Errorf("comfyui models: decode history: %w", err)
			}
			if entry, ok := history[promptID]; ok {
				if output, ok := firstOutput(entry.Outputs); ok {
					return output, nil
				}
				if entry.Status.Completed || strings.EqualFold(entry.Status.StatusStr, "error") {
					return outputFile{}, errors.New("comfyui models: workflow completed without a GLB output")
				}
			}
		}
		timer := time.NewTimer(g.pollInterval)
		select {
		case <-waitContext.Done():
			timer.Stop()
			return outputFile{}, waitContext.Err()
		case <-timer.C:
		}
	}
}

func (g *Generator) fetchOutput(ctx context.Context, output outputFile) ([]byte, error) {
	if output.Filename == "" || output.Type != "output" || strings.Contains(output.Filename, "..") || strings.Contains(output.Subfolder, "..") {
		return nil, errors.New("comfyui models: unsafe output location")
	}
	query := url.Values{"filename": {output.Filename}, "subfolder": {output.Subfolder}, "type": {output.Type}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.route("/view")+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	g.authorize(request)
	response, err := g.doOutputRequest(request)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch GLB: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if g.cloud && response.StatusCode >= 300 && response.StatusCode < 400 {
		location := response.Header.Get("Location")
		signedURL, parseErr := url.Parse(location)
		if parseErr != nil || signedURL.Scheme != "https" || signedURL.Host == "" || signedURL.User != nil {
			return nil, errors.New("comfyui models: cloud returned an unsafe output redirect")
		}
		_ = response.Body.Close()
		signedRequest, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, signedURL.String(), nil)
		if requestErr != nil {
			return nil, requestErr
		}
		// Never forward the Comfy API key to its temporary object-storage URL.
		response, err = g.client.Do(signedRequest)
		if err != nil {
			return nil, fmt.Errorf("comfyui models: follow signed output URL: %w", err)
		}
		defer response.Body.Close()
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comfyui models: GLB returned HTTP %d", response.StatusCode)
	}
	return readLimited(response.Body, modelgen.MaxOutputBytes)
}

func (g *Generator) doOutputRequest(request *http.Request) (*http.Response, error) {
	if !g.cloud {
		return g.client.Do(request)
	}
	client := *g.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(request)
}

func (g *Generator) authorize(request *http.Request) {
	if g.cloud && g.apiKey != "" {
		request.Header.Set("X-API-Key", g.apiKey)
	}
}

func (g *Generator) route(path string) string {
	endpoint := *g.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	return endpoint.String()
}

func firstOutput(outputs map[string]struct {
	ThreeD []outputFile `json:"3d"`
}) (outputFile, bool) {
	if output, ok := outputs["2"]; ok && len(output.ThreeD) > 0 {
		return output.ThreeD[0], true
	}
	for _, output := range outputs {
		if len(output.ThreeD) > 0 {
			return output.ThreeD[0], true
		}
	}
	return outputFile{}, false
}

func isLoopback(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func randomID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("comfyui models: response is too large")
	}
	return data, nil
}

func compactMessage(data []byte) string {
	message := strings.Join(strings.Fields(string(data)), " ")
	if len(message) > 240 {
		message = message[:240]
	}
	return strconv.Quote(message)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "unknown error"
}
