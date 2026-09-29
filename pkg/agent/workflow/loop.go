// Package workflow provides the in-process workflow runner for TODO-loop
// workflows. It eliminates subprocess spawning (the BPM/exec.Command path
// that requires nohup and breaks across OS/process-group boundaries) by
// running the workflow loop in-process as a goroutine with a fresh Agent.
//
// SP-141 phase 1: the loop logic lives here; pkg/agent's workflow_wiring.go
// constructs the fresh Agent (unexported fields make construction
// agent-internal) and calls RunTodoLoop. The arrow is one-way — this
// package defines the LoopAgent seam and never imports pkg/agent.
package workflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

// LoopAgent is the seam the loop needs from its runner agent — the
// exported-method surface the TODO loop actually uses. *agent.Agent
// satisfies it structurally (wired in pkg/agent/workflow_wiring.go).
type LoopAgent interface {
	GetProvider() string
	GetModel() string
	GetMaxIterations() int
	SetMaxIterations(max int)
	GenerateResponse(messages []api.Message) (string, error)
	ProcessQueryWithContinuity(userQuery string) (string, error)
	ClearConversationHistory()
	FleetBudgetExceeded() bool
	// BudgetSnapshot reports (spent, limit) for the heartbeat; a nil-nil
	// return means "no fleet budget — use Cost() instead".
	BudgetSnapshot() (float64, float64, bool)
	Cost() float64
	Iteration() int
}

type WorkflowLoopConfig struct {
	TodoFile       string `json:"todo_file,omitempty"`
	GatePromptFile string `json:"gate_prompt_file,omitempty"`
	MaxRetries     int    `json:"max_retries,omitempty"`
	MaxIterations  int    `json:"max_iterations,omitempty"`
	BuildCommand   string `json:"build_command,omitempty"`
}

// applyDefaults fills in zero-value fields with the same defaults used by
// cmd/agent_workflow_loader.go so the runner behaves identically.
func (c *WorkflowLoopConfig) ApplyDefaults() {
	if c.TodoFile == "" {
		c.TodoFile = "TODO.md"
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 2
	}
	if c.MaxIterations <= 0 {
		c.MaxIterations = 50
	}
	if c.BuildCommand == "" {
		c.BuildCommand = "go build ./..."
	}
}

// WorkflowBudgetConfig is parsed from the "budget" section of a workflow JSON.
type WorkflowBudgetConfig struct {
	USD    float64   `json:"usd,omitempty"`
	WarnAt []float64 `json:"warn_at,omitempty"`
}

// WorkflowProgressConfig is parsed from the "progress" section.
type WorkflowProgressConfig struct {
	HeartbeatSeconds int `json:"heartbeat_seconds,omitempty"`
}

// workflowFileConfig is the top-level structure parsed from the workflow JSON
// file. It mirrors only the fields the in-process runner cares about.
type WorkflowFileConfig struct {
	Description string                  `json:"description,omitempty"`
	Loop        *WorkflowLoopConfig     `json:"loop,omitempty"`
	Budget      *WorkflowBudgetConfig   `json:"budget,omitempty"`
	Progress    *WorkflowProgressConfig `json:"progress,omitempty"`
}

// ---------------------------------------------------------------------------
// Result type
// ---------------------------------------------------------------------------

// WorkflowResult is returned when the workflow completes.
type WorkflowResult struct {
	ItemsProcessed int
	ItemsSkipped   int
	ItemsFailed    int
	Error          error
}

// ---------------------------------------------------------------------------
// loop-scoped gate types (mirror cmd versions)
// ---------------------------------------------------------------------------

// workflowGateResult is the JSON response from the gate LLM call.
type workflowGateResult struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Skip       bool   `json:"skip"`
	SkipReason string `json:"skip_reason"`
}

// workflowGateTriageResult is the JSON response from the triage gate call.
type workflowGateTriageResult struct {
	Action string `json:"action"` // "retry" or "skip"
	Reason string `json:"reason"`
}

