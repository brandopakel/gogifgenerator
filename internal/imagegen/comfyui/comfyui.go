// Package comfyui connects GoGIF to a self-hosted ComfyUI process. It never
// sends prompts or images to Comfy Cloud and uses no API key.
package comfyui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	_ "golang.org/x/image/webp"
)

var (
	ErrUnavailable      = errors.New("comfyui: local server unavailable")
	ErrUnsupportedInput = errors.New("comfyui: unsupported reference input")
)

const maxAPIResponseBytes = 2 << 20

type Options struct {
	Endpoint       string
	Checkpoint     string
	NegativePrompt string
	InputDirectory string
	Client         *http.Client
	PollInterval   time.Duration
	MaxWait        time.Duration
	Steps          int
	CFG            float64
}

type Generator struct {
	endpoint       *url.URL
	checkpoint     string
	negativePrompt string
	inputDirectory string
	client         *http.Client
	pollInterval   time.Duration
	maxWait        time.Duration
	steps          int
	cfg            float64
}

func New(options Options) (*Generator, error) {
	if options.Endpoint == "" {
		options.Endpoint = "http://127.0.0.1:8188"
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || !isLoopback(endpoint.Hostname()) {
		return nil, errors.New("comfyui: endpoint must be an absolute loopback HTTP(S) URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("comfyui: endpoint cannot contain credentials, query, or fragment")
	}
	if options.Checkpoint == "" || filepath.Base(options.Checkpoint) != options.Checkpoint || strings.ContainsAny(options.Checkpoint, `/\\`) {
		return nil, errors.New("comfyui: checkpoint must be a filename from the local checkpoints directory")
	}
	inputDirectory := ""
	if options.InputDirectory != "" {
		inputDirectory, err = filepath.Abs(options.InputDirectory)
		if err != nil {
			return nil, fmt.Errorf("comfyui: resolve input directory: %w", err)
		}
		info, statErr := os.Stat(inputDirectory)
		if statErr != nil || !info.IsDir() {
			return nil, errors.New("comfyui: input directory must be an existing directory")
		}
	}
	if options.NegativePrompt == "" {
		options.NegativePrompt = "blurry, low quality, watermark, distorted text"
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 300 * time.Millisecond
	}
	if options.MaxWait <= 0 {
		options.MaxWait = 3 * time.Minute
	}
	if options.Steps == 0 {
		options.Steps = 20
	}
	if options.Steps < 1 || options.Steps > 100 {
		return nil, errors.New("comfyui: steps must be between 1 and 100")
	}
	if options.CFG == 0 {
		options.CFG = 7
	}
	if options.CFG < 0 || options.CFG > 30 {
		return nil, errors.New("comfyui: CFG must be between 0 and 30")
	}
	return &Generator{
		endpoint:       endpoint,
		checkpoint:     options.Checkpoint,
		negativePrompt: options.NegativePrompt,
		inputDirectory: inputDirectory,
		client:         options.Client,
		pollInterval:   options.PollInterval,
		maxWait:        options.MaxWait,
		steps:          options.Steps,
		cfg:            options.CFG,
	}, nil
}

func (g *Generator) Descriptor() imagegen.Descriptor {
	return imagegen.Descriptor{
		ID:                 "comfyui-local",
		Label:              "ComfyUI (local)",
		Local:              true,
		Semantic:           true,
		SupportsReferences: g.inputDirectory != "",
	}
}

func (g *Generator) Ping(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.route("/system_stats"), nil)
	if err != nil {
		return err
	}
	response, err := g.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: system stats returned HTTP %d", ErrUnavailable, response.StatusCode)
	}
	return nil
}

