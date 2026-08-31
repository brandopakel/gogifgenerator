package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/store"
)

type fakeProvider struct {
	identity account.Identity
}

func (p fakeProvider) AuthorizationURL(state, nonce string) string {
	return "https://identity.example/authorize?state=" + state + "&nonce=" + nonce
}

func (p fakeProvider) Exchange(_ context.Context, code, _ string) (account.Identity, error) {
	if code != "good-code" {
		return account.Identity{}, context.Canceled
	}
	return p.identity, nil
}

func TestDisabledModePreservesSelfHostedLegacyAccess(t *testing.T) {
	manager, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got account.Principal
	handler := manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = account.PrincipalFrom(r.Context()) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !got.Legacy || got.PlanID != account.PlanLegacy {
		t.Fatalf("principal = %#v", got)
	}
}

func TestOIDCFlowCreatesSessionAndResolvesUser(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	repository := account.NewRepository(store.NewMemoryKV())
	manager, err := New(Options{
		Mode: ModeOIDC, SessionSecret: strings.Repeat("s", 32), PublicURL: "https://app.example",
		Repository: repository, Provider: fakeProvider{identity: account.Identity{
			Issuer: "https://identity.example", Subject: "subject", Email: "person@example.com", EmailVerified: true, Name: "Person",
		}}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	loginResponse := httptest.NewRecorder()
	manager.Login(loginResponse, httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil))
	if loginResponse.Code != http.StatusFound || len(loginResponse.Result().Cookies()) == 0 {
		t.Fatalf("login = %d, %#v", loginResponse.Code, loginResponse.Header())
	}
	flowCookie := loginResponse.Result().Cookies()[0]
	var flow flowPayload
	if !manager.decode(flowCookie.Value, &flow) {
		t.Fatal("could not decode login flow")
	}
	callback := httptest.NewRequest(http.MethodGet, "/api/v1/auth/callback?state="+flow.State+"&code=good-code", nil)
	callback.AddCookie(flowCookie)
	callbackResponse := httptest.NewRecorder()
	manager.Callback(callbackResponse, callback)
	if callbackResponse.Code != http.StatusFound {
		t.Fatalf("callback = %d, %s", callbackResponse.Code, callbackResponse.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == "gogif_session" {
			session = cookie
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure {
		t.Fatalf("session cookie = %#v", session)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	request.AddCookie(session)
	var principal account.Principal
	manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { principal = account.PrincipalFrom(r.Context()) })).ServeHTTP(httptest.NewRecorder(), request)
	if !principal.Authenticated || principal.Email != "person@example.com" || principal.PlanID != account.PlanFree {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestSameOriginRejectsCrossSiteWrites(t *testing.T) {
	manager, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://app.example/api/v1/gifs/generate", nil)
	request.Host = "app.example"
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	manager.SameOrigin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler ran") })).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}
