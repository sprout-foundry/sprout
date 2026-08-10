//go:build !js

// Package localmodel manages the lifecycle of the local LLM server
// (cmd/llm_server) that runs on Apple Silicon via MLX. It handles:
//
//   - Detecting whether a server is already running on the configured port
//   - Spawning a new server as a detached background process
//   - Downloading models from HuggingFace with progress reporting
//   - Selecting the best model for the machine's RAM
//
// The package is the glue between sprout's provider system (which treats
// sprout-local as just another OpenAI-compatible endpoint) and the actual
// Go MLX server binary. When a user selects the "Local" provider in
// onboarding, this package ensures the server is running with the right
// model before the provider is used.
package localmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// DefaultPort is the port the local LLM server listens on. Matches the
// sprout-local provider config endpoint.
const DefaultPort = 18081

// DefaultModelsDir is where downloaded models are stored.
var DefaultModelsDir = func() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "dev", "llm-models")
	}
	return filepath.Join(os.TempDir(), "llm-models")
}()

// ServerStatus describes the state of the local LLM server.
type ServerStatus struct {
	Running   bool   `json:"running"`
	Port      int    `json:"port"`
	Model     string `json:"model"`
	Healthy   bool   `json:"healthy"`
	URL       string `json:"url"`
	PID       int    `json:"pid,omitempty"`
	Error     string `json:"error,omitempty"`
}

// HealthCheck pings the server's /health endpoint. Returns nil if the
// server is healthy and running. Returns a non-nil error otherwise.
func HealthCheck(port int) (*ServerStatus, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return &ServerStatus{Running: false, Port: port, URL: fmt.Sprintf("http://127.0.0.1:%d", port)}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &ServerStatus{Running: true, Healthy: false, Port: port}, fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var status ServerStatus
	if err := json.Unmarshal(body, &status); err != nil {
		// Server is running but returned unexpected JSON — still usable
		return &ServerStatus{Running: true, Healthy: true, Port: port, URL: fmt.Sprintf("http://127.0.0.1:%d", port)}, nil
	}
	status.Running = true
	status.Healthy = true
	status.URL = fmt.Sprintf("http://127.0.0.1:%d", port)
	status.Port = port
	return &status, nil
}

// EnsureServer checks if the local LLM server is running and healthy on the
// given port. If not, it spawns a new server with the given model directory.
// Returns the server status when the server is healthy, or an error if it
// couldn't be started within the timeout.
//
// The modelDir must be an absolute path to a directory containing a valid
// model (config.json, tokenizer.json, *.safetensors).
func EnsureServer(ctx context.Context, port int, modelDir string) (*ServerStatus, error) {
	// Fast path: server already running and healthy.
	if status, err := HealthCheck(port); err == nil && status.Healthy {
		return status, nil
	}

	// Spawn the server as a detached background process.
	if err := spawnServer(port, modelDir); err != nil {
		return nil, fmt.Errorf("spawn local server: %w", err)
	}

	// Wait for health.
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
		if status, err := HealthCheck(port); err == nil && status.Healthy {
			return status, nil
		}
	}
	return nil, fmt.Errorf("local server did not become healthy within 60s")
}

// spawnServer launches the llm_server binary as a detached process. The
// binary is expected to be in the same directory as the sprout binary, or
// on PATH.
func spawnServer(port int, modelDir string) error {
	binary, err := findServerBinary()
	if err != nil {
		return err
	}

	cmd := exec.Command(binary,
		"-model", modelDir,
		"-port", fmt.Sprintf("%d", port),
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	// Detach from the parent process group so the server survives CLI exit.
	cmd.SysProcAttr = detachedSysProcAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	// Release the process so it doesn't become a zombie when the parent exits.
	_ = cmd.Process.Release()
	return nil
}

// findServerBinary locates the llm_server binary. On macOS/Linux it looks
// for "llm_server" next to the sprout executable, then on PATH.
func findServerBinary() (string, error) {
	exePath, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), "llm_server")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("llm_server"); err == nil {
		return path, nil
	}
	return "", errors.New("llm_server binary not found — expected next to sprout binary or on PATH")
}

// EnsureServerHealth checks if the server is running and starts it if not,
// using the given model directory. Convenience wrapper for agent integration.
func EnsureServerHealth(ctx context.Context, modelDir string) error {
	status, err := HealthCheck(DefaultPort)
	if err == nil && status.Healthy {
		return nil
	}
	return spawnAndWait(ctx, modelDir)
}

func spawnAndWait(ctx context.Context, modelDir string) error {
	if err := spawnServer(DefaultPort, modelDir); err != nil {
		return fmt.Errorf("spawn local server: %w", err)
	}
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		if status, err := HealthCheck(DefaultPort); err == nil && status.Healthy {
			return nil
		}
	}
	return fmt.Errorf("local server did not become healthy within 60s")
}
func StopServer(port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/shutdown", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
