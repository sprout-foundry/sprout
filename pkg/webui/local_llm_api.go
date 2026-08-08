//go:build !js

package webui

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// localLLMStatus describes the current state of the local LLM engine.
type localLLMStatus struct {
	Available     bool   `json:"available"`       // MLX available + platform supported
	Running       bool   `json:"running"`         // server process is alive and healthy
	ModelPresent  bool   `json:"model_present"`   // at least one model is downloaded
	ModelDir      string `json:"model_dir"`       // path to model cache
	Platform      string `json:"platform"`        // "darwin-arm64", "other"
	Endpoint      string `json:"endpoint"`        // http://127.0.0.1:18081
	RecommendedModel string `json:"recommended_model"`
	Models        []localLLMModel `json:"models"`
}

type localLLMModel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Present  bool   `json:"present"`
	SizeHint string `json:"size_hint"`
}

var (
	localLLMMu       sync.Mutex
	localLLMCached   *localLLMStatus
	localLLMLastCheck time.Time
)

const localLLMCacheTTL = 10 * time.Second
const localLLMEndpoint = "http://127.0.0.1:18081"

// catalogModels is the static catalog of available local models. The backend
// checks for their presence on disk; the UI uses this to show download actions.
var catalogModels = []localLLMModel{
	{ID: "qwen3.5-4b-4bit", Name: "Qwen3.5 4B (4-bit)", SizeHint: "~2.5 GB"},
	{ID: "qwen3.5-9b-4bit", Name: "Qwen3.5 9B (4-bit)", SizeHint: "~5.4 GB"},
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
		Platform:         runtime.GOOS + "-" + runtime.GOARCH,
		Endpoint:         localLLMEndpoint,
		RecommendedModel: "qwen3.5-4b-4bit",
		Models:           catalogModels,
	}

	// Only Apple Silicon supports MLX inference.
	status.Available = runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
	if !status.Available {
		return status
	}

	// Check for downloaded models.
	home, err := os.UserHomeDir()
	if err != nil {
		return status
	}
	modelCacheDir := filepath.Join(home, ".cache", "sprout", "models")
	status.ModelDir = modelCacheDir

	for i, m := range status.Models {
		modelPath := filepath.Join(modelCacheDir, m.ID)
		if _, err := os.Stat(filepath.Join(modelPath, "config.json")); err == nil {
			status.Models[i].Present = true
			status.ModelPresent = true
		}
	}

	// Also check the legacy llm-models path.
	if !status.ModelPresent {
		legacyDir := filepath.Join(home, "dev", "llm-models")
		for _, m := range status.Models {
			if _, err := os.Stat(filepath.Join(legacyDir, m.ID, "config.json")); err == nil {
				status.ModelPresent = true
				break
			}
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
			"Local LLM requires Apple Silicon (M-series Mac)")
		return
	}
	if !status.ModelPresent {
		writeJSONErr(w, http.StatusBadRequest, "no_model",
			"No model downloaded. Download a model first.")
		return
	}
	if status.Running {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "already_running",
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

	modelDir := pickLocalModel(status)
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
		"models":           status.Models,
		"recommended":      status.RecommendedModel,
		"model_dir":        status.ModelDir,
	})
}

func findLocalLLMBinary() string {
	// Check PATH first.
	for _, name := range []string{"llm_server", "sprout-llm-server"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func pickLocalModel(status *localLLMStatus) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	modelCacheDir := filepath.Join(home, ".cache", "sprout", "models")
	for _, m := range status.Models {
		if !m.Present {
			continue
		}
		path := filepath.Join(modelCacheDir, m.ID)
		if _, err := os.Stat(filepath.Join(path, "config.json")); err == nil {
			return path
		}
	}
	return ""
}

// handleLocalLLMDownload handles POST /api/local-llm/download?model=<id>
// Downloads a model using the llm_download binary. Returns immediately with
// a job ID; the client polls /api/local-llm/status to see when the model
// appears. Download runs as a detached background process.
func (ws *ReactWebServer) handleLocalLLMDownload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	status := getLocalLLMStatus()
	if !status.Available {
		writeJSONErr(w, http.StatusBadRequest, "not_available",
			"Local LLM requires Apple Silicon (M-series Mac)")
		return
	}

	modelID := r.URL.Query().Get("model")
	if modelID == "" {
		modelID = status.RecommendedModel
	}

	// Validate the model ID is in our catalog.
	valid := false
	for _, m := range catalogModels {
		if m.ID == modelID {
			valid = true
			break
		}
	}
	if !valid {
		writeJSONErr(w, http.StatusBadRequest, "invalid_model",
			fmt.Sprintf("Unknown model: %s", modelID))
		return
	}

	binaryPath := findLocalLLMDownloadBinary()
	if binaryPath == "" {
		writeJSONErr(w, http.StatusNotFound, "binary_not_found",
			"Model download binary not found. Build with: make build-llm-download")
		return
	}

	// Launch download as a detached process so it survives the request.
	cmd := exec.Command(binaryPath, "-model", modelID)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "download_failed",
			fmt.Sprintf("Failed to start download: %v", err))
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":  "downloading",
		"model":   modelID,
		"pid":     cmd.Process.Pid,
		"message": fmt.Sprintf("Downloading %s in the background. Check status to monitor progress.", modelID),
	})
}

func findLocalLLMDownloadBinary() string {
	for _, name := range []string{"llm_download", "sprout-llm-download"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
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
	modelDir := pickLocalModel(status)
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
