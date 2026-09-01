package sceneworker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/scene"
)

func TestClientControlAndArtifactProtocol(t *testing.T) {
	token := strings.Repeat("w", 40)
	project := scene.Project{ID: "scn_project", Target: scene.TargetUnreal, Format: scene.FormatMP4}
	job := scene.Job{ID: "job_one", ProjectID: project.ID, LeaseToken: "lease_one"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/scene-jobs/claim":
			var hello scene.WorkerHello
			if json.NewDecoder(r.Body).Decode(&hello) != nil || hello.ProtocolVersion != scene.WorkerProtocolVersion {
				http.Error(w, "bad hello", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(scene.ClaimResponse{ProtocolVersion: 1, LeaseSeconds: 120, Claim: scene.Claim{Job: job, Project: project}})
		case r.URL.Path == "/api/v1/scene-jobs/job_one/heartbeat":
			_ = json.NewEncoder(w).Encode(map[string]any{"job": job})
		case r.URL.Path == "/api/v1/scene-jobs/job_one/artifacts/video":
			if r.Header.Get("X-GoGIF-Worker-ID") != "gpu-one" || r.Header.Get("X-GoGIF-Lease-Token") != job.LeaseToken || r.Header.Get("X-GoGIF-Filename") != "master.mp4" {
				http.Error(w, "bad upload metadata", http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(scene.Artifact{Kind: "video", StorageKey: "scenes/scn_project/video/key-master.mp4", ContentType: "video/mp4", SizeBytes: int64(len(data)), SHA256: strings.Repeat("a", 64)})
		case r.URL.Path == "/api/v1/scene-jobs/job_one/finish":
			_ = json.NewEncoder(w).Encode(map[string]any{"terminal": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BaseURL: server.URL, Token: token,
		Hello: scene.WorkerHello{
			ProtocolVersion: 1, WorkerID: "gpu-one", WorkerVersion: "test", Targets: []scene.EngineTarget{scene.TargetUnreal},
			Capabilities: []scene.WorkerCapability{{ID: "blender"}, {ID: "unreal-5"}, {ID: "ffmpeg"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := client.Claim(context.Background())
	if err != nil || !ok || claim.Job.ID != job.ID {
		t.Fatalf("claim = %#v, %v, %v", claim, ok, err)
	}
	if _, err := client.Heartbeat(context.Background(), job.ID, job.LeaseToken, "rendering", 50); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(t.TempDir(), "master.mp4")
	if err := os.WriteFile(artifactPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := client.Upload(context.Background(), job.ID, job.LeaseToken, LocalArtifact{Kind: "video", Path: artifactPath, Filename: "master.mp4", ContentType: "video/mp4"})
	if err != nil || artifact.SizeBytes != 5 {
		t.Fatalf("upload = %#v, %v", artifact, err)
	}
	if err := client.Finish(context.Background(), job.ID, job.LeaseToken, scene.FinishRequest{Success: true, Artifacts: []scene.Artifact{artifact}}); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsInsecureRemoteAPI(t *testing.T) {
	_, err := NewClient(ClientOptions{
		BaseURL: "http://example.com", Token: strings.Repeat("w", 40),
		Hello: scene.WorkerHello{ProtocolVersion: 1, WorkerID: "gpu", WorkerVersion: "test", Targets: []scene.EngineTarget{scene.TargetUnreal},
			Capabilities: []scene.WorkerCapability{{ID: "blender"}, {ID: "unreal-5"}, {ID: "ffmpeg"}}},
	})
	if err == nil {
		t.Fatal("NewClient() error = nil")
	}
}
