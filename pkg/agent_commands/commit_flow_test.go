package commands

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
)

func TestCommitFlow_NewCommitFlow(t *testing.T) {
	// Create a mock agent for testing
	agent := &agent.Agent{} // This will be nil in practice, but good for structure test

	flow := NewCommitFlow(agent)

	if flow == nil {
		t.Fatal("Expected NewCommitFlow to return non-nil flow")
	}

	if flow.agent != agent {
		t.Error("Expected flow.agent to be set to the provided agent")
	}

	if flow.optimizer == nil {
		t.Error("Expected flow.optimizer to be initialized")
	}
}

func TestCommitActionItem_Interface(t *testing.T) {
	item := &CommitActionItem{
		ID:          "test_action",
		DisplayName: "Test Action",
		Description: "This is a test action",
	}

	if item.Display() != "Test Action" {
		t.Errorf("Expected Display() to return 'Test Action', got '%s'", item.Display())
	}

	searchText := item.SearchText()
	if searchText != "Test Action This is a test action" {
		t.Errorf("Expected SearchText() to return combined text, got '%s'", searchText)
	}

	if item.Value() != "test_action" {
		t.Errorf("Expected Value() to return 'test_action', got '%s'", item.Value())
	}
}

func TestFileItem_Interface(t *testing.T) {
	item := &FileItem{
		Filename:    "test.go",
		Description: "📝 Modified Go file",
	}

	if item.Display() != "📝 Modified Go file" {
		t.Errorf("Expected Display() to return description, got '%s'", item.Display())
	}

	searchText := item.SearchText()
	expected := "test.go 📝 Modified Go file"
	if searchText != expected {
		t.Errorf("Expected SearchText() to return '%s', got '%s'", expected, searchText)
	}

	if item.Value() != "test.go" {
		t.Errorf("Expected Value() to return 'test.go', got '%s'", item.Value())
	}
}

func TestCommitFlow_BuildCommitActions(t *testing.T) {
	flow := NewCommitFlow(nil) // Agent can be nil for this test

	// Test with no files
	actions := flow.buildCommitActions([]string{}, []string{})
	if len(actions) != 0 {
		t.Errorf("Expected no actions for empty file lists, got %d", len(actions))
	}

	// Test with staged files only
	actions = flow.buildCommitActions([]string{"staged.go"}, []string{})
	if len(actions) != 1 {
		t.Errorf("Expected 1 action for staged files only, got %d", len(actions))
	}
	if actions[0].ID != "commit_staged" {
		t.Errorf("Expected first action to be 'commit_staged', got '%s'", actions[0].ID)
	}

	// Test with unstaged files only
	actions = flow.buildCommitActions([]string{}, []string{"unstaged.go"})
	if len(actions) != 2 { // select_files and commit_all
		t.Errorf("Expected 2 actions for unstaged files only, got %d", len(actions))
	}

	// Test with both staged and unstaged files
	actions = flow.buildCommitActions([]string{"staged.go"}, []string{"unstaged.go", "another.go"})
	if len(actions) != 4 { // commit_staged, select_files, commit_all, single_file
		t.Errorf("Expected 4 actions for mixed files, got %d", len(actions))
	}
}
