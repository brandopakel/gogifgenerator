// Package httpapi exposes the GoGIF engine to web, mobile, desktop, and
// extension clients through one small HTTP contract.
package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	stdgif "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/auth"
	"github.com/brandopakel/gogifgenerator/internal/billing"
	"github.com/brandopakel/gogifgenerator/internal/cinematic"
	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/modelgen"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/reference"
	"github.com/brandopakel/gogifgenerator/internal/render"
	"github.com/brandopakel/gogifgenerator/internal/store"
	"github.com/brandopakel/gogifgenerator/internal/video"
	"github.com/brandopakel/gogifgenerator/webapp"
)

type Options struct {
	Planner           planner.Planner
	Logger            *slog.Logger
	AIEnabled         bool
	AIModel           string
	GiphyAPIKey       string
	Catalog           store.KV
	CatalogBackend    string
	GeneratedSaver    media.GeneratedSaver
	GeneratedReader   media.GeneratedReader
	ModelSaver        media.ModelSaver
	ModelReader       media.ModelReader
	Providers         []provider.Provider
	ImageGenerator    imagegen.Generator
	ModelGenerator    modelgen.Generator
	CinematicRenderer cinematic.Renderer
	CinematicStatus   cinematic.Descriptor
	ReferenceFetcher  *reference.Fetcher
	VideoDecoder      video.Decoder
	Auth              *auth.Manager
	Accounts          *account.Repository
	Plans             account.Catalog
	Usage             *account.Ledger
	LibraryCatalog    *media.Repository
	Billing           *billing.Stripe
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
	mux.HandleFunc("GET /api/v1/account", server.accountStatus)
	mux.HandleFunc("GET /api/v1/auth/login", server.login)
	mux.HandleFunc("GET /api/v1/auth/callback", server.authCallback)
	mux.HandleFunc("POST /api/v1/auth/logout", server.logout)
	mux.HandleFunc("GET /api/v1/library", server.requireAccount(server.libraryAssets))
	mux.HandleFunc("PATCH /api/v1/library/{id}", server.requireAccount(server.updateLibraryAsset))
	mux.HandleFunc("DELETE /api/v1/library/{id}", server.requireAccount(server.deleteLibraryAsset))
	mux.HandleFunc("POST /api/v1/library/{id}/share", server.requireAccount(server.createLibraryShare))
	mux.HandleFunc("DELETE /api/v1/library/{id}/share", server.requireAccount(server.revokeLibraryShare))
	mux.HandleFunc("GET /api/v1/collections", server.requireAccount(server.listCollections))
	mux.HandleFunc("POST /api/v1/collections", server.requireAccount(server.createCollection))
	mux.HandleFunc("PATCH /api/v1/collections/{id}", server.requireAccount(server.updateCollection))
	mux.HandleFunc("DELETE /api/v1/collections/{id}", server.requireAccount(server.deleteCollection))
	mux.HandleFunc("PUT /api/v1/collections/{id}/assets/{asset}", server.requireAccount(server.addCollectionAsset))
	mux.HandleFunc("DELETE /api/v1/collections/{id}/assets/{asset}", server.requireAccount(server.removeCollectionAsset))
	mux.HandleFunc("POST /api/v1/billing/checkout", server.requireAccount(server.billingCheckout))
	mux.HandleFunc("POST /api/v1/billing/portal", server.requireAccount(server.billingPortal))
	mux.HandleFunc("POST /api/v1/billing/webhook", server.billingWebhook)
	mux.HandleFunc("GET /s/{token}", server.sharedAsset)
	mux.HandleFunc("GET /api/v1/providers/{provider}/search", server.searchProvider)
	mux.HandleFunc("GET /api/v1/providers/{provider}/items/{id}", server.resolveProvider)
	mux.HandleFunc("GET /api/v1/providers/{provider}/items/{id}/quote", server.resolveProviderQuote)
	mux.HandleFunc("POST /api/v1/gifs/plan", server.plan)
	mux.HandleFunc("POST /api/v1/gifs/generate", server.generate)
	mux.HandleFunc("POST /api/v1/gifs/generate-from-reference", server.generateFromReference)
	mux.HandleFunc("POST /api/v1/gifs/generate-from-upload", server.generateFromUpload)
	mux.HandleFunc("GET /api/v1/gifs/{id}", server.generated)
	mux.HandleFunc("POST /api/v1/models/generate", server.generateModel)
	mux.HandleFunc("GET /api/v1/models/{id}", server.generatedModel)
	mux.Handle("/", staticHandler())
	var handler http.Handler = mux
	if options.Auth != nil {
		handler = options.Auth.SameOrigin(handler)
		handler = options.Auth.Middleware(handler)
	}
	return server.securityHeaders(server.accessLog(handler))
}

type server struct {
	options   Options
	providers map[string]provider.Provider
}

type creationPermit struct {
	ledger      *account.Ledger
	actorID     string
	plan        account.Plan
	reservation account.Reservation
	legacy      bool
}

func (p creationPermit) Finish(ctx context.Context, success bool) {
	if p.legacy || p.ledger == nil || p.reservation.ID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if success {
		_ = p.ledger.Complete(ctx, p.actorID, p.plan, p.reservation.ID)
	} else {
		_ = p.ledger.Release(ctx, p.actorID, p.plan, p.reservation.ID)
	}
}

func (s *server) authorizeCreation(w http.ResponseWriter, r *http.Request, operation account.Operation) (creationPermit, bool) {
	principal := s.principal(r)
	if principal.Legacy || s.options.Auth == nil || !s.options.Auth.Enabled() {
		return creationPermit{legacy: true}, true
	}
	quote, err := s.options.Plans.Quote(principal, operation)
	if err != nil {
		s.writeAccessError(w, err)
		return creationPermit{}, false
	}
	if principal.Authenticated && s.options.LibraryCatalog != nil {
		count, bytes, usageErr := s.options.LibraryCatalog.OwnerUsage(r.Context(), principal.UserID)
		if usageErr != nil {
			writeError(w, http.StatusServiceUnavailable, "Your library usage could not be checked.")
			return creationPermit{}, false
		}
		if count >= quote.Plan.LibraryAssets || bytes >= quote.Plan.LibraryBytes {
			writeJSON(w, http.StatusPaymentRequired, map[string]any{"error": map[string]any{
				"status": http.StatusPaymentRequired, "code": "library_limit", "message": "Your private library is full. Delete an item or upgrade your plan.",
			}})
			return creationPermit{}, false
		}
	}
	if s.options.Usage == nil {
		writeError(w, http.StatusServiceUnavailable, "Usage accounting is not configured.")
		return creationPermit{}, false
	}
	reservation, _, err := s.options.Usage.Reserve(r.Context(), principal.ID, quote.Plan, quote.Cost)
	if err != nil {
		s.writeAccessError(w, err)
		return creationPermit{}, false
	}
	w.Header().Set("X-GoGIF-Credit-Cost", strconv.Itoa(quote.Cost))
	return creationPermit{ledger: s.options.Usage, actorID: principal.ID, plan: quote.Plan, reservation: reservation}, true
}

