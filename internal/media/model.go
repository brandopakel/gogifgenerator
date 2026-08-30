// Package media defines provider-neutral asset records and their provenance.
package media

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Kind string

const (
	KindGIF   Kind = "gif"
	KindClip  Kind = "clip"
	KindVideo Kind = "video"
	KindImage Kind = "image"
)

type State string

const (
	StateProcessing State = "processing"
	StateReady      State = "ready"
	StateBlocked    State = "blocked"
	StateDeleted    State = "deleted"
)

type StorageMode string

const (
	StorageManaged  StorageMode = "managed"
	StorageExternal StorageMode = "external"
)

// Permission is intentionally three-valued. Unknown rights must never silently
// become permission to use an asset commercially or to create a derivative.
type Permission string

const (
	PermissionAllowed    Permission = "allowed"
	PermissionProhibited Permission = "prohibited"
	PermissionUnknown    Permission = "unknown"
)

type Asset struct {
	ID          string       `json:"id"`
	OwnerID     string       `json:"owner_id,omitempty"`
	Kind        Kind         `json:"kind"`
	State       State        `json:"state"`
	Title       string       `json:"title,omitempty"`
	Prompt      string       `json:"prompt,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Renditions  []Rendition  `json:"renditions,omitempty"`
	Provenance  Provenance   `json:"provenance"`
	Rights      Rights       `json:"rights"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	DeletedAt   *time.Time   `json:"deleted_at,omitempty"`
	Moderation  Moderation   `json:"moderation"`
	Fingerprint *Fingerprint `json:"fingerprint,omitempty"`
}

type Rendition struct {
	Name        string      `json:"name"`
	Format      string      `json:"format"`
	ContentType string      `json:"content_type"`
	Storage     StorageMode `json:"storage"`
	BlobKey     string      `json:"blob_key,omitempty"`
	ExternalURL string      `json:"external_url,omitempty"`
	Width       int         `json:"width,omitempty"`
	Height      int         `json:"height,omitempty"`
	DurationMS  int64       `json:"duration_ms,omitempty"`
	SizeBytes   int64       `json:"size_bytes,omitempty"`
}

type Provenance struct {
	Provider     string     `json:"provider"`
	ExternalID   string     `json:"external_id,omitempty"`
	Generator    string     `json:"generator,omitempty"`
	SourceURL    string     `json:"source_url,omitempty"`
	Author       string     `json:"author,omitempty"`
	ImportedAt   *time.Time `json:"imported_at,omitempty"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
	RevalidateAt *time.Time `json:"revalidate_at,omitempty"`
}

type Rights struct {
	Status        string     `json:"status"`
	LicenseID     string     `json:"license_id,omitempty"`
	LicenseURL    string     `json:"license_url,omitempty"`
	Attribution   string     `json:"attribution,omitempty"`
	CommercialUse Permission `json:"commercial_use"`
	Derivatives   Permission `json:"derivatives"`
	ShareAlike    bool       `json:"share_alike,omitempty"`
}

type Moderation struct {
	Rating     string     `json:"rating,omitempty"`
	Status     string     `json:"status,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
}

type Fingerprint struct {
	SHA256 string `json:"sha256,omitempty"`
	PHash  string `json:"phash,omitempty"`
}

func (a Asset) Validate() error {
	if !validID(a.ID) {
		return errors.New("asset ID must contain 1-128 letters, numbers, underscores, or hyphens")
	}
	if !oneOf(string(a.Kind), string(KindGIF), string(KindClip), string(KindVideo), string(KindImage)) {
		return fmt.Errorf("unsupported asset kind %q", a.Kind)
	}
	if !oneOf(string(a.State), string(StateProcessing), string(StateReady), string(StateBlocked), string(StateDeleted)) {
		return fmt.Errorf("unsupported asset state %q", a.State)
	}
	if strings.TrimSpace(a.Provenance.Provider) == "" {
		return errors.New("asset provenance provider is required")
	}
	if strings.TrimSpace(a.Rights.Status) == "" {
		return errors.New("asset rights status is required")
	}
	if !validPermission(a.Rights.CommercialUse) || !validPermission(a.Rights.Derivatives) {
		return errors.New("rights permissions must be allowed, prohibited, or unknown")
	}
	if err := optionalHTTPURL(a.Provenance.SourceURL); err != nil {
		return fmt.Errorf("source URL: %w", err)
	}
	if err := optionalHTTPURL(a.Rights.LicenseURL); err != nil {
		return fmt.Errorf("license URL: %w", err)
	}
	for index, rendition := range a.Renditions {
		if err := rendition.Validate(); err != nil {
			return fmt.Errorf("rendition %d: %w", index, err)
		}
	}
	if a.State == StateReady && len(a.Renditions) == 0 {
		return errors.New("ready asset must have at least one rendition")
	}
	return nil
}

func (r Rendition) Validate() error {
	if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.ContentType) == "" {
		return errors.New("name and content type are required")
	}
	switch r.Storage {
	case StorageManaged:
		if r.BlobKey == "" || r.ExternalURL != "" {
			return errors.New("managed rendition requires only a blob key")
		}
	case StorageExternal:
		if r.ExternalURL == "" || r.BlobKey != "" {
			return errors.New("external rendition requires only an external URL")
		}
		if err := optionalHTTPURL(r.ExternalURL); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported storage mode %q", r.Storage)
	}
	if r.Width < 0 || r.Height < 0 || r.DurationMS < 0 || r.SizeBytes < 0 {
		return errors.New("dimensions, duration, and size cannot be negative")
	}
	return nil
}

func validID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

func validPermission(value Permission) bool {
	return oneOf(string(value), string(PermissionAllowed), string(PermissionProhibited), string(PermissionUnknown))
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func optionalHTTPURL(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("must be an absolute HTTP(S) URL")
	}
	return nil
}
