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

// Default timeout for subagent execution (30 minutes)
const DefaultSubagentTimeout = 30 * time.Minute

// GetSubagentTimeout returns the configured timeout for subagent execution.
// It reads from LEDIT_SUBAGENT_TIMEOUT environment variable if set,
// otherwise returns DefaultSubagentTimeout.
//
// Environment variable format: number followed by unit (e.g., "30m", "1h", "90s")
func GetSubagentTimeout() time.Duration {
	envTimeout := os.Getenv("LEDIT_SUBAGENT_TIMEOUT")
	if envTimeout == "" {
		return DefaultSubagentTimeout
	}

	// First try to parse as a duration string (e.g., "30m")
	duration, err := time.ParseDuration(envTimeout)
	if err == nil {
		if duration > 0 {
			return duration
		}
		// Zero or negative duration - fall back to default
		return DefaultSubagentTimeout
	}

	// If that fails, try to parse as just minutes (e.g., "30")
	minutes, err := strconv.Atoi(envTimeout)
	if err == nil && minutes > 0 {
		return time.Duration(minutes) * time.Minute
	}

	// If all else fails, return default
	return DefaultSubagentTimeout
}

// RunSubagent spawns an agent subprocess, waits for completion, and returns all output.
// This enables the planner agent to delegate work to execution sub-agents, wait for them
// to complete, and immediately retrieve their output for evaluation.
//
// The function runs synchronously (blocking) and captures stdout/stderr.
//
// Example workflow:
//   1. Planning Agent: "Create a plan..."
//   2. Planning Agent: run_subagent("Implement JWT token service")
//      → spawns Sub-Agent with that task and WAITS for completion
//   3. Sub-Agent: Works on task... (returns output)
//   4. Planning Agent: Receives stdout/stderr, evaluates with other tools
//   5. Planning Agent: run_subagent("Fix token expiration bug")
//      → spawns another subagent for follow-up
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
//   - timed_out: true if the subprocess was terminated due to timeout
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

	// Create context with timeout to prevent indefinite hangs
	// Timeout is configurable via LEDIT_SUBAGENT_TIMEOUT environment variable
	ctx, cancel := context.WithTimeout(context.Background(), GetSubagentTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, leditPath, args...)

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command and wait for completion (blocking with timeout)
	err = cmd.Run()

	// Determine exit code and timeout status
	exitCode := 0
	timedOut := false

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			// Timeout occurred
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