func (g *Generator) Generate(ctx context.Context, request imagegen.Request) (imagegen.Result, error) {
	if err := request.Validate(); err != nil {
		return imagegen.Result{}, err
	}
	if request.Width%8 != 0 || request.Height%8 != 0 {
		return imagegen.Result{}, fmt.Errorf("%w: dimensions must be multiples of 8", imagegen.ErrInvalidRequest)
	}
	if len(request.Inputs) > 1 {
		return imagegen.Result{}, fmt.Errorf("%w: this workflow accepts one reference image", ErrUnsupportedInput)
	}

	inputName := ""
	cleanup := func() error { return nil }
	if len(request.Inputs) == 1 {
		if g.inputDirectory == "" {
			return imagegen.Result{}, fmt.Errorf("%w: GOGIF_COMFYUI_INPUT_DIR is required so uploaded references can be deleted", ErrUnsupportedInput)
		}
		var err error
		inputName, cleanup, err = g.uploadInput(ctx, request.Inputs[0])
		if err != nil {
			return imagegen.Result{}, err
		}
		defer func() { _ = cleanup() }()
	}

	workflow := g.workflow(request, inputName)
	promptID, err := g.queue(ctx, workflow)
	if err != nil {
		return imagegen.Result{}, err
	}
	output, err := g.waitForOutput(ctx, promptID)
	if err != nil {
		return imagegen.Result{}, err
	}
	data, contentType, err := g.fetchOutput(ctx, output)
	if err != nil {
		return imagegen.Result{}, err
	}
	// The cinematic pipeline launches Blender, Unity, and Unreal after this
	// method returns. Release model weights first so small unified-memory Macs
	// do not keep diffusion and 3D engines resident at the same time.
	g.releaseMemory()
	configuration, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || configuration.Width != request.Width || configuration.Height != request.Height {
		return imagegen.Result{}, errors.New("comfyui: output is not a decodable image with the requested dimensions")
	}
	if err := cleanup(); err != nil {
		return imagegen.Result{}, err
	}
	cleanup = func() error { return nil }
	return imagegen.Result{Data: data, ContentType: contentType, Engine: g.Descriptor().ID}, nil
}

func (g *Generator) releaseMemory() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := bytes.NewBufferString(`{"unload_models":true,"free_memory":true}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.route("/free"), body)
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
}

func (g *Generator) workflow(request imagegen.Request, inputName string) map[string]any {
	seed := uint64(request.Seed) & uint64(^uint64(0)>>1)
	latentInput := any([]any{"5", 0})
	denoise := 1.0
	workflow := map[string]any{
		"3": map[string]any{
			"class_type": "KSampler",
			"inputs": map[string]any{
				"cfg": g.cfg, "denoise": denoise, "latent_image": latentInput,
				"model": []any{"4", 0}, "negative": []any{"7", 0}, "positive": []any{"6", 0},
				"sampler_name": "euler", "scheduler": "normal", "seed": seed, "steps": g.steps,
			},
		},
		"4": map[string]any{"class_type": "CheckpointLoaderSimple", "inputs": map[string]any{"ckpt_name": g.checkpoint}},
		"5": map[string]any{"class_type": "EmptyLatentImage", "inputs": map[string]any{"batch_size": 1, "height": request.Height, "width": request.Width}},
		"6": map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"clip": []any{"4", 1}, "text": imagegen.CinematicPrompt(request.Prompt, request.Width, request.Height)}},
		"7": map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"clip": []any{"4", 1}, "text": g.negativePrompt}},
		"8": map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"3", 0}, "vae": []any{"4", 2}}},
		"9": map[string]any{"class_type": "PreviewImage", "inputs": map[string]any{"images": []any{"8", 0}}},
	}
	if inputName != "" {
		workflow["10"] = map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": inputName}}
		workflow["11"] = map[string]any{"class_type": "ImageScale", "inputs": map[string]any{
			"crop": "center", "height": request.Height, "image": []any{"10", 0}, "upscale_method": "lanczos", "width": request.Width,
		}}
		workflow["12"] = map[string]any{"class_type": "VAEEncode", "inputs": map[string]any{"pixels": []any{"11", 0}, "vae": []any{"4", 2}}}
		inputs := workflow["3"].(map[string]any)["inputs"].(map[string]any)
		inputs["latent_image"] = []any{"12", 0}
		inputs["denoise"] = 0.65
	}
	return workflow
}

func (g *Generator) queue(ctx context.Context, workflow map[string]any) (string, error) {
	clientID, err := randomID()
	if err != nil {
		return "", fmt.Errorf("comfyui: create client ID: %w", err)
	}
	body, err := json.Marshal(map[string]any{"prompt": workflow, "client_id": clientID})
	if err != nil {
		return "", fmt.Errorf("comfyui: encode workflow: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.route("/prompt"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
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
		return "", fmt.Errorf("comfyui: queue workflow returned HTTP %d: %s", response.StatusCode, compactMessage(data))
	}
	var payload struct {
		PromptID  string         `json:"prompt_id"`
		Error     string         `json:"error"`
		NodeError map[string]any `json:"node_errors"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("comfyui: decode queue response: %w", err)
	}
	if payload.PromptID == "" {
		return "", fmt.Errorf("comfyui: workflow rejected: %s", firstNonEmpty(payload.Error, compactMessage(data)))
	}
	return payload.PromptID, nil
}

type outputImage struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

type historyEntry struct {
	Outputs map[string]struct {
		Images []outputImage `json:"images"`
	} `json:"outputs"`
	Status struct {
		StatusStr string `json:"status_str"`
		Completed bool   `json:"completed"`
	} `json:"status"`
}

