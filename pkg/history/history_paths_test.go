package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// Reset globals to defaults before any test runs (helps with parallel test safety)
func init() {
	setPathsForTesting(projectChangesDir, projectRevisionsDir)
}

func TestInitializeHistoryPaths_ProjectScope(t *testing.T) {
	// Save original paths and restore after test
	originalChanges, originalRevisions := getPathsForTesting()
	defer setPathsForTesting(originalChanges, originalRevisions)

	// Create a test config with project scope
	config := &configuration.Config{
		HistoryScope: "project",
	}

	// Set to project paths
	setPathsForTesting(projectChangesDir, projectRevisionsDir)

	// Initialize paths
	InitializeHistoryPaths(config)

	// Verify paths are set to project-scoped locations
	currentChanges, currentRevisions := getPathsForTesting()
	if currentChanges != projectChangesDir {
		t.Errorf("Expected changesDir to be %s, got %s", projectChangesDir, currentChanges)
	}
	if currentRevisions != projectRevisionsDir {
		t.Errorf("Expected revisionsDir to be %s, got %s", projectRevisionsDir, currentRevisions)
	}

	// Verify the getters return expected values
	if GetChangesDir() != projectChangesDir {
		t.Errorf("GetChangesDir() returned unexpected value: %s", GetChangesDir())
	}
	if GetRevisionsDir() != projectRevisionsDir {
		t.Errorf("GetRevisionsDir() returned unexpected value: %s", GetRevisionsDir())
	}
}

func TestInitializeHistoryPaths_GlobalScope(t *testing.T) {
	// Save original paths and restore after test
	originalChanges, originalRevisions := getPathsForTesting()
	defer setPathsForTesting(originalChanges, originalRevisions)

	// Create a test config with global scope
	config := &configuration.Config{
		HistoryScope: "global",
	}

	// Get the expected global config directory
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		t.Fatalf("Failed to get config dir: %v", err)
	}
	expectedChanges := filepath.Join(configDir, "changes")
	expectedRevisions := filepath.Join(configDir, "revisions")

	// Initialize paths
	InitializeHistoryPaths(config)

	// Verify paths are set to global-scoped locations
	currentChanges, currentRevisions := getPathsForTesting()
	if currentChanges != expectedChanges {
		t.Errorf("Expected changesDir to be %s, got %s", expectedChanges, currentChanges)
	}
	if currentRevisions != expectedRevisions {
		t.Errorf("Expected revisionsDir to be %s, got %s", expectedRevisions, currentRevisions)
	}

	// Verify the getters return expected values
	if GetChangesDir() != expectedChanges {
		t.Errorf("GetChangesDir() returned unexpected value: %s", GetChangesDir())
	}
	if GetRevisionsDir() != expectedRevisions {
		t.Errorf("GetRevisionsDir() returned unexpected value: %s", GetRevisionsDir())
	}
}

func TestInitializeHistoryPaths_NilConfig(t *testing.T) {
	// Save original paths and restore after test
	originalChanges, originalRevisions := getPathsForTesting()
	defer setPathsForTesting(originalChanges, originalRevisions)

	// Store current directory
	oldDir, _ := os.Getwd()
	t.Cleanup(func() {
		// Restore working directory
		os.Chdir(oldDir)
	})

	// Change to temp directory to test default behavior
	os.Chdir(t.TempDir())

	// Reset to project paths
	setPathsForTesting(projectChangesDir, projectRevisionsDir)

	// Initialize with nil config (should load from file or use default)
	InitializeHistoryPaths(nil)

	// Initialize should succeed without error - just check it didn't crash
	// The paths should be set to something (either project or global based on what config returns)
	currentChanges, currentRevisions := getPathsForTesting()
	if currentChanges == "" {
		t.Error("changesDir should not be empty")
	}
	if currentRevisions == "" {
		t.Error("revisionsDir should not be empty")
	}
}

func TestGetChangesDir_GetRevisionsDir(t *testing.T) {
	// Test the getter functions directly
	setPathsForTesting(".test/changes", ".test/revisions")

	if got := GetChangesDir(); got != ".test/changes" {
		t.Errorf("GetChangesDir() = %s, want .test/changes", got)
	}
	if got := GetRevisionsDir(); got != ".test/revisions" {
		t.Errorf("GetRevisionsDir() = %s, want .test/revisions", got)
	}
}
