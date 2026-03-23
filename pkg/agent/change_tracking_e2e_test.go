package agent

import (
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/history"
)

// TestChangeTrackingE2E tests the end-to-end change tracking and rollback workflow
func TestChangeTrackingE2E(t *testing.T) {
	// Test constants for test file names and content
	const (
		testFileName    = "tracking_test.go"
		originalContent = "func original() {}"
		newContent      = "func updated() {}"
	)

	// Setup test directory using t.TempDir() for automatic cleanup
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()

	// Set environment variables for testing
	os.Setenv("LEDIT_TEST_ENV", "1")
	os.Setenv("OPENROUTER_API_KEY", "test-key-for-testing")

	// Restore environment and directory in all cases
	defer func() {
		os.Unsetenv("LEDIT_TEST_ENV")
		os.Unsetenv("OPENROUTER_API_KEY")
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()

	// Change to test directory with error handling
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}

	// Create an agent with change tracking enabled
	instructions := "Update test file with new content"
	agent, err := NewAgentWithModel("deepseek/deepseek-chat-v3.1:free")
	if err != nil {
		// If agent creation fails, at least create a change tracker directly
		// This allows us to test the change tracking even without a full agent
		t.Logf("Agent creation failed: %v. Creating tracker directly.", err)
		agent = &Agent{}
		agent.changeTracker = NewChangeTracker(nil, instructions)
		agent.changeTracker.Enable()
	} else {
		agent.EnableChangeTracking(instructions)
	}

	if agent.changeTracker == nil {
		agent.changeTracker = NewChangeTracker(agent, instructions)
		agent.changeTracker.Enable()
	}

	// Verify change tracking is enabled
	if !agent.IsChangeTrackingEnabled() {
		t.Fatal("Change tracking should be enabled")
	}

	// Create a test file and track changes to it
	errWrite := os.WriteFile(testFileName, []byte(originalContent), 0644)
	if errWrite != nil {
		t.Fatalf("Failed to create test file: %v", errWrite)
	}

	// Track a file write (simulating WriteFile tool)
	err = agent.TrackFileWrite(testFileName, newContent)
	if err != nil {
		t.Fatalf("Failed to track file write: %v", err)
	}

	// Verify change was tracked
	if agent.GetChangeCount() != 1 {
		t.Fatalf("Expected 1 tracked change, got %d", agent.GetChangeCount())
	}

	trackedFiles := agent.GetTrackedFiles()
	if len(trackedFiles) != 1 || trackedFiles[0] != testFileName {
		t.Fatalf("Expected tracked file %s, got %v", testFileName, trackedFiles)
	}

	// Modify the actual file to simulate the tool making the change
	err = os.WriteFile(testFileName, []byte(newContent), 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Verify file content is modified
	currentContent, err := os.ReadFile(testFileName)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}
	if string(currentContent) != newContent {
		t.Fatalf("File content should be modified. Expected: %s, Got: %s", newContent, string(currentContent))
	}

	// Commit the changes
	llmResponse := "Changes applied successfully"
	err = agent.CommitChanges(llmResponse)
	if err != nil {
		t.Fatalf("Failed to commit changes: %v", err)
	}

	// Verify get revision ID was generated
	revisionID := agent.GetRevisionID()
	if revisionID == "" {
		t.Fatal("Revision ID should be set after commit")
	}

	// Verify changes were saved to the history system
	allChanges, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("Failed to fetch changes from history: %v", err)
	}
	if len(allChanges) != 1 {
		t.Fatalf("Expected 1 change in history, got %d", len(allChanges))
	}

	change := allChanges[0]
	if change.Filename != testFileName {
		t.Fatalf("Expected filename %s, got %s", testFileName, change.Filename)
	}
	if change.OriginalCode != originalContent {
		t.Fatalf("Expected original code %s, got %s", originalContent, change.OriginalCode)
	}
	if change.NewCode != newContent {
		t.Fatalf("Expected new code %s, got %s", newContent, change.NewCode)
	}
	if change.Status != "active" {
		t.Fatalf("Expected status 'active', got %s", change.Status)
	}

	// Perform rollback
	err = history.RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Failed to rollback changes: %v", err)
	}

	// Verify file was restored
	restoredContent, err := os.ReadFile(testFileName)
	if err != nil {
		t.Fatalf("Failed to read restored file: %v", err)
	}
	if string(restoredContent) != originalContent {
		t.Fatalf("File content should be restored. Expected: %s, Got: %s", originalContent, string(restoredContent))
	}

	// Verify the change status is now "reverted"
	changesAfterRollback, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("Failed to fetch changes after rollback: %v", err)
	}
	found := false
	for _, c := range changesAfterRollback {
		if c.RequestHash == revisionID {
			if c.Status != "reverted" {
				t.Fatalf("Change status should be 'reverted', got: %s", c.Status)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Could not find the change after rollback")
	}

	// Verify we can restore the change back
	err = history.RevertChangeByRevisionID(revisionID)
	if err == nil {
		t.Fatal("Should not be able to rollback reverted changes (no active changes)")
	}

	t.Log("[OK] End-to-end change tracking and rollback test passed!")
}

func TestChangeTrackingSupportsIncrementalCommits(t *testing.T) {
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldDir)
	}()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("change dir: %v", err)
	}

	agent := &Agent{}
	agent.changeTracker = NewChangeTracker(agent, "Make a series of edits")
	agent.changeTracker.Enable()

	fileA := "file_a.go"
	fileB := "file_b.go"

	if err := os.WriteFile(fileA, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write fileA: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write fileB: %v", err)
	}

	if err := agent.TrackFileWrite(fileA, "package main\nfunc a() {}\n"); err != nil {
		t.Fatalf("track fileA: %v", err)
	}
	if err := agent.CommitChanges("checkpoint 1"); err != nil {
		t.Fatalf("commit checkpoint 1: %v", err)
	}
	revisionID := agent.GetRevisionID()
	if revisionID == "" {
		t.Fatal("expected revision ID after first checkpoint")
	}

	if err := agent.TrackFileWrite(fileB, "package main\nfunc b() {}\n"); err != nil {
		t.Fatalf("track fileB: %v", err)
	}
	if err := agent.CommitChanges("checkpoint 2"); err != nil {
		t.Fatalf("commit checkpoint 2: %v", err)
	}

	allChanges, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("fetch changes: %v", err)
	}
	if len(allChanges) != 2 {
		t.Fatalf("expected 2 persisted changes after incremental commits, got %d", len(allChanges))
	}

	foundA := false
	foundB := false
	for _, change := range allChanges {
		if change.RequestHash != revisionID {
			t.Fatalf("expected change revision %s, got %s", revisionID, change.RequestHash)
		}
		switch change.Filename {
		case fileA:
			foundA = true
		case fileB:
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Fatalf("expected both tracked files to be persisted, foundA=%v foundB=%v", foundA, foundB)
	}
}
