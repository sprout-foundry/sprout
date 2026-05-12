package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// StreamCallback is a function that receives streamed output from subagents
// line is the output line from the subagent
// taskID is the optional task identifier for parallel subagents
type StreamCallback func(line string, taskID string)

// emptyStdinReader is a reader that immediately returns EOF
type emptyStdinReader struct{}

func (r *emptyStdinReader) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

// No timeout for subagent execution - runs until completion
// Subagents are designed for long-running tasks and should not be restricted
const DefaultSubagentTimeout = 0 // 0 means no timeout

// Default max tokens for subagent execution
const DefaultSubagentMaxTokens = 2_000_000 // 2M tokens default budget

// subagentSpawnConfig holds configuration for spawning a subagent process
type subagentSpawnConfig struct {
	Ctx              context.Context // Caller-managed context (with optional timeout/budget cancel)
	WorkspaceRoot    string
	Prompt           string
	Model            string
	Provider         string
	SystemPromptPath string
	SystemPromptText string
	Persona          string
	StreamCallback   StreamCallback
	TaskID           string // empty for single subagent, set for parallel
	CallerMethod     string // for structured logging
	EnvExtras        []string
}

// subagentSpawnResult holds the result of a subagent spawn operation
type subagentSpawnResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Completed bool
	TimedOut  bool
	Err       error
}