func (g *Generator) waitForOutput(ctx context.Context, promptID string) (outputImage, error) {
	waitContext, cancel := context.WithTimeout(ctx, g.maxWait)
	defer cancel()
	for {
		request, err := http.NewRequestWithContext(waitContext, http.MethodGet, g.route("/history/")+url.PathEscape(promptID), nil)
		if err != nil {
			return outputImage{}, err
		}
		response, err := g.client.Do(request)
		if err != nil {
			if waitContext.Err() != nil {
				return outputImage{}, waitContext.Err()
			}
			return outputImage{}, fmt.Errorf("%w: read history: %v", ErrUnavailable, err)
		}
		data, readErr := readLimited(response.Body, maxAPIResponseBytes)
		_ = response.Body.Close()
		if readErr != nil {
			return outputImage{}, readErr
		}
		if response.StatusCode != http.StatusOK {
			return outputImage{}, fmt.Errorf("comfyui: history returned HTTP %d", response.StatusCode)
		}
		var history map[string]historyEntry
		if err := json.Unmarshal(data, &history); err != nil {
			return outputImage{}, fmt.Errorf("comfyui: decode history: %w", err)
		}
		if entry, ok := history[promptID]; ok {
			if output, ok := firstOutput(entry.Outputs); ok {
				return output, nil
			}
			if entry.Status.Completed || strings.EqualFold(entry.Status.StatusStr, "error") {
				return outputImage{}, errors.New("comfyui: workflow completed without an image")
			}
		}
		timer := time.NewTimer(g.pollInterval)
		select {
		case <-waitContext.Done():
			timer.Stop()
			return outputImage{}, waitContext.Err()
		case <-timer.C:
		}
	}
}

func (g *Generator) fetchOutput(ctx context.Context, output outputImage) ([]byte, string, error) {
	query := url.Values{"filename": {output.Filename}, "subfolder": {output.Subfolder}, "type": {output.Type}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.route("/view")+"?"+query.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	response, err := g.client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("%w: fetch generated image: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("comfyui: generated image returned HTTP %d", response.StatusCode)
	}
	data, err := readLimited(response.Body, imagegen.MaxInputBytes)
	if err != nil {
		return nil, "", err
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", errors.New("comfyui: output is not an image")
	}
	return data, contentType, nil
}

func (g *Generator) uploadInput(ctx context.Context, input imagegen.Input) (string, func() error, error) {
	id, err := randomID()
	if err != nil {
		return "", nil, err
	}
	extension := extensionFor(input.ContentType)
	if extension == "" {
		return "", nil, ErrUnsupportedInput
	}
	filename := "gogif-" + id + extension
	expectedTarget := filepath.Join(g.inputDirectory, "gogif", filename)
	cleanup := func() error {
		if err := os.Remove(expectedTarget); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("comfyui: remove temporary reference: %w", err)
		}
		return nil
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return "", nil, err
	}
	if _, err := part.Write(input.Data); err != nil {
		return "", nil, err
	}
	_ = writer.WriteField("type", "input")
	_ = writer.WriteField("subfolder", "gogif")
	_ = writer.WriteField("overwrite", "true")
	if err := writer.Close(); err != nil {
		return "", nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.route("/upload/image"), &body)
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := g.client.Do(request)
	if err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("%w: upload reference: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		_ = cleanup()
		return "", nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = cleanup()
		return "", nil, fmt.Errorf("comfyui: upload reference returned HTTP %d: %s", response.StatusCode, compactMessage(data))
	}
	var uploaded struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(data, &uploaded); err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("comfyui: decode upload response: %w", err)
	}
	if uploaded.Name != filename || uploaded.Subfolder != "gogif" || uploaded.Type != "input" {
		_ = cleanup()
		return "", nil, errors.New("comfyui: upload returned an unsafe file location")
	}
	target := filepath.Join(g.inputDirectory, uploaded.Subfolder, uploaded.Name)
	if !withinDirectory(g.inputDirectory, target) || target != expectedTarget {
		_ = cleanup()
		return "", nil, errors.New("comfyui: upload escaped its input directory")
	}
	return uploaded.Subfolder + "/" + uploaded.Name, cleanup, nil
}

func (g *Generator) route(path string) string {
	endpoint := *g.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	return endpoint.String()
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

func firstOutput(outputs map[string]struct {
	Images []outputImage `json:"images"`
}) (outputImage, bool) {
	if output, ok := outputs["9"]; ok && len(output.Images) > 0 {
		return output.Images[0], true
	}
	for _, output := range outputs {
		if len(output.Images) > 0 {
			return output.Images[0], true
		}
	}
	return outputImage{}, false
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("comfyui: response is too large")
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

func extensionFor(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func withinDirectory(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
