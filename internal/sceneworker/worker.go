// Package sceneworker executes leased Scene jobs outside the GoGIF web/API
// process. It is deliberately pull-based so a GPU host needs no inbound port.
package sceneworker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/scene"
)

const Version = "0.1.0"

type LocalArtifact struct {
	Kind        string
	Path        string
	Filename    string
	ContentType string
}

type ProgressFunc func(stage string, progress int)

type Renderer interface {
	Render(context.Context, scene.Project, string, ProgressFunc) ([]LocalArtifact, error)
}

type Options struct {
	API               ControlPlane
	Renderers         map[scene.EngineTarget]Renderer
	Logger            *slog.Logger
	WorkspaceRoot     string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
}

type Worker struct {
	api               ControlPlane
	renderers         map[scene.EngineTarget]Renderer
	logger            *slog.Logger
	workspaceRoot     string
	pollInterval      time.Duration
	heartbeatInterval time.Duration
}

func New(options Options) (*Worker, error) {
	if options.API == nil || len(options.Renderers) == 0 {
		return nil, errors.New("scene worker: API and at least one renderer are required")
	}
	for target, renderer := range options.Renderers {
		if renderer == nil || (target != scene.TargetUnreal && target != scene.TargetUnity) {
			return nil, errors.New("scene worker: renderer target is invalid")
		}
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 5 * time.Second
	}
	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = 30 * time.Second
	}
	return &Worker{
		api: options.API, renderers: options.Renderers, logger: options.Logger, workspaceRoot: options.WorkspaceRoot,
		pollInterval: options.PollInterval, heartbeatInterval: options.HeartbeatInterval,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	for {
		worked, err := w.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			w.logger.Error("Scene worker cycle failed", "error", err)
			worked = false
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if worked {
			continue
		}
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	claim, ok, err := w.api.Claim(ctx)
	if err != nil || !ok {
		return false, err
	}
	renderer := w.renderers[claim.Project.Target]
	if renderer == nil {
		return true, w.fail(ctx, claim, fmt.Errorf("renderer target %q is not configured", claim.Project.Target), false)
	}
	workspace, err := os.MkdirTemp(w.workspaceRoot, "gogif-scene-")
	if err != nil {
		return true, w.fail(ctx, claim, fmt.Errorf("create workspace: %w", err), true)
	}
	defer os.RemoveAll(workspace)
	w.logger.Info("Scene job claimed", "job", claim.Job.ID, "project", claim.Project.ID, "target", claim.Project.Target, "attempt", claim.Job.Attempt)

	renderContext, cancelRender := context.WithCancel(ctx)
	defer cancelRender()
	coordinator := newHeartbeatCoordinator(w.api, claim, w.heartbeatInterval, cancelRender)
	coordinator.progress("preparing", 1)
	coordinator.start(renderContext)

	outputs, renderErr := renderer.Render(renderContext, claim.Project, workspace, coordinator.progress)
	stored := make([]scene.Artifact, 0, len(outputs))
	if renderErr == nil {
		for index, output := range outputs {
			coordinator.progress("uploading", 90+min(8, index))
			artifact, uploadErr := w.api.Upload(renderContext, claim.Job.ID, claim.Job.LeaseToken, output)
			if uploadErr != nil {
				renderErr = fmt.Errorf("upload %s: %w", filepath.Base(output.Path), uploadErr)
				break
			}
			stored = append(stored, artifact)
		}
	}
	status := coordinator.stop()
	if status.err != nil {
		return true, status.err
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	finishContext, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finishCancel()
	if status.cancelRequested {
		if err := w.api.Finish(finishContext, claim.Job.ID, claim.Job.LeaseToken, scene.FinishRequest{}); err != nil {
			return true, err
		}
		w.logger.Info("Scene job canceled", "job", claim.Job.ID)
		return true, nil
	}
	if renderErr != nil {
		if errors.Is(renderErr, ErrLeaseLost) {
			return true, renderErr
		}
		return true, w.fail(finishContext, claim, renderErr, !errors.Is(renderErr, ErrLeaseLost))
	}
	if err := w.api.Finish(finishContext, claim.Job.ID, claim.Job.LeaseToken, scene.FinishRequest{Success: true, Artifacts: stored}); err != nil {
		return true, err
	}
	w.logger.Info("Scene job completed", "job", claim.Job.ID, "artifacts", len(stored))
	return true, nil
}

func (w *Worker) fail(ctx context.Context, claim scene.Claim, jobErr error, retryable bool) error {
	message := jobErr.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	finishErr := w.api.Finish(ctx, claim.Job.ID, claim.Job.LeaseToken, scene.FinishRequest{Retryable: retryable, Error: message})
	if finishErr != nil {
		return errors.Join(jobErr, finishErr)
	}
	return jobErr
}

type heartbeatStatus struct {
	err             error
	cancelRequested bool
}

type heartbeatCoordinator struct {
	api           ControlPlane
	claim         scene.Claim
	interval      time.Duration
	cancel        context.CancelFunc
	stopOnce      sync.Once
	stopCh        chan struct{}
	doneCh        chan heartbeatStatus
	mu            sync.Mutex
	stage         string
	progressValue int
}

func newHeartbeatCoordinator(api ControlPlane, claim scene.Claim, interval time.Duration, cancel context.CancelFunc) *heartbeatCoordinator {
	return &heartbeatCoordinator{api: api, claim: claim, interval: interval, cancel: cancel, stopCh: make(chan struct{}), doneCh: make(chan heartbeatStatus, 1)}
}

func (c *heartbeatCoordinator) progress(stage string, progress int) {
	if progress < 0 || progress > 99 || stage == "" {
		return
	}
	c.mu.Lock()
	c.stage, c.progressValue = stage, progress
	c.mu.Unlock()
}

func (c *heartbeatCoordinator) start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			c.mu.Lock()
			stage, progress := c.stage, c.progressValue
			c.mu.Unlock()
			job, err := c.api.Heartbeat(ctx, c.claim.Job.ID, c.claim.Job.LeaseToken, stage, progress)
			if err != nil {
				c.cancel()
				c.doneCh <- heartbeatStatus{err: err}
				return
			}
			if job.CancelRequested {
				c.cancel()
				c.doneCh <- heartbeatStatus{cancelRequested: true}
				return
			}
			select {
			case <-ctx.Done():
				c.doneCh <- heartbeatStatus{}
				return
			case <-c.stopCh:
				c.doneCh <- heartbeatStatus{}
				return
			case <-ticker.C:
			}
		}
	}()
}

func (c *heartbeatCoordinator) stop() heartbeatStatus {
	c.stopOnce.Do(func() { close(c.stopCh) })
	return <-c.doneCh
}