func (s *server) ensureLibraryRoom(w http.ResponseWriter, r *http.Request, additionalBytes int64) bool {
	principal := s.principal(r)
	if !principal.Authenticated || principal.Legacy || s.options.Auth == nil || !s.options.Auth.Enabled() || s.options.LibraryCatalog == nil {
		return true
	}
	plan, ok := s.options.Plans.Get(principal.PlanID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Your plan could not be checked.")
		return false
	}
	count, bytes, err := s.options.LibraryCatalog.OwnerUsage(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Your library usage could not be checked.")
		return false
	}
	if count+1 > plan.LibraryAssets || bytes+additionalBytes > plan.LibraryBytes {
		writeJSON(w, http.StatusPaymentRequired, map[string]any{"error": map[string]any{
			"status": http.StatusPaymentRequired, "code": "library_limit", "message": "This creation would exceed your private library limit. Delete an item or upgrade your plan. No credits were used.",
		}})
		return false
	}
	return true
}

func (s *server) writeAccessError(w http.ResponseWriter, err error) {
	status := http.StatusForbidden
	code := "plan_restricted"
	switch {
	case errors.Is(err, account.ErrSignInRequired):
		status, code = http.StatusUnauthorized, "sign_in_required"
	case errors.Is(err, account.ErrQuotaExceeded):
		status, code = http.StatusPaymentRequired, "credits_exhausted"
	case errors.Is(err, account.ErrUpgradeRequired):
		status, code = http.StatusPaymentRequired, "upgrade_required"
	case errors.Is(err, account.ErrQualityLimit):
		status, code = http.StatusUnprocessableEntity, "quality_limit"
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"status": status, "code": code, "message": err.Error()}})
}

var (
	errSemanticUnavailable = errors.New("semantic image generation is not configured")
	errSemanticGeneration  = errors.New("semantic image generation failed")
	errStudioUnavailable   = errors.New("studio rendering is not configured")
	errStudioGeneration    = errors.New("studio rendering failed")
)

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
		"status":           status,
		"service":          "gogif",
		"ai":               s.options.AIEnabled,
		"quality_pipeline": s.options.CinematicStatus.Enabled,
		"catalog":          catalogStatus,
		"catalog_backend":  s.options.CatalogBackend,
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
		"planner":          mode,
		"model":            s.options.AIModel,
		"giphy_api_key":    s.options.GiphyAPIKey,
		"providers":        providers,
		"image_generator":  imageGeneratorDescriptor(s.options.ImageGenerator),
		"model_generator":  modelGeneratorDescriptor(s.options.ModelGenerator),
		"quality_pipeline": s.options.CinematicStatus,
		"video_editor":     videoDecoderDescriptor(s.options.VideoDecoder),
	})
}

func (s *server) accountStatus(w http.ResponseWriter, r *http.Request) {
	principal := s.principal(r)
	enabled := s.options.Auth != nil && s.options.Auth.Enabled()
	response := map[string]any{
		"enabled": enabled, "auth_mode": auth.ModeDisabled, "authenticated": principal.Authenticated,
		"account": principal, "plans": s.options.Plans.Public(), "billing_enabled": s.options.Billing != nil,
	}
	if s.options.Auth != nil {
		response["auth_mode"] = s.options.Auth.Mode()
	}
	plan, ok := s.options.Plans.Get(principal.PlanID)
	if !ok && principal.Legacy {
		plan, ok = s.options.Plans.Get(account.PlanLegacy)
	}
	if ok {
		response["plan"] = plan
		if s.options.Usage != nil && principal.ID != "" && !principal.Legacy {
			if usage, err := s.options.Usage.Summary(r.Context(), principal.ID, plan); err == nil {
				held := 0
				for _, reservation := range usage.Reservations {
					held += reservation.Cost
				}
				response["usage"] = map[string]any{
					"used": usage.Used, "reserved": held, "limit": plan.Credits,
					"remaining": max(0, plan.Credits-usage.Used-held), "period": usage.Period,
				}
			}
		}
	}
	if principal.Authenticated && s.options.Accounts != nil && !principal.Legacy {
		if user, err := s.options.Accounts.Get(r.Context(), principal.UserID); err == nil {
			response["subscription"] = map[string]any{
				"status": user.SubscriptionStatus, "current_period_end": user.CurrentPeriodEnd,
				"has_customer": user.StripeCustomerID != "",
			}
		}
		if s.options.LibraryCatalog != nil {
			if count, bytes, err := s.options.LibraryCatalog.OwnerUsage(r.Context(), principal.UserID); err == nil {
				response["library_usage"] = map[string]any{"items": count, "bytes": bytes}
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.options.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "Sign in is not configured yet.")
		return
	}
	s.options.Auth.Login(w, r)
}

func (s *server) authCallback(w http.ResponseWriter, r *http.Request) {
	if s.options.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "Sign in is not configured yet.")
		return
	}
	s.options.Auth.Callback(w, r)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if s.options.Auth == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.options.Auth.Logout(w, r)
}

func (s *server) requireAccount(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.options.Auth == nil || !s.options.Auth.Enabled() {
			writeError(w, http.StatusServiceUnavailable, "Accounts are not configured yet.")
			return
		}
		if !s.principal(r).Authenticated {
			writeError(w, http.StatusUnauthorized, "Sign in to use your private library.")
			return
		}
		next(w, r)
	}
}

func (s *server) principal(r *http.Request) account.Principal {
	principal := account.PrincipalFrom(r.Context())
	if principal.ID == "" && (s.options.Auth == nil || !s.options.Auth.Enabled()) {
		return account.Principal{ID: "legacy", PlanID: account.PlanLegacy, Legacy: true}
	}
	return principal
}

