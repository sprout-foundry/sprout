package history

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
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

	// SP-133: "global" history scope is no longer supported.
	// changes/ and revisions/ are always workspace-local.
	// Verify that even with HistoryScope="global", the paths remain
	// project-scoped.
	config := &configuration.Config{
		HistoryScope: "global",
	}

	// Initialize paths
	InitializeHistoryPaths(config)

	// Verify paths remain project-scoped regardless of HistoryScope
	currentChanges, currentRevisions := getPathsForTesting()
	if currentChanges != projectChangesDir {
		t.Errorf("Expected changesDir to be %s (always workspace-local), got %s", projectChangesDir, currentChanges)
	}
	if currentRevisions != projectRevisionsDir {
		t.Errorf("Expected revisionsDir to be %s (always workspace-local), got %s", projectRevisionsDir, currentRevisions)
	}
}

func TestInitializeHistoryPaths_NilConfig(t *testing.T) {
	// Save original paths and restore after test
	originalChanges, originalRevisions := getPathsForTesting()
	defer setPathsForTesting(originalChanges, originalRevisions)

	// SP-133: InitializeHistoryPaths no longer loads config — nil is fine.
	// The paths should always be project-scoped.
	InitializeHistoryPaths(nil)

	currentChanges, currentRevisions := getPathsForTesting()
	if currentChanges != projectChangesDir {
		t.Errorf("Expected changesDir to be %s, got %s", projectChangesDir, currentChanges)
	}
	if currentRevisions != projectRevisionsDir {
		t.Errorf("Expected revisionsDir to be %s, got %s", projectRevisionsDir, currentRevisions)
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
