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

// syncBuffer is a bytes.Buffer guarded by a Mutex. Used as the early-
// output buffer in runShellCommandAdoptable: the cmd's stdout/stderr
// pipe goroutines keep Write()ing concurrently with the main goroutine's
// String() snapshot read on the background-promotion path (where
// cmd.Wait() has not yet returned), so a plain bytes.Buffer races.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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

		// Buffer to capture output for return value. Must be concurrency-safe:
		// stdout and stderr goroutines write to it simultaneously via io.MultiWriter.
		// A plain bytes.Buffer races under -race (the adoption path in
		// runShellCommandAdoptable uses syncBuffer for the same reason).
		var outputBuf syncBuffer

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
	// Check if we can promote to background on timeout (CLI mode with BPM)
	if bpm := BackgroundProcessManagerFromContext(ctx); bpm != nil {
		// Use adoptable execution — can promote to background on timeout
		return runShellCommandAdoptable(ctx, command, bpm)
	}

	// If a password prompter is registered, use the password-aware path
	// that can detect prompts on stdout and route them to the prompter.
	if pp := PasswordPrompterFromContext(ctx); pp != nil {
		return runShellCommandWithPasswordSupport(ctx, command, pp)
	}

	// Fallback: no BPM available, use standard CommandContext (kills on cancel)
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

	// Build the final output with status header
	finalOutput := buildShellOutputWithStatus(string(output), command, exitCode, err)

	return finalOutput, nil
}

// runShellCommandAdoptable executes a command that can be promoted to background
// on timeout. Instead of using CommandContext (which kills on cancel), it uses
// plain exec.Command and manually monitors context cancellation. If the context
// deadline is exceeded, the running process is adopted into the BackgroundProcessManager.
func runShellCommandAdoptable(ctx context.Context, command string, bpm *BackgroundProcessManager) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", command) // NOT CommandContext — we control lifecycle

	if wd := filesystem.WorkspaceRootFromContext(ctx); wd != "" {
		cmd.Dir = wd
	} else if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}

	// Set process group so we can kill the entire group on stop
	setProcessGroup(cmd)

	// Create temp output file for potential background adoption
	outputFile, err := os.CreateTemp("", "sprout-bg-*.output")
	if err != nil {
		return "", fmt.Errorf("create temp output file: %w", err)
	}
	outputPath := outputFile.Name()

	// Write to both the temp file and a mutex-guarded buffer (for the
	// early-output snapshot if we promote to background mid-run, where
	// the cmd's pipe goroutines are still writing concurrently).
	var outputBuf syncBuffer
	writer := io.MultiWriter(outputFile, &outputBuf)
	cmd.Stdout = writer
	cmd.Stderr = writer

	// Start the process
	if err := cmd.Start(); err != nil {
		outputFile.Close()
		os.Remove(outputPath)
		return "", fmt.Errorf("start command: %w", err)
	}

	// Wait for command completion OR context cancellation
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case waitErr := <-waitCh:
		// Command completed normally
		outputFile.Close()
		os.Remove(outputPath) // clean up temp file

		exitCode := extractExitCode(waitErr)

		finalOutput := buildShellOutputWithStatus(outputBuf.String(), command, exitCode, waitErr)
		return finalOutput, nil

	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			// Tool deadline hit — promote to background instead of killing.
			sessionID, adoptErr := bpm.AdoptProcess(cmd, outputPath, command, cmd.Dir, waitCh)
			if adoptErr == nil {
				// Do NOT close outputFile on successful adoption. The child
				// writes to an internal Go pipe whose drain goroutine keeps
				// outputFile open through io.MultiWriter. Closing it here
				// would stop the drain goroutine, fill the pipe, and SIGPIPE
				// the child on its next echo. The BPM reads the file with
				// os.ReadFile (which opens its own handle) — our leftover
				// write handle is harmless and will be cleaned up when the
				// drain goroutine exits after cmd.Wait().
				return formatBackgroundPromotionMessage(sessionID, command, outputBuf.String()), nil
			}
			// Adoption failed — close our handle and clean up
			outputFile.Close()
			_ = cmd.Process.Kill()
			<-waitCh // reap the zombie
			os.Remove(outputPath)
			return "", fmt.Errorf("command timed out and background promotion failed: %w", adoptErr)
		}

		// Non-deadline cancellation (user Ctrl+C) — kill the process
		_ = cmd.Process.Kill()
		<-waitCh // reap the zombie
		outputFile.Close()
		os.Remove(outputPath)
		return "", ctx.Err()
	}
}
