package commands

import "testing"

func TestIsShellCommand_PositiveCases(t *testing.T) {
	testCases := []string{
		"ls -la",
		"git status",
		"git commit -m 'Initial commit'",
		"cat file.txt",
	}

	for _, input := range testCases {
		if !IsShellCommand(input) {
			t.Errorf("expected %q to be detected as shell command", input)
		}
	}
}

func TestIsShellCommand_NegativeCases(t *testing.T) {
	testCases := []string{
		"how are you?",
		"please explain this code",
		"The price is $5", // dollar signs without env vars shouldn't trigger detection
		"http://example.com",
		"Tell me about /usr/bin/python",
	}

	for _, input := range testCases {
		if IsShellCommand(input) {
			t.Errorf("expected %q to NOT be detected as shell command", input)
		}
	}
}
