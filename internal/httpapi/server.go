// Package httpapi exposes the GoGIF engine to web, mobile, desktop, and
// extension clients through one small HTTP contract.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/reference"
	"github.com/brandopakel/gogifgenerator/internal/render"
	"github.com/brandopakel/gogifgenerator/internal/store"
	"github.com/brandopakel/gogifgenerator/webapp"
)

type Options struct {
	Planner          planner.Planner
	Logger           *slog.Logger
	AIEnabled        bool
	AIModel          string
	GiphyAPIKey      string
	Catalog          store.KV
	CatalogBackend   string
	GeneratedSaver   media.GeneratedSaver
	GeneratedReader  media.GeneratedReader
	Providers        []provider.Provider
	ImageGenerator   imagegen.Generator
	ReferenceFetcher *reference.Fetcher
}

func New(options Options) http.Handler {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	server := &server{options: options, providers: make(map[string]provider.Provider)}
	for _, candidate := range options.Providers {
		if candidate != nil && candidate.Descriptor().ID != "" {
			server.providers[candidate.Descriptor().ID] = candidate
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", server.health)
	mux.HandleFunc("GET /api/v1/config", server.publicConfig)
	mux.HandleFunc("GET /api/v1/providers/{provider}/search", server.searchProvider)
	mux.HandleFunc("POST /api/v1/gifs/plan", server.plan)
	mux.HandleFunc("POST /api/v1/gifs/generate", server.generate)
	mux.HandleFunc("POST /api/v1/gifs/generate-from-reference", server.generateFromReference)
	mux.HandleFunc("GET /api/v1/gifs/{id}", server.generated)
	mux.Handle("/", staticHandler())
	return server.securityHeaders(server.accessLog(mux))
}

type server struct {
	options   Options
	providers map[string]provider.Provider
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	statusCode := http.StatusOK
	status := "ok"
	catalogStatus := "disabled"
	if s.options.Catalog != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		err := s.options.Catalog.Ping(ctx)
		cancel()
		if err != nil {
			statusCode = http.StatusServiceUnavailable
			status = "degraded"
			catalogStatus = "unavailable"
		} else {
			catalogStatus = "ok"
		}
	}
	writeJSON(w, statusCode, map[string]any{
		"status":          status,
		"service":         "gogif",
		"ai":              s.options.AIEnabled,
		"catalog":         catalogStatus,
		"catalog_backend": s.options.CatalogBackend,
	})
}

func (s *server) publicConfig(w http.ResponseWriter, _ *http.Request) {
	mode := "local"
	if s.options.AIEnabled {
		mode = "ai"
	}
	providers := []map[string]any{
		{"id": "generated", "label": "Created here", "enabled": true},
		{"id": "giphy", "label": "GIPHY", "enabled": s.options.GiphyAPIKey != ""},
	}
	for _, candidate := range s.options.Providers {
		if candidate == nil {
			continue
		}
		descriptor := candidate.Descriptor()
		providers = append(providers, map[string]any{"id": descriptor.ID, "label": descriptor.Label, "enabled": true})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"planner":         mode,
		"model":           s.options.AIModel,
		"giphy_api_key":   s.options.GiphyAPIKey,
		"providers":       providers,
		"image_generator": imageGeneratorDescriptor(s.options.ImageGenerator),
	})
}

