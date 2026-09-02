// Package openai turns a prompt or a small set of validated reference images
// into semantic source art through OpenAI's Image API. The API key stays on the
// Go server and is never returned to the browser.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var (
	ErrUnavailable = fmt.Errorf("%w: openai image service unavailable", imagegen.ErrUnavailable)
	ErrRejected    = errors.New("openai image: request rejected")
)

const maxResponseBytes = 40 << 20

type Options struct {
	APIKey  string
	Model   string
	Quality string
	BaseURL string
	Client  *http.Client
}

type Generator struct {
	apiKey  string
	model   string
	quality string
	baseURL *url.URL
	client  *http.Client
}

func New(options Options) (*Generator, error) {
	if strings.TrimSpace(options.APIKey) == "" {
		return nil, errors.New("openai image: API key is required")
	}
	if options.Model == "" {
		options.Model = "gpt-image-2"
	}
	if options.Quality == "" {
		options.Quality = "high"
	}
	switch options.Quality {
	case "low", "medium", "high", "auto":
	default:
		return nil, errors.New("openai image: quality must be low, medium, high, or auto")
	}
	if options.BaseURL == "" {
		options.BaseURL = "https://api.openai.com/v1"
	}
	baseURL, err := url.Parse(options.BaseURL)
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "https" && !(baseURL.Scheme == "http" && isLoopback(baseURL.Hostname()))) {
		return nil, errors.New("openai image: base URL must use HTTPS or loopback HTTP")
	}
	if baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("openai image: base URL cannot contain credentials, query, or fragment")
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 3 * time.Minute}
	}
	return &Generator{
		apiKey: strings.TrimSpace(options.APIKey), model: options.Model, quality: options.Quality,
		baseURL: baseURL, client: options.Client,
	}, nil
}

func (g *Generator) Descriptor() imagegen.Descriptor {
	return imagegen.Descriptor{
		ID: "openai-" + g.model, Label: "OpenAI " + g.model, Semantic: true, SupportsReferences: true,
	}
}

func (g *Generator) Generate(ctx context.Context, request imagegen.Request) (imagegen.Result, error) {
	if err := request.Validate(); err != nil {
		return imagegen.Result{}, err
	}
	prompt := imagegen.CinematicPrompt(request)
	modelWidth, modelHeight := modelDimensions(request.Width, request.Height)
	var httpRequest *http.Request
	var err error
	if len(request.Inputs) == 0 {
		httpRequest, err = g.generationRequest(ctx, prompt, modelWidth, modelHeight)
	} else {
		httpRequest, err = g.editRequest(ctx, prompt, modelWidth, modelHeight, request.Inputs)
	}
	if err != nil {
		return imagegen.Result{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+g.apiKey)
	response, err := g.client.Do(httpRequest)
	if err != nil {
		return imagegen.Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return imagegen.Result{}, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}
	if len(body) > maxResponseBytes {
		return imagegen.Result{}, fmt.Errorf("%w: response exceeds safe size", ErrUnavailable)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return imagegen.Result{}, responseError(response.StatusCode, body)
	}
	var envelope struct {
		Data []struct {
			Base64        string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 || envelope.Data[0].Base64 == "" {
		return imagegen.Result{}, fmt.Errorf("%w: response did not contain image data", ErrUnavailable)
	}
	decoded, err := base64.StdEncoding.DecodeString(envelope.Data[0].Base64)
	if err != nil || len(decoded) == 0 || len(decoded) > imagegen.MaxInputBytes {
		return imagegen.Result{}, fmt.Errorf("%w: image data is invalid or too large", ErrUnavailable)
	}
	normalized, err := normalizePNG(decoded, request.Width, request.Height)
	if err != nil {
		return imagegen.Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return imagegen.Result{
		Data: normalized, ContentType: "image/png", Engine: g.Descriptor().ID, RevisedPrompt: envelope.Data[0].RevisedPrompt,
	}, nil
}

func (g *Generator) generationRequest(ctx context.Context, prompt string, width, height int) (*http.Request, error) {
	payload, err := json.Marshal(map[string]any{
		"model": g.model, "prompt": prompt, "size": fmt.Sprintf("%dx%d", width, height),
		"quality": g.quality, "output_format": "png",
	})
	if err != nil {
		return nil, fmt.Errorf("openai image: encode generation request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.route("/images/generations"), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("openai image: create generation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func (g *Generator) editRequest(ctx context.Context, prompt string, width, height int, inputs []imagegen.Input) (*http.Request, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"model": g.model, "prompt": prompt, "size": fmt.Sprintf("%dx%d", width, height),
		"quality": g.quality, "output_format": "png",
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, fmt.Errorf("openai image: encode edit field: %w", err)
		}
	}
	for index, input := range inputs {
		extension := extensionFor(input.ContentType)
		part, err := writer.CreateFormFile("image[]", fmt.Sprintf("reference-%d%s", index+1, extension))
		if err != nil {
			return nil, fmt.Errorf("openai image: encode reference: %w", err)
		}
		if _, err := part.Write(input.Data); err != nil {
			return nil, fmt.Errorf("openai image: write reference: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("openai image: finish edit request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.route("/images/edits"), &body)
	if err != nil {
		return nil, fmt.Errorf("openai image: create edit request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request, nil
}

func (g *Generator) route(path string) string {
	return strings.TrimRight(g.baseURL.String(), "/") + path
}

func modelDimensions(width, height int) (int, int) {
	ratio := float64(width) / float64(height)
	switch {
	case ratio > 1.2:
		return 1536, 1024
	case ratio < 1/1.2:
		return 1024, 1536
	default:
		return 1024, 1024
	}
}

func normalizePNG(data []byte, width, height int) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("generated image is not decodable")
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	background := image.NewUniform(image.Black)
	draw.Draw(canvas, canvas.Bounds(), background, image.Point{}, draw.Src)
	sourceBounds := source.Bounds()
	sourceRatio := float64(sourceBounds.Dx()) / float64(sourceBounds.Dy())
	targetRatio := float64(width) / float64(height)
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
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), source, crop, xdraw.Src, nil)
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, fmt.Errorf("encode normalized image: %w", err)
	}
	if output.Len() > imagegen.MaxInputBytes {
		return nil, errors.New("normalized image exceeds safe size")
	}
	return output.Bytes(), nil
}

func responseError(status int, body []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		message = http.StatusText(status)
	}
	if len(message) > 300 {
		message = message[:300]
	}
	base := ErrRejected
	if status == http.StatusTooManyRequests || status >= 500 {
		base = ErrUnavailable
	}
	return fmt.Errorf("%w: HTTP %d: %s", base, status, message)
}

func extensionFor(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
