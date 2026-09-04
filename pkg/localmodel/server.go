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

// ServerStatus describes the state of the local LLM server.
type ServerStatus struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	Model   string `json:"model"`
	Healthy bool   `json:"healthy"`
	URL     string `json:"url"`
	PID     int    `json:"pid,omitempty"`
	Error   string `json:"error,omitempty"`
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
	return EnsureServerWithBackend(ctx, port, modelDir, "")
}

// EnsureServerWithBackend is like EnsureServer but lets the caller specify
// which server backend to use: "gomlx" (default, Go-native) or "mlx_lm"
// (Python mlx_lm.server, needed for models without a Go architecture).
func EnsureServerWithBackend(ctx context.Context, port int, modelDir, backend string) (*ServerStatus, error) {
	// Fast path: server already running and healthy.
	if status, err := HealthCheck(port); err == nil && status.Healthy {
		return status, nil
	}

	// Spawn the server as a detached background process.
	if err := spawnServer(port, modelDir, backend); err != nil {
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

// spawnServer launches the appropriate server binary as a detached process.
// For "gomlx" backend (default), uses the Go-native llm_server.
// For "mlx_lm" backend, uses Python mlx_lm.server (needed for models without
// a Go architecture implementation like LFM2).
func spawnServer(port int, modelDir string, backend string) error {
	if backend == "mlx_lm" {
		return spawnMLXServer(port, modelDir)
	}
	return spawnGoServer(port, modelDir)
}

// spawnGoServer launches the Go-native llm_server binary.
func spawnGoServer(port int, modelDir string) error {
	binary, err := findServerBinary()
	if err != nil {
		return err
	}

	// -max-tokens 0 disables the server's request cap so the client's
	// max_tokens is honored as-is. The server's 512 default silently
	// truncated long completions mid-thought while reporting a clean
	// "stop" — the same class of bug fixed for the in-process provider
	// (localMaxOutputTokens). Sprout's provider layer does its own
	// context-aware budgeting, so a second blind cap here only harms.
	cmd := exec.Command(binary,
		"-model", modelDir,
		"-port", fmt.Sprintf("%d", port),
		"-max-tokens", "0",
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = detachedSysProcAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// spawnMLXServer launches Python's mlx_lm.server for models that don't have
// a Go architecture implementation.
func spawnMLXServer(port int, modelDir string) error {
	python, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("python3 not found — required for mlx_lm backend")
	}

	cmd := exec.Command(python, "-m", "mlx_lm.server",
		"--model", modelDir,
		"--port", fmt.Sprintf("%d", port),
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = detachedSysProcAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mlx_lm server: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// findServerBinary locates the llm_server binary. Search order:
//  1. Next to the sprout executable (e.g. ~/go/bin/llm_server or repo dir)
//  2. Current working directory (common during development: ./llm_server)
//  3. The directory of the Go source tree (derived from executable path
//     when running from a dev checkout)
//  4. PATH
func findServerBinary() (string, error) {
	// 1. Next to the running executable.
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidate := filepath.Join(exeDir, "llm_server")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 2. Current working directory.
	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, "llm_server")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 3. PATH.
	if path, err := exec.LookPath("llm_server"); err == nil {
		return path, nil
	}

	return "", errors.New("llm_server binary not found — build it with 'make build-llm-server' and place it next to the sprout binary, in the current directory, or on PATH")
}

// EnsureServerHealth checks if the server is running and starts it if not,
// using the given model directory. Convenience wrapper for agent integration.
func EnsureServerHealth(ctx context.Context, modelDir string) error {
	return EnsureServerHealthWithBackend(ctx, modelDir, "")
}

// EnsureServerHealthWithBackend is like EnsureServerHealth but specifies
// the server backend ("gomlx" or "mlx_lm").
func EnsureServerHealthWithBackend(ctx context.Context, modelDir, backend string) error {
	status, err := HealthCheck(DefaultPort)
	if err == nil && status.Healthy {
		return nil
	}
	if err := spawnServer(DefaultPort, modelDir, backend); err != nil {
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

func spawnAndWait(ctx context.Context, modelDir string) error {
	return spawnAndWaitWithBackend(ctx, modelDir, "")
}

func spawnAndWaitWithBackend(ctx context.Context, modelDir, backend string) error {
	if err := spawnServer(DefaultPort, modelDir, backend); err != nil {
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
