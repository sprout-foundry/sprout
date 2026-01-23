package tools

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
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

	// Propagate important environment variables to subagent processes
	cmd.Env = append(os.Environ(), "LEDIT_FROM_AGENT=1")
	if debug := os.Getenv("LEDIT_DEBUG"); debug != "" {
		cmd.Env = append(cmd.Env, "LEDIT_DEBUG="+debug)
	}
	if unsafe := os.Getenv("LEDIT_UNSAFE_MODE"); unsafe != "" {
		cmd.Env = append(cmd.Env, "LEDIT_UNSAFE_MODE="+unsafe)
	}

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
		exitCode = 124
		timedOut = true} else if exitError, ok := err.(*exec.ExitError); ok {
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

// ParallelSubagentTask represents a single parallel subagent run task
type ParallelSubagentTask struct {
	ID      string
	Prompt  string
	Model   string
	Provider string
}

// ParallelSubagentResult represents the result of a single parallel subagent run
type ParallelSubagentResult struct {
	ID       string
	Stdout   string
	Stderr   string
	ExitCode  int
	Completed bool
	Error     error
}

// RunParallelSubagents spawns multiple agent subprocesses in parallel, waits for all to complete,
// and returns all results. This enables the planning agent to execute independent tasks
// concurrently for faster overall completion time.
//
// Example use case: Writing production code and test cases simultaneously
//
// Parameters:
//   - tasks: List of subagent tasks to run in parallel
//   - noTimeout: If true, uses context.Background() (no timeout). If false, respects GetSubagentTimeout()
//
// Returns map where key is task ID and value contains that task's result
func RunParallelSubagents(tasks []ParallelSubagentTask, noTimeout bool) (map[string]map[string]string, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks provided")
	}

	var wg sync.WaitGroup
	results := make(chan *ParallelSubagentResult, len(tasks))

	// Determine the caller method for logging
	callerMethod := "RunParallelSubagents"

	// Launch all subagents in parallel goroutines using the shared helper
	for _, task := range tasks {
		wg.Add(1)
		go func(t ParallelSubagentTask) {
			defer wg.Done()

			// Use spawnSubagent helper with the provided noTimeout flag
			result := spawnSubagent(t, noTimeout, callerMethod)
			results <- result
		}(task)
	}

	// Wait for all goroutines to complete, then close the results channel
	wg.Wait()
	close(results)

	// Collect all results into output map
	outputMap := make(map[string]map[string]string)
	for result := range results {
		if result.Error != nil {
			outputMap[result.ID] = map[string]string{
				"error":     result.Error.Error(),
				"exit_code": "-1",
				"completed": "false",
				"timed_out": "false",
			}
			continue
		}

		timedOut := result.ExitCode == -1 && result.Completed

		outputMap[result.ID] = map[string]string{
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"exit_code": fmt.Sprintf("%d", result.ExitCode),
			"completed": fmt.Sprintf("%t", result.Completed),
			"timed_out": fmt.Sprintf("%t", timedOut),
		}
	}

	return outputMap, nil
}

// spawnSubagent is a shared helper that spawns a single subagent subprocess.
// It handles all the common logic for building and executing the subagent command.
//
// Parameters:
//   - task: The subagent task to run
//   - noTimeout: If true, use context.Background() (no timeout). If false, respect GetSubagentTimeout()
//   - callerMethod: Name of the calling method for audit logging (e.g., "RunParallelSubagents")
//
// Returns the result of the subagent execution.
func spawnSubagent(task ParallelSubagentTask, noTimeout bool, callerMethod string) *ParallelSubagentResult {
	// Generate a unique task ID for tracking
	taskID := task.ID
	if taskID == "" {
		taskID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}

	// Log spawn event
	log.Printf("[SUBAGENT_SPAWN] method=%s task_id=%s model=%s provider=%s timeout=%v",
		callerMethod, taskID, task.Model, task.Provider, !noTimeout)

	// Build command: ledit agent with the given prompt
	args := []string{"agent"}

	// Add provider/model if specified
	if task.Provider != "" {
		args = append(args, "--provider", task.Provider)
		if task.Model != "" {
			args = append(args, "--model", task.Model)
		}
	} else if task.Model != "" {
		args = append(args, "--model", task.Model)
	}

	args = append(args, task.Prompt)

	// Use the currently running ledit binary path to ensure consistency
	leditPath, err := os.Executable()
	if err != nil {
		log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=get_executable_failed details=%v",
			callerMethod, taskID, err)
		return &ParallelSubagentResult{
			ID:    taskID,
			Error: fmt.Errorf("failed to get current executable path: %w", err),
		}
	}

	// Create context based on noTimeout flag
	var ctx context.Context
	var cancel context.CancelFunc

	if noTimeout {
		ctx = context.Background()
	} else {
		timeout := GetSubagentTimeout()
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), timeout)
			defer cancel()
		} else {
			ctx = context.Background()
		}
	}

	cmd := exec.CommandContext(ctx, leditPath, args...)

	// Propagate important environment variables to subagent processes
	cmd.Env = append(os.Environ(), "LEDIT_FROM_AGENT=1")
	if debug := os.Getenv("LEDIT_DEBUG"); debug != "" {
		cmd.Env = append(cmd.Env, "LEDIT_DEBUG="+debug)
	}
	if unsafe := os.Getenv("LEDIT_UNSAFE_MODE"); unsafe != "" {
		cmd.Env = append(cmd.Env, "LEDIT_UNSAFE_MODE="+unsafe)
	}

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command and wait for completion (blocking)
	err = cmd.Run()

	// Determine exit code and timeout status
	exitCode := 0
	completed := true

	if err != nil {
		if !noTimeout && ctx.Err() == context.DeadlineExceeded {
			// Timeout occurred (only if timeout was configured)
			exitCode = 124
			log.Printf("[SUBAGENT_TIMEOUT] method=%s task_id=%s", callerMethod, taskID)
		} else if exitError, ok := err.(*exec.ExitError); ok {
			// Command ran but exited with non-zero status
			exitCode = exitError.ExitCode()
			log.Printf("[SUBAGENT_FAILED] method=%s task_id=%s exit_code=%d",
				callerMethod, taskID, exitCode)
		} else {
			// Couldn't start the command (e.g., ledit not found)
			exitCode = -1
			completed = false
			log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=exec_failed details=%v",
				callerMethod, taskID, err)

			return &ParallelSubagentResult{
				ID:    taskID,
				Error: err,
			}
		}
	}

	// Log completion
	if exitCode == 0 {
		log.Printf("[SUBAGENT_COMPLETE] method=%s task_id=%s status=success",
			callerMethod, taskID)
	} else if completed {
		log.Printf("[SUBAGENT_COMPLETE] method=%s task_id=%s status=non_zero_exit exit_code=%d",
			callerMethod, taskID, exitCode)
	}

	return &ParallelSubagentResult{
		ID:        taskID,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		ExitCode:  exitCode,
		Completed: completed,
		Error:     nil,
	}
}


