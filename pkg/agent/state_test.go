package agent

import (
	"encoding/json"
	"os"
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
	if len(agent2.taskActions) != 1 {
		t.Errorf("Expected 1 task action after import, got %d", len(agent2.taskActions))
	}

	if agent2.GetSessionID() != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123' after import, got %q", agent2.GetSessionID())
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
	if len(agent2.taskActions) != 1 {
		t.Errorf("Expected 1 task action after loading, got %d", len(agent2.taskActions))
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
	if len(agent.taskActions) != 0 {
		t.Errorf("Expected 0 initial task actions, got %d", len(agent.taskActions))
	}

	// Add first action
	agent.AddTaskAction("file_created", "Created new file", "example.go")
	if len(agent.taskActions) != 1 {
		t.Errorf("Expected 1 task action after adding, got %d", len(agent.taskActions))
	}

	action := agent.taskActions[0]
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
	if len(agent.taskActions) != 2 {
		t.Errorf("Expected 2 task actions after adding second, got %d", len(agent.taskActions))
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