type libraryItem struct {
	ID          string       `json:"id"`
	Kind        media.Kind   `json:"kind"`
	Title       string       `json:"title,omitempty"`
	Prompt      string       `json:"prompt,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Favorite    bool         `json:"favorite"`
	Shared      bool         `json:"shared"`
	ShareExpiry *time.Time   `json:"share_expiry,omitempty"`
	URL         string       `json:"url"`
	Width       int          `json:"width,omitempty"`
	Height      int          `json:"height,omitempty"`
	DurationMS  int64        `json:"duration_ms,omitempty"`
	SizeBytes   int64        `json:"size_bytes,omitempty"`
	Engine      string       `json:"engine,omitempty"`
	Rights      media.Rights `json:"rights"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func publicLibraryItem(asset media.Asset) libraryItem {
	item := libraryItem{
		ID: asset.ID, Kind: asset.Kind, Title: asset.Title, Prompt: asset.Prompt, Tags: asset.Tags,
		Favorite: asset.Favorite, Shared: asset.Shared, ShareExpiry: asset.ShareExpiry,
		Engine: asset.Provenance.Generator, Rights: asset.Rights, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt,
	}
	if asset.Kind == media.KindModel {
		item.URL = "/api/v1/models/" + asset.ID
	} else {
		item.URL = "/api/v1/gifs/" + asset.ID
	}
	for _, rendition := range asset.Renditions {
		if rendition.Name == "original" {
			item.Width, item.Height, item.DurationMS, item.SizeBytes = rendition.Width, rendition.Height, rendition.DurationMS, rendition.SizeBytes
			break
		}
	}
	return item
}

func (s *server) libraryAssets(w http.ResponseWriter, r *http.Request) {
	if s.options.LibraryCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "The private library is not configured.")
		return
	}
	limit := 24
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 50")
			return
		}
		limit = parsed
	}
	page, err := s.options.LibraryCatalog.ListOwner(r.Context(), s.principal(r).UserID, r.URL.Query().Get("kind"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items := make([]libraryItem, 0, len(page.Items))
	for _, asset := range page.Items {
		items = append(items, publicLibraryItem(asset))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": page.NextCursor})
}

func (s *server) updateLibraryAsset(w http.ResponseWriter, r *http.Request) {
	if s.options.LibraryCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "The private library is not configured.")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var patch media.AssetPatch
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid library update: "+err.Error())
		return
	}
	asset, err := s.options.LibraryCatalog.UpdateOwnerAsset(r.Context(), s.principal(r).UserID, r.PathValue("id"), patch)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicLibraryItem(asset))
}

func (s *server) deleteLibraryAsset(w http.ResponseWriter, r *http.Request) {
	if s.options.LibraryCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "The private library is not configured.")
		return
	}
	if err := s.options.LibraryCatalog.DeleteOwnerAsset(r.Context(), s.principal(r).UserID, r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "The asset could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) createLibraryShare(w http.ResponseWriter, r *http.Request) {
	if s.options.LibraryCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "The private library is not configured.")
		return
	}
	var request struct {
		Hours int `json:"hours"`
	}
	request.Hours = 24
	if r.Body != nil && r.Body != http.NoBody {
		defer r.Body.Close()
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid share request: "+err.Error())
			return
		}
	}
	if request.Hours < 1 || request.Hours > 24*30 {
		writeError(w, http.StatusBadRequest, "share duration must be between one hour and 30 days")
		return
	}
	grant, err := s.options.LibraryCatalog.CreateShare(r.Context(), s.principal(r).UserID, r.PathValue("id"), time.Duration(request.Hours)*time.Hour)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": "/s/" + grant.Token, "expires_at": grant.ExpiresAt})
}

func (s *server) revokeLibraryShare(w http.ResponseWriter, r *http.Request) {
	if s.options.LibraryCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "The private library is not configured.")
		return
	}
	if err := s.options.LibraryCatalog.RevokeShare(r.Context(), s.principal(r).UserID, r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "The share could not be revoked.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listCollections(w http.ResponseWriter, r *http.Request) {
	collections, err := s.options.LibraryCatalog.ListCollections(r.Context(), s.principal(r).UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Collections could not be loaded.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": collections})
}

func (s *server) createCollection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if !decodeSmallJSON(w, r, &request) {
		return
	}
	collection, err := s.options.LibraryCatalog.CreateCollection(r.Context(), s.principal(r).UserID, request.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, collection)
}

func (s *server) updateCollection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if !decodeSmallJSON(w, r, &request) {
		return
	}
	collection, err := s.options.LibraryCatalog.UpdateCollection(r.Context(), s.principal(r).UserID, r.PathValue("id"), request.Name, nil, false)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (s *server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	if err := s.options.LibraryCatalog.DeleteCollection(r.Context(), s.principal(r).UserID, r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "The collection could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) addCollectionAsset(w http.ResponseWriter, r *http.Request) {
	s.updateCollectionAsset(w, r, true)
}

func (s *server) removeCollectionAsset(w http.ResponseWriter, r *http.Request) {
	s.updateCollectionAsset(w, r, false)
}

func (s *server) updateCollectionAsset(w http.ResponseWriter, r *http.Request, add bool) {
	assetID := r.PathValue("asset")
	collection, err := s.options.LibraryCatalog.UpdateCollection(r.Context(), s.principal(r).UserID, r.PathValue("id"), "", &assetID, add)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func decodeSmallJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return false
	}
	return true
}

func (s *server) billingCheckout(w http.ResponseWriter, r *http.Request) {
	if s.options.Billing == nil || s.options.Accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "Billing is not configured yet.")
		return
	}
	var request struct {
		PlanID string `json:"plan_id"`
	}
	if !decodeSmallJSON(w, r, &request) {
		return
	}
	user, err := s.options.Accounts.Get(r.Context(), s.principal(r).UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The account could not be loaded.")
		return
	}
	checkoutURL, err := s.options.Billing.CreateCheckout(r.Context(), user, request.PlanID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Checkout could not be started: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": checkoutURL})
}

func (s *server) billingPortal(w http.ResponseWriter, r *http.Request) {
	if s.options.Billing == nil || s.options.Accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "Billing is not configured yet.")
		return
	}
	user, err := s.options.Accounts.Get(r.Context(), s.principal(r).UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The account could not be loaded.")
		return
	}
	portalURL, err := s.options.Billing.CreatePortal(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Billing management could not be opened: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": portalURL})
}

