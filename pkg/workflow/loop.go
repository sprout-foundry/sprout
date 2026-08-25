//go:build !js

package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// gateResult is the JSON response from the gate LLM call.
type gateResult struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Skip       bool   `json:"skip"`
	SkipReason string `json:"skip_reason"`
}

// gateTriageResult is the JSON response from the triage gate call.
type gateTriageResult struct {
	Action string `json:"action"` // "retry" or "skip"
	Reason string `json:"reason"`
}

// findNextTodoItem reads a markdown file and returns:
//   - lineNum: the 1-based line number of the first "[ ]" item found after startAfterLine
//   - sectionText: the text of the enclosing ## section (from the ## header above
//     the item to just before the next ## header or end of file)
//   - err: non-nil if the file can't be read or no unchecked items exist
//
// startAfterLine is 0-based: lines before this index are skipped.
// Pass 0 to scan from the beginning.
func findNextTodoItem(todoFile string, startAfterLine int) (lineNum int, sectionText string, err error) {
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

// markTodoDone changes "- [ ]" to "- [x]" at the given 1-based line number
// in the specified markdown file.
func markTodoDone(todoFile string, lineNum int) error {
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

// gateCall makes a stateless chat completion call using the agent's client.
// This replaces the old raw-HTTP approach, getting retry logic, rate limiting,
// cost tracking, and correct API routing for free.
func gateCall(ctx context.Context, chatAgent *agent.Agent, gatePrompt, userContent string) (string, error) {
	messages := []api.Message{
		{Role: "system", Content: gatePrompt},
		{Role: "user", Content: userContent},
	}
	return chatAgent.GenerateResponse(messages)
}

// parseGateResponse extracts a gateResult from the LLM's text response,
// stripping markdown fences if present.
func parseGateResponse(text string) (gateResult, error) {
	// Strip markdown code fences if present.
	text = trimMarkdownFence(text)

	var result gateResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return gateResult{}, fmt.Errorf("failed to parse gate JSON: %w (text: %s)", err, text)
	}
	return result, nil
}

// parseTriageResponse extracts a gateTriageResult from the LLM's text response.
func parseTriageResponse(text string) (gateTriageResult, error) {
	text = trimMarkdownFence(text)

	var result gateTriageResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return gateTriageResult{}, fmt.Errorf("failed to parse triage JSON: %w (text: %s)", err, text)
	}
	return result, nil
}

// loopOutcome classifies the outcome of a single TODO item after processing,
// build verification, and optional retry/triage. This is the single source of
// truth for the completion decision tree.
type loopOutcome int

const (
	outcomeProcessed  loopOutcome = iota // agent completed + build passed → mark [x]
	outcomeFailed                        // build failed after all retries
	outcomeIncomplete                    // build passed but agent didn't complete (max iterations)
	outcomeSkipped                       // triage gate said skip (fundamental blocker)
)

// classifyLoopOutcome is the pure decision logic for Step 7 of the TODO loop.
// It maps the four boolean-like signals into a single classification.
func classifyLoopOutcome(buildFailed bool, processErr error, retrySucceeded bool, triageSkipped bool) loopOutcome {
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

// trimMarkdownFence strips opening and closing markdown code fences from text.
// Handles fenced blocks like ```json ... ``` and plain ``` ... ```.
func trimMarkdownFence(text string) string {
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
		// Inside the fence: skip closing fence too.
		if strings.HasPrefix(line, "```") {
			continue
		}
		inner = append(inner, line)
	}
	return strings.Join(inner, "\n")
}

