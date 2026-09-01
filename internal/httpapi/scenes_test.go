package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/auth"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/scene"
	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestSceneHTTPJobLifecycle(t *testing.T) {
	handler, token := newSceneTestHandler(t)
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/scenes", bytes.NewBufferString(`{
      "name":"Glass city","prompt":"a glass city at dusk","engine_target":"unreal","master_format":"mp4",
      "width":720,"height":720,"fps":24,"duration_ms":4000
    }`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d; body = %s", response.Code, response.Body.String())
	}
	var project scene.Project
	if err := json.NewDecoder(response.Body).Decode(&project); err != nil {
		t.Fatal(err)
	}
	if project.State != scene.StateQueued || response.Header().Get("Location") != "/api/v1/scenes/"+project.ID {
		t.Fatalf("project = %#v; headers = %#v", project, response.Header())
	}

	claimRequest := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/scene-jobs/claim", bytes.NewBufferString(`{
      "protocol_version":1,"worker_id":"gpu-one","worker_version":"test","engine_targets":["unreal"],
      "capabilities":[{"id":"blender","version":"test"},{"id":"unreal-5","version":"5.x"},{"id":"ffmpeg","version":"test"}]
    }`))
	claimRequest.Header.Set("Authorization", "Bearer "+token)
	claimRequest.Header.Set("Content-Type", "application/json")
	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, claimRequest)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("claim status = %d; body = %s", claimResponse.Code, claimResponse.Body.String())
	}
	var envelope scene.ClaimResponse
	if err := json.NewDecoder(claimResponse.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	claim := envelope.Claim

	heartbeatRequest := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/scene-jobs/"+claim.Job.ID+"/heartbeat", bytes.NewBufferString(`{
      "worker_id":"gpu-one","lease_token":"`+claim.Job.LeaseToken+`","stage":"blender-assets","progress":20
    }`))
	heartbeatRequest.Header.Set("Authorization", "Bearer "+token)
	heartbeatRequest.Header.Set("Content-Type", "application/json")
	heartbeatResponse := httptest.NewRecorder()
	handler.ServeHTTP(heartbeatResponse, heartbeatRequest)
	if heartbeatResponse.Code != http.StatusOK || !strings.Contains(heartbeatResponse.Body.String(), `"progress":20`) {
		t.Fatalf("heartbeat status = %d; body = %s", heartbeatResponse.Code, heartbeatResponse.Body.String())
	}

	uploadRequest := httptest.NewRequest(http.MethodPut, "http://example.com/api/v1/scene-jobs/"+claim.Job.ID+"/artifacts/video", bytes.NewBufferString("bounded-video"))
	uploadRequest.Header.Set("Authorization", "Bearer "+token)
	uploadRequest.Header.Set("Content-Type", "video/mp4")
	uploadRequest.Header.Set("X-GoGIF-Worker-ID", "gpu-one")
	uploadRequest.Header.Set("X-GoGIF-Lease-Token", claim.Job.LeaseToken)
	uploadRequest.Header.Set("X-GoGIF-Filename", "master.mp4")
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d; body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var artifact scene.Artifact
	if err := json.NewDecoder(uploadResponse.Body).Decode(&artifact); err != nil {
		t.Fatal(err)
	}
	artifactJSON, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	finishRequest := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/scene-jobs/"+claim.Job.ID+"/finish", bytes.NewBufferString(`{
	  "worker_id":"gpu-one","lease_token":"`+claim.Job.LeaseToken+`","result":{"success":true,"artifacts":[`+string(artifactJSON)+`]}
    }`))
	finishRequest.Header.Set("Authorization", "Bearer "+token)
	finishRequest.Header.Set("Content-Type", "application/json")
	finishResponse := httptest.NewRecorder()
	handler.ServeHTTP(finishResponse, finishRequest)
	if finishResponse.Code != http.StatusOK || !strings.Contains(finishResponse.Body.String(), `"state":"succeeded"`) {
		t.Fatalf("finish status = %d; body = %s", finishResponse.Code, finishResponse.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/scenes/"+project.ID, nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"progress":100`) {
		t.Fatalf("get status = %d; body = %s", getResponse.Code, getResponse.Body.String())
	}
}

func TestSceneWorkersRequireTokenAndTargetsAreAllowlisted(t *testing.T) {
	handler, _ := newSceneTestHandler(t)
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/scene-jobs/claim", bytes.NewBufferString(`{"worker_id":"gpu-one"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("worker status = %d; body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/scenes", bytes.NewBufferString(`{"prompt":"test","engine_target":"unity"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "not enabled") {
		t.Fatalf("target status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestPublicConfigReportsSceneFoundationWithoutExposingWorkerSecret(t *testing.T) {
	handler, token := newSceneTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/config", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"scene_workspace":{"enabled":true`) || !strings.Contains(response.Body.String(), `"engine_targets":["unreal"]`) {
		t.Fatalf("config status = %d; body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), token) {
		t.Fatal("public config exposed the Scene worker token")
	}
}

func newSceneTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	kv := store.NewMemoryKV()
	repository, err := scene.NewRepository(kv, scene.Options{AllowedTargets: []scene.EngineTarget{scene.TargetUnreal}})
	if err != nil {
		t.Fatal(err)
	}
	authManager, err := auth.New(auth.Options{
		Mode: auth.ModeLocal, SessionSecret: strings.Repeat("s", 32), PublicURL: "http://example.com", LocalEmail: "owner@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := store.NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := scene.NewArtifactRepository(kv, blobs, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("w", 40)
	return New(Options{
		Planner: planner.Local{}, Auth: authManager, Plans: account.NewCatalog(account.CatalogOptions{}),
		Scenes: repository, SceneArtifacts: artifacts, SceneWorkerToken: token, SceneLease: time.Minute,
	}), token
}
