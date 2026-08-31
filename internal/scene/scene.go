// Package scene defines persistent, hosting-neutral scene projects and the
// lease-based jobs consumed by remote Blender/render workers.
package scene

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	MaxAttempts  = 3
	MaxArtifacts = 16
)

var (
	ErrInvalid   = errors.New("scene: invalid request")
	ErrNotFound  = errors.New("scene: not found")
	ErrLeaseLost = errors.New("scene: worker lease was lost")
	ErrNoJob     = errors.New("scene: no compatible job is queued")
)

type EngineTarget string

const (
	TargetUnity  EngineTarget = "unity"
	TargetUnreal EngineTarget = "unreal"
)

type MasterFormat string

const (
	FormatMP4  MasterFormat = "mp4"
	FormatWebM MasterFormat = "webm"
)

type State string

const (
	StateQueued    State = "queued"
	StateLeased    State = "leased"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

func (s State) Terminal() bool {
	return s == StateSucceeded || s == StateFailed || s == StateCanceled
}

type CreateRequest struct {
	Name       string       `json:"name"`
	Prompt     string       `json:"prompt"`
	Target     EngineTarget `json:"engine_target"`
	Format     MasterFormat `json:"master_format"`
	Width      int          `json:"width"`
	Height     int          `json:"height"`
	FPS        int          `json:"fps"`
	DurationMS int          `json:"duration_ms"`
	Seed       int64        `json:"seed"`
}

func (r CreateRequest) Normalize(allowedTargets []EngineTarget) (CreateRequest, error) {
	r.Name = strings.TrimSpace(r.Name)
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Prompt == "" || len(r.Prompt) > 2000 {
		return CreateRequest{}, fmt.Errorf("%w: prompt must contain between 1 and 2000 characters", ErrInvalid)
	}
	if r.Name == "" {
		r.Name = r.Prompt
		if len(r.Name) > 80 {
			r.Name = strings.TrimSpace(r.Name[:80])
		}
	}
	if len(r.Name) > 120 {
		return CreateRequest{}, fmt.Errorf("%w: name must not exceed 120 characters", ErrInvalid)
	}
	if r.Target == "" {
		r.Target = TargetUnreal
	}
	if !slices.Contains(allowedTargets, r.Target) {
		return CreateRequest{}, fmt.Errorf("%w: engine_target %q is not enabled", ErrInvalid, r.Target)
	}
	if r.Format == "" {
		r.Format = FormatMP4
	}
	if r.Format != FormatMP4 && r.Format != FormatWebM {
		return CreateRequest{}, fmt.Errorf("%w: master_format must be mp4 or webm", ErrInvalid)
	}
	if r.Width == 0 {
		r.Width = 720
	}
	if r.Height == 0 {
		r.Height = 720
	}
	if r.Width < 256 || r.Width > 3840 || r.Height < 256 || r.Height > 3840 {
		return CreateRequest{}, fmt.Errorf("%w: dimensions must be between 256 and 3840 pixels", ErrInvalid)
	}
	if r.FPS == 0 {
		r.FPS = 24
	}
	if r.FPS < 12 || r.FPS > 60 {
		return CreateRequest{}, fmt.Errorf("%w: fps must be between 12 and 60", ErrInvalid)
	}
	if r.DurationMS == 0 {
		r.DurationMS = 4000
	}
	if r.DurationMS < 1000 || r.DurationMS > 30000 {
		return CreateRequest{}, fmt.Errorf("%w: duration_ms must be between 1000 and 30000", ErrInvalid)
	}
	return r, nil
}

type Artifact struct {
	Kind        string `json:"kind"`
	StorageKey  string `json:"storage_key"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

func (a Artifact) Validate(projectID string) error {
	allowedKinds := []string{"blend", "asset", "scene", "video", "gif", "poster", "log"}
	if !slices.Contains(allowedKinds, a.Kind) {
		return fmt.Errorf("%w: unsupported artifact kind %q", ErrInvalid, a.Kind)
	}
	if !strings.HasPrefix(a.StorageKey, "scenes/"+projectID+"/") || strings.Contains(a.StorageKey, "..") || len(a.StorageKey) > 512 {
		return fmt.Errorf("%w: artifact storage key is outside the project prefix", ErrInvalid)
	}
	if a.ContentType == "" || len(a.ContentType) > 120 || a.SizeBytes < 1 || a.SizeBytes > 10<<30 {
		return fmt.Errorf("%w: artifact metadata is invalid", ErrInvalid)
	}
	if len(a.SHA256) != 64 {
		return fmt.Errorf("%w: artifact sha256 must contain 64 hexadecimal characters", ErrInvalid)
	}
	for _, value := range a.SHA256 {
		if !strings.ContainsRune("0123456789abcdef", value) {
			return fmt.Errorf("%w: artifact sha256 must be lowercase hexadecimal", ErrInvalid)
		}
	}
	return nil
}

type Project struct {
	ID              string       `json:"id"`
	OwnerID         string       `json:"owner_id"`
	JobID           string       `json:"job_id"`
	Name            string       `json:"name"`
	Prompt          string       `json:"prompt"`
	Target          EngineTarget `json:"engine_target"`
	Format          MasterFormat `json:"master_format"`
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	FPS             int          `json:"fps"`
	DurationMS      int          `json:"duration_ms"`
	Seed            int64        `json:"seed"`
	State           State        `json:"state"`
	Progress        int          `json:"progress"`
	Stage           string       `json:"stage,omitempty"`
	Error           string       `json:"error,omitempty"`
	CancelRequested bool         `json:"cancel_requested,omitempty"`
	Artifacts       []Artifact   `json:"artifacts,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Job struct {
	ID              string       `json:"id"`
	ProjectID       string       `json:"project_id"`
	OwnerID         string       `json:"owner_id"`
	Target          EngineTarget `json:"engine_target"`
	State           State        `json:"state"`
	Attempt         int          `json:"attempt"`
	MaxAttempts     int          `json:"max_attempts"`
	WorkerID        string       `json:"worker_id,omitempty"`
	LeaseToken      string       `json:"lease_token,omitempty"`
	LeaseExpiresAt  time.Time    `json:"lease_expires_at,omitempty"`
	Progress        int          `json:"progress"`
	Stage           string       `json:"stage,omitempty"`
	Error           string       `json:"error,omitempty"`
	CancelRequested bool         `json:"cancel_requested,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Claim struct {
	Job     Job     `json:"job"`
	Project Project `json:"project"`
}

type FinishRequest struct {
	Success   bool       `json:"success"`
	Retryable bool       `json:"retryable"`
	Error     string     `json:"error,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}
