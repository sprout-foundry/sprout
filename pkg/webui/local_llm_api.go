//go:build !js

package webui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sprout-foundry/sinter/llm/catalog"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// localLLMStatus describes the current state of the local LLM engine.
type localLLMStatus struct {
	Available        bool            `json:"available"`     // platform supports the local LLM backend
	MLXAvailable     bool            `json:"mlx_available"` // MLX C library found at runtime (optional)
	Hint             string          `json:"hint,omitempty"`
	Running          bool            `json:"running"`       // server process is alive and healthy
	ModelPresent     bool            `json:"model_present"` // at least one model is downloaded
	ModelDir         string          `json:"model_dir"`     // path to model cache
	Platform         string          `json:"platform"`      // "darwin-arm64", "other"
	Endpoint         string          `json:"endpoint"`      // http://127.0.0.1:18081
	RecommendedModel string          `json:"recommended_model"`
	Models           []localLLMModel `json:"models"`
}

type localLLMModel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	Present  bool   `json:"present"`
	SizeHint string `json:"size_hint"`
	// Tier is "suggested", "stretch", or "blocked" for this machine's RAM —
	// see catalog.TieredCatalogForRAM. Additive field (omitempty): older
	// frontend builds that don't know about it simply ignore it.
	Tier string `json:"tier,omitempty"`
	// Description explains the tier classification (e.g. why a model is
	// blocked, or how much RAM a stretch pick risks).
	Description string `json:"description,omitempty"`
	// Download carries live in-process download state (nil when no job has
	// run this session). Additive.
	Download *localmodel.DownloadStatusPayload `json:"download,omitempty"`
}

var (
	localLLMMu        sync.Mutex
	localLLMCached    *localLLMStatus
	localLLMLastCheck time.Time
)

const localLLMCacheTTL = 10 * time.Second
const localLLMEndpoint = "http://127.0.0.1:18081"

// catalogModelsForRAM builds the local model list from the real RAM-tier
// catalog (sinter's catalog.TieredCatalogForRAM via pkg/localmodel), the
// same source /model in the CLI uses. IDs are catalog Names (the stable
// selection ID everywhere else in sprout). Downloads are tracked in-process
// by pkg/localmodel's download registry — no external binary.
func catalogModelsForRAM(ram uint64) []localLLMModel {
	tiered := catalog.TieredCatalogForRAM(ram)
	models := make([]localLLMModel, 0, len(tiered))
	for _, tm := range tiered {
		sizeHint := ""
		if tm.Model.MinRAMSelect > 0 {
			sizeHint = fmt.Sprintf("~%.0f GB RAM to run", float64(tm.Model.MinRAMSelect)/(1024*1024*1024))
		}
		description := ""
		switch tm.Status {
		case catalog.TierSuggested:
			description = "Suggested for this machine"
		case catalog.TierEligible:
			description = "Smaller than suggested — a safe, lighter-weight choice"
		case catalog.TierStretch:
			description = "Fits, but risks running out of memory"
		default:
			description = fmt.Sprintf("Requires more RAM than this machine has (%.0f GB)", float64(ram)/(1024*1024*1024))
		}
		models = append(models, localLLMModel{
			ID:          tm.Model.Name,
			Name:        tm.Model.Name,
			Dir:         tm.Model.Dir,
			SizeHint:    sizeHint,
			Tier:        tm.Status.String(),
			Description: description,
		})
	}

	// Overlay live download state so the client sees progress on every
	// status poll without a separate endpoint.
	for i := range models {
		if job := localmodel.DownloadStatus(models[i].ID); job != nil {
			models[i].Download = job
		}
	}
	return models
}

// getLocalLLMStatus returns the cached status, refreshing if stale.
func getLocalLLMStatus() *localLLMStatus {
	localLLMMu.Lock()
	defer localLLMMu.Unlock()

	if localLLMCached != nil && time.Since(localLLMLastCheck) < localLLMCacheTTL {
		return localLLMCached
	}

	status := probeLocalLLMStatus()
	localLLMCached = status
	localLLMLastCheck = time.Now()
	return status
}

