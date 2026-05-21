//go:build !js

package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// extractExitCode extracts the exit code from an error, if it's an exit error.
// Returns 0 if the error is nil or not an exit error.
func extractExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 0
}

// runShellCommand is the native (os/exec) implementation of shell command
// execution. The js/wasm variant in shell_js.go routes through wasmshell.
func runShellCommand(ctx context.Context, command string, streamOutput bool) (string, error) {
	if streamOutput {
		// STREAMING MODE: Use pipes for real-time output
		// Use exec.CommandContext to respect context cancellation
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd := exec.CommandContext(ctx, shell, "-c", command)

		if wd := filesystem.WorkspaceRootFromContext(ctx); wd != "" {
			cmd.Dir = wd
		} else if wd, err := os.Getwd(); err == nil {
			cmd.Dir = wd
		}

		// Get pipes for stdout and stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return "", fmt.Errorf("get stdout pipe: %w", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return "", fmt.Errorf("get stderr pipe: %w", err)
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("start command: %w", err)
		}

		// Buffer to capture output for return value
		var outputBuf bytes.Buffer

		// Stream stdout and stderr in real-time
		// Use goroutines to handle both concurrently
		var wg sync.WaitGroup
		wg.Add(2)

		// Copy stdout to both terminal and buffer
		go func() {
			defer wg.Done()
			io.Copy(io.MultiWriter(os.Stdout, &outputBuf), stdout)
		}()

		// Copy stderr to both terminal and buffer
		go func() {
			defer wg.Done()
			io.Copy(io.MultiWriter(os.Stderr, &outputBuf), stderr)
		}()

		// Wait for both streams to finish
		wg.Wait()

		// Wait for command to complete
		err = cmd.Wait()

		// Get the exit code for status reporting
		exitCode := extractExitCode(err)

		// Build the final output with status header
		finalOutput := buildShellOutputWithStatus(outputBuf.String(), command, exitCode, err)

		// Shell tool execution is always successful as long as we can run the command
		// Non-zero exit codes are normal command outcomes, not tool failures
		// The output includes the command's stderr and exit status information
		return finalOutput, nil
	}

	// SILENT MODE: Capture output without streaming (for LLM tool calls)
	// Use exec.CommandContext to respect context cancellation
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command)

	if wd := filesystem.WorkspaceRootFromContext(ctx); wd != "" {
		cmd.Dir = wd
	} else if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}

	// Capture combined stdout and stderr
	output, err := cmd.CombinedOutput()

	// Get the exit code for status reporting
	exitCode := extractExitCode(err)

	// For LLM tool calls, truncate output to 2 lines
	truncatedOutput := truncateOutput(string(output), 2)

	// Print truncated output to terminal unless we're in tests/CI.
	if truncatedOutput != "" && shouldPrintCapturedShellPreview() {
		fmt.Printf("%s\n", truncatedOutput)
	}

	// Build the final output with status header
	finalOutput := buildShellOutputWithStatus(string(output), command, exitCode, err)

	return finalOutput, nil
}