func (s *server) billingWebhook(w http.ResponseWriter, r *http.Request) {
	if s.options.Billing == nil {
		http.NotFound(w, r)
		return
	}
	defer r.Body.Close()
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "The webhook body could not be read.")
		return
	}
	if err := s.options.Billing.HandleWebhook(r.Context(), payload, r.Header.Get("Stripe-Signature")); err != nil {
		s.options.Logger.Warn("reject Stripe webhook", "error", err)
		writeError(w, http.StatusBadRequest, "The Stripe event could not be verified.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) sharedAsset(w http.ResponseWriter, r *http.Request) {
	if s.options.LibraryCatalog == nil {
		http.NotFound(w, r)
		return
	}
	_, asset, err := s.options.LibraryCatalog.ResolveShare(r.Context(), r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if asset.Kind == media.KindModel {
		s.serveSharedModel(w, r, asset)
		return
	}
	s.serveSharedGIF(w, r, asset)
}

func (s *server) serveSharedGIF(w http.ResponseWriter, r *http.Request, asset media.Asset) {
	if s.options.GeneratedReader == nil {
		http.NotFound(w, r)
		return
	}
	_, reader, err := s.options.GeneratedReader.OpenGenerated(r.Context(), asset.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Content-Disposition", `inline; filename="`+asset.ID+`.gif"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, reader)
}

func (s *server) serveSharedModel(w http.ResponseWriter, r *http.Request, asset media.Asset) {
	if s.options.ModelReader == nil {
		http.NotFound(w, r)
		return
	}
	_, reader, err := s.options.ModelReader.OpenModel(r.Context(), asset.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "model/gltf-binary")
	w.Header().Set("Content-Disposition", `inline; filename="`+asset.ID+`.glb"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, reader)
}

func (s *server) generateModel(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request modelgen.Request
	if err := decoder.Decode(&request); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON request: "+err.Error())
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	permit, ok := s.authorizeCreation(w, r, account.Operation{Kind: "model"})
	if !ok {
		return
	}
	succeeded := false
	defer func() { permit.Finish(r.Context(), succeeded) }()
	if s.options.ModelGenerator == nil {
		writeError(w, http.StatusServiceUnavailable, "3D generation needs a configured ComfyUI 3D workflow and API key")
		return
	}
	result, err := s.options.ModelGenerator.Generate(r.Context(), request)
	if err != nil {
		s.options.Logger.Warn("3D model generation failed", "error", err)
		writeError(w, http.StatusBadGateway, "3D model generation failed: "+err.Error())
		return
	}
	if len(result.Data) < 12 || len(result.Data) > modelgen.MaxOutputBytes || string(result.Data[:4]) != "glTF" || result.ContentType != "model/gltf-binary" {
		s.options.Logger.Warn("3D model generator returned invalid output", "engine", result.Engine)
		writeError(w, http.StatusBadGateway, "3D model generator returned an invalid GLB")
		return
	}
	principal := s.principal(r)
	requireSave := principal.Authenticated && !principal.Legacy && s.options.Auth != nil && s.options.Auth.Enabled()
	if requireSave && !s.ensureLibraryRoom(w, r, int64(len(result.Data))) {
		return
	}
	if requireSave && s.options.ModelSaver == nil {
		writeError(w, http.StatusServiceUnavailable, "Your 3D model could not be added to the private library. No credits were used.")
		return
	}
	if s.options.ModelSaver != nil {
		asset, saveErr := s.options.ModelSaver.SaveModel(r.Context(), media.GeneratedModel{
			OwnerID: principal.OwnerID(), Prompt: request.Prompt, Engine: result.Engine, Data: result.Data,
		})
		if saveErr != nil {
			s.options.Logger.Warn("save generated 3D model", "error", saveErr)
			if requireSave {
				writeError(w, http.StatusServiceUnavailable, "Your 3D model could not be added to the private library. No credits were used.")
				return
			}
		} else {
			w.Header().Set("X-GoGIF-Asset-ID", asset.ID)
			w.Header().Set("Location", "/api/v1/models/"+asset.ID)
		}
	}
	w.Header().Set("Content-Type", "model/gltf-binary")
	w.Header().Set("Content-Disposition", `inline; filename="gogif-model.glb"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-GoGIF-Engine", result.Engine)
	succeeded = true
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(result.Data); err != nil {
		s.options.Logger.Error("write GLB response", "error", err)
	}
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

func (s *server) resolveProvider(w http.ResponseWriter, r *http.Request) {
	candidate, ok := s.providers[r.PathValue("provider")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown media provider")
		return
	}
	resolver, ok := candidate.(provider.Resolver)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "provider does not expose item details")
		return
	}
	result, err := resolver.Resolve(r.Context(), r.PathValue("id"), r.URL.Query().Get("locale"))
	if err != nil {
		switch {
		case errors.Is(err, provider.ErrInvalidQuery):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, provider.ErrUnsupported):
			writeError(w, http.StatusUnprocessableEntity, "provider does not expose item details")
		case errors.Is(err, provider.ErrNotFound):
			writeError(w, http.StatusNotFound, "provider item was not found")
		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "media provider timed out")
		case errors.Is(err, context.Canceled):
			return
		default:
			s.options.Logger.Warn("resolve media provider item", "provider", candidate.Descriptor().ID, "id", r.PathValue("id"), "error", err)
			writeError(w, http.StatusBadGateway, "media provider is temporarily unavailable")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) resolveProviderQuote(w http.ResponseWriter, r *http.Request) {
	candidate, ok := s.providers[r.PathValue("provider")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown media provider")
		return
	}
	resolver, ok := candidate.(provider.QuoteResolver)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "provider does not support quote matching")
		return
	}
	result, err := resolver.ResolveQuote(r.Context(), r.PathValue("id"), r.URL.Query().Get("locale"), r.URL.Query().Get("q"))
	if err != nil {
		switch {
		case errors.Is(err, provider.ErrInvalidQuery):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, provider.ErrUnsupported):
			writeError(w, http.StatusUnprocessableEntity, "provider does not support quote matching")
		case errors.Is(err, provider.ErrNotFound):
			writeError(w, http.StatusNotFound, "provider item was not found")
		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "media provider timed out")
		case errors.Is(err, context.Canceled):
			return
		default:
			s.options.Logger.Warn("match provider item quote", "provider", candidate.Descriptor().ID, "id", r.PathValue("id"), "error", err)
			writeError(w, http.StatusBadGateway, "media provider is temporarily unavailable")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
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
	permit, ok := s.authorizeCreation(w, r, account.Operation{Kind: "gif", Mode: request.GenerationMode, Width: result.Spec.Width, Height: result.Spec.Height, Frames: result.Spec.Frames})
	if !ok {
		return
	}
	succeeded := false
	defer func() { permit.Finish(r.Context(), succeeded) }()
	data, engine, err := s.createGIF(r.Context(), request, result, nil, false)
	if err != nil {
		switch {
		case errors.Is(err, errSemanticUnavailable):
			writeError(w, http.StatusServiceUnavailable, "Realistic AI generation is not configured. Configure OpenAI Images or ComfyUI, or choose Fast local.")
		case errors.Is(err, errStudioUnavailable):
			writeError(w, http.StatusServiceUnavailable, "Studio Local is not configured. Choose Realistic AI for hosted generation without launching local 3D editors.")
		case errors.Is(err, errStudioGeneration):
			s.options.Logger.Warn("studio GIF generation failed", "error", err)
			writeError(w, http.StatusBadGateway, "Studio Local could not finish the render. Choose Realistic AI to avoid running Blender, Unity, and Unreal on this computer.")
		case errors.Is(err, errSemanticGeneration):
			s.options.Logger.Warn("semantic GIF generation failed", "error", err)
			message := "The semantic image generator could not create this scene. Try again or choose Fast local."
			if errors.Is(err, imagegen.ErrUnavailable) {
				descriptor := imageGeneratorDescriptorValue(s.options.ImageGenerator)
				switch descriptor.ID {
				case "comfyui-local":
					message = "ComfyUI Desktop is not running or stopped responding. Open ComfyUI, then try Realistic AI again—or choose Fast local."
				case "comfyui-partner-flux-ultra":
					message = "The hosted Comfy GPU service is temporarily unavailable. Check the Comfy subscription and credits, then try again."
				}
			}
			writeError(w, http.StatusBadGateway, message)
		default:
			s.options.Logger.Error("create GIF", "error", err)
			writeError(w, http.StatusInternalServerError, "could not render GIF")
		}
		return
	}
	succeeded = s.writeGenerated(w, r, request, result, data, engine, nil)
}

const (
	maxUploadBytes           = 20 << 20
	maxUploadRequest         = maxUploadBytes + (256 << 10)
	maxUploadDimension       = 8192
	maxUploadPixels          = 32_000_000
	maxUploadSourceFrames    = 120
	maxUploadGIFPixels       = 48_000_000
	maxUploadCompositePixels = 24_000_000
	minTargetGIFBytes        = 256 << 10
	maxTargetGIFBytes        = 20 << 20
	maxVideoStartMS          = 300_000
	maxVideoDurationMS       = 15_000
)

type uploadGenerateRequest struct {
	Caption         string
	Width           int
	Height          int
	Frames          int
	DelayMS         int
	Motion          string
	Seed            int64
	CropX           float64
	CropY           float64
	Zoom            float64
	CaptionPosition string
	Loop            bool
	TrimStartMS     int
	TrimEndMS       int
	MaxBytes        int
}

func (s *server) generateFromUpload(w http.ResponseWriter, r *http.Request) {
	request, uploaded, ok := decodeUploadRequest(w, r)
	if !ok {
		return
	}

	spec := gifdomain.Defaults()
	spec.Width, spec.Height = request.Width, request.Height
	spec.Frames, spec.DelayMS = request.Frames, request.DelayMS
	spec.Caption, spec.Motion, spec.Seed = request.Caption, request.Motion, request.Seed
	spec.ShowPrompt = strings.TrimSpace(request.Caption) != ""
	spec, err := spec.Normalize()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	permit, ok := s.authorizeCreation(w, r, account.Operation{Kind: "gif", Mode: "upload", Width: spec.Width, Height: spec.Height, Frames: spec.Frames})
	if !ok {
		return
	}
	succeeded := false
	defer func() { permit.Finish(r.Context(), succeeded) }()
	options := render.EditOptions{
		CropX: request.CropX, CropY: request.CropY, Zoom: request.Zoom,
		CaptionPosition: request.CaptionPosition, Loop: request.Loop,
	}
	configuration, detectedFormat, imageConfigErr := image.DecodeConfig(bytes.NewReader(uploaded.Data))
	var renderSource func(gifdomain.Spec) ([]byte, error)
	engine := "upload-photo+go"
	if imageConfigErr == nil && (detectedFormat == "jpeg" || detectedFormat == "png" || detectedFormat == "gif") {
		if configuration.Width < 1 || configuration.Height < 1 || configuration.Width > maxUploadDimension || configuration.Height > maxUploadDimension ||
			int64(configuration.Width)*int64(configuration.Height) > maxUploadPixels {
			writeError(w, http.StatusUnprocessableEntity, "uploaded image dimensions are too large")
			return
		}
		if detectedFormat == "gif" {
			sourceFrames, sourcePixels, inspectErr := inspectGIF(uploaded.Data)
			compositePixels := int64(configuration.Width) * int64(configuration.Height) * int64(min(sourceFrames, spec.Frames))
			if inspectErr != nil {
				writeError(w, http.StatusUnsupportedMediaType, "uploaded GIF could not be decoded")
				return
			}
			if sourceFrames > maxUploadSourceFrames || sourcePixels > maxUploadGIFPixels || compositePixels > maxUploadCompositePixels {
				writeError(w, http.StatusUnprocessableEntity, "uploaded GIF is too complex to edit safely")
				return
			}
			animation, decodeErr := stdgif.DecodeAll(bytes.NewReader(uploaded.Data))
			if decodeErr != nil {
				writeError(w, http.StatusUnsupportedMediaType, "uploaded GIF could not be decoded")
				return
			}
			if len(animation.Image) > 1 {
				engine = "upload-gif+go"
				renderSource = func(candidate gifdomain.Spec) ([]byte, error) {
					var output bytes.Buffer
					err := render.EditedGIF(&output, animation, candidate, options)
					return output.Bytes(), err
				}
			} else {
				source := animation.Image[0]
				renderSource = func(candidate gifdomain.Spec) ([]byte, error) {
					var output bytes.Buffer
					err := render.EditedImageGIF(&output, source, candidate, options)
					return output.Bytes(), err
				}
			}
		} else {
			source, _, decodeErr := image.Decode(bytes.NewReader(uploaded.Data))
			if decodeErr != nil {
				writeError(w, http.StatusUnsupportedMediaType, "uploaded image could not be decoded")
				return
			}
			renderSource = func(candidate gifdomain.Spec) ([]byte, error) {
				var output bytes.Buffer
				err := render.EditedImageGIF(&output, source, candidate, options)
				return output.Bytes(), err
			}
		}
	} else if isVideoUpload(uploaded) {
		if !videoDecoderAvailable(s.options.VideoDecoder) {
			writeError(w, http.StatusServiceUnavailable, "short-video editing requires local FFmpeg; install it and restart GoGIF")
			return
		}
		animation, decodeErr := s.options.VideoDecoder.Decode(r.Context(), video.Request{
			Data: uploaded.Data, Filename: uploaded.Filename, StartMS: request.TrimStartMS, EndMS: request.TrimEndMS, Frames: spec.Frames,
		})
		if decodeErr != nil {
			s.options.Logger.Warn("decode uploaded video", "error", decodeErr)
			writeError(w, http.StatusUnprocessableEntity, "video could not be decoded; check the trim range and local FFmpeg codecs")
			return
		}
		engine = "upload-video+" + s.options.VideoDecoder.Descriptor().ID + "+go"
		renderSource = func(candidate gifdomain.Spec) ([]byte, error) {
			var output bytes.Buffer
			err := render.EditedGIF(&output, animation, candidate, options)
			return output.Bytes(), err
		}
	} else {
		writeError(w, http.StatusUnsupportedMediaType, "upload must be a JPEG, PNG, GIF, MP4, MOV, M4V, or WebM file")
		return
	}

	data, exportedSpec, err := optimizeUploadGIF(spec, request.MaxBytes, renderSource)
	if err != nil {
		if errors.Is(err, errTargetSize) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	prompt := strings.TrimSpace(request.Caption)
	if prompt == "" {
		prompt = "Edited upload"
	}
	w.Header().Set("X-GoGIF-Width", strconv.Itoa(exportedSpec.Width))
	w.Header().Set("X-GoGIF-Height", strconv.Itoa(exportedSpec.Height))
	w.Header().Set("X-GoGIF-Frames", strconv.Itoa(exportedSpec.Frames))
	w.Header().Set("X-GoGIF-Bytes", strconv.Itoa(len(data)))
	if request.MaxBytes > 0 {
		w.Header().Set("X-GoGIF-Target-Bytes", strconv.Itoa(request.MaxBytes))
	}
	succeeded = s.writeGenerated(w, r, planner.Request{
		Prompt: prompt, Width: exportedSpec.Width, Height: exportedSpec.Height, Frames: exportedSpec.Frames,
		DelayMS: exportedSpec.DelayMS, Seed: exportedSpec.Seed,
	}, planner.Result{Spec: exportedSpec, Engine: engine}, data, engine, nil)
}

var errTargetSize = errors.New("could not meet the requested GIF size target at the minimum 128px and 4 frames")

func optimizeUploadGIF(spec gifdomain.Spec, maxBytes int, renderSource func(gifdomain.Spec) ([]byte, error)) ([]byte, gifdomain.Spec, error) {
	candidate := spec
	for attempt := 0; attempt < 12; attempt++ {
		data, err := renderSource(candidate)
		if err != nil {
			return nil, gifdomain.Spec{}, err
		}
		if maxBytes == 0 || len(data) <= maxBytes {
			return data, candidate, nil
		}
		if candidate.Width == gifdomain.MinDimension && candidate.Height == gifdomain.MinDimension && candidate.Frames == gifdomain.MinFrames {
			return nil, gifdomain.Spec{}, errTargetSize
		}
		ratio := float64(maxBytes) / float64(len(data))
		scale := min(0.82, max(0.55, math.Sqrt(ratio)*0.94))
		candidate.Width = reducedDimension(candidate.Width, scale)
		candidate.Height = reducedDimension(candidate.Height, scale)
		candidate.Frames = max(gifdomain.MinFrames, min(candidate.Frames-1, int(math.Floor(float64(candidate.Frames)*0.78))))
	}
	return nil, gifdomain.Spec{}, errTargetSize
}

func reducedDimension(value int, scale float64) int {
	if value <= gifdomain.MinDimension {
		return gifdomain.MinDimension
	}
	reduced := int(math.Floor(float64(value)*scale/16)) * 16
	return max(gifdomain.MinDimension, min(value-1, reduced))
}

type uploadedMedia struct {
	Data     []byte
	Filename string
}

func isVideoUpload(uploaded uploadedMedia) bool {
	if len(uploaded.Data) >= 12 && string(uploaded.Data[4:8]) == "ftyp" {
		return true
	}
	return len(uploaded.Data) >= 4 && bytes.Equal(uploaded.Data[:4], []byte{0x1a, 0x45, 0xdf, 0xa3})
}

func inspectGIF(data []byte) (int, int64, error) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return 0, 0, errors.New("invalid GIF header")
	}
	canvasWidth := int(binary.LittleEndian.Uint16(data[6:8]))
	canvasHeight := int(binary.LittleEndian.Uint16(data[8:10]))
	if canvasWidth < 1 || canvasHeight < 1 {
		return 0, 0, errors.New("invalid GIF canvas")
	}
	position := 13
	if data[10]&0x80 != 0 {
		position += 3 * (1 << ((data[10] & 0x07) + 1))
	}
	if position > len(data) {
		return 0, 0, errors.New("truncated GIF color table")
	}
	frames := 0
	var pixels int64
	for position < len(data) {
		block := data[position]
		position++
		switch block {
		case 0x3b:
			if frames == 0 {
				return 0, 0, errors.New("GIF has no frames")
			}
			return frames, pixels, nil
		case 0x21:
			if position >= len(data) {
				return 0, 0, errors.New("truncated GIF extension")
			}
			position++ // Extension label.
			var err error
			position, err = skipGIFSubBlocks(data, position)
			if err != nil {
				return 0, 0, err
			}
		case 0x2c:
			if position+9 > len(data) {
				return 0, 0, errors.New("truncated GIF image descriptor")
			}
			left := int(binary.LittleEndian.Uint16(data[position : position+2]))
			top := int(binary.LittleEndian.Uint16(data[position+2 : position+4]))
			width := int(binary.LittleEndian.Uint16(data[position+4 : position+6]))
			height := int(binary.LittleEndian.Uint16(data[position+6 : position+8]))
			packed := data[position+8]
			position += 9
			if width < 1 || height < 1 || left+width > canvasWidth || top+height > canvasHeight {
				return 0, 0, errors.New("invalid GIF frame bounds")
			}
			frames++
			pixels += int64(width) * int64(height)
			if frames > maxUploadSourceFrames || pixels > maxUploadGIFPixels {
				return frames, pixels, nil
			}
			if packed&0x80 != 0 {
				position += 3 * (1 << ((packed & 0x07) + 1))
			}
			if position >= len(data) {
				return 0, 0, errors.New("truncated GIF image data")
			}
			position++ // LZW minimum code size.
			var err error
			position, err = skipGIFSubBlocks(data, position)
			if err != nil {
				return 0, 0, err
			}
		default:
			return 0, 0, errors.New("invalid GIF block")
		}
	}
	return 0, 0, errors.New("GIF trailer is missing")
}