// workflowOutcome classifies a single TODO item's outcome.
type workflowOutcome int

const (
	outcomeProcessed workflowOutcome = iota
	outcomeFailed
	outcomeIncomplete
	outcomeSkipped
)

// ---------------------------------------------------------------------------
// Helper: generate a session ID
// ---------------------------------------------------------------------------

// GenerateWorkflowSessionID returns a fresh workflow session identifier.
func GenerateWorkflowSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("wf-inproc-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("wf-inproc-%s", hex.EncodeToString(b))
}

// RunTodoLoop runs the TODO loop against a prepared loop agent (see
// pkg/agent/workflow_wiring.go for construction). ctx governs
// cancellation; configPath resolves the todo file relative to the workflow
// JSON; loop carries the parsed loop config; gatePromptText is the gate
// prompt's file contents; stopBudget retires the heartbeat/budget
// teardown started by the constructor.
func RunTodoLoop(ctx context.Context, loopAgent LoopAgent, configPath string, loop *WorkflowLoopConfig, gatePromptText string, stopBudget func()) (*WorkflowResult, error) {
	// TODO file path — resolve relative to the workflow config file's directory.
	todoDir := filepath.Dir(configPath)
	todoFile := filepath.Join(todoDir, loop.TodoFile)

	// -----------------------------------------------------------------------
	// Run the TODO loop
	// -----------------------------------------------------------------------
	result := &WorkflowResult{}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "TODO loop: provider=%s model=%s todo=%s\n",
		loopAgent.GetProvider(), loopAgent.GetModel(), todoFile)

	startAfter := 0 // 0-based line index for scan start

	for {
		// Check context cancellation.
		if err := ctx.Err(); err != nil {
			stopBudget()
			result.Error = agenterrors.NewAgent("workflow_runner", "workflow cancelled", err)
			return result, nil
		}

		// Check budget exceeded.
		if loopAgent.FleetBudgetExceeded() {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "Budget exceeded — stopping workflow loop\n")
			stopBudget()
			return result, nil
		}

		// Find next unchecked item.
		lineNum, sectionText, findErr := findNextTodoItemInFile(todoFile, startAfter)
		startAfter = 0 // Reset after first scan so subsequent iterations start from the beginning.
		if findErr != nil {
			if strings.Contains(findErr.Error(), "no unchecked") {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "TODO loop complete: processed=%d skipped=%d failed=%d\n",
					result.ItemsProcessed, result.ItemsSkipped, result.ItemsFailed)
				stopBudget()
				return result, nil
			}
			stopBudget()
			return nil, agenterrors.NewAgent("workflow_runner", "failed to find next TODO item", findErr)
		}

		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "TODO item at line %d\n", lineNum)

		// --- Gate call ---
		gateText, gateErr := loopAgent.GenerateResponse([]api.Message{
			{Role: "system", Content: gatePromptText},
			{Role: "user", Content: sectionText},
		})
		if gateErr != nil {
			fmt.Fprintf(os.Stderr, "Gate call failed: %v\n", gateErr)
			result.ItemsFailed++
			continue
		}

		gateRes, parseErr := parseWorkflowGateResponse(gateText)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "Gate parse failed: %v\n", parseErr)
			result.ItemsFailed++
			continue
		}

		fmt.Fprintf(os.Stderr, "Gate: title=%q skip=%v\n", gateRes.Title, gateRes.Skip)

		// Skip?
		if gateRes.Skip {
			reason := gateRes.SkipReason
			if reason == "" {
				reason = "no reason given"
			}
			fmt.Fprintf(os.Stderr, "Skipping: %s\n", reason)
			if mErr := markTodoDoneInFile(todoFile, lineNum); mErr != nil {
				fmt.Fprintf(os.Stderr, "Failed to mark item done: %v\n", mErr)
			}
			result.ItemsSkipped++
			continue
		}

		if gateRes.Prompt == "" {
			fmt.Fprintf(os.Stderr, "Gate returned empty prompt, skipping item\n")
			result.ItemsFailed++
			continue
		}

		// --- Process the item ---
		fmt.Fprintf(os.Stderr, "Processing: %s\n", gateRes.Title)

		// Save original max iterations, override with loop config.
		prevMaxIter := loopAgent.GetMaxIterations()
		loopAgent.SetMaxIterations(loop.MaxIterations)

		_, processErr := loopAgent.ProcessQueryWithContinuity(gateRes.Prompt)

		// Restore max iterations.
		loopAgent.SetMaxIterations(prevMaxIter)

		if processErr != nil {
			fmt.Fprintf(os.Stderr, "Agent processing failed: %v\n", processErr)
		}

		// --- Build verification ---
		buildFailed := false
		buildCmd := strings.TrimSpace(loop.BuildCommand)
		if buildCmd != "" {
			fmt.Fprintf(os.Stderr, "%s\n", buildCmd)
			cmd := shellexec.CommandContext(ctx, buildCmd) // #nosec G204 G702 -- build_command comes from the user's own workflow JSON; running it IS the feature
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if bErr := cmd.Run(); bErr != nil {
				fmt.Fprintf(os.Stderr, "Build failed: %v\n", bErr)
				buildFailed = true
			} else {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Build passed\n")
			}
		}

		// --- Triage on failure ---
		retries := 0
		retrySucceeded := false
		triageSkipped := false
		for buildFailed && retries < loop.MaxRetries {
			retries++
			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "Build failed — triaging (attempt %d/%d)\n", retries, loop.MaxRetries)

			triageText, triageErr := loopAgent.GenerateResponse([]api.Message{
				{Role: "system", Content: "You are a build error triage agent. Given a task title and context, decide: retry (transient/fixable) or skip (fundamental/blocking). Return ONLY JSON: {\"action\": \"retry\"|\"skip\", \"reason\": \"...\"}"},
				{Role: "user", Content: fmt.Sprintf("Task: %s\n\nPrevious attempt failed. Decide whether to retry or skip.", gateRes.Title)},
			})
			if triageErr != nil {
				fmt.Fprintf(os.Stderr, "Triage gate call failed: %v — defaulting to retry\n", triageErr)
				triageText = `{"action": "retry", "reason": "triage failed"}`
			}

			triageRes, pErr := parseWorkflowTriageResponse(triageText)
			if pErr != nil {
				fmt.Fprintf(os.Stderr, "Triage parse failed: %v — defaulting to retry\n", pErr)
				triageRes = workflowGateTriageResult{Action: "retry", Reason: "parse failed"}
			}

			fmt.Fprintf(os.Stderr, "Triage: action=%s reason=%s\n", triageRes.Action, triageRes.Reason)

			if strings.EqualFold(triageRes.Action, "skip") {
				triageSkipped = true
				result.ItemsSkipped++
				fmt.Fprintf(os.Stderr, "Triage skipped: %s\n", gateRes.Title)
				break
			}

			// Retry: clear conversation history and re-run with a fix prompt.
			loopAgent.ClearConversationHistory()

			retryPrompt := fmt.Sprintf(
				"Previous attempt failed. Fix the issue and ensure the build passes.\n\nOriginal task:\n%s",
				gateRes.Prompt)

			retryMaxIter := loop.MaxIterations / 2
			if retryMaxIter < 5 {
				retryMaxIter = 5
			}
			loopAgent.SetMaxIterations(retryMaxIter)

			_, retryErr := loopAgent.ProcessQueryWithContinuity(retryPrompt)
			loopAgent.SetMaxIterations(prevMaxIter)

			if retryErr != nil {
				fmt.Fprintf(os.Stderr, "Retry agent processing failed: %v\n", retryErr)
			}

			// Re-check build.
			if buildCmd != "" {
				cmd := shellexec.CommandContext(ctx, buildCmd) // #nosec G204 G702 -- retry of the same user-authored build_command
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if bErr := cmd.Run(); bErr != nil {
					fmt.Fprintf(os.Stderr, "Build still fails after retry: %v\n", bErr)
				} else {
					buildFailed = false
					retrySucceeded = retryErr == nil
					fmt.Fprintln(os.Stderr)
					fmt.Fprintf(os.Stderr, "Build passed after retry\n")
				}
			}
		}

		// --- Classify outcome ---
		switch classifyWorkflowOutcome(buildFailed, processErr, retrySucceeded, triageSkipped) {
		case outcomeSkipped:
			// Already counted.
		case outcomeFailed:
			result.ItemsFailed++
			fmt.Fprintf(os.Stderr, "Item failed after retries: %s\n", gateRes.Title)
		case outcomeIncomplete:
			result.ItemsFailed++
			fmt.Fprintf(os.Stderr, "Build passes but agent didn't complete: %v\n", processErr)
		case outcomeProcessed:
			if mErr := markTodoDoneInFile(todoFile, lineNum); mErr != nil {
				fmt.Fprintf(os.Stderr, "Failed to mark item done: %v\n", mErr)
			} else {
				result.ItemsProcessed++
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Item complete: %s\n", gateRes.Title)
			}
		}

		// Clear conversation context for the next item.
		loopAgent.ClearConversationHistory()
	}
}

