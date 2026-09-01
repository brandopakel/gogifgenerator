package httpapi

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/scene"
	"github.com/brandopakel/gogifgenerator/internal/store"
)

func sceneWorkspaceDescriptor(repository *scene.Repository) map[string]any {
	if repository == nil {
		return map[string]any{"enabled": false, "ui_enabled": false, "engine_targets": []string{}}
	}
	targets := repository.AllowedTargets()
	return map[string]any{"enabled": true, "ui_enabled": false, "engine_targets": targets}
}

func (s *server) createScene(w http.ResponseWriter, r *http.Request) {
	if s.options.Scenes == nil {
		writeError(w, http.StatusServiceUnavailable, "Scene jobs are not enabled on this deployment.")
		return
	}
	var request scene.CreateRequest
	if !decodeSmallJSON(w, r, &request) {
		return
	}
	normalized, err := request.Normalize(s.options.Scenes.AllowedTargets())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	principal := s.principal(r)
	planID := principal.PlanID
	if principal.Legacy {
		planID = account.PlanLegacy
	}
	plan, ok := s.options.Plans.Get(planID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Your Scene entitlement could not be checked.")
		return
	}
	if !plan.Studio {
		s.writeAccessError(w, account.ErrUpgradeRequired)
		return
	}
	if normalized.Width > plan.MaxDimension || normalized.Height > plan.MaxDimension {
		s.writeAccessError(w, account.ErrQualityLimit)
		return
	}
	project, err := s.options.Scenes.Create(r.Context(), principal.UserID, normalized)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Location", "/api/v1/scenes/"+project.ID)
	writeJSON(w, http.StatusAccepted, project)
}

func (s *server) getScene(w http.ResponseWriter, r *http.Request) {
	if s.options.Scenes == nil {
		writeError(w, http.StatusServiceUnavailable, "Scene jobs are not enabled on this deployment.")
		return
	}
	project, err := s.options.Scenes.GetProject(r.Context(), s.principal(r).UserID, r.PathValue("id"))
	if errors.Is(err, scene.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The Scene project could not be loaded.")
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *server) listScenes(w http.ResponseWriter, r *http.Request) {
	if s.options.Scenes == nil {
		writeError(w, http.StatusServiceUnavailable, "Scene jobs are not enabled on this deployment.")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	projects, err := s.options.Scenes.ListProjects(r.Context(), s.principal(r).UserID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Scene projects could not be loaded.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": projects})
}

func (s *server) cancelScene(w http.ResponseWriter, r *http.Request) {
	if s.options.Scenes == nil {
		writeError(w, http.StatusServiceUnavailable, "Scene jobs are not enabled on this deployment.")
		return
	}
	project, changed, err := s.options.Scenes.Cancel(r.Context(), s.principal(r).UserID, r.PathValue("id"))
	if errors.Is(err, scene.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The Scene project could not be canceled.")
		return
	}
	status := http.StatusOK
	if changed && !project.State.Terminal() {
		status = http.StatusAccepted
	}
	writeJSON(w, status, project)
}

func (s *server) sceneWorker(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.options.Scenes == nil || len(s.options.SceneWorkerToken) < 32 {
			writeError(w, http.StatusServiceUnavailable, "Scene workers are not configured.")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(provided) != len(s.options.SceneWorkerToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.options.SceneWorkerToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "A valid Scene worker token is required.")
			return
		}
		next(w, r)
	}
}

func (s *server) claimSceneJob(w http.ResponseWriter, r *http.Request) {
	var request scene.WorkerHello
	if !decodeSmallJSON(w, r, &request) {
		return
	}
	if err := request.Validate(s.options.Scenes.AllowedTargets()); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	claim, err := s.options.Scenes.Claim(r.Context(), request.WorkerID, request.Targets, s.sceneLease())
	if errors.Is(err, scene.ErrNoJob) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scene.ClaimResponse{
		ProtocolVersion: scene.WorkerProtocolVersion,
		LeaseSeconds:    int(s.sceneLease() / time.Second),
		Claim:           claim,
	})
}

func (s *server) heartbeatSceneJob(w http.ResponseWriter, r *http.Request) {
	var request struct {
		WorkerID   string `json:"worker_id"`
		LeaseToken string `json:"lease_token"`
		Stage      string `json:"stage"`
		Progress   int    `json:"progress"`
	}
	if !decodeSmallJSON(w, r, &request) {
		return
	}
	job, err := s.options.Scenes.Heartbeat(r.Context(), r.PathValue("id"), request.WorkerID, request.LeaseToken, request.Stage, request.Progress, s.sceneLease())
	if errors.Is(err, scene.ErrLeaseLost) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, scene.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "cancel_requested": job.CancelRequested})
}

func (s *server) finishSceneJob(w http.ResponseWriter, r *http.Request) {
	var request struct {
		WorkerID   string              `json:"worker_id"`
		LeaseToken string              `json:"lease_token"`
		Result     scene.FinishRequest `json:"result"`
	}
	if !decodeSmallJSON(w, r, &request) {
		return
	}
	project, err := s.options.Scenes.LeasedProject(r.Context(), r.PathValue("id"), request.WorkerID, request.LeaseToken)
	if errors.Is(err, scene.ErrLeaseLost) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, scene.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(request.Result.Artifacts) > 0 {
		if s.options.SceneArtifacts == nil {
			writeError(w, http.StatusServiceUnavailable, "Scene artifact storage is not configured.")
			return
		}
		if err := s.options.SceneArtifacts.Verify(r.Context(), project.ID, request.Result.Artifacts); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	project, terminal, err := s.options.Scenes.Finish(r.Context(), r.PathValue("id"), request.WorkerID, request.LeaseToken, request.Result)
	if errors.Is(err, scene.ErrLeaseLost) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, scene.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project, "terminal": terminal})
}

func (s *server) uploadSceneArtifact(w http.ResponseWriter, r *http.Request) {
	if s.options.SceneArtifacts == nil {
		writeError(w, http.StatusServiceUnavailable, "Scene artifact storage is not configured.")
		return
	}
	workerID := strings.TrimSpace(r.Header.Get("X-GoGIF-Worker-ID"))
	leaseToken := strings.TrimSpace(r.Header.Get("X-GoGIF-Lease-Token"))
	project, err := s.options.Scenes.LeasedProject(r.Context(), r.PathValue("id"), workerID, leaseToken)
	if errors.Is(err, scene.ErrLeaseLost) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, scene.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute))
	artifact, err := s.options.SceneArtifacts.Put(
		r.Context(), project.ID, r.PathValue("kind"), r.Header.Get("X-GoGIF-Filename"), r.Header.Get("Content-Type"), r.Body,
	)
	if errors.Is(err, store.ErrTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "The Scene artifact exceeds this deployment's upload limit.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, artifact)
}

func (s *server) sceneLease() time.Duration {
	if s.options.SceneLease >= 30*time.Second && s.options.SceneLease <= 15*time.Minute {
		return s.options.SceneLease
	}
	return 2 * time.Minute
}
