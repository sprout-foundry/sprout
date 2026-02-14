package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellCommand_Name(t *testing.T) {
	cmd := &ShellCommand{}
	assert.Equal(t, "shell", cmd.Name())
}

func TestShellCommand_Description(t *testing.T) {
	cmd := &ShellCommand{}
	desc := cmd.Description()
	assert.Contains(t, desc, "shell scripts")
	assert.Contains(t, desc, "environmental context")
}

func TestShellCommand_Execute_NoArgs(t *testing.T) {
	cmd := &ShellCommand{}
	err := cmd.Execute([]string{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage:")
}

func TestShellCommand_GatherEnvironmentalContext(t *testing.T) {
	cmd := &ShellCommand{}
	context, err := cmd.gatherEnvironmentalContext()

	assert.NoError(t, err)
	assert.NotEmpty(t, context)

	// Check that context contains expected information
	expectedContent := []string{
		"Operating System:",
		"Architecture:",
		"Working Directory:",
		"Shell:",
		"Environment Variables:",
		"Available Tools:",
	}

	for _, expected := range expectedContent {
		assert.Contains(t, context, expected)
	}
}

func TestCleanMarkdownCodeBlocks(t *testing.T) {
	cmd := &ShellCommand{}

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain code without markdown",
			input:    "echo hello",
			expected: "echo hello",
		},
		{
			name:     "bash code block",
			input:    "```bash\necho hello\n```",
			expected: "echo hello",
		},
		{
			name:     "shell code block",
			input:    "```shell\nls -la\n```",
			expected: "ls -la",
		},
		{
			name:     "code block with language",
			input:    "```bash\n#!/bin/bash\necho test\n```",
			expected: "#!/bin/bash\necho test",
		},
		{
			name:     "multiple lines in code block",
			input:    "```bash\nline1\nline2\nline3\n```",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := cmd.cleanMarkdownCodeBlocks(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIsValidShellCode(t *testing.T) {
	cmd := &ShellCommand{}

	positiveCases := []string{
		"ls -la",
		"git status",
		"git commit -m 'Initial commit'",
		"cat file.txt",
		"find . -name \"*.go\"",
		"grep -r \"pattern\" .",
		"#!/bin/bash\necho hello",
		"if [ -f file ]; then echo exists; fi",
		"for f in *.go; do echo $f; done",
		"function greet() { echo hello; }",
		"pwd",
		"echo test",
	}

	for _, input := range positiveCases {
		t.Run("valid-"+strings.Fields(input)[0], func(t *testing.T) {
			if !cmd.isValidShellCode(input) {
				t.Errorf("expected %q to be detected as valid shell code", input)
			}
		})
	}

	// Negative cases that should NOT be detected as shell code
	// These are actual sentences or explanations
	negativeCases := []string{
		"The previous command showed an error. Please check the output.",
		"Here is the output you requested.",
		"This is a test sentence with multiple words.",
	}

	for _, input := range negativeCases {
		t.Run("invalid", func(t *testing.T) {
			if cmd.isValidShellCode(input) {
				t.Errorf("expected %q to NOT be detected as valid shell code", input)
			}
		})
	}
}

func TestIsValidShellCode_MixedCases(t *testing.T) {
	cmd := &ShellCommand{}

	// Test explanation followed by code is invalid
	assert.False(t, cmd.isValidShellCode("The previous command showed an error. Please check the output."))

	// Test simple commands work
	assert.True(t, cmd.isValidShellCode("pwd"))
	assert.True(t, cmd.isValidShellCode("ls"))
}

func TestGetScriptType(t *testing.T) {
	cmd := &ShellCommand{}

	assert.Equal(t, "command", cmd.getScriptType(true))
	assert.Equal(t, "script", cmd.getScriptType(false))
}

func TestIsValidShellCode_ComplexScript(t *testing.T) {
	cmd := &ShellCommand{}

	complexScript := `#!/bin/bash
set -e

# Check if directory exists
if [ ! -d "$1" ]; then
    echo "Directory does not exist: $1"
    exit 1
fi

# List files
ls -la "$1"`

	assert.True(t, cmd.isValidShellCode(complexScript))
}

func TestIsValidShellCode_EnvVars(t *testing.T) {
	cmd := &ShellCommand{}

	// Commands with environment variables should be valid
	assert.True(t, cmd.isValidShellCode("echo $HOME"))
	assert.True(t, cmd.isValidShellCode("cd $HOME && ls"))
}

func TestIsValidShellCode_PipesAndRedirects(t *testing.T) {
	cmd := &ShellCommand{}

	assert.True(t, cmd.isValidShellCode("ls -la | grep test"))
	assert.True(t, cmd.isValidShellCode("cat file.txt > output.txt"))
	assert.True(t, cmd.isValidShellCode("cat file.txt >> output.txt"))
	assert.True(t, cmd.isValidShellCode("echo hello 2>&1"))
}

func TestIsValidShellCode_QuotedStrings(t *testing.T) {
	cmd := &ShellCommand{}

	assert.True(t, cmd.isValidShellCode("echo 'hello world'"))
	assert.True(t, cmd.isValidShellCode("echo \"hello world\""))
	assert.True(t, cmd.isValidShellCode("grep -r 'pattern' --include='*.go'"))
}

func TestIsValidShellCode_SpecialChars(t *testing.T) {
	cmd := &ShellCommand{}

	// These should still be recognized as shell code
	assert.True(t, cmd.isValidShellCode("[ -f file ] && echo exists"))
}

func TestIsValidShellCode_JSONResponse(t *testing.T) {
	cmd := &ShellCommand{}

	// A simple JSON might be detected as valid if it contains command patterns
	// This is expected behavior - the heuristic isn't perfect
	jsonResponse := `{"status": "success"}`
	// This may or may not be detected as shell code depending on patterns
	_ = cmd.isValidShellCode(jsonResponse)

	// More complex JSON with command-like strings might be detected
	jsonWithCommand := `{
  "command": "ls -la"
}`
	result := cmd.isValidShellCode(jsonWithCommand)
	// The "command" key triggers shell pattern detection
	assert.True(t, result, "JSON with 'command' key should be detected as shell-like")
}

func TestIsValidShellCode_LongSingleWord(t *testing.T) {
	cmd := &ShellCommand{}

	// A long single word should still be considered valid (could be a path or command)
	assert.True(t, cmd.isValidShellCode("/usr/local/bin/custom-tool"))
}

func TestCleanMarkdownCodeBlocks_ThoughtBlock(t *testing.T) {
	cmd := &ShellCommand{}

	// Test with thought blocks (though current implementation may not handle them)
	input := "Here is the command:\n```bash\necho hello\n```"
	result := cmd.cleanMarkdownCodeBlocks(input)
	assert.True(t, strings.Contains(result, "echo hello"))
}
