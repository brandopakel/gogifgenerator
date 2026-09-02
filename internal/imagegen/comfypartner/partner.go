// Package comfypartner generates semantic source imagery through a small,
// server-owned set of ComfyUI Partner Node workflows. The workflow can run
// through Comfy Cloud or a local ComfyUI orchestrator, but model inference is
// hosted and never consumes the GoGIF machine's GPU memory.
package comfypartner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var (
	ErrUnavailable      = fmt.Errorf("%w: comfy hosted GPU unavailable", imagegen.ErrUnavailable)
	ErrUnsupportedInput = errors.New("comfy partner: reference inputs are not enabled for this recipe")
	ErrQualityRejected  = errors.New("comfy partner: generated image did not pass visual quality review")
)

const (
	maxAPIResponseBytes   = 8 << 20
	maxGeneratedImageSize = 40 << 20
)

type Options struct {
	Endpoint     string
	APIKey       string
	Recipe       string
	Client       *http.Client
	PollInterval time.Duration
	MaxWait      time.Duration
	// ValidationAttempts enables hosted vision QA and bounds total image
	// generations. Zero keeps validation disabled for private/local recipes.
	ValidationAttempts int
}

type Generator struct {
	endpoint           *url.URL
	apiKey             string
	recipe             string
	client             *http.Client
	pollInterval       time.Duration
	maxWait            time.Duration
	cloud              bool
	validationAttempts int
}

func New(options Options) (*Generator, error) {
	if options.Endpoint == "" {
		options.Endpoint = "https://cloud.comfy.org/api"
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, errors.New("comfy partner: endpoint must be an absolute HTTP(S) URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("comfy partner: endpoint cannot contain credentials, query, or fragment")
	}
	local := isLoopback(endpoint.Hostname())
	if !local && endpoint.Scheme != "https" {
		return nil, errors.New("comfy partner: remote endpoints require HTTPS")
	}
	if strings.TrimSpace(options.APIKey) == "" {
		return nil, errors.New("comfy partner: a Comfy API key is required")
	}
	if options.Recipe == "" {
		options.Recipe = "flux-ultra"
	}
	if options.Recipe != "flux-ultra" {
		return nil, fmt.Errorf("comfy partner: unsupported recipe %q", options.Recipe)
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 45 * time.Second}
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 750 * time.Millisecond
	}
	if options.MaxWait <= 0 {
		options.MaxWait = 5 * time.Minute
	}
	if options.ValidationAttempts < 0 || options.ValidationAttempts > 3 {
		return nil, errors.New("comfy partner: validation attempts must be between 0 and 3")
	}
	return &Generator{
		endpoint: endpoint, apiKey: strings.TrimSpace(options.APIKey), recipe: options.Recipe,
		client: options.Client, pollInterval: options.PollInterval, maxWait: options.MaxWait, cloud: !local,
		validationAttempts: options.ValidationAttempts,
	}, nil
}

func (g *Generator) Descriptor() imagegen.Descriptor {
	return imagegen.Descriptor{
		ID: "comfyui-partner-flux-ultra", Label: "ComfyUI · FLUX 1.1 Pro Ultra",
		Semantic: true, SupportsReferences: false,
	}
}

func (g *Generator) Generate(ctx context.Context, request imagegen.Request) (imagegen.Result, error) {
	if err := request.Validate(); err != nil {
		return imagegen.Result{}, err
	}
	if len(request.Inputs) != 0 {
		return imagegen.Result{}, ErrUnsupportedInput
	}
	attempts := max(1, g.validationAttempts)
	workingRequest := request
	var lastReview qualityReview
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			workingRequest.Seed += int64(104729 * attempt)
			workingRequest.Prompt = retryPrompt(request.Prompt, lastReview)
		}
		normalized, err := g.generateImage(ctx, workingRequest)
		if err != nil {
			return imagegen.Result{}, err
		}
		if g.validationAttempts == 0 {
			return imagegen.Result{Data: normalized, ContentType: "image/png", Engine: g.Descriptor().ID, RevisedPrompt: imagegen.ExpandConcept(request.Prompt)}, nil
		}
		lastReview, err = g.reviewImage(ctx, request.Prompt, normalized, workingRequest.Seed)
		if err != nil {
			return imagegen.Result{}, err
		}
		if lastReview.Accepted() {
			return imagegen.Result{Data: normalized, ContentType: "image/png", Engine: g.Descriptor().ID, RevisedPrompt: imagegen.ExpandConcept(request.Prompt)}, nil
		}
	}
	return imagegen.Result{}, fmt.Errorf("%w: %s", ErrQualityRejected, firstNonEmpty(lastReview.Reason, "the image did not match the prompt"))
}

