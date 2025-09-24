package components

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/filesystem"
)

func TestAgentConsole_DefaultConfig(t *testing.T) {
	config := DefaultAgentConsoleConfig()

	if config.Prompt != "> " {
		t.Errorf("Default prompt incorrect: expected '> ', got %s", config.Prompt)
	}

	// Should have a default history file path
	if !strings.Contains(config.HistoryFile, ".ledit_agent_history") {
		t.Errorf("Default history file path incorrect: %s", config.HistoryFile)
	}
}

func TestAgentConsole_IsShellCommand(t *testing.T) {
	// Create a mock console with dummy agent (we're only testing the shell detection)
	// This avoids the complexity of mocking the full agent interface
	console := &AgentConsole{}

	testCases := []struct {
		input    string
		expected bool
	}{
		{"ls", true},
		{"git status", true},
		{"pwd", true},
		{"echo hello", true},
		{"./script.sh", true},
		{"/bin/ls", true},
		{"ls | grep test", true},
		{"hello world", false},
		{"how are you?", false},
		{"", false},
	}

	for _, tc := range testCases {
		result := console.isShellCommand(tc.input)
		if result != tc.expected {
			t.Errorf("isShellCommand(%q) = %v, expected %v", tc.input, result, tc.expected)
		}
	}
}

func TestAgentConsole_FormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{150 * time.Second, "2m"},
		{3700 * time.Second, "1h1m"},
		{7260 * time.Second, "2h1m"},
	}

	for _, test := range tests {
		result := formatDuration(test.duration)
		if result != test.expected {
			t.Errorf("formatDuration(%v) = %s, expected %s", test.duration, result, test.expected)
		}
	}
}

func TestAgentConsole_LoadSaveHistory(t *testing.T) {
	// Test the standalone history functions
	history := []string{"command1", "command2", "command3"}

	// Create a temporary file
	tmpFile, err := filesystem.CreateTempFile("", "test_history_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Save history
	err = saveHistory(tmpFile.Name(), history)
	if err != nil {
		t.Fatalf("Failed to save history: %v", err)
	}

	// Load history
	loadedHistory, err := loadHistory(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load history: %v", err)
	}

	// Verify
	if len(loadedHistory) != len(history) {
		t.Fatalf("Loaded history length mismatch: expected %d, got %d", len(history), len(loadedHistory))
	}

	for i, cmd := range history {
		if loadedHistory[i] != cmd {
			t.Errorf("Loaded history[%d]: expected %s, got %s", i, cmd, loadedHistory[i])
		}
	}
}

func TestAgentConsole_ShellCommandDetection(t *testing.T) {
	console := &AgentConsole{}

	// Test various shell command patterns
	shellCommands := []string{
		"ls -la",
		"git status",
		"npm install",
		"docker ps",
		"kubectl get pods",
		"find . -name '*.go'",
		"echo $HOME",
		"grep -r 'test' .",
		"cat /etc/hosts",
		"sudo systemctl restart nginx",
	}

	for _, cmd := range shellCommands {
		if !console.isShellCommand(cmd) {
			t.Errorf("Expected %q to be detected as shell command", cmd)
		}
	}

	// Test non-shell commands
	nonShellCommands := []string{
		"what is the weather?",
		"explain this code",
		"help me with debugging",
		"please analyze this function",
		"can you tell me about Go?",
	}

	for _, cmd := range nonShellCommands {
		if console.isShellCommand(cmd) {
			t.Errorf("Expected %q to NOT be detected as shell command", cmd)
		}
	}
}

func TestAgentConsole_ShellOperators(t *testing.T) {
	console := &AgentConsole{}

	// Test commands with shell operators
	operatorCommands := []string{
		"ls | grep test",
		"cat file.txt > output.txt",
		"command1 && command2",
		"env | sort",
		"ps aux | grep nginx",
		"echo $USER",
	}

	for _, cmd := range operatorCommands {
		if !console.isShellCommand(cmd) {
			t.Errorf("Expected command with operator %q to be detected as shell command", cmd)
		}
	}
}

func TestAgentConsole_PathDetection(t *testing.T) {
	console := &AgentConsole{}

	// Test path-based commands
	pathCommands := []string{
		"./run.sh",
		"../scripts/build.sh",
		"/usr/bin/python3",
		"/bin/bash",
	}

	for _, cmd := range pathCommands {
		if !console.isShellCommand(cmd) {
			t.Errorf("Expected path command %q to be detected as shell command", cmd)
		}
	}
}

func TestAgentConsoleConfig_CustomConfig(t *testing.T) {
	config := &AgentConsoleConfig{
		HistoryFile: "/custom/path/history.txt",
		Prompt:      "custom> ",
	}

	if config.Prompt != "custom> " {
		t.Errorf("Custom prompt not set correctly: expected 'custom> ', got %s", config.Prompt)
	}

	if config.HistoryFile != "/custom/path/history.txt" {
		t.Errorf("Custom history file not set correctly: expected '/custom/path/history.txt', got %s", config.HistoryFile)
	}
}

// Test utility functions work independently
func TestAgentConsole_UtilityFunctions(t *testing.T) {
	// Test that utility functions are available and work
	d := 65 * time.Second
	formatted := formatDuration(d)
	if formatted != "1m" {
		t.Errorf("formatDuration utility function failed: expected '1m', got %s", formatted)
	}

	// Test empty history handling
	emptyHistory := []string{}
	tmpFile, err := filesystem.CreateTempFile("", "empty_history_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	err = saveHistory(tmpFile.Name(), emptyHistory)
	if err != nil {
		t.Fatalf("Failed to save empty history: %v", err)
	}

	loaded, err := loadHistory(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load empty history: %v", err)
	}

	if len(loaded) != 0 {
		t.Errorf("Expected empty history, got %d items", len(loaded))
	}
}
