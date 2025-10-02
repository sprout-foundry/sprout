package commands

import "testing"

func TestIsShellCommand_PositiveCases(t *testing.T) {
	testCases := []string{
		"ls -la",
		"./script.sh --verbose",
		"../scripts/build.sh",
		"/usr/bin/python3",
		"~/bin/run-task",
		"command1 && command2",
		"cat file.txt > output.txt",
		"echo $HOME",
		"echo ${PATH}",
		"env | sort",
		"foo | bar", // operators should trigger detection even if first token unknown
		"C:\\Windows\\system32\\cmd.exe",
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