func (g *Generator) generateImage(ctx context.Context, request imagegen.Request) ([]byte, error) {
	promptID, err := g.queue(ctx, fluxUltraWorkflow(request))
	if err != nil {
		return nil, err
	}
	output, err := g.waitForOutput(ctx, promptID)
	if err != nil {
		return nil, err
	}
	data, err := g.fetchOutput(ctx, output)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizePNG(data, request.Width, request.Height)
	if err != nil {
		return nil, fmt.Errorf("comfy partner: normalize generated image: %w", err)
	}
	return normalized, nil
}

func fluxUltraWorkflow(request imagegen.Request) map[string]any {
	seed := uint64(request.Seed) & uint64(^uint64(0)>>1)
	return map[string]any{
		"1": map[string]any{
			"class_type": "FluxProUltraImageNode",
			"inputs": map[string]any{
				"prompt":            imagegen.CinematicPrompt(request),
				"prompt_upsampling": imagegen.ShouldUpsamplePrompt(request.Prompt), "seed": seed, "aspect_ratio": aspectRatio(request.Width, request.Height), "raw": true,
			},
		},
		"2": map[string]any{
			"class_type": "SaveImage",
			"inputs":     map[string]any{"images": []any{"1", 0}, "filename_prefix": "gogif/semantic/flux-ultra"},
		},
	}
}