// ---------------------------------------------------------------------------
// Heartbeat (lightweight version for budget visibility during long runs)
// ---------------------------------------------------------------------------

// StartWorkflowHeartbeat runs the budget/iteration heartbeat until the
// returned stop func is called.
func StartWorkflowHeartbeat(chatAgent LoopAgent, interval time.Duration) func() {
	if chatAgent == nil || interval <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	started := time.Now()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				spent, limit, capped := chatAgent.BudgetSnapshot()
				if !capped {
					limit = 0
					spent = chatAgent.Cost()
				}
				iter := chatAgent.Iteration()
				elapsed := time.Since(started).Round(time.Second)
				if limit > 0 {
					fmt.Fprintf(os.Stderr, "\n$%.2f of $%.2f · iter %d · elapsed %s\n",
						spent, limit, iter, elapsed)
				} else {
					fmt.Fprintf(os.Stderr, "\n$%.2f (no cap) · iter %d · elapsed %s\n",
						spent, iter, elapsed)
				}
			}
		}
	}()
	return func() { close(stop) }
}

// ---------------------------------------------------------------------------
// TODO file helpers (mirror cmd/agent_workflow_loop.go)
// ---------------------------------------------------------------------------

// findNextTodoItemInFile reads a markdown file and returns:
// - lineNum: the 1-based line number of the first "[ ]" item found
// - sectionText: the text of the enclosing ## section
// - err: non-nil if the file can't be read or no unchecked items exist
func findNextTodoItemInFile(todoFile string, startAfterLine int) (lineNum int, sectionText string, err error) {
	data, err := os.ReadFile(filepath.Clean(todoFile))
	if err != nil {
		return 0, "", agenterrors.NewAgent("workflow_runner", fmt.Sprintf("failed to read %s", todoFile), err)
	}

	lines := strings.Split(string(data), "\n")
	uncheckedRe := regexp.MustCompile(`^\s*- \[ \]`)

	// Find first unchecked item at or after startAfterLine.
	itemLine := -1
	for i, line := range lines {
		if i < startAfterLine {
			continue
		}
		if uncheckedRe.MatchString(line) {
			itemLine = i
			break
		}
	}
	if itemLine < 0 {
		return 0, "", agenterrors.NewInvalidInputError(fmt.Sprintf("no unchecked [ ] items found in %s", todoFile), nil)
	}

	// Find the enclosing ## section header by searching upward.
	headerRe := regexp.MustCompile(`^## `)
	sectionStart := 0
	for i := itemLine - 1; i >= 0; i-- {
		if headerRe.MatchString(lines[i]) {
			sectionStart = i
			break
		}
	}

	// Find the next ## header after the item (search downward).
	sectionEnd := len(lines)
	for i := itemLine + 1; i < len(lines); i++ {
		if headerRe.MatchString(lines[i]) {
			sectionEnd = i
			break
		}
	}

	sectionText = strings.Join(lines[sectionStart:sectionEnd], "\n")
	return itemLine + 1, sectionText, nil // return 1-based line number
}

