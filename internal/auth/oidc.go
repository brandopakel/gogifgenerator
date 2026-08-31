package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCOptions struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type OIDCProvider struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func NewOIDCProvider(ctx context.Context, options OIDCOptions) (*OIDCProvider, error) {
	if strings.TrimSpace(options.Issuer) == "" || strings.TrimSpace(options.ClientID) == "" || strings.TrimSpace(options.ClientSecret) == "" || strings.TrimSpace(options.RedirectURL) == "" {
		return nil, errors.New("OIDC issuer, client ID, client secret, and redirect URL are required")
	}
	provider, err := oidc.NewProvider(ctx, options.Issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCProvider{
		oauth: oauth2.Config{
			ClientID: options.ClientID, ClientSecret: options.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: options.RedirectURL,
			Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: options.ClientID}),
	}, nil
}

func (p *OIDCProvider) AuthorizationURL(state, nonce string) string {
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.AccessTypeOnline)
}

func (p *OIDCProvider) Exchange(ctx context.Context, code, nonce string) (account.Identity, error) {
	token, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return account.Identity{}, err
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return account.Identity{}, errors.New("OIDC response omitted id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return account.Identity{}, err
	}
	if idToken.Nonce != nonce {
		return account.Identity{}, errors.New("OIDC nonce did not match")
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return account.Identity{}, err
	}
	return account.Identity{
		Issuer: idToken.Issuer, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified,
		Name: claims.Name, PictureURL: claims.Picture,
	}, nil
}
