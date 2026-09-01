package scene

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

const (
	projectPrefix = "scene:project:v1:"
	jobPrefix     = "scene:job:v1:"
	ownerPrefix   = "scene:owner:v1:"
	queueKey      = "scene:queue:v1"
)

type Options struct {
	AllowedTargets []EngineTarget
	Now            func() time.Time
}

type Repository struct {
	kv             store.KV
	allowedTargets []EngineTarget
	now            func() time.Time
	mu             sync.Mutex
}

func NewRepository(kv store.KV, options Options) (*Repository, error) {
	if kv == nil {
		return nil, errors.New("scene: KV is required")
	}
	targets := slices.Compact(append([]EngineTarget(nil), options.AllowedTargets...))
	if len(targets) == 0 {
		targets = []EngineTarget{TargetUnreal}
	}
	for _, target := range targets {
		if target != TargetUnity && target != TargetUnreal {
			return nil, fmt.Errorf("scene: unsupported target %q", target)
		}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Repository{kv: kv, allowedTargets: targets, now: now}, nil
}

func (r *Repository) AllowedTargets() []EngineTarget {
	return append([]EngineTarget(nil), r.allowedTargets...)
}

func (r *Repository) LeasedProject(ctx context.Context, jobID, workerID, leaseToken string) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, err := r.getJob(ctx, jobID)
	if err != nil {
		return Project{}, ErrNotFound
	}
	if !validLease(job, workerID, leaseToken, r.now().UTC()) {
		return Project{}, ErrLeaseLost
	}
	project, err := r.getProject(ctx, job.ProjectID)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (r *Repository) Create(ctx context.Context, ownerID string, request CreateRequest) (Project, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return Project{}, fmt.Errorf("%w: owner is required", ErrInvalid)
	}
	request, err := request.Normalize(r.allowedTargets)
	if err != nil {
		return Project{}, err
	}
	projectID, err := randomID("scn_", 12)
	if err != nil {
		return Project{}, err
	}
	jobID, err := randomID("job_", 12)
	if err != nil {
		return Project{}, err
	}
	now := r.now().UTC()
	project := Project{
		ID: projectID, OwnerID: ownerID, JobID: jobID, Name: request.Name, Prompt: request.Prompt,
		Target: request.Target, Format: request.Format, Width: request.Width, Height: request.Height,
		FPS: request.FPS, DurationMS: request.DurationMS, Seed: request.Seed, State: StateQueued,
		Stage: "queued", CreatedAt: now, UpdatedAt: now,
	}
	job := Job{
		ID: jobID, ProjectID: projectID, OwnerID: ownerID, Target: request.Target,
		State: StateQueued, MaxAttempts: MaxAttempts, Stage: "queued", CreatedAt: now, UpdatedAt: now,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.putProject(ctx, project); err != nil {
		return Project{}, err
	}
	if err := r.putJob(ctx, job); err != nil {
		_ = r.kv.Delete(ctx, projectPrefix+project.ID)
		return Project{}, err
	}
	owners, err := r.readIDs(ctx, ownerPrefix+ownerID)
	if err != nil {
		return Project{}, err
	}
	owners = append(owners, project.ID)
	if err := r.writeIDs(ctx, ownerPrefix+ownerID, owners); err != nil {
		return Project{}, err
	}
	queue, err := r.readIDs(ctx, queueKey)
	if err != nil {
		return Project{}, err
	}
	queue = append(queue, job.ID)
	if err := r.writeIDs(ctx, queueKey, queue); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (r *Repository) GetProject(ctx context.Context, ownerID, id string) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	project, err := r.getProject(ctx, id)
	if err != nil || project.OwnerID != ownerID {
		return Project{}, ErrNotFound
	}
	return project, nil
}

func (r *Repository) ListProjects(ctx context.Context, ownerID string, limit int) ([]Project, error) {
	if limit < 1 || limit > 100 {
		limit = 24
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ids, err := r.readIDs(ctx, ownerPrefix+ownerID)
	if err != nil {
		return nil, err
	}
	projects := make([]Project, 0, min(limit, len(ids)))
	for index := len(ids) - 1; index >= 0 && len(projects) < limit; index-- {
		project, getErr := r.getProject(ctx, ids[index])
		if errors.Is(getErr, store.ErrNotFound) {
			continue
		}
		if getErr != nil {
			return nil, getErr
		}
		if project.OwnerID == ownerID {
			projects = append(projects, project)
		}
	}
	return projects, nil
}

func (r *Repository) Cancel(ctx context.Context, ownerID, projectID string) (Project, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	project, err := r.getProject(ctx, projectID)
	if err != nil || project.OwnerID != ownerID {
		return Project{}, false, ErrNotFound
	}
	if project.State.Terminal() {
		return project, false, nil
	}
	job, err := r.getJob(ctx, project.JobID)
	if err != nil {
		return Project{}, false, err
	}
	now := r.now().UTC()
	job.CancelRequested = true
	project.CancelRequested = true
	job.UpdatedAt, project.UpdatedAt = now, now
	if job.State == StateQueued {
		job.State, job.Stage = StateCanceled, "canceled"
		project.State, project.Stage = StateCanceled, "canceled"
		if err := r.removeQueued(ctx, job.ID); err != nil {
			return Project{}, false, err
		}
	}
	if err := r.putJob(ctx, job); err != nil {
		return Project{}, false, err
	}
	if err := r.putProject(ctx, project); err != nil {
		return Project{}, false, err
	}
	return project, true, nil
}

func (r *Repository) Claim(ctx context.Context, workerID string, targets []EngineTarget, lease time.Duration) (Claim, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || len(workerID) > 120 {
		return Claim{}, fmt.Errorf("%w: worker_id is required", ErrInvalid)
	}
	if lease < 30*time.Second || lease > 15*time.Minute {
		lease = 2 * time.Minute
	}
	if len(targets) == 0 {
		targets = r.allowedTargets
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	queue, err := r.readIDs(ctx, queueKey)
	if err != nil {
		return Claim{}, err
	}
	now := r.now().UTC()
	for _, id := range queue {
		job, getErr := r.getJob(ctx, id)
		if errors.Is(getErr, store.ErrNotFound) {
			continue
		}
		if getErr != nil {
			return Claim{}, getErr
		}
		if (job.State == StateLeased || job.State == StateRunning) && !job.LeaseExpiresAt.After(now) {
			if job.Attempt >= job.MaxAttempts {
				job.State, job.Stage, job.Error = StateFailed, "failed", "worker lease expired"
				job.LeaseToken, job.WorkerID = "", ""
				job.LeaseExpiresAt, job.UpdatedAt = time.Time{}, now
				if err := r.putJob(ctx, job); err != nil {
					return Claim{}, err
				}
				if err := r.syncProject(ctx, job, nil); err != nil {
					return Claim{}, err
				}
				continue
			}
			job.State, job.Stage = StateQueued, "queued"
			job.LeaseToken, job.WorkerID = "", ""
		}
		if job.State != StateQueued || job.CancelRequested || !slices.Contains(targets, job.Target) {
			continue
		}
		token, tokenErr := randomID("lease_", 24)
		if tokenErr != nil {
			return Claim{}, tokenErr
		}
		job.State, job.Stage = StateLeased, "preparing"
		job.Attempt++
		job.WorkerID, job.LeaseToken = workerID, token
		job.LeaseExpiresAt, job.UpdatedAt = now.Add(lease), now
		if err := r.putJob(ctx, job); err != nil {
			return Claim{}, err
		}
		if err := r.syncProject(ctx, job, nil); err != nil {
			return Claim{}, err
		}
		project, err := r.getProject(ctx, job.ProjectID)
		if err != nil {
			return Claim{}, err
		}
		return Claim{Job: job, Project: project}, nil
	}
	return Claim{}, ErrNoJob
}

func (r *Repository) Heartbeat(ctx context.Context, jobID, workerID, leaseToken, stage string, progress int, lease time.Duration) (Job, error) {
	if lease < 30*time.Second || lease > 15*time.Minute {
		lease = 2 * time.Minute
	}
	stage = strings.TrimSpace(stage)
	if stage == "" || len(stage) > 80 || progress < 0 || progress > 99 {
		return Job{}, fmt.Errorf("%w: stage and progress from 0 through 99 are required", ErrInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, err := r.getJob(ctx, jobID)
	if err != nil {
		return Job{}, ErrNotFound
	}
	now := r.now().UTC()
	if !validLease(job, workerID, leaseToken, now) {
		return Job{}, ErrLeaseLost
	}
	job.State, job.Stage, job.Progress = StateRunning, stage, progress
	job.LeaseExpiresAt, job.UpdatedAt = now.Add(lease), now
	if err := r.putJob(ctx, job); err != nil {
		return Job{}, err
	}
	if err := r.syncProject(ctx, job, nil); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (r *Repository) Finish(ctx context.Context, jobID, workerID, leaseToken string, result FinishRequest) (Project, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, err := r.getJob(ctx, jobID)
	if err != nil {
		return Project{}, false, ErrNotFound
	}
	now := r.now().UTC()
	if !validLease(job, workerID, leaseToken, now) {
		return Project{}, false, ErrLeaseLost
	}
	project, err := r.getProject(ctx, job.ProjectID)
	if err != nil {
		return Project{}, false, err
	}
	if len(result.Artifacts) > MaxArtifacts {
		return Project{}, false, fmt.Errorf("%w: at most %d artifacts are allowed", ErrInvalid, MaxArtifacts)
	}
	for _, artifact := range result.Artifacts {
		if err := artifact.Validate(project.ID); err != nil {
			return Project{}, false, err
		}
	}
	terminal := true
	job.LeaseToken, job.WorkerID = "", ""
	job.LeaseExpiresAt = time.Time{}
	job.UpdatedAt = now
	switch {
	case job.CancelRequested:
		job.State, job.Stage, job.Error = StateCanceled, "canceled", ""
	case result.Success:
		if !hasMasterArtifact(result.Artifacts, project.Format) {
			return Project{}, false, fmt.Errorf("%w: a successful scene job requires a matching MP4 or WebM master", ErrInvalid)
		}
		job.State, job.Stage, job.Progress, job.Error = StateSucceeded, "complete", 100, ""
	case result.Retryable && job.Attempt < job.MaxAttempts:
		terminal = false
		job.State, job.Stage, job.Progress = StateQueued, "queued", 0
		job.Error = cleanError(result.Error)
	default:
		job.State, job.Stage, job.Error = StateFailed, "failed", cleanError(result.Error)
		if job.Error == "" {
			job.Error = "scene worker failed"
		}
	}
	if err := r.putJob(ctx, job); err != nil {
		return Project{}, false, err
	}
	if err := r.syncProject(ctx, job, result.Artifacts); err != nil {
		return Project{}, false, err
	}
	if terminal {
		if err := r.removeQueued(ctx, job.ID); err != nil {
			return Project{}, false, err
		}
	}
	project, err = r.getProject(ctx, job.ProjectID)
	return project, terminal, err
}

func hasMasterArtifact(artifacts []Artifact, format MasterFormat) bool {
	wanted := "video/" + string(format)
	for _, artifact := range artifacts {
		if artifact.Kind == "video" && artifact.ContentType == wanted {
			return true
		}
	}
	return false
}

func validLease(job Job, workerID, leaseToken string, now time.Time) bool {
	return (job.State == StateLeased || job.State == StateRunning) && job.WorkerID == workerID && job.LeaseToken != "" && job.LeaseToken == leaseToken && job.LeaseExpiresAt.After(now)
}

func (r *Repository) syncProject(ctx context.Context, job Job, artifacts []Artifact) error {
	project, err := r.getProject(ctx, job.ProjectID)
	if err != nil {
		return err
	}
	project.State, project.Stage, project.Progress = job.State, job.Stage, job.Progress
	project.Error, project.CancelRequested = job.Error, job.CancelRequested
	project.UpdatedAt = job.UpdatedAt
	if len(artifacts) > 0 {
		project.Artifacts = append([]Artifact(nil), artifacts...)
	}
	return r.putProject(ctx, project)
}

func (r *Repository) putProject(ctx context.Context, project Project) error {
	data, err := json.Marshal(project)
	if err != nil {
		return err
	}
	return r.kv.Put(ctx, projectPrefix+project.ID, data, 0)
}

func (r *Repository) getProject(ctx context.Context, id string) (Project, error) {
	if !validID(id, "scn_") {
		return Project{}, ErrNotFound
	}
	data, err := r.kv.Get(ctx, projectPrefix+id)
	if err != nil {
		return Project{}, err
	}
	var project Project
	if err := json.Unmarshal(data, &project); err != nil {
		return Project{}, fmt.Errorf("decode scene project: %w", err)
	}
	return project, nil
}

func (r *Repository) putJob(ctx context.Context, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return r.kv.Put(ctx, jobPrefix+job.ID, data, 0)
}

func (r *Repository) getJob(ctx context.Context, id string) (Job, error) {
	if !validID(id, "job_") {
		return Job{}, ErrNotFound
	}
	data, err := r.kv.Get(ctx, jobPrefix+id)
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, fmt.Errorf("decode scene job: %w", err)
	}
	return job, nil
}

func (r *Repository) readIDs(ctx context.Context, key string) ([]string, error) {
	data, err := r.kv.Get(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("decode scene index: %w", err)
	}
	return ids, nil
}

func (r *Repository) writeIDs(ctx context.Context, key string, ids []string) error {
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return r.kv.Put(ctx, key, data, 0)
}

func (r *Repository) removeQueued(ctx context.Context, jobID string) error {
	queue, err := r.readIDs(ctx, queueKey)
	if err != nil {
		return err
	}
	return r.writeIDs(ctx, queueKey, slices.DeleteFunc(queue, func(id string) bool { return id == jobID }))
}

func validID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) || len(value) > 80 {
		return false
	}
	for _, char := range strings.TrimPrefix(value, prefix) {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func randomID(prefix string, bytes int) (string, error) {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func cleanError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}