// markTodoDoneInFile changes "- [ ]" to "- [x]" at the given 1-based line
// number in the specified markdown file.
func markTodoDoneInFile(todoFile string, lineNum int) error {
	data, err := os.ReadFile(filepath.Clean(todoFile))
	if err != nil {
		return agenterrors.NewAgent("workflow_runner", fmt.Sprintf("failed to read %s", todoFile), err)
	}

	lines := bytes.Split(data, []byte("\n"))
	if lineNum < 1 || lineNum > len(lines) {
		return agenterrors.NewInvalidInputError(fmt.Sprintf("line number %d out of range (file has %d lines)", lineNum, len(lines)), nil)
	}

	idx := lineNum - 1 // 0-based
	orig := lines[idx]
	modified := bytes.Replace(orig, []byte("- [ ]"), []byte("- [x]"), 1)

	if bytes.Equal(orig, modified) {
		return agenterrors.NewInvalidInputError(fmt.Sprintf("line %d does not contain '- [ ]': %s", lineNum, orig), nil)
	}

	lines[idx] = modified
	return os.WriteFile(filepath.Clean(todoFile), bytes.Join(lines, []byte("\n")), 0644) // #nosec G703 -- todoFile resolves within the workflow JSON's directory (the user's own file)
}

// ---------------------------------------------------------------------------
// Gate response parsing
// ---------------------------------------------------------------------------

