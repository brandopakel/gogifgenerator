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
	ErrNotFound     = errors.New("provider: item not found")
	ErrUnsupported  = errors.New("provider: operation unsupported")
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

// HandlingMode records what GoGIF may do with provider-hosted media. Providers
// can allow more than one mode. Displaying a remote rendition is deliberately
// separate from fetching it into a transformation job.
type HandlingMode string

const (
	HandlingLink               HandlingMode = "link"
	HandlingDisplay            HandlingMode = "display"
	HandlingTemporaryTransform HandlingMode = "temporary-transform"
)

// Rendition describes one provider-hosted representation of a result. URL is
// external unless the result represents content created and managed by GoGIF.
type Rendition struct {
	Name        string `json:"name"`
	Format      string `json:"format,omitempty"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	HasAudio    bool   `json:"has_audio,omitempty"`
}

// CaptionTrack is a provider-hosted subtitle or transcript rendition.
type CaptionTrack struct {
	Language string `json:"language"`
	Format   string `json:"format"`
	URL      string `json:"url"`
}

// QuoteMatch identifies the time range matched by a quote-oriented provider.
// Confidence is in the inclusive range 0..1 when the provider supplies it.
type QuoteMatch struct {
	Text       string  `json:"text"`
	StartMS    int64   `json:"start_ms,omitempty"`
	EndMS      int64   `json:"end_ms,omitempty"`
	Exact      bool    `json:"exact,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

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
	ReferenceURL    string           `json:"reference_url,omitempty"`
	ContentType     string           `json:"content_type"`
	Width           int              `json:"width,omitempty"`
	Height          int              `json:"height,omitempty"`
	DurationMS      int64            `json:"duration_ms,omitempty"`
	SizeBytes       int64            `json:"size_bytes,omitempty"`
	HasAudio        bool             `json:"has_audio,omitempty"`
	Renditions      []Rendition      `json:"renditions,omitempty"`
	Captions        []CaptionTrack   `json:"captions,omitempty"`
	QuoteMatch      *QuoteMatch      `json:"quote_match,omitempty"`
	AllowedHandling []HandlingMode   `json:"allowed_handling,omitempty"`
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

// Resolver revalidates a selected search result directly with its provider
// before any source bytes can enter a transformation job.
type Resolver interface {
	Provider
	Resolve(context.Context, string, string) (Result, error)
}

// QuoteResolver searches a selected item's provider-hosted captions and adds
// the best time-aligned quote match to the resolved result.
type QuoteResolver interface {
	Resolver
	ResolveQuote(context.Context, string, string, string) (Result, error)
}