// GetSubagentTimeout returns the configured timeout for subagent execution.
// It reads from SPROUT_SUBAGENT_TIMEOUT environment variable if set.
// A value of "0" or unset means NO timeout (runs indefinitely).
//
// Environment variable format: number followed by unit (e.g., "30m", "1h", "90s")
// Set to "0" or leave unset to disable timeout completely (recommended)
func GetSubagentTimeout() time.Duration {
	envTimeout := configuration.GetEnvSimple("SUBAGENT_TIMEOUT")
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

// GetSubagentMaxTokens returns the configured token budget for subagent execution.
// It reads from SPROUT_SUBAGENT_MAX_TOKENS environment variable if set.
// A value of "0" or unset means use the default 2M token budget.
//
// Environment variable format: integer number of tokens (e.g., "1000000" for 1M tokens)
func GetSubagentMaxTokens() int {
	envMaxTokens := configuration.GetEnvSimple("SUBAGENT_MAX_TOKENS")
	if envMaxTokens == "" {
		return DefaultSubagentMaxTokens // Use default
	}

	// Parse as integer
	maxTokens, err := strconv.Atoi(envMaxTokens)
	if err == nil && maxTokens >= 0 {
		return maxTokens
	}

	// If parsing failed, return default
	log.Printf("[WARNING] Invalid SPROUT_SUBAGENT_MAX_TOKENS value '%s', using default %d\n", envMaxTokens, DefaultSubagentMaxTokens)
	return DefaultSubagentMaxTokens
}

// spawnSubagentProcess is the shared helper that spawns a subagent process.
// It handles all the common logic for building and executing the subagent command.
//
// Parameters:
//   - config: Configuration struct with all spawn parameters
//
// Returns the result of the subagent execution.
func spawnSubagentProcess(config subagentSpawnConfig) subagentSpawnResult {
	// Generate a unique task ID for tracking
	taskID := config.TaskID
	if taskID == "" {
		taskID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}

	// Build command: sprout agent with the given prompt
	args := []string{"agent"}

	// Add persona prompt override, preferring inline text if provided
	if config.SystemPromptText != "" {
		args = append(args, "--system-prompt-str", config.SystemPromptText)
	} else if config.SystemPromptPath != "" {
		args = append(args, "--system-prompt", config.SystemPromptPath)
	}

	// Add provider/model if specified
	if config.Provider != "" {
		args = append(args, "--provider", config.Provider)
		if config.Model != "" {
			args = append(args, "--model", config.Model)
		}
	} else if config.Model != "" {
		args = append(args, "--model", config.Model)
	}

	args = append(args, "--prompt-stdin")

	// Use the currently running sprout binary path to ensure consistency
	sproutPath, err := os.Executable()
	if err != nil {
		log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=get_executable_failed details=%v",
			config.CallerMethod, taskID, err)
		return subagentSpawnResult{
			Err: fmt.Errorf("get current executable path: %w", err),
		}
	}

	// Create stdin pipe to pass the prompt to the subagent (avoids ARG_MAX limits)
	promptReader, promptWriter, err := os.Pipe()
	if err != nil {
		log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=stdin_pipe_failed details=%v",
			config.CallerMethod, taskID, err)
		return subagentSpawnResult{
			Err: fmt.Errorf("create stdin pipe for prompt: %w", err),
		}
	}

	// Create pipes for stdout and stderr to enable streaming
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=pipe_creation_failed details=%v",
			config.CallerMethod, taskID, err)
		promptReader.Close()
		promptWriter.Close()
		return subagentSpawnResult{
			Err: fmt.Errorf("create stdout pipe: %w", err),
		}
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=pipe_creation_failed details=%v",
			config.CallerMethod, taskID, err)
		promptReader.Close()
		promptWriter.Close()
		stdoutReader.Close()
		stdoutWriter.Close()
		return subagentSpawnResult{
			Err: fmt.Errorf("create stderr pipe: %w", err),
		}
	}

	cmd := exec.CommandContext(config.Ctx, sproutPath, args...)

	// Pass the prompt via stdin (child reads it with --prompt-stdin)
	cmd.Stdin = promptReader

	// Explicitly set working directory to the caller workspace so subagents do not
	// depend on the process-global cwd.
	if config.WorkspaceRoot != "" {
		cmd.Dir = config.WorkspaceRoot
	} else if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}

	// Build environment: base vars + config extras
	cmd.Env = append(os.Environ(),
		"SPROUT_FROM_AGENT=1",
		"LEDIT_FROM_AGENT=1",
		"SPROUT_SUBAGENT=1",
		"LEDIT_SUBAGENT=1",
	)

	if config.Persona != "" {
		cmd.Env = append(cmd.Env, "SPROUT_PERSONA="+config.Persona, "LEDIT_PERSONA="+config.Persona)
	}

	if debug := configuration.GetEnvSimple("DEBUG"); debug != "" {
		cmd.Env = append(cmd.Env, "SPROUT_DEBUG="+debug, "LEDIT_DEBUG="+debug)
	}

	if unsafe := configuration.GetEnvSimple("UNSAFE_MODE"); unsafe != "" {
		cmd.Env = append(cmd.Env, "SPROUT_UNSAFE_MODE="+unsafe, "LEDIT_UNSAFE_MODE="+unsafe)
	}

	// Add any extra environment variables from config
	cmd.Env = append(cmd.Env, config.EnvExtras...)

	// Also collect full output for return value
	var stdoutBuffer, stderrBuffer bytes.Buffer

	// Set up multi-writers: write to both buffer and pipe for streaming
	cmd.Stdout = io.MultiWriter(&stdoutBuffer, stdoutWriter)
	cmd.Stderr = io.MultiWriter(&stderrBuffer, stderrWriter)

	// Start the command (non-blocking)
	if err = cmd.Start(); err != nil {
		log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=start_failed details=%v",
			config.CallerMethod, taskID, err)
		promptReader.Close()
		promptWriter.Close()
		stdoutReader.Close()
		stdoutWriter.Close()
		stderrReader.Close()
		stderrWriter.Close()
		return subagentSpawnResult{
			Err: fmt.Errorf("start subagent: %w", err),
		}
	}

	// Write the prompt to stdin and close the write end
	if _, err := promptWriter.Write([]byte(config.Prompt)); err != nil {
		log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=prompt_write_failed details=%v\n",
			config.CallerMethod, taskID, err)
	}
	promptWriter.Close()

	// Close the stdin read end in the parent (child has its own copy)
	promptReader.Close()

	// Stream output in real-time if callback provided
	var wg sync.WaitGroup
	if config.StreamCallback != nil {
		// Stream stdout (with task ID for parallel subagents)
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stdoutReader)
			for scanner.Scan() {
				line := scanner.Text()
				config.StreamCallback(line, taskID)
			}
		}()

		// Stream stderr (with task ID for parallel subagents)
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stderrReader)
			for scanner.Scan() {
				line := scanner.Text()
				if line != "" {
					config.StreamCallback("STDERR: "+line, taskID)
				}
			}
		}()
	} else {
		// No callback, just drain the pipes to prevent blocking
		wg.Add(2)
		go func() {
			defer wg.Done()
			io.Copy(&stdoutBuffer, stdoutReader)
		}()
		go func() {
			defer wg.Done()
			io.Copy(&stderrBuffer, stderrReader)
		}()
	}

	// Wait for the command to complete
	err = cmd.Wait()

	// Close the write ends of the pipes to signal EOF to readers
	// This is critical - otherwise the pipe-draining goroutines will block indefinitely
	stdoutWriter.Close()
	stderrWriter.Close()

	// Wait for all streaming to complete
	wg.Wait()

	// Close readers
	stdoutReader.Close()
	stderrReader.Close()

	// Determine exit code and status
	exitCode := 0
	completed := true
	timedOut := false

	if err != nil {
		if config.Ctx.Err() == context.DeadlineExceeded {
			// Timeout occurred
			exitCode = 124
			timedOut = true
			log.Printf("[SUBAGENT_TIMEOUT] method=%s task_id=%s", config.CallerMethod, taskID)
		} else if exitError, ok := err.(*exec.ExitError); ok {
			// Command ran but exited with non-zero status
			exitCode = exitError.ExitCode()
			log.Printf("[SUBAGENT_FAILED] method=%s task_id=%s exit_code=%d",
				config.CallerMethod, taskID, exitCode)
		} else {
			// Couldn't start the command (e.g., sprout not found)
			exitCode = -1
			completed = false
			log.Printf("[SUBAGENT_ERROR] method=%s task_id=%s error=exec_failed details=%v",
				config.CallerMethod, taskID, err)
		}
	}

	// Log completion
	if exitCode == 0 {
		log.Printf("[SUBAGENT_COMPLETE] method=%s task_id=%s status=success",
			config.CallerMethod, taskID)
	} else if completed {
		if timedOut {
			log.Printf("[SUBAGENT_COMPLETE] method=%s task_id=%s status=timeout exit_code=%d",
				config.CallerMethod, taskID, exitCode)
		} else {
			log.Printf("[SUBAGENT_COMPLETE] method=%s task_id=%s status=non_zero_exit exit_code=%d",
				config.CallerMethod, taskID, exitCode)
		}
	}

	if !completed && err != nil {
		return subagentSpawnResult{
			Stdout:    stdoutBuffer.String(),
			Stderr:    stderrBuffer.String(),
			ExitCode:  exitCode,
			Completed: completed,
			TimedOut:  timedOut,
			Err:       err,
		}
	}

	return subagentSpawnResult{
		Stdout:    stdoutBuffer.String(),
		Stderr:    stderrBuffer.String(),
		ExitCode:  exitCode,
		Completed: completed,
		TimedOut:  timedOut,
		Err:       nil,
	}
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
//   - workspaceRoot: The workspace directory for the subagent
//   - prompt: The task/prompt for the subagent
//   - model: Optional model override (e.g., "qwen/qwen-coder-32b")
//   - provider: Optional provider override (e.g., "openrouter")
//   - streamCallback: Optional callback for real-time output streaming
//   - systemPromptPath: Optional path to system prompt file for specialized personas
//   - systemPromptText: Optional inline system prompt text (takes precedence over systemPromptPath)
//   - persona: Optional persona identifier (e.g., "coder", "planner")
//
// Returns map containing:
//   - stdout: Combined stdout output
//   - stderr: Combined stderr output
//   - exit_code: Process exit code (0 for success)
//   - completed: true if process ran to completion (always true for blocking mode)
//   - timed_out: true if the subprocess was terminated due to timeout (always false with no timeout)
//   - budget_exceeded: true if the subprocess was terminated due to token budget exceeded
func RunSubagent(workspaceRoot string, prompt, model, provider string, streamCallback StreamCallback, systemPromptPath, systemPromptText, persona string) (map[string]string, error) {
	// Create context (with optional timeout)
	timeout := GetSubagentTimeout()
	maxTokens := GetSubagentMaxTokens()
	var ctx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		// Only create timeout context if explicitly configured
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		// No timeout - create cancelable context for token budget monitoring
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	// Create temporary metrics file for token budget monitoring
	metricsFile := ""
	if maxTokens > 0 {
		tmpFile, err := os.CreateTemp("", "sprout-subagent-metrics-*.txt")
		if err != nil {
			log.Printf("[WARNING] Failed to create metrics file: %v (token budget monitoring disabled)", err)
		} else {
			metricsFile = tmpFile.Name()
			tmpFile.Close()
			defer os.Remove(metricsFile) // Clean up after subagent completes
			log.Printf("[SUBAGENT] Token budget monitoring enabled: max_tokens=%d, metrics_file=%s\n", maxTokens, metricsFile)
		}
	}

	// Monitor token budget if enabled
	var budgetExceeded bool
	if metricsFile != "" && maxTokens > 0 {
		monitorDone := make(chan bool)
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					tokens, _, _ := readSubagentMetrics(metricsFile)
					if tokens >= maxTokens {
						log.Printf("[SUBAGENT] Token budget exceeded: %d >= %d, cancelling subagent\n", tokens, maxTokens)
						budgetExceeded = true
						cancel() // Cancel the subagent context
						return
					}
				case <-monitorDone:
					return
				case <-ctx.Done():
					return
				}
			}
		}()
		defer close(monitorDone)
	}

	// Build spawn config
	config := subagentSpawnConfig{
		Ctx:              ctx,
		WorkspaceRoot:    workspaceRoot,
		Prompt:           prompt,
		Model:            model,
		Provider:         provider,
		SystemPromptPath: systemPromptPath,
		SystemPromptText: systemPromptText,
		Persona:          persona,
		StreamCallback:   streamCallback,
		TaskID:           "", // Empty for single subagent
		CallerMethod:     "RunSubagent",
	}

	// Spawn the subagent process
	result := spawnSubagentProcess(config)

	// Override exit code and timed_out if budget exceeded
	exitCode := result.ExitCode
	timedOut := result.TimedOut
	if budgetExceeded {
		exitCode = 125
		timedOut = false // Don't report timeout if budget exceeded
	}

	// Return all output with exit status and timeout/budget indicator
	return map[string]string{
		"stdout":          result.Stdout,
		"stderr":          result.Stderr,
		"exit_code":       fmt.Sprintf("%d", exitCode),
		"completed":       "true",
		"timed_out":       fmt.Sprintf("%t", timedOut),
		"budget_exceeded": fmt.Sprintf("%t", budgetExceeded),
	}, nil
}

