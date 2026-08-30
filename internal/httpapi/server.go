// Package httpapi exposes the GoGIF engine to web, mobile, desktop, and
// extension clients through one small HTTP contract.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/render"
	"github.com/brandopakel/gogifgenerator/internal/store"
	"github.com/brandopakel/gogifgenerator/webapp"
)

type Options struct {
	Planner         planner.Planner
	Logger          *slog.Logger
	AIEnabled       bool
	AIModel         string
	GiphyAPIKey     string
	Catalog         store.KV
	CatalogBackend  string
	GeneratedSaver  media.GeneratedSaver
	GeneratedReader media.GeneratedReader
	Providers       []provider.Provider
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
		"planner":       mode,
		"model":         s.options.AIModel,
		"giphy_api_key": s.options.GiphyAPIKey,
		"providers":     providers,
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
	var output bytes.Buffer
	if err := render.GIF(&output, result.Spec); err != nil {
		s.options.Logger.Error("render GIF", "error", err)
		writeError(w, http.StatusInternalServerError, "could not render GIF")
		return
	}
	if s.options.GeneratedSaver != nil {
		asset, err := s.options.GeneratedSaver.SaveGenerated(r.Context(), media.GeneratedAsset{
			Prompt: request.Prompt,
			Engine: result.Engine,
			Spec:   result.Spec,
			Data:   output.Bytes(),
		})
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
	w.Header().Set("X-GoGIF-Engine", result.Engine)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(output.Bytes()); err != nil {
		s.options.Logger.Error("write GIF response", "error", err)
	}
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data: https://*.giphy.com https://upload.wikimedia.org; connect-src 'self' https://api.giphy.com; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
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
