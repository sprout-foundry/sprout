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
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// sproutLocalProviderID is the provider identifier for the local LLM.
const sproutLocalProviderID = "sprout-local"

// idleTimeout is how long the model stays loaded with no activity before
// being unloaded to reclaim GPU memory. The timer resets on every request.
const idleTimeout = 10 * time.Minute

// lastActivity tracks the last local provider request for idle management.
var (
	activityMu   sync.Mutex
	lastActivity time.Time
	reaperOnce   sync.Once
)

// EnsureServerForProvider ensures the local model is loaded and ready.
// On Apple Silicon with MLX, this loads the model in-process (direct GPU
// call, no HTTP server). On other platforms it returns an error.
//
// Safe to call on every request — short-circuits when the model is
// already loaded.
func EnsureServerForProvider(ctx context.Context, providerID string) error {
	if providerID != sproutLocalProviderID {
		return nil
	}
	return EnsureServerForProviderWithCheck(ctx, providerID)
}

// resolveModelForCurrentMachine finds the best model directory and server
// backend for this machine. Prefers installed models; falls back to the
// RAM-recommended catalog entry.
func resolveModelForCurrentMachine() (modelDir string, backend string, err error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return "", "", fmt.Errorf("local LLM requires Apple Silicon (M1/M2/M3/M4); current platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	ram := mlx.TotalSystemRAM()

	if rec := RecommendedModel(ram); rec != nil {
		if rec.Installed {
			return rec.Dir, rec.ServerBackend, nil
		}
		return "", "", fmt.Errorf("recommended model %q is not downloaded yet — run onboarding (sprout) or llm_download to install it", rec.Name)
	}

	rec := llm.RecommendModelForRAM(ram)
	if rec == nil {
		return "", "", fmt.Errorf("no suitable local model for this machine (%.0f GB RAM)", float64(ram)/1073741824)
	}

	dir := filepath.Join(DefaultModelsDir, rec.Dir)
	if installed := hasModelWeights(dir); installed {
		return dir, rec.ServerBackend, nil
	}

	return "", "", fmt.Errorf("model %q not installed at %s — run onboarding or llm_download", rec.Name, dir)
}

// TouchActivity records that the local model is in use, resetting the
// idle timer. Called before each request to the local provider.
func TouchActivity() {
	activityMu.Lock()
	lastActivity = time.Now()
	activityMu.Unlock()
}

// LastActivity returns the timestamp of the last request, or zero if
// the model has never been used.
func LastActivity() time.Time {
	activityMu.Lock()
	defer activityMu.Unlock()
	return lastActivity
}

// startIdleReaper launches a background goroutine that periodically checks
// whether the local model has been idle. After idleTimeout with no activity,
// it unloads the model to reclaim GPU memory.
func startIdleReaper() {
	activityMu.Lock()
	lastActivity = time.Now()
	activityMu.Unlock()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			last := LastActivity()
			if last.IsZero() || time.Since(last) < idleTimeout {
				continue
			}

			// Unload the model to free GPU memory. It will be reloaded
			// lazily on the next request.
			if globalProvider != nil {
				_ = globalProvider.Close()
			}
		}
	}()
}

// IsRunning reports whether the local model is loaded.
func IsRunning() bool {
	return globalProvider != nil && globalProvider.isModelLoaded()
}

// ServerModelDir returns the model directory of the loaded model, or
// empty string if no model is loaded.
func ServerModelDir() string {
	if globalProvider != nil {
		return globalProvider.loadedModelDir()
	}
	return ""
}

// markServerActivityForTest exposes the activity setter for tests.
func markServerActivityForTest(t time.Time) {
	activityMu.Lock()
	lastActivity = t
	activityMu.Unlock()
}

// EnsureServerForProviderWithCheck loads the local model in-process.
// No HTTP server is spawned — the model runs directly via MLX.
// This is the proactive pre-load path called when the user switches to
// sprout-local. Returns nil if the model is already loaded.
func EnsureServerForProviderWithCheck(ctx context.Context, providerID string) error {
	if providerID != sproutLocalProviderID {
		return nil
	}

	// Verify a model is available before attempting to load.
	if _, _, err := resolveModelForCurrentMachine(); err != nil {
		return err
	}

	// Load the model in-process via the singleton provider.
	p := GetLocalProvider()
	if err := p.CheckConnection(); err != nil {
		return err
	}

	// Start the idle reaper (once).
	reaperOnce.Do(startIdleReaper)
	return nil
}

// ResetIdleForTest resets the idle reaper state.
func ResetIdleForTest() {
	activityMu.Lock()
	lastActivity = time.Time{}
	activityMu.Unlock()
}

// IsServerPresent reports whether any local model backend is available.
// Used by provider readiness checks.
func IsServerPresent() bool {
	return PlatformSupported() && HasInstalledModel()
}

// HasInstalledModel reports whether any model is installed locally.
func HasInstalledModel() bool {
	ram := mlx.TotalSystemRAM()
	if rec := RecommendedModel(ram); rec != nil && rec.Installed {
		return true
	}
	for _, m := range ListModels() {
		if m.Installed {
			return true
		}
	}
	return false
}

// PlatformSupported reports whether the local LLM engine is supported on
// this platform (Apple Silicon only for now).
func PlatformSupported() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}
