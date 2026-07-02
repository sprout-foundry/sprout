//go:build !js

package tools

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// =============================================================================
// SP-103-C3 — VisionConfig tests
//
// Exercises VisionConfig.Resolve() defaults, clamping, and the
// getVisionParallelWorkers precedence chain:
//   1. config.json vision.parallel_workers (highest)
//   2. VISION_PARALLEL_WORKERS env var
//   3. Hard-coded default of 3
// =============================================================================

// Test 1: Resolve zero-valued VisionConfig → defaults populated
func TestVisionConfig_Resolve_Defaults(t *testing.T) {
	var c *configuration.VisionConfig // nil receiver
	resolved := c.Resolve()

	if resolved.ParallelWorkers != 3 {
		t.Errorf("ParallelWorkers: expected 3 (default), got %d", resolved.ParallelWorkers)
	}
	if resolved.MaxParallelRequests != 8 {
		t.Errorf("MaxParallelRequests: expected 8 (default), got %d", resolved.MaxParallelRequests)
	}
	if !resolved.EnableBatchProcessing {
		t.Error("EnableBatchProcessing: expected true (default), got false")
	}
	if resolved.MaxBatchSize != 4 {
		t.Errorf("MaxBatchSize: expected 4 (default), got %d", resolved.MaxBatchSize)
	}
}

// Test 2: Resolve with out-of-range values → clamped
func TestVisionConfig_Resolve_Clamping(t *testing.T) {
	c := &configuration.VisionConfig{
		ParallelWorkers:       -5,  // below min
		MaxParallelRequests:   200, // above max
		MaxBatchSize:          20,  // above max
		EnableBatchProcessing: false,
	}
	resolved := c.Resolve()

	if resolved.ParallelWorkers != 3 {
		t.Errorf("ParallelWorkers: expected 3 (default, negative treated as zero), got %d", resolved.ParallelWorkers)
	}
	if resolved.MaxParallelRequests != 64 {
		t.Errorf("MaxParallelRequests: expected 64 (clamped from 200), got %d", resolved.MaxParallelRequests)
	}
	if resolved.MaxBatchSize != 8 {
		t.Errorf("MaxBatchSize: expected 8 (clamped from 20), got %d", resolved.MaxBatchSize)
	}
	if resolved.EnableBatchProcessing {
		t.Error("EnableBatchProcessing: expected false (explicitly set), got true")
	}
}

// Test 3: Resolve with in-range values → preserved
func TestVisionConfig_Resolve_InRange(t *testing.T) {
	c := &configuration.VisionConfig{
		ParallelWorkers:       10,
		MaxParallelRequests:   16,
		MaxBatchSize:          6,
		EnableBatchProcessing: true,
	}
	resolved := c.Resolve()

	if resolved.ParallelWorkers != 10 {
		t.Errorf("ParallelWorkers: expected 10, got %d", resolved.ParallelWorkers)
	}
	if resolved.MaxParallelRequests != 16 {
		t.Errorf("MaxParallelRequests: expected 16, got %d", resolved.MaxParallelRequests)
	}
	if resolved.MaxBatchSize != 6 {
		t.Errorf("MaxBatchSize: expected 6, got %d", resolved.MaxBatchSize)
	}
	if !resolved.EnableBatchProcessing {
		t.Error("EnableBatchProcessing: expected true, got false")
	}
}

// Test 4: getVisionParallelWorkers falls back to env var when config is zero
func TestGetVisionParallelWorkers_EnvVarFallback(t *testing.T) {
	// When the persisted config's Vision.ParallelWorkers is 0 (not set),
	// the env var should still be consulted. The test environment's
	// config file has no vision.parallel_workers set, so GetVisionConfig()
	// returns a zero-valued ParallelWorkers.
	setEnv := func(key, val string) {
		t.Setenv("SPROUT_"+key, val)
		t.Setenv("LEDIT_"+key, val)
	}

	// With env var set, it should be picked up.
	setEnv("VISION_PARALLEL_WORKERS", "7")
	if got := getVisionParallelWorkers(); got != 7 {
		t.Errorf("env var 7: expected 7, got %d", got)
	}

	// With env var unset, default to 3.
	setEnv("VISION_PARALLEL_WORKERS", "")
	if got := getVisionParallelWorkers(); got != 3 {
		t.Errorf("env var unset: expected 3, got %d", got)
	}
}