func skipGIFSubBlocks(data []byte, position int) (int, error) {
	for position < len(data) {
		size := int(data[position])
		position++
		if size == 0 {
			return position, nil
		}
		if position+size > len(data) {
			return 0, errors.New("truncated GIF data block")
		}
		position += size
	}
	return 0, errors.New("truncated GIF data blocks")
}

type referenceGenerateRequest struct {
	Provider       string `json:"provider"`
	ExternalID     string `json:"external_id"`
	Locale         string `json:"locale,omitempty"`
	Prompt         string `json:"prompt"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	Frames         int    `json:"frames,omitempty"`
	DelayMS        int    `json:"delay_ms,omitempty"`
	Seed           int64  `json:"seed,omitempty"`
	GenerationMode string `json:"generation_mode,omitempty"`
}

func (s *server) generateFromReference(w http.ResponseWriter, r *http.Request) {
	if !s.generationSupportsReferences() || s.options.ReferenceFetcher == nil {
		writeError(w, http.StatusServiceUnavailable, "local reference generation is not configured")
		return
	}
	request, ok := decodeReferenceRequest(w, r)
	if !ok {
		return
	}
	plannerRequest := request.plannerRequest()
	plan, err := s.options.Planner.Plan(r.Context(), plannerRequest)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	permit, ok := s.authorizeCreation(w, r, account.Operation{Kind: "gif", Mode: request.GenerationMode, Width: plan.Spec.Width, Height: plan.Spec.Height, Frames: plan.Spec.Frames})
	if !ok {
		return
	}
	succeeded := false
	defer func() { permit.Finish(r.Context(), succeeded) }()
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
	succeeded = s.writeGenerated(w, r, plannerRequest, plan, data, engine, &resolved)
}

func (s *server) createGIF(ctx context.Context, request planner.Request, result planner.Result, inputs []imagegen.Input, requireGenerator bool) ([]byte, string, error) {
	mode := strings.ToLower(strings.TrimSpace(request.GenerationMode))
	studioRequested := mode == "studio"
	semanticRequired := (mode == "semantic" || studioRequested) && len(inputs) == 0
	if semanticRequired {
		// The result screen already presents the prompt beneath the media. Keep
		// semantic source art clean so text does not cover the generated scene.
		result.Spec.ShowPrompt = false
	}
	if semanticRequired && (s.options.ImageGenerator == nil || !s.options.ImageGenerator.Descriptor().Semantic) {
		return nil, "", errSemanticUnavailable
	}
	if studioRequested && s.options.CinematicRenderer == nil {
		return nil, "", errStudioUnavailable
	}
	imageSupportsReferences := s.options.ImageGenerator != nil && s.options.ImageGenerator.Descriptor().SupportsReferences
	studioRequired := studioRequested || (len(inputs) > 0 && requireGenerator && !imageSupportsReferences)
	if studioRequired && s.options.CinematicRenderer != nil {
		generated, renderErr := s.options.CinematicRenderer.Render(ctx, cinematic.Request{
			Prompt: request.Prompt, Inputs: inputs, Spec: result.Spec,
		})
		if renderErr == nil {
			return generated.Data, generated.Engine + "+" + result.Engine, nil
		}
		if studioRequested {
			return nil, "", fmt.Errorf("%w: %w", errStudioGeneration, renderErr)
		}
		return nil, "", renderErr
	}
	var output bytes.Buffer
	engine := result.Engine
	useImageGenerator := semanticRequired || requireGenerator
	if useImageGenerator && s.options.ImageGenerator != nil {
		generated, generateErr := s.options.ImageGenerator.Generate(ctx, imagegen.Request{
			Prompt: request.Prompt, Inputs: inputs, Width: result.Spec.Width, Height: result.Spec.Height, Seed: result.Spec.Seed,
		})
		if generateErr != nil {
			if requireGenerator || semanticRequired {
				if semanticRequired {
					return nil, "", fmt.Errorf("%w: %w", errSemanticGeneration, generateErr)
				}
				return nil, "", generateErr
			}
			s.options.Logger.Warn("local image generator unavailable; using Go renderer", "generator", s.options.ImageGenerator.Descriptor().ID, "error", generateErr)
		} else if source, _, decodeErr := image.Decode(bytes.NewReader(generated.Data)); decodeErr != nil {
			if requireGenerator || semanticRequired {
				if semanticRequired {
					return nil, "", fmt.Errorf("%w: decode generated image: %v", errSemanticGeneration, decodeErr)
				}
				return nil, "", fmt.Errorf("decode generated image: %w", decodeErr)
			}
			s.options.Logger.Warn("decode locally generated image; using Go renderer", "generator", generated.Engine, "error", decodeErr)
		} else if renderErr := render.ImageGIF(&output, source, result.Spec); renderErr != nil {
			if requireGenerator || semanticRequired {
				if semanticRequired {
					return nil, "", fmt.Errorf("%w: animate generated image: %v", errSemanticGeneration, renderErr)
				}
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

func (s *server) generationSupportsReferences() bool {
	if s.options.CinematicRenderer != nil && s.options.CinematicRenderer.Descriptor().SupportsReferences {
		return true
	}
	return s.options.ImageGenerator != nil && s.options.ImageGenerator.Descriptor().SupportsReferences
}

func (s *server) writeGenerated(w http.ResponseWriter, r *http.Request, request planner.Request, result planner.Result, data []byte, engine string, source *provider.Result) bool {
	principal := s.principal(r)
	shouldSave := principal.Authenticated || principal.Legacy || s.options.Auth == nil || !s.options.Auth.Enabled()
	requireSave := principal.Authenticated && !principal.Legacy && s.options.Auth != nil && s.options.Auth.Enabled()
	if requireSave && !s.ensureLibraryRoom(w, r, int64(len(data))) {
		return false
	}
	if requireSave && s.options.GeneratedSaver == nil {
		writeError(w, http.StatusServiceUnavailable, "Your creation could not be added to the private library. No credits were used.")
		return false
	}
	if s.options.GeneratedSaver != nil && shouldSave {
		generated := media.GeneratedAsset{
			OwnerID: principal.OwnerID(),
			Prompt:  request.Prompt,
			Engine:  engine,
			Spec:    result.Spec,
			Data:    data,
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
			if requireSave {
				writeError(w, http.StatusServiceUnavailable, "Your creation could not be added to the private library. No credits were used.")
				return false
			}
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
	return true
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
	return planner.Request{
		Prompt: r.Prompt, Width: r.Width, Height: r.Height, Frames: r.Frames,
		DelayMS: r.DelayMS, Seed: r.Seed, GenerationMode: r.GenerationMode,
	}
}

func imageGeneratorDescriptor(generator imagegen.Generator) any {
	if generator == nil {
		return nil
	}
	return generator.Descriptor()
}

func imageGeneratorDescriptorValue(generator imagegen.Generator) imagegen.Descriptor {
	if generator == nil {
		return imagegen.Descriptor{}
	}
	return generator.Descriptor()
}

func modelGeneratorDescriptor(generator modelgen.Generator) any {
	if generator == nil {
		return nil
	}
	return generator.Descriptor()
}

func videoDecoderDescriptor(decoder video.Decoder) any {
	if !videoDecoderAvailable(decoder) {
		return map[string]any{"enabled": false}
	}
	descriptor := decoder.Descriptor()
	return map[string]any{"enabled": true, "id": descriptor.ID, "label": descriptor.Label, "local": descriptor.Local}
}

func videoDecoderAvailable(decoder video.Decoder) bool {
	if decoder == nil {
		return false
	}
	value := reflect.ValueOf(decoder)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
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
	principal := s.principal(r)
	if asset.OwnerID != "" && asset.OwnerID != principal.UserID && !principal.IsAdmin() {
		_ = reader.Close()
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Content-Disposition", `inline; filename="`+asset.ID+`.gif"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if asset.Provenance.Generator != "" {
		w.Header().Set("X-GoGIF-Engine", asset.Provenance.Generator)
	}
	if _, err := io.Copy(w, reader); err != nil {
		s.options.Logger.Error("serve generated GIF", "id", asset.ID, "error", err)
	}
}

