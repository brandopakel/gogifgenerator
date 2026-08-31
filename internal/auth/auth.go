// Package auth implements GoGIF's server-side browser sessions and OIDC flow.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
)

const (
	ModeDisabled = "disabled"
	ModeLocal    = "local"
	ModeOIDC     = "oidc"
)

type Provider interface {
	AuthorizationURL(state, nonce string) string
	Exchange(context.Context, string, string) (account.Identity, error)
}

type Options struct {
	Mode          string
	SessionSecret string
	PublicURL     string
	Repository    *account.Repository
	Provider      Provider
	LocalEmail    string
	Now           func() time.Time
}

type Manager struct {
	mode       string
	secret     []byte
	secure     bool
	repository *account.Repository
	provider   Provider
	local      account.Principal
	now        func() time.Time
}

type sessionPayload struct {
	UserID    string `json:"uid"`
	ExpiresAt int64  `json:"exp"`
}

type guestPayload struct {
	ID        string `json:"id"`
	ExpiresAt int64  `json:"exp"`
}

type flowPayload struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"exp"`
}

func New(options Options) (*Manager, error) {
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	if mode == "" {
		mode = ModeDisabled
	}
	manager := &Manager{mode: mode, repository: options.Repository, provider: options.Provider, now: options.Now}
	if manager.now == nil {
		manager.now = time.Now
	}
	if mode == ModeDisabled {
		return manager, nil
	}
	if len(options.SessionSecret) < 32 {
		return nil, errors.New("GOGIF_SESSION_SECRET must contain at least 32 characters")
	}
	manager.secret = []byte(options.SessionSecret)
	parsed, err := url.Parse(options.PublicURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("GOGIF_PUBLIC_URL must be an absolute URL when accounts are enabled")
	}
	manager.secure = parsed.Scheme == "https"
	switch mode {
	case ModeLocal:
		email := strings.ToLower(strings.TrimSpace(options.LocalEmail))
		if email == "" {
			return nil, errors.New("GOGIF_LOCAL_OWNER_EMAIL is required for local auth")
		}
		manager.local = account.Principal{ID: "usr_local", UserID: "usr_local", Email: email, Name: "Local owner", Role: "admin", PlanID: account.PlanLegacy, Authenticated: true, Legacy: true}
	case ModeOIDC:
		if options.Repository == nil || options.Provider == nil {
			return nil, errors.New("OIDC auth requires an account repository and provider")
		}
	default:
		return nil, errors.New("GOGIF_AUTH_MODE must be disabled, local, or oidc")
	}
	return manager, nil
}

func (m *Manager) Enabled() bool { return m != nil && m.mode != ModeDisabled }

func (m *Manager) Mode() string {
	if m == nil {
		return ModeDisabled
	}
	return m.mode
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := m.resolve(w, r)
		next.ServeHTTP(w, r.WithContext(account.WithPrincipal(r.Context(), principal)))
	})
}

func (m *Manager) resolve(w http.ResponseWriter, r *http.Request) account.Principal {
	if m == nil || m.mode == ModeDisabled {
		return account.Principal{ID: "legacy", PlanID: account.PlanLegacy, Legacy: true}
	}
	if m.mode == ModeLocal {
		return m.local
	}
	if cookie, err := r.Cookie("gogif_session"); err == nil {
		var payload sessionPayload
		if m.decode(cookie.Value, &payload) && payload.UserID != "" && m.now().Unix() < payload.ExpiresAt {
			if user, err := m.repository.Get(r.Context(), payload.UserID); err == nil {
				return account.Principal{ID: user.ID, UserID: user.ID, Email: user.Email, Name: user.Name, Role: user.Role, PlanID: user.PlanID, Authenticated: true}
			}
		}
	}
	guestID := ""
	if cookie, err := r.Cookie("gogif_guest"); err == nil {
		var payload guestPayload
		if m.decode(cookie.Value, &payload) && m.now().Unix() < payload.ExpiresAt {
			guestID = payload.ID
		}
	}
	if guestID == "" {
		guestID, _ = randomToken("guest_", 16)
		expires := m.now().Add(365 * 24 * time.Hour)
		m.setCookie(w, "gogif_guest", m.encode(guestPayload{ID: guestID, ExpiresAt: expires.Unix()}), expires, true)
	}
	return account.Principal{ID: guestID, PlanID: account.PlanGuest}
}

func (m *Manager) Me(w http.ResponseWriter, r *http.Request) {
	principal := account.PrincipalFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"account": principal, "auth_mode": m.Mode()})
}

func (m *Manager) Login(w http.ResponseWriter, r *http.Request) {
	if m == nil || m.mode != ModeOIDC {
		writeError(w, http.StatusServiceUnavailable, "Sign in is not configured yet.")
		return
	}
	state, err := randomToken("", 24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not begin sign in.")
		return
	}
	nonce, err := randomToken("", 24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not begin sign in.")
		return
	}
	expires := m.now().Add(10 * time.Minute)
	m.setCookie(w, "gogif_login", m.encode(flowPayload{State: state, Nonce: nonce, ExpiresAt: expires.Unix()}), expires, true)
	http.Redirect(w, r, m.provider.AuthorizationURL(state, nonce), http.StatusFound)
}

func (m *Manager) Callback(w http.ResponseWriter, r *http.Request) {
	if m == nil || m.mode != ModeOIDC {
		writeError(w, http.StatusServiceUnavailable, "Sign in is not configured yet.")
		return
	}
	cookie, err := r.Cookie("gogif_login")
	if err != nil {
		writeError(w, http.StatusBadRequest, "The sign-in request expired. Please try again.")
		return
	}
	var flow flowPayload
	if !m.decode(cookie.Value, &flow) || m.now().Unix() >= flow.ExpiresAt || !hmac.Equal([]byte(flow.State), []byte(r.URL.Query().Get("state"))) {
		writeError(w, http.StatusBadRequest, "The sign-in request could not be verified.")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "The identity provider did not return an authorization code.")
		return
	}
	identity, err := m.provider.Exchange(r.Context(), code, flow.Nonce)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The identity provider could not complete sign in.")
		return
	}
	user, err := m.repository.UpsertIdentity(r.Context(), identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The account could not be saved.")
		return
	}
	expires := m.now().Add(30 * 24 * time.Hour)
	m.setCookie(w, "gogif_session", m.encode(sessionPayload{UserID: user.ID, ExpiresAt: expires.Unix()}), expires, true)
	m.clearCookie(w, "gogif_login")
	http.Redirect(w, r, "/?signed_in=1", http.StatusFound)
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	m.clearCookie(w, "gogif_session")
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) RequireAccount(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !account.PrincipalFrom(r.Context()).Authenticated {
			writeError(w, http.StatusUnauthorized, "Sign in to use your private library.")
			return
		}
		next(w, r)
	}
}

func (m *Manager) SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && r.URL.Path != "/api/v1/billing/webhook" {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				parsed, err := url.Parse(origin)
				expectedScheme := "http"
				if m.secure {
					expectedScheme = "https"
				}
				if err != nil || !strings.EqualFold(parsed.Scheme, expectedScheme) || !strings.EqualFold(parsed.Host, r.Host) {
					writeError(w, http.StatusForbidden, "Cross-origin requests are not allowed.")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) encode(value any) string {
	payload, _ := json.Marshal(value)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (m *Manager) decode(value string, target any) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	return err == nil && json.Unmarshal(payload, target) == nil
}

func (m *Manager) setCookie(w http.ResponseWriter, name, value string, expires time.Time, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, MaxAge: int(expires.Sub(m.now()).Seconds()), HttpOnly: httpOnly, Secure: m.secure, SameSite: http.SameSiteLaxMode})
}

func (m *Manager) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode})
}

func randomToken(prefix string, size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"status": status, "message": message}})
}
