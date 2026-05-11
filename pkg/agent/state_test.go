package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestExportImportState tests state export and import functionality
func TestExportImportState(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Add some test data
	agent.AddTaskAction("file_created", "Created test file", "test.go")
	agent.SetPreviousSummary("Test summary")
	agent.SetSessionID("test-session-123")
	agent.state.SetTurnCheckpoints([]TurnCheckpoint{{
		StartIndex: 0,
		EndIndex:   1,
		Summary:    "Compacted earlier conversation state:\n- Latest compacted user request: test",
	}})

	// Export state
	stateData, err := agent.ExportState()
	if err != nil {
		t.Fatalf("Failed to export state: %v", err)
	}

	if len(stateData) == 0 {
		t.Error("Expected non-empty state data")
	}

	// Verify it's valid JSON
	var state AgentState
	err = json.Unmarshal(stateData, &state)
	if err != nil {
		t.Fatalf("Exported state is not valid JSON: %v", err)
	}

	// Check exported data
	if len(state.TaskActions) != 1 {
		t.Errorf("Expected 1 task action, got %d", len(state.TaskActions))
	}

	if state.SessionID != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123', got %q", state.SessionID)
	}
	if len(state.TurnCheckpoints) != 1 {
		t.Errorf("Expected 1 turn checkpoint, got %d", len(state.TurnCheckpoints))
	}

	// Create new agent and import state
	agent2, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	err = agent2.ImportState(stateData)
	if err != nil {
		t.Fatalf("Failed to import state: %v", err)
	}

	// Verify imported data
	actions := agent2.GetTaskActions()
	if len(actions) != 1 {
		t.Errorf("Expected 1 task action after import, got %d", len(actions))
	}

	if agent2.GetSessionID() != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123' after import, got %q", agent2.GetSessionID())
	}
	if len(agent2.state.GetTurnCheckpoints()) != 1 {
		t.Errorf("Expected 1 turn checkpoint after import, got %d", len(agent2.state.GetTurnCheckpoints()))
	}
}

// TestSaveLoadStateToFile tests file-based state persistence
func TestSaveLoadStateToFile(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Add test data
	agent.AddTaskAction("file_modified", "Modified test file", "test.go")
	agent.SetSessionID("file-test-session")

	// Save to file
	testFile := "test_state.json"
	err = agent.SaveStateToFile(testFile)
	if err != nil {
		t.Fatalf("Failed to save state to file: %v", err)
	}
	defer os.Remove(testFile)

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("State file was not created")
	}

	// Create new agent and load state
	agent2, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	err = agent2.LoadStateFromFile(testFile)
	if err != nil {
		t.Fatalf("Failed to load state from file: %v", err)
	}

	// Verify loaded data
	actions := agent2.GetTaskActions()
	if len(actions) != 1 {
		t.Errorf("Expected 1 task action after loading, got %d", len(actions))
	}

	if agent2.GetSessionID() != "file-test-session" {
		t.Errorf("Expected session ID 'file-test-session' after loading, got %q", agent2.GetSessionID())
	}
}

// TestAddTaskAction tests task action recording
func TestAddTaskAction(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Initially should have no actions
	actions := agent.GetTaskActions()
	if len(actions) != 0 {
		t.Errorf("Expected 0 initial task actions, got %d", len(actions))
	}

	// Add first action
	agent.AddTaskAction("file_created", "Created new file", "example.go")
	actions = agent.GetTaskActions()
	if len(actions) != 1 {
		t.Errorf("Expected 1 task action after adding, got %d", len(actions))
	}

	action := actions[0]
	if action.Type != "file_created" {
		t.Errorf("Expected action type 'file_created', got %q", action.Type)
	}
	if action.Description != "Created new file" {
		t.Errorf("Expected description 'Created new file', got %q", action.Description)
	}
	if action.Details != "example.go" {
		t.Errorf("Expected details 'example.go', got %q", action.Details)
	}

	// Add second action
	agent.AddTaskAction("file_modified", "Modified existing file", "example.go")
	actions = agent.GetTaskActions()
	if len(actions) != 2 {
		t.Errorf("Expected 2 task actions after adding second, got %d", len(actions))
	}
}

