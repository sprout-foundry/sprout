package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// No timeout for subagent execution - runs until completion
// Subagents are designed for long-running tasks and should not be restricted
const DefaultSubagentTimeout = 0 // 0 means no timeout

// GetSubagentTimeout returns the configured timeout for subagent execution.
// It reads from LEDIT_SUBAGENT_TIMEOUT environment variable if set.
// A value of "0" or unset means NO timeout (runs indefinitely).
//
// Environment variable format: number followed by unit (e.g., "30m", "1h", "90s")
// Set to "0" or leave unset to disable timeout completely (recommended)
func GetSubagentTimeout() time.Duration {
	envTimeout := os.Getenv("LEDIT_SUBAGENT_TIMEOUT")
	if envTimeout == "" {
		return 0 // No timeout by default
	}

	// Check if explicitly set to 0 (no timeout)
	if envTimeout == "0" {
		return 0
	}

	// First try to parse as a duration string (e.g., "30m")
	duration, err := time.ParseDuration(envTimeout)
	if err == nil {
		if duration > 0 {
			return duration
		}
		// Zero or negative duration - no timeout
		return 0
	}

	// If that fails, try to parse as just minutes (e.g., "30")
	minutes, err := strconv.Atoi(envTimeout)
	if err == nil && minutes > 0 {
		return time.Duration(minutes) * time.Minute
	}

	// If all else fails, return no timeout
	return 0
}

// RunSubagent spawns an agent subprocess, waits for completion, and returns all output.
// This enables the planner agent to delegate work to execution sub-agents, wait for them
// to complete, and immediately retrieve their output for evaluation.
//
// The function runs synchronously (blocking) with NO TIMEOUT by default.
// Subagents are designed for long-running implementation tasks and should complete
// their work regardless of how long it takes.
//
// Example workflow:
//  1. Planning Agent: "Create a plan..."
//  2. Planning Agent: run_subagent("Implement JWT token service")
//     → spawns Sub-Agent with that task and WAITS for completion
//  3. Sub-Agent: Works on task... (returns output)
//  4. Planning Agent: Receives stdout/stderr, evaluates with other tools
//  5. Planning Agent: run_subagent("Fix token expiration bug")
//     → spawns another subagent for follow-up
//
// Parameters:
//   - prompt: The task/prompt for the subagent
//   - model: Optional model override (e.g., "qwen/qwen-coder-32b")
//   - provider: Optional provider override (e.g., "openrouter")
//
// Returns map containing:
//   - stdout: Combined stdout output
//   - stderr: Combined stderr output
//   - exit_code: Process exit code (0 for success)
//   - completed: true if process ran to completion (always true for blocking mode)
//   - timed_out: true if the subprocess was terminated due to timeout (always false with no timeout)
func RunSubagent(prompt, model, provider string) (map[string]string, error) {
	// Build command: ledit agent with the given prompt
	args := []string{"agent"}

	// Add provider/model if specified
	if provider != "" {
		args = append(args, "--provider", provider)
		if model != "" {
			args = append(args, "--model", model)
		}
	} else if model != "" {
		args = append(args, "--model", model)
	}

	args = append(args, prompt)

	// Use the currently running ledit binary path to ensure consistency
	// This avoids issues where exec.LookPath might find a different binary
	leditPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get current executable path: %w", err)
	}

	// Create context (with optional timeout)
	timeout := GetSubagentTimeout()
	var ctx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		// Only create timeout context if explicitly configured
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	} else {
		// No timeout - run until completion
		ctx = context.Background()
	}

	cmd := exec.CommandContext(ctx, leditPath, args...)

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command and wait for completion (blocking)
	err = cmd.Run()

	// Determine exit code and timeout status
	exitCode := 0
	timedOut := false

	if err != nil {
		if timeout > 0 && ctx.Err() == context.DeadlineExceeded {
			// Timeout occurred (only if timeout was configured)
			exitCode = -1
			timedOut = true
		} else if exitError, ok := err.(*exec.ExitError); ok {
			// Command ran but exited with non-zero status
			exitCode = exitError.ExitCode()
		} else {
			// Couldn't start the command (e.g., ledit not found)
			exitCode = -1
		}
	}

	// Return all output with exit status and timeout indicator
	return map[string]string{
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": fmt.Sprintf("%d", exitCode),
		"completed": "true",
		"timed_out": fmt.Sprintf("%t", timedOut),
	}, nil
}
