// Package provider defines policy-aware search across external media catalogs.
package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/media"
)

var (
	ErrInvalidQuery = errors.New("provider: invalid query")
	ErrUnavailable  = errors.New("provider: unavailable")
)

const (
	DefaultLimit = 12
	MaxLimit     = 24
)

// TransformPolicy says whether a provider item may be used as a generation
// reference. It never grants permission to mirror or permanently host source
// bytes.
type TransformPolicy string

const (
	TransformAllowed   TransformPolicy = "allowed"
	TransformReview    TransformPolicy = "review"
	TransformReference TransformPolicy = "reference-only"
)

type Query struct {
	Text   string `json:"text"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
	Locale string `json:"locale,omitempty"`
}

func (q Query) Normalize() (Query, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" || len(q.Text) > 200 {
		return Query{}, fmt.Errorf("%w: text must contain between 1 and 200 characters", ErrInvalidQuery)
	}
	if q.Limit == 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit < 1 || q.Limit > MaxLimit {
		return Query{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidQuery, MaxLimit)
	}
	if q.Locale == "" {
		q.Locale = "en"
	}
	if len(q.Locale) > 16 {
		return Query{}, fmt.Errorf("%w: locale is too long", ErrInvalidQuery)
	}
	return q, nil
}

type Page struct {
	Provider string   `json:"provider"`
	Results  []Result `json:"results"`
	Cursor   string   `json:"cursor,omitempty"`
}

type Result struct {
	Provider        string           `json:"provider"`
	ExternalID      string           `json:"external_id"`
	Title           string           `json:"title"`
	Description     string           `json:"description,omitempty"`
	Kind            media.Kind       `json:"kind"`
	SourceURL       string           `json:"source_url"`
	PreviewURL      string           `json:"preview_url"`
	OriginalURL     string           `json:"original_url"`
	ContentType     string           `json:"content_type"`
	Width           int              `json:"width,omitempty"`
	Height          int              `json:"height,omitempty"`
	SizeBytes       int64            `json:"size_bytes,omitempty"`
	Author          string           `json:"author,omitempty"`
	Attribution     string           `json:"attribution,omitempty"`
	LicenseID       string           `json:"license_id,omitempty"`
	LicenseName     string           `json:"license_name,omitempty"`
	LicenseURL      string           `json:"license_url,omitempty"`
	Restrictions    []string         `json:"restrictions,omitempty"`
	CommercialUse   media.Permission `json:"commercial_use"`
	Derivatives     media.Permission `json:"derivatives"`
	ShareAlike      bool             `json:"share_alike,omitempty"`
	TransformPolicy TransformPolicy  `json:"transform_policy"`
}

type Descriptor struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Provider interface {
	Descriptor() Descriptor
	Search(context.Context, Query) (Page, error)
}