func TestAddTaskActionConcurrent(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			agent.AddTaskAction("file_read", fmt.Sprintf("Read file %d", index), fmt.Sprintf("file-%d.txt", index))
		}(i)
	}

	wg.Wait()

	actions := agent.GetTaskActions()
	if len(actions) != workers {
		t.Fatalf("Expected %d task actions after concurrent adds, got %d", workers, len(actions))
	}

	seen := make(map[string]bool, workers)
	for _, action := range actions {
		seen[action.Details] = true
	}

	for i := 0; i < workers; i++ {
		key := fmt.Sprintf("file-%d.txt", i)
		if !seen[key] {
			t.Fatalf("Missing task action for %s", key)
		}
	}
}

// TestGenerateActionSummary tests action summary generation
func TestGenerateActionSummary(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Test with no actions
	summary := agent.GenerateActionSummary()
	expected := "No actions completed yet."
	if summary != expected {
		t.Errorf("Expected %q for empty actions, got %q", expected, summary)
	}

	// Add some actions
	agent.AddTaskAction("file_created", "Created test file", "test.go")
	agent.AddTaskAction("file_modified", "Updated test file", "test.go")

	summary = agent.GenerateActionSummary()
	if summary == expected {
		t.Error("Expected non-empty summary after adding actions")
	}

	// Check that summary contains action information
	if !contains(summary, "file_created") {
		t.Error("Expected summary to contain 'file_created'")
	}
	if !contains(summary, "file_modified") {
		t.Error("Expected summary to contain 'file_modified'")
	}
	if !contains(summary, "test.go") {
		t.Error("Expected summary to contain 'test.go'")
	}
}

// TestSessionIDMethods tests session ID getter and setter
func TestSessionIDMethods(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Initially should be empty
	if agent.GetSessionID() != "" {
		t.Errorf("Expected empty session ID initially, got %q", agent.GetSessionID())
	}

	// Set session ID
	testID := "test-session-456"
	agent.SetSessionID(testID)

	if agent.GetSessionID() != testID {
		t.Errorf("Expected session ID %q, got %q", testID, agent.GetSessionID())
	}
}

// TestPreviousSummaryMethods tests previous summary getter and setter
func TestPreviousSummaryMethods(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Set previous summary
	testSummary := "This is a test summary of previous actions."
	agent.SetPreviousSummary(testSummary)

	if agent.GetPreviousSummary() != testSummary {
		t.Errorf("Expected summary %q, got %q", testSummary, agent.GetPreviousSummary())
	}
}

// TestValidateStateFilePath tests the validateStateFilePath function
// for path traversal and security validation.
func TestValidateStateFilePath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		// Valid cases
		{"simple filename", ".coder_state.json", false},
		{"regular filename", "test_state.json", false},
		{"filename with multiple dots", "my.state.file.json", false},
		{"hidden file with dot prefix", ".hidden_state", false},
		{"whitespace trimmed to valid", "  test_state.json  ", false},

		// Invalid cases: empty/whitespace
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"tab only", "\t", true},
		{"newline only", "\n", true},

		// Invalid cases: absolute paths
		{"absolute Unix path", "/etc/passwd", true},

		// Note: On Linux, backslashes are valid filename characters,
		// so Windows paths are NOT rejected. This is expected behavior
		// on Unix-like systems (filepath.IsAbs uses OS-specific rules).

		// Invalid cases: null bytes
		{"null byte in filename", "state\x00.json", true},
		{"null byte path traversal", "state.json\x00/../../etc/passwd", true},

		// Invalid cases: path traversal
		{"parent traversal", "../../etc/passwd", true},
		{"disguised traversal", "foo/../../bar", true},
		{"double dot alone", "..", true},
		{"complex traversal", "./../../../tmp/evil", true},

		// Edge case: single dot (filepath.Clean(".") = ".", which is a no-op for directory writes)
		{"single dot current dir", ".", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateStateFilePath(tt.input)
			if tt.expectErr && err == nil {
				t.Errorf("expected error for input %q, got nil", tt.input)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error for input %q, got: %v", tt.input, err)
			}
		})
	}

	// Test symlink rejection (requires filesystem setup)
	t.Run("symlink rejected", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "target.txt")
		link := filepath.Join(tmpDir, "link_state.json")
		if err := os.WriteFile(target, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create target: %v", err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}
		// Change to temp dir so the relative symlink path resolves
		origDir, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}
		defer os.Chdir(origDir)
		_, err := validateStateFilePath("link_state.json")
		if err == nil {
			t.Error("expected error for symlink path, got nil")
		}
	})
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsHelper(s, substr))))
}

func containsHelper(s, substr string) bool {
	for i := 1; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