func (g *Generator) queue(ctx context.Context, workflow map[string]any) (string, error) {
	clientID, err := randomID()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]any{
		"prompt": workflow, "client_id": clientID,
		"extra_data": map[string]any{"api_key_comfy_org": g.apiKey},
	})
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
		return "", fmt.Errorf("%w: queue returned HTTP %d: %s", ErrUnavailable, response.StatusCode, compactMessage(data))
	}
	var queued struct {
		PromptID string `json:"prompt_id"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(data, &queued); err != nil {
		return "", fmt.Errorf("comfy partner: decode queue response: %w", err)
	}
	if queued.PromptID == "" {
		return "", fmt.Errorf("comfy partner: workflow rejected: %s", firstNonEmpty(queued.Error, compactMessage(data)))
	}
	return queued.PromptID, nil
}

type outputImage struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

type outputGroup struct {
	Images []outputImage `json:"images"`
	Videos []outputImage `json:"videos"`
	Text   []string      `json:"text"`
}

type localHistoryEntry struct {
	Outputs map[string]outputGroup `json:"outputs"`
	Status  struct {
		StatusStr string            `json:"status_str"`
		Completed bool              `json:"completed"`
		Messages  []json.RawMessage `json:"messages"`
	} `json:"status"`
}

type cloudJob struct {
	Status         string                 `json:"status"`
	Outputs        map[string]outputGroup `json:"outputs"`
	ExecutionError struct {
		Message string `json:"exception_message"`
	} `json:"execution_error"`
}

func (g *Generator) waitForOutput(ctx context.Context, promptID string) (outputImage, error) {
	waitContext, cancel := context.WithTimeout(ctx, g.maxWait)
	defer cancel()
	for {
		output, done, err := g.readOutput(waitContext, promptID)
		if err != nil {
			return outputImage{}, err
		}
		if done {
			return output, nil
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

func (g *Generator) readOutput(ctx context.Context, promptID string) (outputImage, bool, error) {
	path := "/history/" + url.PathEscape(promptID)
	if g.cloud {
		path = "/jobs/" + url.PathEscape(promptID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.route(path), nil)
	if err != nil {
		return outputImage{}, false, err
	}
	g.authorize(request)
	response, err := g.client.Do(request)
	if err != nil {
		return outputImage{}, false, fmt.Errorf("%w: read job status: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return outputImage{}, false, err
	}
	if response.StatusCode == http.StatusNotFound {
		return outputImage{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return outputImage{}, false, fmt.Errorf("%w: job status returned HTTP %d: %s", ErrUnavailable, response.StatusCode, compactMessage(data))
	}
	if g.cloud {
		var job cloudJob
		if err := json.Unmarshal(data, &job); err != nil {
			return outputImage{}, false, fmt.Errorf("comfy partner: decode cloud job: %w", err)
		}
		if output, ok := firstOutput(job.Outputs); ok {
			return output, true, nil
		}
		switch strings.ToLower(job.Status) {
		case "failed", "error", "cancelled":
			return outputImage{}, false, fmt.Errorf("comfy partner: hosted workflow %s: %s", job.Status, firstNonEmpty(job.ExecutionError.Message, "no image output"))
		case "completed":
			return outputImage{}, false, errors.New("comfy partner: hosted workflow completed without an image")
		}
		return outputImage{}, false, nil
	}
	var history map[string]localHistoryEntry
	if err := json.Unmarshal(data, &history); err != nil {
		return outputImage{}, false, fmt.Errorf("comfy partner: decode local history: %w", err)
	}
	entry, ok := history[promptID]
	if !ok {
		return outputImage{}, false, nil
	}
	if output, ok := firstOutput(entry.Outputs); ok {
		return output, true, nil
	}
	if entry.Status.Completed || strings.EqualFold(entry.Status.StatusStr, "error") {
		message := localExecutionError(entry.Status.Messages)
		if strings.Contains(strings.ToLower(message), "credits") || strings.Contains(strings.ToLower(message), "payment required") {
			return outputImage{}, false, fmt.Errorf("%w: %s", ErrUnavailable, message)
		}
		return outputImage{}, false, fmt.Errorf("comfy partner: workflow completed without an image: %s", message)
	}
	return outputImage{}, false, nil
}

func localExecutionError(messages []json.RawMessage) string {
	for _, raw := range messages {
		var event []json.RawMessage
		if json.Unmarshal(raw, &event) != nil || len(event) != 2 {
			continue
		}
		var kind string
		if json.Unmarshal(event[0], &kind) != nil || kind != "execution_error" {
			continue
		}
		var detail struct {
			Message string `json:"exception_message"`
		}
		if json.Unmarshal(event[1], &detail) == nil && strings.TrimSpace(detail.Message) != "" {
			message := strings.Join(strings.Fields(detail.Message), " ")
			if len(message) > 300 {
				message = message[:300]
			}
			return message
		}
	}
	return "no image output"
}

func (g *Generator) fetchOutput(ctx context.Context, output outputImage) ([]byte, error) {
	if output.Filename == "" || output.Type != "output" || strings.Contains(output.Filename, "..") || strings.Contains(output.Subfolder, "..") {
		return nil, errors.New("comfy partner: unsafe output location")
	}
	query := url.Values{"filename": {output.Filename}, "subfolder": {output.Subfolder}, "type": {output.Type}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.route("/view")+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	g.authorize(request)
	response, err := g.doOutputRequest(request)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch image: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if g.cloud && response.StatusCode >= 300 && response.StatusCode < 400 {
		signedURL, parseErr := url.Parse(response.Header.Get("Location"))
		if parseErr != nil || signedURL.Scheme != "https" || signedURL.Host == "" || signedURL.User != nil {
			return nil, errors.New("comfy partner: cloud returned an unsafe output redirect")
		}
		_ = response.Body.Close()
		signedRequest, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, signedURL.String(), nil)
		if requestErr != nil {
			return nil, requestErr
		}
		response, err = g.client.Do(signedRequest)
		if err != nil {
			return nil, fmt.Errorf("%w: follow signed output URL: %v", ErrUnavailable, err)
		}
		defer response.Body.Close()
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: image output returned HTTP %d", ErrUnavailable, response.StatusCode)
	}
	return readLimited(response.Body, maxGeneratedImageSize)
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
	if g.cloud {
		request.Header.Set("X-API-Key", g.apiKey)
	}
}

func (g *Generator) route(path string) string {
	endpoint := *g.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	return endpoint.String()
}

func firstOutput(outputs map[string]outputGroup) (outputImage, bool) {
	if output, ok := outputs["2"]; ok && len(output.Images) > 0 {
		return output.Images[0], true
	}
	for _, output := range outputs {
		if len(output.Images) > 0 {
			return output.Images[0], true
		}
	}
	return outputImage{}, false
}

func normalizePNG(data []byte, width, height int) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("generated image is not decodable")
	}
	sourceBounds := source.Bounds()
	if sourceBounds.Dx() < 1 || sourceBounds.Dy() < 1 || int64(sourceBounds.Dx())*int64(sourceBounds.Dy()) > 32_000_000 {
		return nil, errors.New("generated image dimensions are unsafe")
	}
	targetRatio := float64(width) / float64(height)
	sourceRatio := float64(sourceBounds.Dx()) / float64(sourceBounds.Dy())
	crop := sourceBounds
	if sourceRatio > targetRatio {
		cropWidth := max(1, int(float64(sourceBounds.Dy())*targetRatio))
		crop.Min.X += (sourceBounds.Dx() - cropWidth) / 2
		crop.Max.X = crop.Min.X + cropWidth
	} else if sourceRatio < targetRatio {
		cropHeight := max(1, int(float64(sourceBounds.Dx())/targetRatio))
		crop.Min.Y += (sourceBounds.Dy() - cropHeight) / 2
		crop.Max.Y = crop.Min.Y + cropHeight
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), source, crop, xdraw.Src, nil)
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, err
	}
	if output.Len() > imagegen.MaxInputBytes {
		return nil, errors.New("normalized image exceeds safe size")
	}
	return output.Bytes(), nil
}

func aspectRatio(width, height int) string {
	if float64(width)/float64(height) > 4 {
		return "4:1"
	}
	if float64(width)/float64(height) < 0.25 {
		return "1:4"
	}
	divisor := greatestCommonDivisor(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}

func greatestCommonDivisor(left, right int) int {
	for right != 0 {
		left, right = right, left%right
	}
	return max(1, left)
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
		return nil, errors.New("comfy partner: response is too large")
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
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown error"
}