// ParallelSubagentTask represents a single parallel subagent run task
type ParallelSubagentTask struct {
	ID       string
	Prompt   string
	Model    string
	Provider string
}

// ParallelSubagentResult represents the result of a single parallel subagent run
type ParallelSubagentResult struct {
	ID             string
	Stdout         string
	Stderr         string
	ExitCode       int
	Completed      bool
	TimedOut       bool
	BudgetExceeded bool
	Error          error
}

// RunParallelSubagents spawns multiple agent subprocesses in parallel, waits for all to complete,
// and returns all results. This enables the planning agent to execute independent tasks
// concurrently for faster overall completion time.
//
// Example use case: Writing production code and test cases simultaneously
//
// Parameters:
//   - workspaceRoot: The workspace directory for the subagents
//   - tasks: List of subagent tasks to run in parallel
//   - noTimeout: If true, uses context.Background() (no timeout). If false, respects GetSubagentTimeout()
//   - streamCallback: Optional callback for real-time output streaming
//
// Returns map where key is task ID and value contains that task's result
func RunParallelSubagents(workspaceRoot string, tasks []ParallelSubagentTask, noTimeout bool, streamCallback StreamCallback) (map[string]map[string]string, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks provided")
	}

	var wg sync.WaitGroup
	results := make(chan *ParallelSubagentResult, len(tasks))

	// Launch all subagents in parallel goroutines
	for _, task := range tasks {
		wg.Add(1)
		go func(t ParallelSubagentTask) {
			defer wg.Done()

			// Create context based on noTimeout flag
			var ctx context.Context
			if noTimeout {
				ctx = context.Background()
			} else {
				timeout := GetSubagentTimeout()
				if timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(context.Background(), timeout)
					defer cancel()
				} else {
					ctx = context.Background()
				}
			}

			// Build spawn config
			config := subagentSpawnConfig{
				Ctx:              ctx,
				WorkspaceRoot:    workspaceRoot,
				Prompt:           t.Prompt,
				Model:            t.Model,
				Provider:         t.Provider,
				SystemPromptPath: "", // Parallel subagents don't use system prompts
				SystemPromptText: "",
				Persona:          "", // Parallel subagents don't use personas
				StreamCallback:   streamCallback,
				TaskID:           t.ID,
				CallerMethod:     "RunParallelSubagents",
			}

			// Spawn the subagent process
			result := spawnSubagentProcess(config)

			// Convert to ParallelSubagentResult
			if result.Err != nil {
				results <- &ParallelSubagentResult{
					ID:             t.ID,
					Error:          result.Err,
					ExitCode:       -1,
					Completed:      false,
					TimedOut:       false,
					BudgetExceeded: false,
				}
			} else {
				results <- &ParallelSubagentResult{
					ID:             t.ID,
					Stdout:         result.Stdout,
					Stderr:         result.Stderr,
					ExitCode:       result.ExitCode,
					Completed:      result.Completed,
					TimedOut:       result.TimedOut,
					BudgetExceeded: false, // Parallel subagents don't have budget monitoring
					Error:          nil,
				}
			}
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
				"error":           result.Error.Error(),
				"exit_code":       "-1",
				"completed":       "false",
				"timed_out":       "false",
				"budget_exceeded": "false",
			}
			continue
		}

		outputMap[result.ID] = map[string]string{
			"stdout":           result.Stdout,
			"stderr":           result.Stderr,
			"exit_code":        fmt.Sprintf("%d", result.ExitCode),
			"completed":        fmt.Sprintf("%t", result.Completed),
			"timed_out":        fmt.Sprintf("%t", result.TimedOut),
			"budget_exceeded":  fmt.Sprintf("%t", result.BudgetExceeded),
		}
	}

	return outputMap, nil
}