func (s *server) generatedModel(w http.ResponseWriter, r *http.Request) {
	if s.options.ModelReader == nil {
		http.NotFound(w, r)
		return
	}
	asset, reader, err := s.options.ModelReader.OpenModel(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.options.Logger.Error("open generated 3D model", "id", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not read generated 3D model")
		return
	}
	principal := s.principal(r)
	if asset.OwnerID != "" && asset.OwnerID != principal.UserID && !principal.IsAdmin() {
		_ = reader.Close()
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "model/gltf-binary")
	w.Header().Set("Content-Disposition", `inline; filename="`+asset.ID+`.glb"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if _, err := io.Copy(w, reader); err != nil {
		s.options.Logger.Error("serve generated 3D model", "id", asset.ID, "error", err)
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

func decodeUploadRequest(w http.ResponseWriter, r *http.Request) (uploadGenerateRequest, uploadedMedia, bool) {
	empty := uploadedMedia{}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		writeError(w, http.StatusUnsupportedMediaType, "upload request must use multipart/form-data")
		return uploadGenerateRequest{}, empty, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequest)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "uploaded media must not exceed 20 MiB")
		} else {
			writeError(w, http.StatusBadRequest, "invalid multipart upload: "+err.Error())
		}
		return uploadGenerateRequest{}, empty, false
	}
	defer r.MultipartForm.RemoveAll()
	allowedValues := map[string]bool{
		"caption": true, "width": true, "height": true, "frames": true, "delay_ms": true,
		"motion": true, "seed": true, "crop_x": true, "crop_y": true, "zoom": true,
		"caption_position": true, "loop": true, "trim_start_ms": true, "trim_end_ms": true,
		"max_bytes": true,
	}
	for key, values := range r.MultipartForm.Value {
		if !allowedValues[key] || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "unsupported or repeated upload field: "+key)
			return uploadGenerateRequest{}, empty, false
		}
	}
	for key := range r.MultipartForm.File {
		if key != "media" {
			writeError(w, http.StatusBadRequest, "unsupported upload file field: "+key)
			return uploadGenerateRequest{}, empty, false
		}
	}
	mediaFiles := r.MultipartForm.File["media"]
	if len(mediaFiles) != 1 {
		writeError(w, http.StatusBadRequest, "exactly one media file is required")
		return uploadGenerateRequest{}, empty, false
	}
	if mediaFiles[0].Size > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "uploaded media must not exceed 20 MiB")
		return uploadGenerateRequest{}, empty, false
	}
	file, err := mediaFiles[0].Open()
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read uploaded media")
		return uploadGenerateRequest{}, empty, false
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		writeError(w, http.StatusBadRequest, "could not read uploaded media")
		return uploadGenerateRequest{}, empty, false
	}
	if len(data) > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "uploaded media must not exceed 20 MiB")
		return uploadGenerateRequest{}, empty, false
	}

	values := r.MultipartForm.Value
	caption := strings.TrimSpace(uploadValue(values, "caption", ""))
	if len([]rune(caption)) > 42 {
		writeError(w, http.StatusBadRequest, "caption must not exceed 42 characters")
		return uploadGenerateRequest{}, empty, false
	}
	width, err := uploadInt(values, "width", 480)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	height, err := uploadInt(values, "height", 480)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	frames, err := uploadInt(values, "frames", 18)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	delayMS, err := uploadInt(values, "delay_ms", 70)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	seed, err := uploadInt64(values, "seed", 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	cropX, err := uploadFloat(values, "crop_x", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	cropY, err := uploadFloat(values, "crop_y", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	zoom, err := uploadFloat(values, "zoom", 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	loop, err := uploadBool(values, "loop", true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	trimStartMS, err := uploadInt(values, "trim_start_ms", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	trimEndMS, err := uploadInt(values, "trim_end_ms", 3000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	if trimStartMS < 0 || trimStartMS > maxVideoStartMS || trimEndMS <= trimStartMS || trimEndMS-trimStartMS > maxVideoDurationMS {
		writeError(w, http.StatusBadRequest, "trim range must be positive, start within 5 minutes, and no longer than 15 seconds")
		return uploadGenerateRequest{}, empty, false
	}
	maxBytes, err := uploadInt(values, "max_bytes", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return uploadGenerateRequest{}, empty, false
	}
	if maxBytes != 0 && (maxBytes < minTargetGIFBytes || maxBytes > maxTargetGIFBytes) {
		writeError(w, http.StatusBadRequest, "max_bytes must be between 256 KiB and 20 MiB")
		return uploadGenerateRequest{}, empty, false
	}
	return uploadGenerateRequest{
		Caption: caption, Width: width, Height: height, Frames: frames, DelayMS: delayMS,
		Motion: uploadValue(values, "motion", "pulse"), Seed: seed,
		CropX: cropX, CropY: cropY, Zoom: zoom,
		CaptionPosition: uploadValue(values, "caption_position", "bottom"), Loop: loop,
		TrimStartMS: trimStartMS, TrimEndMS: trimEndMS, MaxBytes: maxBytes,
	}, uploadedMedia{Data: data, Filename: mediaFiles[0].Filename}, true
}

func uploadValue(values map[string][]string, key, fallback string) string {
	if entries := values[key]; len(entries) == 1 {
		return entries[0]
	}
	return fallback
}

func uploadInt(values map[string][]string, key string, fallback int) (int, error) {
	raw := uploadValue(values, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", key)
	}
	return value, nil
}

func uploadInt64(values map[string][]string, key string, fallback int64) (int64, error) {
	raw := uploadValue(values, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", key)
	}
	return value, nil
}

func uploadFloat(values map[string][]string, key string, fallback float64) (float64, error) {
	raw := uploadValue(values, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", key)
	}
	return value, nil
}

func uploadBool(values map[string][]string, key string, fallback bool) (bool, error) {
	raw := uploadValue(values, key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return value, nil
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data: https://*.giphy.com https://media0.giphy.com https://media1.giphy.com https://media2.giphy.com https://media3.giphy.com https://media4.giphy.com https://i.giphy.com https://upload.wikimedia.org https://blob.gifcities.org https://archive.org https://images-assets.nasa.gov; media-src 'self' blob: https://*.giphy.com https://archive.org https://*.archive.org https://images-assets.nasa.gov; connect-src 'self' blob: https://api.giphy.com https://www.gstatic.com; style-src 'self'; script-src 'self' https://ajax.googleapis.com https://www.gstatic.com 'wasm-unsafe-eval'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
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
