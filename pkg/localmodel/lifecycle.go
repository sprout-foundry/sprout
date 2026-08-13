//go:build !js

package localmodel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
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
// On Apple Silicon with MLX, and on Linux ARM64 with the GGML backend, this
// loads the model in-process (direct compute call, no HTTP server). On other
// platforms it returns an error.
//
// Safe to call on every request — short-circuits when the model is
// already loaded.
func EnsureServerForProvider(ctx context.Context, providerID string) error {
	if providerID != sproutLocalProviderID {
		return nil
	}
	if !PlatformSupported() {
		return fmt.Errorf("local LLM requires Apple Silicon (M1/M2/M3/M4) or Linux ARM64 with GGML; current platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return EnsureServerForProviderWithCheck(ctx, providerID)
}

// resolveModelForCurrentMachine finds the best model directory and server
// backend for this machine. Prefers installed models; falls back to the
// RAM-recommended catalog entry.
func resolveModelForCurrentMachine() (modelDir string, backend string, err error) {
	if !PlatformSupported() {
		return "", "", fmt.Errorf("local LLM requires Apple Silicon (M1/M2/M3/M4) or Linux ARM64 with GGML; current platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// SPROUT_LOCAL_MODEL pins a specific model, bypassing RAM-based selection.
	// Accepts an absolute path to a model directory, or a path relative to
	// DefaultModelsDir. Useful for comparing models on the same machine.
	if override := strings.TrimSpace(os.Getenv("SPROUT_LOCAL_MODEL")); override != "" {
		dir := override
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(DefaultModelsDir, dir)
		}
		if !hasModelWeights(dir) {
			return "", "", fmt.Errorf("SPROUT_LOCAL_MODEL=%q: no model weights found at %s", override, dir)
		}
		return dir, localBackendMLX, nil
	}

	// Platform-agnostic RAM probe (sysinfo_{darwin,linux}.go) — mlx.TotalSystemRAM
	// is darwin-only and would not link on the Linux/GGML build.
	ram := tensorTotalSystemRAM()

	if rec := RecommendedModel(ram); rec != nil && rec.Installed {
		return rec.Dir, rec.ServerBackend, nil
	}

	if rec := llm.RecommendModelForRAM(ram); rec != nil {
		dir := filepath.Join(DefaultModelsDir, rec.Dir)
		if hasModelWeights(dir) {
			return dir, rec.ServerBackend, nil
		}
	}

	// The RAM recommendation is a suggestion, not a gate: if the suggested
	// model isn't downloaded but others are, use the best installed one
	// rather than refusing to run.
	if best := bestInstalledModel(ram); best != nil {
		return best.Dir, best.ServerBackend, nil
	}

	return "", "", fmt.Errorf("no local model installed in %s — run onboarding (sprout) or llm_download, or set SPROUT_LOCAL_MODEL to a model directory", DefaultModelsDir)
}

// bestInstalledModel picks the most capable installed model, preferring ones
// that fit in RAM and tuned variants over base ones at the same size.
func bestInstalledModel(ram uint64) *ModelStatus {
	models := ListModels()
	var best *ModelStatus
	for i := range models {
		m := &models[i]
		if !m.Installed {
			continue
		}
		if m.MinRAM > 0 && m.MinRAM > ram {
			continue
		}
		if best == nil || betterModel(m, best) {
			best = m
		}
	}
	return best
}

// betterModel reports whether a should be preferred over b: larger parameter
// count first, then tuned over base at the same size.
func betterModel(a, b *ModelStatus) bool {
	av, bv := paramSizeValue(a.ParamSize), paramSizeValue(b.ParamSize)
	if av != bv {
		return av > bv
	}
	return a.IsTuned && !b.IsTuned
}

// paramSizeValue parses a catalog param size ("4b", "2.6b", "0.8b") into a
// comparable number of billions. Unparseable sizes sort last.
func paramSizeValue(s string) float64 {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, "b")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
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
	ram := tensorTotalSystemRAM()
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