// readSubagentMetrics reads token usage from a metrics file
func readSubagentMetrics(metricsFile string) (tokens int, cost float64, err error) {
	if metricsFile == "" {
		return 0, 0, fmt.Errorf("no metrics file specified")
	}

	data, err := os.ReadFile(metricsFile)
	if err != nil {
		// File doesn't exist yet (subagent hasn't written anything)
		return 0, 0, nil
	}

	// Parse format: "tokens:123,cost:0.123"
	content := string(data)
	tokensStr := ""
	costStr := ""

	_, err = fmt.Sscanf(content, "tokens:%s,cost:%s", &tokensStr, &costStr)
	if err != nil {
		return 0, 0, fmt.Errorf("parse metrics: %w", err)
	}

	// Parse and validate token count
	tokens, err = strconv.Atoi(tokensStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid token value '%s': %w", tokensStr, err)
	}

	// Parse and validate cost
	cost, err = strconv.ParseFloat(costStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cost value '%s': %w", costStr, err)
	}

	// Validate values are non-negative
	if tokens < 0 {
		return 0, 0, fmt.Errorf("tokens cannot be negative: %d", tokens)
	}
	if cost < 0 {
		return 0, 0, fmt.Errorf("cost cannot be negative: %f", cost)
	}

	return tokens, cost, nil
}
