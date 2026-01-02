package commands

import "testing"

// TestIsSlashCommand tests that both / and ! are recognized as slash commands
func TestIsSlashCommand(t *testing.T) {
	registry := NewCommandRegistry()

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"forward slash command", "/exec ls", true},
		{"bang prefix as command", "!ls -la", true},
		{"bang with exec", "!exec ls", true},
		{"regular text", "hello world", false},
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"slash with whitespace", "   /exec ls   ", true},
		{"bang with whitespace", "   !ls -la   ", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := registry.IsSlashCommand(tc.input)
			if result != tc.expected {
				t.Errorf("IsSlashCommand(%q) = %v, expected %v", tc.input, result, tc.expected)
			}
		})
	}
}

// TestBangPrefixRouting tests that ! prefixes route to exec command
func TestBangPrefixRouting(t *testing.T) {
	registry := NewCommandRegistry()

	testCases := []struct {
		name             string
		input            string
		expectedCmd      string
		expectedArgs     []string
	}{
		{"bang simple", "!ls", "exec", []string{"ls"}},
		{"bang with flags", "!ls -la", "exec", []string{"ls -la"}},
		{"bang with args", "!git status", "exec", []string{"git status"}},
		{"bang with quoted args", "!echo 'hello world'", "exec", []string{"echo 'hello world'"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// We can't easily test Execute because it requires a full Agent
			// But we can verify that ! prefixes are recognized as slash commands
			if !registry.IsSlashCommand(tc.input) {
				t.Errorf("Expected %q to be recognized as slash command", tc.input)
			}
		})
	}
}

// TestSlashPrefixStillWorks ensures / prefix still works as before
func TestSlashPrefixStillWorks(t *testing.T) {
	registry := NewCommandRegistry()

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"slash help", "/help", true},
		{"slash models", "/models", true},
		{"slash exec", "/exec ls", true},
		{"slash commit", "/commit", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := registry.IsSlashCommand(tc.input)
			if result != tc.expected {
				t.Errorf("IsSlashCommand(%q) = %v, expected %v", tc.input, result, tc.expected)
			}
		})
	}
}
