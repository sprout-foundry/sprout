// Package agent provides the in-process workflow runner for TODO-loop
// workflows. It eliminates subprocess spawning (the BPM/exec.Command path
// that requires nohup and breaks across OS/process-group boundaries) by
// running the workflow loop in-process as a goroutine with a fresh Agent.
package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// ---------------------------------------------------------------------------
// Config types — lightweight subset of cmd/AgentWorkflowConfig, parsed
// directly from the workflow JSON so the agent package has no import cycle
// with cmd/.
// ---------------------------------------------------------------------------

// WorkflowLoopConfig is parsed from the "loop" section of a workflow JSON
// file. Only the fields relevant to the in-process runner are included.
type WorkflowLoopConfig struct {
	TodoFile       string `json:"todo_file,omitempty"`
	GatePromptFile string `json:"gate_prompt_file,omitempty"`
	MaxRetries     int    `json:"max_retries,omitempty"`
	MaxIterations  int    `json:"max_iterations,omitempty"`
	BuildCommand   string `json:"build_command,omitempty"`
}

// applyDefaults fills in zero-value fields with the same defaults used by
// cmd/agent_workflow_loader.go so the runner behaves identically.
func (c *WorkflowLoopConfig) applyDefaults() {
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
type workflowFileConfig struct {
	Description string                 `json:"description,omitempty"`
	Loop        *WorkflowLoopConfig    `json:"loop,omitempty"`
	Budget      *WorkflowBudgetConfig  `json:"budget,omitempty"`
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

func generateWorkflowSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("wf-inproc-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("wf-inproc-%s", hex.EncodeToString(b))
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

// RunWorkflowLoopInProcess creates a fresh agent and runs the TODO loop
// workflow in the calling goroutine (blocking). For non-blocking use,
// call it from a goroutine.
//
// The fresh agent is created using the same pattern as subagents:
// new client from factory, new state managers, proper interrupt context,
// full tool wiring via the seed tool registry, and budget tracking.
//
// configPath is the path to the workflow JSON file. The file is parsed for
// the "loop" section; if no loop section is found, an error is returned.
func RunWorkflowLoopInProcess(ctx context.Context, parentAgent *Agent, configPath string, eventBus *events.EventBus) (*WorkflowResult, error) {
	if parentAgent == nil {
		return nil, agenterrors.NewValidation("parentAgent is required", nil)
	}
	if parentAgent.configManager == nil {
		return nil, agenterrors.NewConfig("parent config manager is required", nil)
	}

	// Parse the workflow config file.
	cfg, err := parseWorkflowFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse workflow config %q: %w", configPath, err)
	}

	if cfg.Loop == nil {
		return nil, fmt.Errorf("workflow %q has no 'loop' section", configPath)
	}

	loop := cfg.Loop
	loop.applyDefaults()

	// Read the gate prompt file.
	gatePromptBytes, err := os.ReadFile(filepath.Clean(loop.GatePromptFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read gate_prompt_file %q: %w", loop.GatePromptFile, err)
	}
	gatePromptText := strings.TrimSpace(string(gatePromptBytes))
	if gatePromptText == "" {
		return nil, fmt.Errorf("gate_prompt_file %q is empty", loop.GatePromptFile)
	}

	// Derive provider/model from the parent agent.
	provider := parentAgent.GetProvider()
	model := parentAgent.GetModel()

	// Resolve client type from config.
	clientType, finalModel, err := parentAgent.configManager.ResolveProviderModel(provider, model)
	if err != nil {
		return nil, agenterrors.Wrap(err, "resolve provider/model for workflow agent")
	}

	// Create client via factory.
	client, err := factory.CreateProviderClient(clientType, finalModel)
	if err != nil {
		return nil, agenterrors.Wrap(err, "create client for workflow agent")
	}

	// Build system prompt.
	systemPrompt := "You are a helpful coding assistant executing a TODO-based workflow."

	// Determine effective workspace root.
	effectiveWorkspaceRoot := parentAgent.workspaceRoot
	if effectiveWorkspaceRoot == "" {
		effectiveWorkspaceRoot, _ = os.Getwd()
	}

	// Create interrupt context derived from the caller's context so
	// cancellation propagates into the workflow agent's LLM calls.
	interruptCtx, interruptCancel := context.WithCancel(ctx)

	// Create fresh sub-managers for isolation from the parent agent.
	stateMgr := NewAgentStateManager(false)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	// Construct the fresh agent struct — mirrors createSubagent() from
	// subagent_creation.go.
	workflowAgent := &Agent{
		client:              client,
		clientType:          clientType,
		systemPrompt:        systemPrompt,
		baseSystemPrompt:    systemPrompt,
		maxIterations:       loop.MaxIterations,
		configManager:       parentAgent.configManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        interruptCtx,
		interruptCancel:     interruptCancel,
		parentInterruptCtx:  ctx,
		workspaceRoot:       effectiveWorkspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
		todoMgr:             tools.NewTodoManager(),
		eventBus:            eventBus,
		shellCwd:            &shellCwdTracker{},
		subagentDepth:       parentAgent.subagentDepth + 1,
		rootPersonaID:       parentAgent.rootPersonaID,
	}

	// Propagate risk profile override from the parent so that a
	// --risk-profile=readonly applies inside the loop agent too.
	if parentAgent.riskProfileOverride != "" {
		workflowAgent.riskProfileOverride = parentAgent.riskProfileOverride
	}

	// Inherit the parent's TerminalManager so shell_command with
	// background=true / check_background works inside the loop.
	if tm := parentAgent.GetTerminalManager(); tm != nil {
		workflowAgent.terminalManager = tm
	}

	// Share the parent's clarificationManager so the workflow agent
	// can call request_clarification through the same instance.
	if parentAgent.clarificationManager != nil {
		workflowAgent.clarificationManager = parentAgent.clarificationManager
	}

	// Enable lightweight change tracking.
	workflowAgent.EnableChangeTracking("workflow loop")

	// Wire the event bus for publishing events.
	if eventBus != nil {
		workflowAgent.SetEventBus(eventBus)
	}

	// Set event metadata so events carry routing keys for the WebUI.
	workflowAgent.SetEventMetadata(map[string]interface{}{
		"subagent_depth": workflowAgent.subagentDepth,
		"active_persona": "workflow-loop",
	})

	// -----------------------------------------------------------------------
	// Budget setup
	// -----------------------------------------------------------------------
	stopBudget := func() {}
	if cfg.Budget != nil && cfg.Budget.USD > 0 {
		warnAt := cfg.Budget.WarnAt
		if len(warnAt) == 0 {
			warnAt = []float64{0.50, 0.80}
		}
		budget := NewFleetUsdBudget(cfg.Budget.USD, warnAt)
		workflowAgent.SetFleetUsdBudget(budget)

		workflowAgent.SetBudgetWarningCallback(func(threshold, spent, limit float64) {
			fmt.Fprintf(os.Stderr, "\nWARNING — crossed %.0f%% threshold: $%.2f of $%.2f spent\n",
				threshold*100, spent, limit)
		})
		workflowAgent.SetBudgetExceededCallback(func(spent, limit float64) {
			fmt.Fprintf(os.Stderr, "\nCAP HIT — $%.2f of $%.2f spent; stopping.\n", spent, limit)
		})

		heartbeatSeconds := 600
		if cfg.Progress != nil && cfg.Progress.HeartbeatSeconds > 0 {
			heartbeatSeconds = cfg.Progress.HeartbeatSeconds
		}
		stopBudget = startWorkflowHeartbeat(workflowAgent, time.Duration(heartbeatSeconds)*time.Second)
	}

	// TODO file path — resolve relative to the workflow config file's directory.
	todoDir := filepath.Dir(configPath)
	todoFile := filepath.Join(todoDir, loop.TodoFile)

	// -----------------------------------------------------------------------
	// Run the TODO loop
	// -----------------------------------------------------------------------
	result := &WorkflowResult{}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "TODO loop: provider=%s model=%s todo=%s\n",
		workflowAgent.GetProvider(), workflowAgent.GetModel(), todoFile)

	startAfter := 0 // 0-based line index for scan start

	for {
		// Check context cancellation.
		if err := ctx.Err(); err != nil {
			stopBudget()
			result.Error = fmt.Errorf("workflow cancelled: %w", err)
			return result, nil
		}

		// Check budget exceeded.
		if workflowAgent.FleetBudgetExceeded() {
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
			return nil, fmt.Errorf("failed to find next TODO item: %w", findErr)
		}

		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "TODO item at line %d\n", lineNum)

		// --- Gate call ---
		gateText, gateErr := workflowAgent.GenerateResponse([]api.Message{
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
		prevMaxIter := workflowAgent.GetMaxIterations()
		workflowAgent.SetMaxIterations(loop.MaxIterations)

		_, processErr := workflowAgent.ProcessQueryWithContinuity(gateRes.Prompt)

		// Restore max iterations.
		workflowAgent.SetMaxIterations(prevMaxIter)

		if processErr != nil {
			fmt.Fprintf(os.Stderr, "Agent processing failed: %v\n", processErr)
		}

		// --- Build verification ---
		buildFailed := false
		buildCmd := strings.TrimSpace(loop.BuildCommand)
		if buildCmd != "" {
			fmt.Fprintf(os.Stderr, "%s\n", buildCmd)
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/sh"
			}
			cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
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

			triageText, triageErr := workflowAgent.GenerateResponse([]api.Message{
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
			workflowAgent.ClearConversationHistory()

			retryPrompt := fmt.Sprintf(
				"Previous attempt failed. Fix the issue and ensure the build passes.\n\nOriginal task:\n%s",
				gateRes.Prompt)

			retryMaxIter := loop.MaxIterations / 2
			if retryMaxIter < 5 {
				retryMaxIter = 5
			}
			workflowAgent.SetMaxIterations(retryMaxIter)

			_, retryErr := workflowAgent.ProcessQueryWithContinuity(retryPrompt)
			workflowAgent.SetMaxIterations(prevMaxIter)

			if retryErr != nil {
				fmt.Fprintf(os.Stderr, "Retry agent processing failed: %v\n", retryErr)
			}

			// Re-check build.
			if buildCmd != "" {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
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
		workflowAgent.ClearConversationHistory()
	}
}

// ---------------------------------------------------------------------------
// Heartbeat (lightweight version for budget visibility during long runs)
// ---------------------------------------------------------------------------

func startWorkflowHeartbeat(chatAgent *Agent, interval time.Duration) func() {
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
				spent, limit := 0.0, 0.0
				if b := chatAgent.GetFleetUsdBudget(); b != nil {
					spent, limit = b.Snapshot()
				} else {
					spent = chatAgent.GetTotalCost()
				}
				iter := chatAgent.GetCurrentIteration()
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
		return 0, "", fmt.Errorf("failed to read %s: %w", todoFile, err)
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
		return 0, "", fmt.Errorf("no unchecked [ ] items found in %s", todoFile)
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
		return fmt.Errorf("failed to read %s: %w", todoFile, err)
	}

	lines := bytes.Split(data, []byte("\n"))
	if lineNum < 1 || lineNum > len(lines) {
		return fmt.Errorf("line number %d out of range (file has %d lines)", lineNum, len(lines))
	}

	idx := lineNum - 1 // 0-based
	orig := lines[idx]
	modified := bytes.Replace(orig, []byte("- [ ]"), []byte("- [x]"), 1)

	if bytes.Equal(orig, modified) {
		return fmt.Errorf("line %d does not contain '- [ ]': %s", lineNum, orig)
	}

	lines[idx] = modified
	return os.WriteFile(filepath.Clean(todoFile), bytes.Join(lines, []byte("\n")), 0644)
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
		return workflowGateResult{}, fmt.Errorf("failed to parse gate JSON: %w (text: %s)", err, text)
	}
	return result, nil
}

// parseWorkflowTriageResponse extracts a workflowGateTriageResult from the
// LLM's text response.
func parseWorkflowTriageResponse(text string) (workflowGateTriageResult, error) {
	text = trimWorkflowMarkdownFence(text)
	var result workflowGateTriageResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return workflowGateTriageResult{}, fmt.Errorf("failed to parse triage JSON: %w (text: %s)", err, text)
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
func parseWorkflowFile(path string) (*workflowFileConfig, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read %q: %w", path, err)
	}
	var cfg workflowFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %q: %w", path, err)
	}
	return &cfg, nil
}
