package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellCommandReturnsFullOutputOnFailure(t *testing.T) {
	// Test case 1: Command that fails with non-zero exit code should return full output
	t.Run("FailedCommandReturnsFullOutput", func(t *testing.T) {
		ctx := context.Background()

		// Use a command that will definitely fail and produce output
		command := "go build /nonexistent/path"

		output, err := ExecuteShellCommand(ctx, command)

		// The tool should NOT return an error for command failures
		assert.NoError(t, err, "Shell tool should not return error for command failure")
		assert.NotEmpty(t, output, "Should return output")

		// Should contain the actual error output from go build (different on different systems)
		assert.True(t,
			strings.Contains(output, "no such file or directory") ||
				strings.Contains(output, "directory not found") ||
				strings.Contains(output, "does not exist"),
			"Should contain actual error message, got: %s", output)
	})

	// Test case 2: Command that succeeds should work normally
	t.Run("SuccessfulCommandWorks", func(t *testing.T) {
		ctx := context.Background()

		command := "echo 'hello world'"

		output, err := ExecuteShellCommand(ctx, command)

		assert.NoError(t, err, "Shell tool should not return error for successful command")
		assert.Contains(t, output, "hello world", "Should contain command output")
	})

	// Test case 3: Command with stderr output should capture both stdout and stderr
	t.Run("CapturesStderrAndStdout", func(t *testing.T) {
		ctx := context.Background()

		// Command that outputs to both stdout and stderr
		command := `sh -c "echo 'stdout message'; echo 'stderr message' >&2"`

		output, err := ExecuteShellCommand(ctx, command)

		assert.NoError(t, err, "Shell tool should not return error")
		assert.Contains(t, output, "stdout message", "Should capture stdout")
		assert.Contains(t, output, "stderr message", "Should capture stderr")
	})

	// Test case 4: Test with a realistic build failure scenario
	t.Run("RealBuildFailureScenario", func(t *testing.T) {
		ctx := context.Background()

		// Create a temporary Go file with syntax error
		tempDir := t.TempDir()
		badGoFile := tempDir + "/bad.go"

		// Write invalid Go code
		badCode := `package main

import "fmt"

func main() {
	fmt.Println("hello"
	// Missing closing parenthesis - syntax error
}`

		// Write the bad code to file
		err := os.WriteFile(badGoFile, []byte(badCode), 0644)
		require.NoError(t, err)

		command := "go build " + badGoFile

		output, err := ExecuteShellCommand(ctx, command)

		// This is the critical test: the agent should see the actual Go compiler error
		assert.NoError(t, err, "Shell tool should not return error for build failure")
		assert.Contains(t, output, "syntax error", "Should contain actual Go compiler error")
	})
}

func TestShellCommandTimeout(t *testing.T) {
	t.Skip("Timeout testing is complex and platform-dependent - skipping for now")
}

func TestShellCommandInvalidBinary(t *testing.T) {
	t.Run("NonExistentCommand", func(t *testing.T) {
		ctx := context.Background()

		command := "nonexistent_binary_12345"

		output, err := ExecuteShellCommand(ctx, command)

		// Should not return tool error, but should capture the OS error
		assert.NoError(t, err, "Shell tool should not return error for nonexistent command")
		assert.Contains(t, output, "not found", "Should contain 'not found' error")
	})
}

func TestBuildShellOutputWithStatus(t *testing.T) {
	t.Run("EmptyOutputWithSuccess", func(t *testing.T) {
		output := buildShellOutputWithStatus("", "echo test", 0, nil)
		assert.Contains(t, output, "✅", "Should contain success icon")
		assert.Contains(t, output, "SUCCESS", "Should contain success status")
	})

	t.Run("EmptyOutputWithFailure", func(t *testing.T) {
		output := buildShellOutputWithStatus("", "false", 1, nil)
		assert.Contains(t, output, "❌", "Should contain failure icon")
		assert.Contains(t, output, "FAILED", "Should contain failure status")
	})

	t.Run("NonEmptyOutput", func(t *testing.T) {
		input := "some output content"
		output := buildShellOutputWithStatus(input, "echo test", 0, nil)
		assert.Equal(t, input, output, "Should return original output unchanged")
	})
}