func probeLocalLLMStatus() *localLLMStatus {
	status := &localLLMStatus{
		Platform: runtime.GOOS + "-" + runtime.GOARCH,
		Endpoint: localLLMEndpoint,
	}

	// Only Apple Silicon supports MLX inference.
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		// MLX is an optional runtime dependency (dlopen'd by sinter): the
		// rest of sprout works without it, the local LLM backend needs it.
		status.MLXAvailable = localmodel.MLXAvailable()
		if !status.MLXAvailable {
			status.Hint = "The MLX C libraries are not installed. Install with: brew install mlx-c, then restart sprout."
		}
		status.Available = status.MLXAvailable
	}
	if !status.Available {
		return status
	}

	ram := localmodel.TotalSystemRAM()
	status.Models = catalogModelsForRAM(ram)
	// RecommendedModel is a catalog Name (the unified selection ID).
	status.RecommendedModel = catalog.RecommendModelForRAM(ram).Name
	status.ModelDir = localmodel.DefaultModelsDir

	// Check for downloaded models under the models root. Presence = the
	// catalog entry's directory holding actual weights (config.json is the
	// marker every downloader guarantees post-download).
	for i, m := range status.Models {
		modelPath := filepath.Join(status.ModelDir, m.Dir)
		if _, err := os.Stat(filepath.Join(modelPath, "config.json")); err == nil {
			status.Models[i].Present = true
			status.ModelPresent = true
		}
	}

	// Health check the local server.
	if status.ModelPresent {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", localLLMEndpoint+"/health", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				status.Running = true
			}
		}
	}

	return status
}

// localLLMUnavailableMessage explains why the local LLM backend is
// unavailable: either the platform is unsupported, or the optional MLX C
// library was not found at runtime.
func localLLMUnavailableMessage(status *localLLMStatus) string {
	if status.Platform != "darwin-arm64" {
		return "Local LLM requires Apple Silicon (M-series Mac)"
	}
	if !status.MLXAvailable {
		return status.Hint
	}
	return "Local LLM is unavailable"
}

// handleLocalLLMStatus handles GET /api/local-llm/status
func (ws *ReactWebServer) handleLocalLLMStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	status := getLocalLLMStatus()
	writeJSON(w, http.StatusOK, status)
}

// handleLocalLLMStart handles POST /api/local-llm/start
// Launches the local LLM server process. This is a no-op if already running.
func (ws *ReactWebServer) handleLocalLLMStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	status := getLocalLLMStatus()
	if !status.Available {
		writeJSONErr(w, http.StatusBadRequest, "not_available",
			localLLMUnavailableMessage(status))
		return
	}
	if !status.ModelPresent {
		writeJSONErr(w, http.StatusBadRequest, "no_model",
			"No model downloaded. Download a model first.")
		return
	}
	if status.Running {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":   "already_running",
			"endpoint": status.Endpoint,
		})
		return
	}

	// Attempt to start the server. The binary is expected to be on PATH
	// or at a well-known location relative to the sprout binary.
	binaryPath := findLocalLLMBinary()
	if binaryPath == "" {
		writeJSONErr(w, http.StatusNotFound, "binary_not_found",
			"Local LLM server binary not found. Build with: make build-llm-server")
		return
	}

	// Start as a detached subprocess. The optional ?model= parameter picks
	// WHICH installed model to load (catalog Name); without it the first
	// present model in tier order is used, as before.
	modelDir := pickLocalModel(status, r.URL.Query().Get("model"))
	if modelDir == "" {
		writeJSONErr(w, http.StatusBadRequest, "no_model_dir",
			"Could not find a model directory")
		return
	}

	// Start as a detached subprocess.
	cmd := exec.CommandContext(r.Context(), binaryPath,
		"-model", modelDir,
		"-port", "18081")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(), "GO_QUANTIZE=4")
	if err := cmd.Start(); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "start_failed",
			fmt.Sprintf("Failed to start local LLM server: %v", err))
		return
	}

	// Wait briefly for the server to come up.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	healthy := false
	for attempt := 0; attempt < 30; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", localLLMEndpoint+"/health", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				healthy = true
				break
			}
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(time.Second):
		}
	}

	if !healthy {
		writeJSONErr(w, http.StatusGatewayTimeout, "health_check_failed",
			"Server started but health check timed out. It may still be loading the model.")
		return
	}

	// Invalidate cache so next status check reflects the running state.
	localLLMMu.Lock()
	localLLMCached = nil
	localLLMMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "started",
		"endpoint": localLLMEndpoint,
		"pid":      cmd.Process.Pid,
	})
}

