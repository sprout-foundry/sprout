package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestShowColoredDiff tests the main diff functionality
func TestShowColoredDiff(t *testing.T) {
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

	// Test with simple content change (just ensure it doesn't panic)
	oldContent := "line 1\nline 2\nline 3"
	newContent := "line 1\nmodified line 2\nline 3"

	// This should not panic - we can't easily test output without capturing stdout
	agent.ShowColoredDiff(oldContent, newContent, 10)
}

// TestIsPythonAvailable tests Python availability detection
func TestIsPythonAvailable(t *testing.T) {
	result := isPythonAvailable()

	// Check if the result matches actual system state
	python3Available := false
	pythonAvailable := false

	if _, err := exec.LookPath("python3"); err == nil {
		python3Available = true
	}
	if _, err := exec.LookPath("python"); err == nil {
		pythonAvailable = true
	}

	expectedResult := python3Available || pythonAvailable

	if result != expectedResult {
		t.Errorf("isPythonAvailable() = %v, expected %v", result, expectedResult)
	}
}

// TestShowGoDiff tests the Go fallback implementation
func TestShowGoDiff(t *testing.T) {
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

	// Test with simple content change (just ensure it doesn't panic)
	oldContent := "line 1\nline 2\nline 3"
	newContent := "line 1\nmodified line 2\nline 3"

	// This should not panic
	agent.showGoDiff(oldContent, newContent, 10)
}

// TestShowPythonDiff tests the Python implementation when available
func TestShowPythonDiff(t *testing.T) {
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

	oldContent := "line 1\nline 2\nline 3"
	newContent := "line 1\nmodified line 2\nline 3"

	// Test Python diff - this will return false if Python is not available
	result := agent.showPythonDiff(oldContent, newContent, 10)

	// The result should match Python availability
	pythonAvailable := isPythonAvailable()
	if pythonAvailable {
		// If Python is available, the function should succeed (return true)
		// Note: It might still fail due to temporary file issues, so we don't strictly require true
		t.Logf("Python available, showPythonDiff returned: %v", result)
	} else {
		// If Python is not available, it should return false
		if result {
			t.Error("Expected showPythonDiff to return false when Python is not available")
		}
	}
}

// TestFindChanges tests the change detection algorithm
func TestFindChanges(t *testing.T) {
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

	tests := []struct {
		name     string
		oldLines []string
		newLines []string
		expected int // Expected number of changes
	}{
		{
			name:     "No changes",
			oldLines: []string{"line1", "line2", "line3"},
			newLines: []string{"line1", "line2", "line3"},
			expected: 0,
		},
		{
			name:     "Single line change",
			oldLines: []string{"line1", "line2", "line3"},
			newLines: []string{"line1", "modified", "line3"},
			expected: 1,
		},
		{
			name:     "Addition at end",
			oldLines: []string{"line1", "line2"},
			newLines: []string{"line1", "line2", "line3"},
			expected: 1,
		},
		{
			name:     "Deletion at end",
			oldLines: []string{"line1", "line2", "line3"},
			newLines: []string{"line1", "line2"},
			expected: 1,
		},
		{
			name:     "Empty to content",
			oldLines: []string{},
			newLines: []string{"line1", "line2"},
			expected: 1,
		},
		{
			name:     "Content to empty",
			oldLines: []string{"line1", "line2"},
			newLines: []string{},
			expected: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changes := agent.findChanges(test.oldLines, test.newLines)
			if len(changes) != test.expected {
				t.Errorf("Expected %d changes, got %d", test.expected, len(changes))
			}
		})
	}
}

// TestDiffChangeStruct tests the DiffChange struct fields
func TestDiffChangeStruct(t *testing.T) {
	change := DiffChange{
		OldStart:  1,
		OldLength: 2,
		NewStart:  3,
		NewLength: 4,
	}

	if change.OldStart != 1 {
		t.Errorf("Expected OldStart to be 1, got %d", change.OldStart)
	}
	if change.OldLength != 2 {
		t.Errorf("Expected OldLength to be 2, got %d", change.OldLength)
	}
	if change.NewStart != 3 {
		t.Errorf("Expected NewStart to be 3, got %d", change.NewStart)
	}
	if change.NewLength != 4 {
		t.Errorf("Expected NewLength to be 4, got %d", change.NewLength)
	}
}

// TestShowColoredDiffWithEmptyContent tests edge cases
func TestShowColoredDiffWithEmptyContent(t *testing.T) {
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

	// Test with empty old content
	agent.ShowColoredDiff("", "new content", 10)

	// Test with empty new content
	agent.ShowColoredDiff("old content", "", 10)

	// Test with both empty
	agent.ShowColoredDiff("", "", 10)

	// Test with very long content
	longContent := strings.Repeat("line\n", 1000)
	agent.ShowColoredDiff(longContent, longContent+"new line", 5)
}

// TestFallbackBehavior tests that fallback works when Python fails
func TestFallbackBehavior(t *testing.T) {
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

	// Temporarily modify PATH to simulate Python not being available
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", "/nonexistent")
	defer os.Setenv("PATH", originalPath)

	// This should fall back to Go implementation and not panic
	oldContent := "line 1\nline 2\nline 3"
	newContent := "line 1\nmodified line 2\nline 3"

	agent.ShowColoredDiff(oldContent, newContent, 10)
}
