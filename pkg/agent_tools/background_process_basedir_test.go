//go:build !js

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// TestGetBackgroundOutputBaseDir — package-level helper returns default path
// =============================================================================

func TestGetBackgroundOutputBaseDir(t *testing.T) {
	t.Parallel()

	expected := filepath.Join(os.TempDir(), "sprout-bg")
	result := GetBackgroundOutputBaseDir()
	assert.Equal(t, expected, result, "GetBackgroundOutputBaseDir should return <temp>/sprout-bg")
}

// =============================================================================
// TestBPM_GetBaseDir — matches the default
// =============================================================================

func TestBPM_GetBaseDir(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	expected := filepath.Join(os.TempDir(), "sprout-bg")
	actual := bpm.GetBaseDir()
	assert.Equal(t, expected, actual, "BPM's GetBaseDir should match the default from GetBackgroundOutputBaseDir")
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