func (s *server) searchProvider(w http.ResponseWriter, r *http.Request) {
	candidate, ok := s.providers[r.PathValue("provider")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown media provider")
		return
	}
	limit := 0
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		var err error
		limit, err = strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be a number")
			return
		}
	}
	page, err := candidate.Search(r.Context(), provider.Query{
		Text:   r.URL.Query().Get("q"),
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
		Locale: r.URL.Query().Get("locale"),
	})
	if err != nil {
		switch {
		case errors.Is(err, provider.ErrInvalidQuery):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "media provider timed out")
		case errors.Is(err, context.Canceled):
			return
		default:
			s.options.Logger.Warn("search media provider", "provider", candidate.Descriptor().ID, "error", err)
			writeError(w, http.StatusBadGateway, "media provider is temporarily unavailable")
		}
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *server) plan(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeRequest(w, r)
	if !ok {
		return
	}
	result, err := s.options.Planner.Plan(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engine": result.Engine, "spec": result.Spec})
}

func (s *server) generate(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeRequest(w, r)
	if !ok {
		return
	}
	result, err := s.options.Planner.Plan(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, engine, err := s.createGIF(r.Context(), request, result, nil, false)
	if err != nil {
		s.options.Logger.Error("create GIF", "error", err)
		writeError(w, http.StatusInternalServerError, "could not render GIF")
		return
	}
	s.writeGenerated(w, r, request, result, data, engine, nil)
}

type referenceGenerateRequest struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	Locale     string `json:"locale,omitempty"`
	Prompt     string `json:"prompt"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Frames     int    `json:"frames,omitempty"`
	DelayMS    int    `json:"delay_ms,omitempty"`
	Seed       int64  `json:"seed,omitempty"`
}

func (s *server) generateFromReference(w http.ResponseWriter, r *http.Request) {
	if s.options.ImageGenerator == nil || s.options.ReferenceFetcher == nil {
		writeError(w, http.StatusServiceUnavailable, "local reference generation is not configured")
		return
	}
	if !s.options.ImageGenerator.Descriptor().SupportsReferences {
		writeError(w, http.StatusUnprocessableEntity, "configured local image generator does not accept reference images")
		return
	}
	request, ok := decodeReferenceRequest(w, r)
	if !ok {
		return
	}
	candidate, ok := s.providers[request.Provider]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown media provider")
		return
	}
	resolver, ok := candidate.(provider.Resolver)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "provider does not support reference transformation")
		return
	}
	resolved, err := resolver.Resolve(r.Context(), request.ExternalID, request.Locale)
	if err != nil {
		switch {
		case errors.Is(err, provider.ErrInvalidQuery):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, provider.ErrNotFound):
			writeError(w, http.StatusNotFound, "provider item was not found")
		default:
			s.options.Logger.Warn("resolve provider reference", "provider", request.Provider, "external_id", request.ExternalID, "error", err)
			writeError(w, http.StatusBadGateway, "could not revalidate provider item")
		}
		return
	}
	temporary, err := s.options.ReferenceFetcher.Fetch(r.Context(), resolved)
	if err != nil {
		switch {
		case errors.Is(err, reference.ErrNotTransformable):
			writeError(w, http.StatusUnprocessableEntity, "provider item is not approved for transformation")
		case errors.Is(err, reference.ErrTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		case errors.Is(err, reference.ErrUnsupportedMedia):
			writeError(w, http.StatusUnsupportedMediaType, err.Error())
		default:
			s.options.Logger.Warn("fetch provider reference", "provider", request.Provider, "external_id", request.ExternalID, "error", err)
			writeError(w, http.StatusBadGateway, "could not fetch provider reference")
		}
		return
	}
	defer temporary.Close()
	input, err := temporary.Input()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read temporary reference")
		return
	}
	plannerRequest := request.plannerRequest()
	plan, err := s.options.Planner.Plan(r.Context(), plannerRequest)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, engine, err := s.createGIF(r.Context(), plannerRequest, plan, []imagegen.Input{input}, true)
	if err != nil {
		s.options.Logger.Warn("generate from provider reference", "provider", request.Provider, "external_id", request.ExternalID, "error", err)
		writeError(w, http.StatusBadGateway, "local image generator could not transform the reference")
		return
	}
	if err := temporary.Close(); err != nil {
		s.options.Logger.Error("delete provider reference", "error", err)
		writeError(w, http.StatusInternalServerError, "could not delete temporary reference")
		return
	}
	s.writeGenerated(w, r, plannerRequest, plan, data, engine, &resolved)
}

func (s *server) createGIF(ctx context.Context, request planner.Request, result planner.Result, inputs []imagegen.Input, requireGenerator bool) ([]byte, string, error) {
	var output bytes.Buffer
	engine := result.Engine
	if s.options.ImageGenerator != nil {
		generated, generateErr := s.options.ImageGenerator.Generate(ctx, imagegen.Request{
			Prompt: request.Prompt, Inputs: inputs, Width: result.Spec.Width, Height: result.Spec.Height, Seed: result.Spec.Seed,
		})
		if generateErr != nil {
			if requireGenerator {
				return nil, "", generateErr
			}
			s.options.Logger.Warn("local image generator unavailable; using Go renderer", "generator", s.options.ImageGenerator.Descriptor().ID, "error", generateErr)
		} else if source, _, decodeErr := image.Decode(bytes.NewReader(generated.Data)); decodeErr != nil {
			if requireGenerator {
				return nil, "", fmt.Errorf("decode generated image: %w", decodeErr)
			}
			s.options.Logger.Warn("decode locally generated image; using Go renderer", "generator", generated.Engine, "error", decodeErr)
		} else if renderErr := render.ImageGIF(&output, source, result.Spec); renderErr != nil {
			if requireGenerator {
				return nil, "", fmt.Errorf("animate generated image: %w", renderErr)
			}
			s.options.Logger.Warn("animate locally generated image; using Go renderer", "generator", generated.Engine, "error", renderErr)
			output.Reset()
		} else {
			engine = generated.Engine + "+" + result.Engine
		}
	} else if requireGenerator {
		return nil, "", errors.New("local image generator is required")
	}
	if output.Len() == 0 {
		if err := render.GIF(&output, result.Spec); err != nil {
			return nil, "", err
		}
	}
	return output.Bytes(), engine, nil
}

func (s *server) writeGenerated(w http.ResponseWriter, r *http.Request, request planner.Request, result planner.Result, data []byte, engine string, source *provider.Result) {
	if s.options.GeneratedSaver != nil {
		generated := media.GeneratedAsset{
			Prompt: request.Prompt,
			Engine: engine,
			Spec:   result.Spec,
			Data:   data,
		}
		if source != nil {
			generated.Source = &media.GeneratedSource{
				Provider: source.Provider, ExternalID: source.ExternalID, SourceURL: source.SourceURL,
				Author: source.Author, LicenseID: source.LicenseID, LicenseURL: source.LicenseURL,
				Attribution: source.Attribution, CommercialUse: source.CommercialUse,
				Derivatives: source.Derivatives, ShareAlike: source.ShareAlike,
			}
		}
		asset, err := s.options.GeneratedSaver.SaveGenerated(r.Context(), generated)
		if err != nil {
			s.options.Logger.Warn("save generated GIF", "error", err)
		} else {
			w.Header().Set("X-GoGIF-Asset-ID", asset.ID)
			w.Header().Set("Location", "/api/v1/gifs/"+asset.ID)
		}
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Content-Disposition", `inline; filename="gogif.gif"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-GoGIF-Engine", engine)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		s.options.Logger.Error("write GIF response", "error", err)
	}
}

