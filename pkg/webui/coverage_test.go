package webui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// TestEmptyAgentStateSnapshot tests that emptyAgentStateSnapshot returns
// a valid JSON representation of an empty agent state.
func TestEmptyAgentStateSnapshot(t *testing.T) {
	data := emptyAgentStateSnapshot()

	if len(data) == 0 {
		t.Fatal("emptyAgentStateSnapshot returned empty bytes")
	}

	// Verify it's valid JSON for an empty agent state
	var state agent.AgentState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to unmarshal empty agent state: %v", err)
	}

	// Messages should be an empty slice
	if state.Messages == nil {
		t.Error("Messages should not be nil, should be an empty slice")
	}

	if len(state.Messages) != 0 {
		t.Errorf("Messages should be empty, got %d messages", len(state.Messages))
	}
}

// TestGetAllGitFiles tests that getAllGitFiles correctly aggregates files
// from all sections of a GitStatus.
func TestGetAllGitFiles(t *testing.T) {
	tests := []struct {
		name     string
		status   *GitStatus
		expected []string
	}{
		{
			name: "all sections populated",
			status: &GitStatus{
				Staged: []GitFile{
					{Path: "staged1.txt", Status: "M"},
					{Path: "staged2.txt", Status: "A"},
				},
				Modified: []GitFile{
					{Path: "modified1.txt", Status: "M"},
				},
				Untracked: []GitFile{
					{Path: "untracked1.txt", Status: "?"},
				},
				Deleted: []GitFile{
					{Path: "deleted1.txt", Status: "D"},
				},
				Renamed: []GitFile{
					{Path: "renamed1.txt", Status: "R"},
				},
			},
			expected: []string{
				"staged1.txt", "staged2.txt",
				"modified1.txt",
				"untracked1.txt",
				"deleted1.txt",
				"renamed1.txt",
			},
		},
		{
			name: "empty status",
			status: &GitStatus{
				Staged:    []GitFile{},
				Modified:  []GitFile{},
				Untracked: []GitFile{},
				Deleted:   []GitFile{},
				Renamed:   []GitFile{},
			},
			expected: []string{},
		},
		{
			name: "nil sections",
			status: &GitStatus{
				Staged:    nil,
				Modified:  nil,
				Untracked: nil,
				Deleted:   nil,
				Renamed:   nil,
			},
			expected: []string{},
		},
		{
			name: "only staged files",
			status: &GitStatus{
				Staged: []GitFile{
					{Path: "file1.txt", Status: "M"},
					{Path: "file2.txt", Status: "A"},
				},
			},
			expected: []string{"file1.txt", "file2.txt"},
		},
		{
			name: "only modified files",
			status: &GitStatus{
				Modified: []GitFile{
					{Path: "modified.txt", Status: "M"},
				},
			},
			expected: []string{"modified.txt"},
		},
		{
			name: "only untracked files",
			status: &GitStatus{
				Untracked: []GitFile{
					{Path: "newfile.txt", Status: "?"},
				},
			},
			expected: []string{"newfile.txt"},
		},
		{
			name: "only deleted files",
			status: &GitStatus{
				Deleted: []GitFile{
					{Path: "removed.txt", Status: "D"},
				},
			},
			expected: []string{"removed.txt"},
		},
		{
			name: "only renamed files",
			status: &GitStatus{
				Renamed: []GitFile{
					{Path: "renamed.txt", Status: "R"},
				},
			},
			expected: []string{"renamed.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getAllGitFiles(tt.status)

			// Check length
			if len(got) != len(tt.expected) {
				t.Errorf("getAllGitFiles() returned %d files, want %d", len(got), len(tt.expected))
			}

			// Check that all expected paths are present
			gotPaths := make(map[string]bool)
			for _, f := range got {
				gotPaths[f.Path] = true
			}

			for _, expectedPath := range tt.expected {
				if !gotPaths[expectedPath] {
					t.Errorf("getAllGitFiles() missing expected path: %s", expectedPath)
				}
			}

			// Verify no unexpected paths
			for path := range gotPaths {
				found := false
				for _, expectedPath := range tt.expected {
					if path == expectedPath {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("getAllGitFiles() returned unexpected path: %s", path)
				}
			}
		})
	}
}

// TestGetAllGitFiles_Ordering tests that getAllGitFiles preserves the order
// of files from each section (staged -> modified -> untracked -> deleted -> renamed).
func TestGetAllGitFiles_Ordering(t *testing.T) {
	status := &GitStatus{
		Staged: []GitFile{
			{Path: "a.txt", Status: "M"},
			{Path: "b.txt", Status: "M"},
		},
		Modified: []GitFile{
			{Path: "c.txt", Status: "M"},
		},
		Untracked: []GitFile{
			{Path: "d.txt", Status: "?"},
		},
		Deleted: []GitFile{
			{Path: "e.txt", Status: "D"},
		},
		Renamed: []GitFile{
			{Path: "f.txt", Status: "R"},
		},
	}

	got := getAllGitFiles(status)
	expectedOrder := []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt", "f.txt"}

	if len(got) != len(expectedOrder) {
		t.Fatalf("getAllGitFiles() returned %d files, want %d", len(got), len(expectedOrder))
	}

	for i, expected := range expectedOrder {
		if got[i].Path != expected {
			t.Errorf("file at index %d: got %s, want %s", i, got[i].Path, expected)
		}
	}
}

// TestSanitizePathComponent tests that sanitizePathComponent correctly replaces
// unsafe characters with underscores.
func TestSanitizePathComponent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "alphanumeric only",
			input:    "MyBranch123",
			expected: "MyBranch123",
		},
		{
			name:     "with hyphens and underscores",
			input:    "my-branch_name",
			expected: "my-branch_name",
		},
		{
			name:     "with dots",
			input:    "feature.v1.2",
			expected: "feature.v1.2",
		},
		{
			name:     "spaces become underscores",
			input:    "my branch",
			expected: "my_branch",
		},
		{
			name:     "slashes become underscores",
			input:    "feature/branch",
			expected: "feature_branch",
		},
		{
			name:     "backslashes become underscores",
			input:    "feature\\branch",
			expected: "feature_branch",
		},
		{
			name:     "special chars become underscores",
			input:    "branch@#$%^&*()",
			expected: "branch_________",
		},
		{
			name:     "mixed safe and unsafe",
			input:    "feat/my-branch_name.v2",
			expected: "feat_my-branch_name.v2",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only unsafe characters",
			input:    "/\\@#$",
			expected: "_____",
		},
		{
			name:     "unicode characters",
			input:    "branch日本語",
			expected: "branch___",
		},
		{
			name:     "trailing spaces trimmed not sanitized",
			input:    "branch ",
			expected: "branch_",
		},
		{
			name:     "multiple consecutive unsafe chars",
			input:    "branch@@@",
			expected: "branch___",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizePathComponent(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizePathComponent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestNewChatSession tests that newChatSession creates a properly initialized
// chat session.
func TestNewChatSession(t *testing.T) {
	now := time.Now()
	id := "test-chat-id"
	name := "Test Chat"

	session := newChatSession(id, name)

	// Verify ID is set correctly
	if session.ID != id {
		t.Errorf("newChatSession() ID = %q, want %q", session.ID, id)
	}

	// Verify name is set correctly
	if session.Name != name {
		t.Errorf("newChatSession() Name = %q, want %q", session.Name, name)
	}

	// Verify timestamps are set (within reasonable time window)
	if session.CreatedAt.Before(now.Add(-1 * time.Second)) {
		t.Error("newChatSession() CreatedAt is too far in the past")
	}
	if session.CreatedAt.After(now.Add(1 * time.Second)) {
		t.Error("newChatSession() CreatedAt is too far in the future")
	}
	if session.LastActiveAt != session.CreatedAt {
		t.Error("newChatSession() LastActiveAt should equal CreatedAt")
	}

	// Verify agent state is set
	if len(session.AgentState) == 0 {
		t.Error("newChatSession() AgentState should not be empty")
	}
	var state agent.AgentState
	if err := json.Unmarshal(session.AgentState, &state); err != nil {
		t.Errorf("newChatSession() AgentState is not valid JSON: %v", err)
	}

	// Verify IsPinned is false
	if session.IsPinned {
		t.Error("newChatSession() IsPinned should be false")
	}
}

// TestNewChatSession_EmptyID tests that newChatSession generates an ID
// when given an empty string.
func TestNewChatSession_EmptyID(t *testing.T) {
	session := newChatSession("", "Test")

	// ID should be generated (non-empty)
	if session.ID == "" {
		t.Error("newChatSession() should generate ID when given empty string")
	}

	// Generated ID should start with "chat-"
	if !startsWith(session.ID, "chat-") {
		t.Errorf("newChatSession() generated ID %q should start with 'chat-'", session.ID)
	}
}

// TestNewDefaultChatSession tests that newDefaultChatSession creates a chat
// session with the default ID and name.
func TestNewDefaultChatSession(t *testing.T) {
	session := newDefaultChatSession()

	// ID should be the default
	if session.ID != defaultChatID {
		t.Errorf("newDefaultChatSession() ID = %q, want %q", session.ID, defaultChatID)
	}

	// Name should be "Chat"
	if session.Name != "Chat" {
		t.Errorf("newDefaultChatSession() Name = %q, want 'Chat'", session.Name)
	}

	// Verify other fields are properly initialized
	if session.CreatedAt.IsZero() {
		t.Error("newDefaultChatSession() CreatedAt should not be zero")
	}
	if session.IsPinned {
		t.Error("newDefaultChatSession() IsPinned should be false")
	}
}

// Helper function to check if a string starts with a prefix
func startsWith(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
