//go:build !js

package workflow

// loop.go — the agent workflow loop: the RunAgentWorkflowLoop entry
// point and its main iteration. The gate / todo / outcome helpers live in
// loop_gate.go.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

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
	// Consecutive gate call/parse failures without an intervening success.
	// Reset on any successful gate; trips the circuit breaker at
	// gateFailureLimit. Without this cap a run stuck on an unparseable gate
	// response would spin forever — a subscription-billed provider never
	// trips the USD budget check, so the budget guard cannot stop it.
	gateFailures := 0

	fmt.Println()
	console.GlyphAction.Printf("TODO loop: provider=%s model=%s todo=%s",
		chatAgent.GetProvider(), chatAgent.GetModel(), todoFile)

	// Derive the work directory from the TODO file's location for checkpoint
	// storage. Using filepath.Dir(todoFile) instead of os.Getwd() ensures
	// correctness in both production (project root) and test (temp dir).
	todoWorkDir := filepath.Dir(todoFile)

	// Determine the start-after line for checkpoint/resume.
	// Both resume paths VALIDATE the line before trusting it: a checkpoint
	// is only meaningful if the line it points at still holds an unchecked
	// item. A stale checkpoint (TODO file edited/reorganized since the last
	// run, or a line number persisted by an aborted run that never processed
	// anything) silently redirects the entire run at the wrong item — worse
	// than a full rescan, which can only redo work the checkboxes say is
	// already done.
	validateResumeLine := func(line int) bool {
		if line <= 0 {
			return false
		}
		data, err := os.ReadFile(filepath.Clean(todoFile))
		if err != nil {
			return false
		}
		lines := strings.Split(string(data), "\n")
		if line > len(lines) {
			return false
		}
		return regexp.MustCompile(`^\s*- \[ \]`).MatchString(lines[line-1])
	}

	startAfter := 0
	if state.CurrentTodoLineNum > 0 {
		if validateResumeLine(state.CurrentTodoLineNum) {
			console.GlyphInfo.Printf("Resuming from TODO line %d (checkpoint)", state.CurrentTodoLineNum)
			startAfter = state.CurrentTodoLineNum - 1 // 1-based → subtract 1 for 0-based skip
		} else {
			console.GlyphWarning.Printf("Checkpoint line %d no longer holds an unchecked item — ignoring stale checkpoint, rescanning %s", state.CurrentTodoLineNum, todoFile)
			state.CurrentTodoLineNum = 0
		}
	}

	// Fallback: try loading the lightweight loop checkpoint file when
	// orchestration checkpoint didn't provide a usable resume line.
	if startAfter == 0 {
		if fallbackLine, fbErr := LoadLoopCheckpoint(todoWorkDir); fbErr == nil && fallbackLine > 0 {
			if validateResumeLine(fallbackLine) {
				console.GlyphInfo.Printf("Resuming from fallback TODO checkpoint: line %d", fallbackLine)
				startAfter = fallbackLine - 1
			} else {
				console.GlyphWarning.Printf("Fallback checkpoint line %d no longer holds an unchecked item — ignoring stale checkpoint, rescanning %s", fallbackLine, todoFile)
				RemoveLoopCheckpoint(todoWorkDir)
			}
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
		// One repair round: if the gate answers in prose (models do this
		// exactly when the excerpt confuses them — e.g. a stale checkpoint
		// surfaced the wrong section), re-ask ONCE with the parse error and
		// an explicit JSON-only instruction. This self-heals formatting
		// failures without changing the gate's semantics.
		gateText, gateErr := gateCall(ctx, chatAgent, gatePrompt, sectionText)
		if gateErr == nil {
			if _, parseErr := parseGateResponse(gateText); parseErr != nil {
				console.GlyphWarning.Printf("Gate response not JSON — retrying once with repair instruction")
				repairPrompt := "Your previous response was not valid JSON (" + parseErr.Error() + "). Respond with ONLY the JSON object: {\"title\": string, \"prompt\": string, \"skip\": bool, \"skip_reason\": string}. No prose, no markdown fences."
				gateText, gateErr = gateCall(ctx, chatAgent, repairPrompt, sectionText)
			}
		}
		if gateErr != nil {
			console.GlyphWarning.Printf("Gate call failed: %v", gateErr)
			itemsFailed++
			gateFailures++
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  "unknown",
				"line":   lineNum,
				"reason": fmt.Sprintf("gate_call_failed: %v", gateErr),
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
			if gateFailures >= gateFailureLimit(loop.MaxRetries) {
				return false, fmt.Errorf("gate failed %d consecutive times (limit %d) — aborting TODO loop rather than spinning. Last error: %w", gateFailures, gateFailureLimit(loop.MaxRetries), gateErr)
			}
			continue
		}
		// NOTE: no reset here — a nil call error with an unparseable body
		// (the repair-then-prose path) must keep the counter accumulating.
		// The reset happens only on a successful parse below.

		gateRes, parseErr := parseGateResponse(gateText)
		if parseErr != nil {
			console.GlyphWarning.Printf("Gate parse failed: %v", parseErr)
			itemsFailed++
			gateFailures++
			if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  "unknown",
				"line":   lineNum,
				"reason": fmt.Sprintf("gate_parse_failed: %v", parseErr),
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
			if gateFailures >= gateFailureLimit(loop.MaxRetries) {
				return false, fmt.Errorf("gate parse failed %d consecutive times (limit %d) — aborting TODO loop rather than spinning. Last error: %w", gateFailures, gateFailureLimit(loop.MaxRetries), parseErr)
			}
			continue
		}
		console.GlyphInfo.Printf("Gate: title=%q skip=%v", gateRes.Title, gateRes.Skip)
		gateFailures = 0 // successful parse — the run is healthy again

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
			cmd := shellexec.CommandContext(ctx, buildCmd)
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
				cmd := shellexec.CommandContext(ctx, buildCmd)
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
			gateFailures = 0
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