func decodeReferenceRequest(w http.ResponseWriter, r *http.Request) (referenceGenerateRequest, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request referenceGenerateRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request: "+err.Error())
		return referenceGenerateRequest{}, false
	}
	request.Provider = strings.TrimSpace(request.Provider)
	request.ExternalID = strings.TrimSpace(request.ExternalID)
	if request.Provider == "" || len(request.Provider) > 64 || request.ExternalID == "" || len(request.ExternalID) > 128 {
		writeError(w, http.StatusBadRequest, "provider and external_id are required")
		return referenceGenerateRequest{}, false
	}
	if request.Locale == "" {
		request.Locale = "en"
	}
	if err := request.plannerRequest().Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return referenceGenerateRequest{}, false
	}
	return request, true
}

func (r referenceGenerateRequest) plannerRequest() planner.Request {
	return planner.Request{Prompt: r.Prompt, Width: r.Width, Height: r.Height, Frames: r.Frames, DelayMS: r.DelayMS, Seed: r.Seed}
}

func imageGeneratorDescriptor(generator imagegen.Generator) any {
	if generator == nil {
		return nil
	}
	return generator.Descriptor()
}

func (s *server) generated(w http.ResponseWriter, r *http.Request) {
	if s.options.GeneratedReader == nil {
		http.NotFound(w, r)
		return
	}
	asset, reader, err := s.options.GeneratedReader.OpenGenerated(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.options.Logger.Error("open generated GIF", "id", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not read generated GIF")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Content-Disposition", `inline; filename="`+asset.ID+`.gif"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if _, err := io.Copy(w, reader); err != nil {
		s.options.Logger.Error("serve generated GIF", "id", asset.ID, "error", err)
	}
}

func decodeRequest(w http.ResponseWriter, r *http.Request) (planner.Request, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request planner.Request
	if err := decoder.Decode(&request); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request is too large")
			return planner.Request{}, false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON request: "+err.Error())
		return planner.Request{}, false
	}
	if err := request.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return planner.Request{}, false
	}
	return request, true
}

func staticHandler() http.Handler {
	files := webapp.Files()
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(files, path); err != nil {
			http.NotFound(w, r)
			return
		}
		if path == "service-worker.js" {
			w.Header().Set("Service-Worker-Allowed", "/")
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data: https://*.giphy.com https://upload.wikimedia.org https://blob.gifcities.org; connect-src 'self' https://api.giphy.com; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.options.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"status":  status,
			"message": message,
		},
	})
}

func AddressURL(address string) string {
	if strings.HasPrefix(address, ":") {
		return "http://localhost" + address
	}
	return fmt.Sprintf("http://%s", address)
}