// handleLocalLLMModels returns the list of local models with download status.
func (ws *ReactWebServer) handleLocalLLMModels(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	status := getLocalLLMStatus()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models":      status.Models,
		"recommended": status.RecommendedModel,
		"model_dir":   status.ModelDir,
	})
}

func findLocalLLMBinary() string {
	// Check PATH first.
	for _, name := range []string{"llm_server", "sprout-llm-server"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	// Check next to the sprout executable.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "llm_server")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// pickLocalModel resolves a catalog Name (or directory basename) to the
// absolute model directory under the models root. Empty preferredID (or an
// ID that isn't installed) falls back to the first present model in tier
// order — the historical behavior.
func pickLocalModel(status *localLLMStatus, preferredID string) string {
	if preferredID != "" {
		if st, err := localmodel.ResolveModelID(preferredID); err == nil && st.Installed {
			return st.Dir
		}
	}
	for _, m := range status.Models {
		if !m.Present {
			continue
		}
		path := filepath.Join(status.ModelDir, m.Dir)
		if _, err := os.Stat(filepath.Join(path, "config.json")); err == nil {
			return path
		}
	}
	return ""
}

// handleLocalLLMDownload handles POST /api/local-llm/download?model=<id>
// Starts an in-process download job for a catalog model (by catalog Name —
// the same ID every other selection surface uses) and returns immediately;
// the client polls /api/local-llm/status, whose models[] entries carry
// live progress in the download field. Cancel with
// POST /api/local-llm/download/cancel?model=<id>.
func (ws *ReactWebServer) handleLocalLLMDownload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	status := getLocalLLMStatus()
	if !status.Available {
		writeJSONErr(w, http.StatusBadRequest, "not_available",
			localLLMUnavailableMessage(status))
		return
	}

	modelID := r.URL.Query().Get("model")
	if modelID == "" {
		modelID = status.RecommendedModel
	}

	job, err := localmodel.StartDownload(modelID)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "download_start_failed", err.Error())
		return
	}

	// Invalidate the status cache so the next poll immediately carries the
	// new job's downloading state.
	localLLMMu.Lock()
	localLLMCached = nil
	localLLMMu.Unlock()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":  job.Status,
		"model":   job.ModelID,
		"message": fmt.Sprintf("Downloading %s. Progress appears in the model list.", job.ModelID),
	})
}

// handleLocalLLMDownloadCancel handles POST
// /api/local-llm/download/cancel?model=<id>.
func (ws *ReactWebServer) handleLocalLLMDownloadCancel(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	modelID := r.URL.Query().Get("model")
	if modelID == "" {
		writeJSONErr(w, http.StatusBadRequest, "model_required", "model parameter is required")
		return
	}
	if err := localmodel.CancelDownload(modelID); err != nil {
		writeJSONErr(w, http.StatusConflict, "cancel_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "canceled",
		"model":  modelID,
	})
}

// ensureLocalLLMRunning starts the local LLM server if the platform supports
// it and a model is available. Called when sprout-local is selected as the
// provider. Returns the endpoint URL if running, or "" if not started.
func ensureLocalLLMRunning() string {
	status := getLocalLLMStatus()
	if !status.Available || !status.ModelPresent {
		return ""
	}
	if status.Running {
		return status.Endpoint
	}

	binaryPath := findLocalLLMBinary()
	if binaryPath == "" {
		return ""
	}
	modelDir := pickLocalModel(status, "")
	if modelDir == "" {
		return ""
	}

	cmd := exec.Command(binaryPath, "-model", modelDir, "-port", "18081")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(), "GO_QUANTIZE=4")
	if err := cmd.Start(); err != nil {
		return ""
	}

	// Wait for health (model load can take 10-30s).
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := 0; i < 60; i++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", localLLMEndpoint+"/health", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				localLLMMu.Lock()
				localLLMCached = nil
				localLLMMu.Unlock()
				return localLLMEndpoint
			}
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(time.Second):
		}
	}
	return ""
}

// stopLocalLLMServer attempts a graceful shutdown of the local LLM server
// via POST /shutdown. Best-effort: if the server doesn't respond, the
// detached process continues running. Called during daemon Shutdown().
func stopLocalLLMServer(log *slog.Logger) {
	if log == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", localLLMEndpoint+"/shutdown", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Debug("local LLM server not stopped (likely not running)", "err", err)
		return
	}
	resp.Body.Close()
	localLLMMu.Lock()
	localLLMCached = nil
	localLLMMu.Unlock()
	log.Info("local LLM server stopped")
}
