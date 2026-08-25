//go:build !js

package tools

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// TestGetBackgroundOutputBaseDir — package-level helper returns default path
// =============================================================================

func TestGetBackgroundOutputBaseDir(t *testing.T) {
	t.Parallel()

	result := GetBackgroundOutputBaseDir()
	assert.NotEmpty(t, result, "GetBackgroundOutputBaseDir should return a non-empty path")
	assert.True(t, len(result) > len("bg-processes"), "path should be longer than just 'bg-processes'")
	assert.Contains(t, result, "bg-processes", "path should contain 'bg-processes'")
}

// =============================================================================
// TestBPM_GetBaseDir — matches GetBackgroundOutputBaseDir
// =============================================================================

func TestBPM_GetBaseDir(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	actual := bpm.GetBaseDir()
	assert.NotEmpty(t, actual, "BPM's GetBaseDir should return a non-empty path")
	assert.Equal(t, GetBackgroundOutputBaseDir(), actual,
		"BPM's GetBaseDir should match GetBackgroundOutputBaseDir")
}

// =============================================================================
// TestBPM_GetBaseDir_MatchesPackageHelper — cross-check both return the same
// =============================================================================

func TestBPM_GetBaseDir_MatchesPackageHelper(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	assert.Equal(t, GetBackgroundOutputBaseDir(), bpm.GetBaseDir(),
		"bpm.GetBaseDir and GetBackgroundOutputBaseDir should return the same path")
}

// =============================================================================
// TestBPM_GetBaseDir_DirectoryExists — base directory should exist after construction
// =============================================================================

func TestBPM_GetBaseDir_DirectoryExists(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	info, err := os.Stat(bpm.GetBaseDir())
	assert.NoError(t, err, "base directory should exist after BPM construction")
	assert.True(t, info.IsDir(), "base path should be a directory")
}