// parseWorkflowGateResponse extracts a workflowGateResult from the LLM's
// text response, stripping markdown fences if present.
func parseWorkflowGateResponse(text string) (workflowGateResult, error) {
	text = trimWorkflowMarkdownFence(text)
	var result workflowGateResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return workflowGateResult{}, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse gate JSON (text: %s)", text), err)
	}
	return result, nil
}

// parseWorkflowTriageResponse extracts a workflowGateTriageResult from the
// LLM's text response.
func parseWorkflowTriageResponse(text string) (workflowGateTriageResult, error) {
	text = trimWorkflowMarkdownFence(text)
	var result workflowGateTriageResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return workflowGateTriageResult{}, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse triage JSON (text: %s)", text), err)
	}
	return result, nil
}

// trimWorkflowMarkdownFence strips opening and closing markdown code fences
// from text.
func trimWorkflowMarkdownFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	var inner []string
	inFence := false
	for _, line := range lines {
		if !inFence {
			if strings.HasPrefix(line, "```") {
				inFence = true
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		inner = append(inner, line)
	}
	return strings.Join(inner, "\n")
}

// classifyWorkflowOutcome is the pure decision logic for categorizing a TODO
// item's result. It maps the four boolean-like signals into a single outcome.
func classifyWorkflowOutcome(buildFailed bool, processErr error, retrySucceeded bool, triageSkipped bool) workflowOutcome {
	if triageSkipped {
		return outcomeSkipped
	}
	if buildFailed {
		return outcomeFailed
	}
	if processErr != nil && !retrySucceeded {
		return outcomeIncomplete
	}
	return outcomeProcessed
}

// ---------------------------------------------------------------------------
// File parsing helper
// ---------------------------------------------------------------------------

// parseWorkflowFile reads and parses a workflow JSON file for its loop
// configuration. Returns only the fields the in-process runner needs.
// ParseWorkflowFile reads and parses a workflow JSON definition.
func ParseWorkflowFile(path string) (*WorkflowFileConfig, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, agenterrors.NewAgent("workflow_runner", fmt.Sprintf("failed to read %q", path), err)
	}
	var cfg WorkflowFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse %q", path), err)
	}
	return &cfg, nil
}