// RunAgentWorkflowLoop iterates over unchecked TODO items, processing each
// with a fresh agent context. Between items, the conversation is cleared.
func RunAgentWorkflowLoop(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, cfg *AgentWorkflowConfig, state *WorkflowExecutionState, queryExecutor QueryExecutor, overrides *CLIOverrides) (bool, error) {
	if cfg == nil || cfg.Loop == nil {
		return false, nil
	}

	loop := cfg.Loop
	todoFile := loop.TodoFile

	// Read the gate system prompt.
	gatePrompt, err := ResolveWorkflowTextOrFile("", loop.GatePromptFile, "gate_prompt")
	if err != nil {
		return false, fmt.Errorf("failed to resolve gate_prompt_file: %w", err)
	}

	itemsProcessed := 0
	itemsSkipped := 0
	itemsFailed := 0

	fmt.Println()
	console.GlyphAction.Printf("TODO loop: provider=%s model=%s todo=%s",
		chatAgent.GetProvider(), chatAgent.GetModel(), todoFile)

	// Derive the work directory from the TODO file's location for checkpoint
	// storage. Using filepath.Dir(todoFile) instead of os.Getwd() ensures
	// correctness in both production (project root) and test (temp dir).
	todoWorkDir := filepath.Dir(todoFile)

	// Determine the start-after line for checkpoint/resume.
	startAfter := 0
	if state.CurrentTodoLineNum > 0 {
		console.GlyphInfo.Printf("Resuming from TODO line %d (checkpoint)", state.CurrentTodoLineNum)
		startAfter = state.CurrentTodoLineNum - 1 // 1-based → subtract 1 for 0-based skip
	}

	// Fallback: try loading the lightweight loop checkpoint file when
	// orchestration checkpoint didn't provide a resume line.
	if startAfter == 0 {
		if fallbackLine, fbErr := LoadLoopCheckpoint(todoWorkDir); fbErr == nil && fallbackLine > 0 {
			console.GlyphInfo.Printf("Resuming from fallback TODO checkpoint: line %d", fallbackLine)
			startAfter = fallbackLine - 1
		}
	}

	for {
		// Check for context cancellation.
		if err := ctx.Err(); err != nil {
			// Persist checkpoint so we can resume from this line.
			if persistErr := PersistWorkflowCheckpoint(cfg, state, chatAgent); persistErr != nil {
				console.GlyphWarning.Printf("Failed to persist checkpoint: %v", persistErr)
			}
			// Also persist fallback checkpoint.
			if state.CurrentTodoLineNum > 0 {
				if fbErr := PersistLoopCheckpoint(todoWorkDir, state.CurrentTodoLineNum); fbErr != nil {
					console.GlyphWarning.Printf("Failed to persist fallback checkpoint: %v", fbErr)
				}
			}
			return false, fmt.Errorf("loop cancelled: %w", err)
		}

		// Check budget exceeded.
		if chatAgent.FleetBudgetExceeded() {
			fmt.Println()
			console.GlyphWarning.Print("Budget exceeded — stopping TODO loop")
			// Persist checkpoint so we can resume from the last item's line.
			if persistErr := PersistWorkflowCheckpoint(cfg, state, chatAgent); persistErr != nil {
				console.GlyphWarning.Printf("Failed to persist checkpoint: %v", persistErr)
			}
			// Also persist fallback checkpoint.
			if state.CurrentTodoLineNum > 0 {
				if fbErr := PersistLoopCheckpoint(todoWorkDir, state.CurrentTodoLineNum); fbErr != nil {
					console.GlyphWarning.Printf("Failed to persist fallback checkpoint: %v", fbErr)
				}
			}
			break
		}

		// Step 1: Find next unchecked item, respecting checkpoint/resume.
		lineNum, sectionText, findErr := findNextTodoItem(todoFile, startAfter)
		// Reset startAfter so subsequent iterations scan from the beginning.
		startAfter = 0
		if findErr != nil {
			// No more items or read error.
			if strings.Contains(findErr.Error(), "no unchecked") {
				fmt.Println()
				console.GlyphSuccess.Printf("TODO loop complete: processed=%d skipped=%d failed=%d",
					itemsProcessed, itemsSkipped, itemsFailed)
				state.CurrentTodoLineNum = 0
				state.Complete = true
				if persistErr := PersistWorkflowCheckpoint(cfg, state, chatAgent); persistErr != nil {
					console.GlyphWarning.Printf("Failed to persist final state: %v", persistErr)
				}
				// Remove fallback checkpoint on successful completion.
				RemoveLoopCheckpoint(todoWorkDir)
				return false, nil
			}
			return false, fmt.Errorf("failed to find next TODO item: %w", findErr)
		}

		// Step 1b: Persist checkpoint before processing the item.
		state.CurrentTodoLineNum = lineNum
		if persistErr := PersistWorkflowCheckpoint(cfg, state, chatAgent); persistErr != nil {
			console.GlyphWarning.Printf("Failed to persist checkpoint: %v", persistErr)
		}

		fmt.Println()
		console.GlyphAction.Printf("TODO item at line %d", lineNum)

		// Step 2: Gate call — parse section into delegation prompt.
		gateText, gateErr := gateCall(ctx, chatAgent, gatePrompt, sectionText)
		if gateErr != nil {
			console.GlyphWarning.Printf("Gate call failed: %v", gateErr)
			itemsFailed++
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  "unknown",
				"line":   lineNum,
				"reason": fmt.Sprintf("gate_call_failed: %v", gateErr),
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
			continue
		}

		gateRes, parseErr := parseGateResponse(gateText)
		if parseErr != nil {
			console.GlyphWarning.Printf("Gate parse failed: %v", parseErr)
			itemsFailed++
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  "unknown",
				"line":   lineNum,
				"reason": fmt.Sprintf("gate_parse_failed: %v", parseErr),
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
			continue
		}

		console.GlyphInfo.Printf("Gate: title=%q skip=%v", gateRes.Title, gateRes.Skip)

		// Step 3: If skip, mark done and continue.
		if gateRes.Skip {
			reason := gateRes.SkipReason
			if reason == "" {
				reason = "no reason given"
			}
			console.GlyphInfo.Printf("Skipping: %s", reason)
			if markErr := markTodoDone(todoFile, lineNum); markErr != nil {
				console.GlyphWarning.Printf("Failed to mark item done: %v", markErr)
			}
			itemsSkipped++
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_skipped", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": reason,
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
			continue
		}

		if gateRes.Prompt == "" {
			console.GlyphWarning.Printf("Gate returned empty prompt, skipping item")
			itemsFailed++
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": "empty_prompt",
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
			continue
		}

		// Step 4: Process the item with the agent.
		console.GlyphAction.Printf("Processing: %s", gateRes.Title)
		if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_started", map[string]interface{}{
			"title": gateRes.Title,
			"line":  lineNum,
		}); err != nil {
			console.GlyphWarning.Printf("Failed to emit event: %v", err)
		}

		// Override max iterations for this item.
		prevMaxIter := chatAgent.GetMaxIterations()
		chatAgent.SetMaxIterations(loop.MaxIterations)

		// Run the agent with the gate-generated prompt.
		processErr := queryExecutor(ctx, chatAgent, eventBus, gateRes.Prompt)

		// Restore max iterations.
		chatAgent.SetMaxIterations(prevMaxIter)

		if processErr != nil {
			console.GlyphWarning.Printf("Agent processing failed: %v", processErr)
		}

		// Step 5: Build verification. Run the build regardless of whether
		// the agent reported an error — the agent may have hit max iterations
		// but still produced compiling partial work.
		buildFailed := false
		buildCmd := strings.TrimSpace(loop.BuildCommand)
		if buildCmd != "" {
			console.GlyphShell.Printf("%s", buildCmd)
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/sh"
			}
			cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if buildErr := cmd.Run(); buildErr != nil {
				console.GlyphWarning.Printf("Build failed: %v", buildErr)
				buildFailed = true
			} else {
				fmt.Println()
				console.GlyphSuccess.Print("Build passed")
			}
		}

		// Step 6: Triage on failure — retry up to MaxRetries.
		retries := 0
		retrySucceeded := false
		triageSkipped := false
		for buildFailed && retries < loop.MaxRetries {
			retries++
			fmt.Println()
			console.GlyphAction.Printf("Build failed — triaging (attempt %d/%d)", retries, loop.MaxRetries)

			triagePrompt := fmt.Sprintf(
				"Task: %s\n\nPrevious attempt failed. Decide whether to retry or skip.\nReturn JSON: {\"action\": \"retry\"|\"skip\", \"reason\": \"...\"}",
				gateRes.Title)

			triageText, triageErr := gateCall(ctx, chatAgent,
				"You are a build error triage agent. Given a task title and context, decide: retry (transient/fixable) or skip (fundamental/blocking). Return ONLY JSON: {\"action\": \"retry\"|\"skip\", \"reason\": \"...\"}",
				triagePrompt)
			if triageErr != nil {
				console.GlyphWarning.Printf("Triage gate call failed: %v — defaulting to retry", triageErr)
				triageText = `{"action": "retry", "reason": "triage failed"}`
			}

			triageRes, parseErr := parseTriageResponse(triageText)
			if parseErr != nil {
				console.GlyphWarning.Printf("Triage parse failed: %v — defaulting to retry", parseErr)
				triageRes = gateTriageResult{Action: "retry", Reason: "parse failed"}
			}

			console.GlyphInfo.Printf("Triage: action=%s reason=%s", triageRes.Action, triageRes.Reason)

			if strings.EqualFold(triageRes.Action, "skip") {
				triageSkipped = true
				itemsSkipped++
				console.GlyphInfo.Printf("Triage skipped: %s", gateRes.Title)
				break
			}

			// Retry: clear the failed attempt's context and re-run.
			chatAgent.ClearConversationHistory()

			retryPrompt := fmt.Sprintf(
				"Previous attempt failed. Fix the issue and ensure the build passes.\n\nOriginal task:\n%s",
				gateRes.Prompt)

			prevMaxIter := chatAgent.GetMaxIterations()
			retryMaxIter := loop.MaxIterations / 2
			if retryMaxIter < 5 {
				retryMaxIter = 5
			}
			chatAgent.SetMaxIterations(retryMaxIter)

			retryErr := queryExecutor(ctx, chatAgent, eventBus, retryPrompt)
			chatAgent.SetMaxIterations(prevMaxIter)

			if retryErr != nil {
				console.GlyphWarning.Printf("Retry agent processing failed: %v", retryErr)
			}

			// Re-check build.
			buildCmd := strings.TrimSpace(loop.BuildCommand)
			if buildCmd != "" {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if buildErr := cmd.Run(); buildErr != nil {
					console.GlyphWarning.Printf("Build still fails after retry: %v", buildErr)
				} else {
					buildFailed = false
					retrySucceeded = retryErr == nil
					fmt.Println()
					console.GlyphSuccess.Print("Build passed after retry")
				}
			}
		}

		// Step 7: Mark completion based on actual outcome.
		// Use classifyLoopOutcome as the single source of truth for the decision tree.
		switch classifyLoopOutcome(buildFailed, processErr, retrySucceeded, triageSkipped) {
		case outcomeSkipped:
			// Triage said skip — already counted in itemsSkipped.
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_skipped", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": "triage_skip",
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
		case outcomeFailed:
			itemsFailed++
			console.GlyphWarning.Printf("Item failed after retries: %s", gateRes.Title)
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": "build_failed_after_retries",
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
		case outcomeIncomplete:
			// Build passes but agent didn't complete (e.g., max iterations).
			// Don't mark done — the work may be incomplete.
			itemsFailed++
			console.GlyphWarning.Printf("Build passes but agent didn't complete: %v", processErr)
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": fmt.Sprintf("agent_incomplete: %v", processErr),
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
		case outcomeProcessed:
			// Both agent completed AND build passes → mark done.
			if markErr := markTodoDone(todoFile, lineNum); markErr != nil {
				console.GlyphWarning.Printf("Failed to mark item done: %v", markErr)
			} else {
				itemsProcessed++
				fmt.Println()
				console.GlyphSuccess.Printf("Item complete: %s", gateRes.Title)
			}
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_completed", map[string]interface{}{
				"title": gateRes.Title,
				"line":  lineNum,
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
			// Persist fallback checkpoint with the NEXT line number so a
			// crash after this item persists only the successfully completed
			// work. The next run will resume with the unchecked item at
			// lineNum+1 (or detect loop completion).
			if fbErr := PersistLoopCheckpoint(todoWorkDir, lineNum+1); fbErr != nil {
				console.GlyphWarning.Printf("Failed to persist fallback checkpoint: %v", fbErr)
			}
		}

		// Step 8: CRITICAL — clear conversation between items.
		chatAgent.ClearConversationHistory()
	}

	// If we reach here, the loop exited via budget (break) or another
	// non-completion reason. Don't set Complete=true or clear CurrentTodoLineNum —
	// budget exceeded is an interruption, not completion. The checkpoint was
	// saved before the break above, so the next run can resume.
	return false, nil
}
