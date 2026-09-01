package sceneworker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/scene"
)

func TestWorkerCompletesRenderedArtifacts(t *testing.T) {
	control := &fakeControl{claim: testClaim()}
	renderer := RendererFunc(func(_ context.Context, _ scene.Project, workspace string, progress ProgressFunc) ([]LocalArtifact, error) {
		progress("unreal-render", 60)
		filename := filepath.Join(workspace, "master.mp4")
		if err := os.WriteFile(filename, []byte("video"), 0o600); err != nil {
			return nil, err
		}
		return []LocalArtifact{{Kind: "video", Path: filename, Filename: "master.mp4", ContentType: "video/mp4"}}, nil
	})
	worker, err := New(Options{
		API: control, Renderers: map[scene.EngineTarget]Renderer{scene.TargetUnreal: renderer}, WorkspaceRoot: t.TempDir(), HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("RunOnce() = %v, %v", worked, err)
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.heartbeats == 0 || len(control.finished.Artifacts) != 1 || !control.finished.Success {
		t.Fatalf("control = %#v", control)
	}
}

func TestWorkerCooperativelyCancels(t *testing.T) {
	control := &fakeControl{claim: testClaim(), cancel: true}
	renderer := RendererFunc(func(ctx context.Context, _ scene.Project, _ string, _ ProgressFunc) ([]LocalArtifact, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	worker, err := New(Options{
		API: control, Renderers: map[scene.EngineTarget]Renderer{scene.TargetUnreal: renderer}, WorkspaceRoot: t.TempDir(), HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("RunOnce() = %v, %v", worked, err)
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.finished.Success || control.finished.Retryable || len(control.finished.Artifacts) != 0 {
		t.Fatalf("finish = %#v", control.finished)
	}
}

type RendererFunc func(context.Context, scene.Project, string, ProgressFunc) ([]LocalArtifact, error)

func (f RendererFunc) Render(ctx context.Context, project scene.Project, workspace string, progress ProgressFunc) ([]LocalArtifact, error) {
	return f(ctx, project, workspace, progress)
}

type fakeControl struct {
	mu         sync.Mutex
	claim      scene.Claim
	claimed    bool
	cancel     bool
	heartbeats int
	finished   scene.FinishRequest
}

func (f *fakeControl) Claim(context.Context) (scene.Claim, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimed {
		return scene.Claim{}, false, nil
	}
	f.claimed = true
	return f.claim, true, nil
}

func (f *fakeControl) Heartbeat(_ context.Context, _, _, stage string, progress int) (scene.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if stage == "" || progress < 0 {
		return scene.Job{}, errors.New("invalid heartbeat")
	}
	f.heartbeats++
	job := f.claim.Job
	job.CancelRequested = f.cancel
	return job, nil
}

func (f *fakeControl) Upload(_ context.Context, _, _ string, artifact LocalArtifact) (scene.Artifact, error) {
	data, err := os.ReadFile(artifact.Path)
	if err != nil {
		return scene.Artifact{}, err
	}
	return scene.Artifact{Kind: artifact.Kind, StorageKey: "scenes/scn_project/video/key-master.mp4", ContentType: artifact.ContentType, SizeBytes: int64(len(data)), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, nil
}

func (f *fakeControl) Finish(_ context.Context, _, _ string, result scene.FinishRequest) error {
	f.mu.Lock()
	f.finished = result
	f.mu.Unlock()
	return nil
}

func testClaim() scene.Claim {
	project := scene.Project{ID: "scn_project", Target: scene.TargetUnreal, Format: scene.FormatMP4, Width: 720, Height: 720, FPS: 24, DurationMS: 4000}
	return scene.Claim{Project: project, Job: scene.Job{ID: "job_one", ProjectID: project.ID, Target: scene.TargetUnreal, LeaseToken: "lease_one", Attempt: 1}}
}
