package scene

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestProjectJobLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	repository, err := NewRepository(store.NewMemoryKV(), Options{AllowedTargets: []EngineTarget{TargetUnreal}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.Create(context.Background(), "usr_one", CreateRequest{Prompt: "a glass city at dusk", Target: TargetUnreal})
	if err != nil {
		t.Fatal(err)
	}
	if project.State != StateQueued || project.Format != FormatMP4 || project.Width != 720 || project.JobID == "" {
		t.Fatalf("project = %#v", project)
	}
	claim, err := repository.Claim(context.Background(), "worker-a", []EngineTarget{TargetUnreal}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Job.Attempt != 1 || claim.Job.LeaseToken == "" || claim.Project.State != StateLeased {
		t.Fatalf("claim = %#v", claim)
	}
	now = now.Add(20 * time.Second)
	job, err := repository.Heartbeat(context.Background(), claim.Job.ID, "worker-a", claim.Job.LeaseToken, "unreal-render", 45, time.Minute)
	if err != nil || job.State != StateRunning || job.Progress != 45 {
		t.Fatalf("heartbeat = %#v, %v", job, err)
	}
	if _, _, err := repository.Finish(context.Background(), job.ID, "worker-a", job.LeaseToken, FinishRequest{Success: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("success without master artifact = %v", err)
	}
	artifact := Artifact{Kind: "video", StorageKey: "scenes/" + project.ID + "/master.mp4", ContentType: "video/mp4", SizeBytes: 1024, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	finished, terminal, err := repository.Finish(context.Background(), job.ID, "worker-a", job.LeaseToken, FinishRequest{Success: true, Artifacts: []Artifact{artifact}})
	if err != nil || !terminal || finished.State != StateSucceeded || finished.Progress != 100 || len(finished.Artifacts) != 1 {
		t.Fatalf("finish = %#v, %v, %v", finished, terminal, err)
	}
	if _, err := repository.Claim(context.Background(), "worker-a", nil, time.Minute); !errors.Is(err, ErrNoJob) {
		t.Fatalf("claim after finish = %v", err)
	}
}

func TestRetryLeaseExpiryAndCancellation(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	repository, err := NewRepository(store.NewMemoryKV(), Options{AllowedTargets: []EngineTarget{TargetUnity, TargetUnreal}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.Create(context.Background(), "usr_one", CreateRequest{Prompt: "storm", Target: TargetUnity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Claim(context.Background(), "unreal-only", []EngineTarget{TargetUnreal}, time.Minute); !errors.Is(err, ErrNoJob) {
		t.Fatalf("incompatible claim = %v", err)
	}
	claim, err := repository.Claim(context.Background(), "unity-worker", []EngineTarget{TargetUnity}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	reclaimed, err := repository.Claim(context.Background(), "unity-worker-2", []EngineTarget{TargetUnity}, time.Minute)
	if err != nil || reclaimed.Job.Attempt != 2 {
		t.Fatalf("reclaim = %#v, %v", reclaimed, err)
	}
	if _, err := repository.Heartbeat(context.Background(), claim.Job.ID, "unity-worker", claim.Job.LeaseToken, "late", 5, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old heartbeat = %v", err)
	}
	canceled, changed, err := repository.Cancel(context.Background(), "usr_one", project.ID)
	if err != nil || !changed || !canceled.CancelRequested || canceled.State != StateLeased {
		t.Fatalf("cancel = %#v, %v, %v", canceled, changed, err)
	}
	finished, terminal, err := repository.Finish(context.Background(), reclaimed.Job.ID, "unity-worker-2", reclaimed.Job.LeaseToken, FinishRequest{})
	if err != nil || !terminal || finished.State != StateCanceled {
		t.Fatalf("cancel finish = %#v, %v, %v", finished, terminal, err)
	}
}

func TestValidationAndOwnership(t *testing.T) {
	repository, err := NewRepository(store.NewMemoryKV(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(context.Background(), "usr", CreateRequest{Prompt: "x", Target: TargetUnity}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("disabled target error = %v", err)
	}
	if _, err := repository.Create(context.Background(), "usr", CreateRequest{Prompt: "x", Width: 721, Height: 720}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("odd video dimension error = %v", err)
	}
	project, err := repository.Create(context.Background(), "usr", CreateRequest{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetProject(context.Background(), "another", project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ownership error = %v", err)
	}
	items, err := repository.ListProjects(context.Background(), "usr", 20)
	if err != nil || len(items) != 1 || items[0].ID != project.ID {
		t.Fatalf("list = %#v, %v", items, err)
	}
}

func TestWorkerHelloRequiresTargetCapabilities(t *testing.T) {
	hello := WorkerHello{
		ProtocolVersion: WorkerProtocolVersion, WorkerID: "gpu-one", WorkerVersion: "test", Targets: []EngineTarget{TargetUnreal},
		Capabilities: []WorkerCapability{{ID: "blender"}, {ID: "unreal-5"}, {ID: "ffmpeg"}},
	}
	if err := hello.Validate([]EngineTarget{TargetUnreal}); err != nil {
		t.Fatal(err)
	}
	hello.Capabilities = hello.Capabilities[:2]
	if err := hello.Validate([]EngineTarget{TargetUnreal}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing FFmpeg capability = %v", err)
	}
}
