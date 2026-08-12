//go:build !js

package localmodel

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// ServerEndpoint is the URL the local LLM server is expected at.
var ServerEndpoint = fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", DefaultPort)

// sproutLocalProviderID is the provider identifier for the local LLM.
const sproutLocalProviderID = "sprout-local"

// idleTimeout is how long the server stays alive with no activity before
// being shut down to reclaim GPU memory. The timer resets on every request.
const idleTimeout = 10 * time.Minute

// lastActivity is updated on every local provider request and checked by
// the idle reaper goroutine.
var (
	activityMu   sync.Mutex
	lastActivity time.Time
	reaperOnce   sync.Once
)

// EnsureServerForProvider is the main entry point for agent integration.
// When the active provider is "sprout-local", this ensures the local LLM
// server is running with the right model for the machine. It is safe to
// call on every request — it short-circuits via health check when the
// server is already healthy.
func EnsureServerForProvider(ctx context.Context, providerID string) error {
	if providerID != sproutLocalProviderID {
		return nil
	}

	if !PlatformSupported() {
		return fmt.Errorf("local LLM requires Apple Silicon (M1/M2/M3/M4) or Linux ARM64 with GGML; current platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Determine which model to load.
	modelDir, backend, err := resolveModelForCurrentMachine()
	if err != nil {
		return fmt.Errorf("resolve local model: %w", err)
	}

	// Ensure the server is up (health check first, spawn if needed).
	if err := EnsureServerHealthWithBackend(ctx, modelDir, backend); err != nil {
		return err
	}

	// Start the idle reaper (once).
	reaperOnce.Do(startIdleReaper)
	return nil
}

// resolveModelForCurrentMachine finds the best model directory and server
// backend for this machine. Prefers installed models; falls back to the
// RAM-recommended catalog entry.
func resolveModelForCurrentMachine() (modelDir string, backend string, err error) {
	ram := tensorTotalSystemRAM()

	// Try to find an installed model that fits the machine.
	if rec := RecommendedModel(ram); rec != nil {
		if rec.Installed {
			return rec.Dir, rec.ServerBackend, nil
		}
		// Model needs download — the server can't start without weights.
		// Return a clear error guiding the user to download it.
		return "", "", fmt.Errorf("recommended model %q is not downloaded yet — run onboarding (sprout) or llm_download to install it", rec.Name)
	}

	// Fall back to the catalog recommendation directly.
	rec := llm.RecommendModelForRAM(ram)
	if rec == nil {
		return "", "", fmt.Errorf("no suitable local model for this machine (%.0f GB RAM)", float64(ram)/1073741824)
	}

	dir := filepath.Join(DefaultModelsDir, rec.Dir)
	if installed := hasModelWeights(dir); installed {
		return dir, rec.ServerBackend, nil
	}

	return "", "", fmt.Errorf("model %q not installed at %s — run onboarding (sprout) or llm_download", rec.Name, dir)
}

// TouchActivity records that the local server is in use, resetting the
// idle timer. Called before each request to the local provider.
func TouchActivity() {
	activityMu.Lock()
	lastActivity = time.Now()
	activityMu.Unlock()
}

// LastActivity returns the timestamp of the last request, or zero if
// the server has never been used.
func LastActivity() time.Time {
	activityMu.Lock()
	defer activityMu.Unlock()
	return lastActivity
}

// startIdleReaper launches a background goroutine that periodically checks
// whether the local LLM server has been idle. After idleTimeout with no
// activity, it shuts down the server to reclaim GPU memory.
func startIdleReaper() {
	// Initialize lastActivity so we don't immediately shut down a freshly
	// started server.
	activityMu.Lock()
	lastActivity = time.Now()
	activityMu.Unlock()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			last := LastActivity()
			if last.IsZero() {
				continue
			}

			if time.Since(last) < idleTimeout {
				continue
			}

			// Server has been idle long enough — check if it's running.
			status, err := HealthCheck(DefaultPort)
			if err != nil || !status.Running {
				continue
			}

			// Shut it down.
			_ = StopServer(DefaultPort)
		}
	}()
}

// IsRunning reports whether the local LLM server is currently healthy.
func IsRunning() bool {
	status, err := HealthCheck(DefaultPort)
	return err == nil && status.Healthy
}

// ServerModelDir returns the model directory of the running server, or
// empty string if the server isn't running.
func ServerModelDir() string {
	status, err := HealthCheck(DefaultPort)
	if err != nil || !status.Healthy {
		return ""
	}
	return status.Model
}

// markServerActivityForTest exposes the activity setter for tests.
func markServerActivityForTest(t time.Time) {
	activityMu.Lock()
	lastActivity = t
	activityMu.Unlock()
}

// ensureServerBinaryExists is a pre-flight check that the llm_server
// binary is findable, returning a helpful error if not.
func ensureServerBinaryExists() error {
	if _, err := findServerBinary(); err != nil {
		return fmt.Errorf("llm_server binary not found: %w — build it with 'make build-llm-server'", err)
	}
	return nil
}

// EnsureServerForProviderWithCheck is like EnsureServerForProvider but
// also verifies the binary exists, giving a better error message before
// attempting to spawn.
func EnsureServerForProviderWithCheck(ctx context.Context, providerID string) error {
	if providerID != sproutLocalProviderID {
		return nil
	}
	if err := ensureServerBinaryExists(); err != nil {
		return err
	}
	return EnsureServerForProvider(ctx, providerID)
}

// ResetIdleForTest resets the idle reaper state. For tests only.
func ResetIdleForTest() {
	activityMu.Lock()
	lastActivity = time.Time{}
	activityMu.Unlock()
}

// IsServerPresent checks if the llm_server binary exists without starting
// anything. Used by provider readiness checks.
func IsServerPresent() bool {
	if _, err := findServerBinary(); err == nil {
		return true
	}
	// Also check if a server is already running (e.g. started manually).
	if status, err := HealthCheck(DefaultPort); err == nil && status.Running {
		return true
	}
	return false
}

// HasInstalledModel reports whether any model is installed locally,
// meaning the server could be started right now without a download.
func HasInstalledModel() bool {
	ram := tensorTotalSystemRAM()
	if rec := RecommendedModel(ram); rec != nil && rec.Installed {
		return true
	}
	// Check catalog entries.
	for _, m := range ListModels() {
		if m.Installed {
			return true
		}
	}
	return false
}

// PlatformSupported reports whether the local LLM engine is supported on
// this platform (Linux ARM64, or Apple Silicon).
func PlatformSupported() bool {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return true
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		return true
	}
	return false
}
