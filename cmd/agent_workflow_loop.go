//go:build !js

package cmd

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
// - lineNum: the 1-based line number of the first "[ ]" item found
// - sectionText: the text of the enclosing ## section (from the ## header above
//   the item to just before the next ## header or end of file)
// - err: non-nil if the file can't be read or no unchecked items exist
func findNextTodoItem(todoFile string) (lineNum int, sectionText string, err error) {
	data, err := os.ReadFile(filepath.Clean(todoFile))
	if err != nil {
		return 0, "", fmt.Errorf("failed to read %s: %w", todoFile, err)
	}

	lines := strings.Split(string(data), "\n")
	uncheckedRe := regexp.MustCompile(`^\s*- \[ \]`)

	// Find first unchecked item line (1-based).
	var itemLine int // 0-based index
	for i, line := range lines {
		if uncheckedRe.MatchString(line) {
			itemLine = i
			break
		}
	}
	if itemLine == 0 && !uncheckedRe.MatchString(lines[0]) {
		// No unchecked items found (itemLine stayed at 0 but first line doesn't match).
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

// runAgentWorkflowLoop iterates over unchecked TODO items, processing each
// with a fresh agent context. Between items, the conversation is cleared.
func runAgentWorkflowLoop(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, cfg *AgentWorkflowConfig, state *workflowExecutionState) (bool, error) {
	if cfg == nil || cfg.Loop == nil {
		return false, nil
	}

	loop := cfg.Loop
	todoFile := loop.TodoFile

	// Read the gate system prompt.
	gatePrompt, err := resolveWorkflowTextOrFile("", loop.GatePromptFile, "gate_prompt")
	if err != nil {
		return false, fmt.Errorf("failed to resolve gate_prompt_file: %w", err)
	}

	itemsProcessed := 0
	itemsSkipped := 0
	itemsFailed := 0

	fmt.Println()
	console.GlyphAction.Printf("TODO loop: provider=%s model=%s todo=%s",
		chatAgent.GetProvider(), chatAgent.GetModel(), todoFile)

	for {
		// Check for context cancellation.
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("loop cancelled: %w", err)
		}

		// Check budget exceeded.
		if chatAgent.FleetBudgetExceeded() {
			fmt.Println()
			console.GlyphWarning.Print("Budget exceeded — stopping TODO loop")
			break
		}

		// Step 1: Find next unchecked item.
		lineNum, sectionText, findErr := findNextTodoItem(todoFile)
		if findErr != nil {
			// No more items or read error.
			if strings.Contains(findErr.Error(), "no unchecked") {
				fmt.Println()
				console.GlyphSuccess.Printf("TODO loop complete: processed=%d skipped=%d failed=%d",
					itemsProcessed, itemsSkipped, itemsFailed)
				state.Complete = true
				return false, nil
			}
			return false, fmt.Errorf("failed to find next TODO item: %w", findErr)
		}

		fmt.Println()
		console.GlyphAction.Printf("TODO item at line %d", lineNum)

		// Step 2: Gate call — parse section into delegation prompt.
		gateText, gateErr := gateCall(ctx, chatAgent, gatePrompt, sectionText)
		if gateErr != nil {
			console.GlyphWarning.Printf("Gate call failed: %v", gateErr)
			itemsFailed++
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
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
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
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
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_skipped", map[string]interface{}{
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
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
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
		if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_started", map[string]interface{}{
			"title": gateRes.Title,
			"line":  lineNum,
		}); err != nil {
			console.GlyphWarning.Printf("Failed to emit event: %v", err)
		}

		// Override max iterations for this item.
		prevMaxIter := chatAgent.GetMaxIterations()
		chatAgent.SetMaxIterations(loop.MaxIterations)

		// Run the agent with the gate-generated prompt.
		processErr := ProcessQuery(ctx, chatAgent, eventBus, gateRes.Prompt)

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

			retryErr := ProcessQuery(ctx, chatAgent, eventBus, retryPrompt)
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
		if triageSkipped {
			// Triage said skip — already counted in itemsSkipped.
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_skipped", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": "triage_skip",
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
		} else if buildFailed {
			itemsFailed++
			console.GlyphWarning.Printf("Item failed after retries: %s", gateRes.Title)
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": "build_failed_after_retries",
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
		} else if processErr != nil && !retrySucceeded {
			// Build passes but agent didn't complete (e.g., max iterations).
			// Don't mark done — the work may be incomplete.
			itemsFailed++
			console.GlyphWarning.Printf("Build passes but agent didn't complete: %v", processErr)
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_failed", map[string]interface{}{
				"title":  gateRes.Title,
				"line":   lineNum,
				"reason": fmt.Sprintf("agent_incomplete: %v", processErr),
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
		} else {
			// Both agent completed AND build passes → mark done.
			if markErr := markTodoDone(todoFile, lineNum); markErr != nil {
				console.GlyphWarning.Printf("Failed to mark item done: %v", markErr)
			} else {
				itemsProcessed++
				fmt.Println()
				console.GlyphSuccess.Printf("Item complete: %s", gateRes.Title)
			}
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_loop_item_completed", map[string]interface{}{
				"title": gateRes.Title,
				"line":  lineNum,
			}); err != nil {
				console.GlyphWarning.Printf("Failed to emit event: %v", err)
			}
		}

		// Step 8: CRITICAL — clear conversation between items.
		chatAgent.ClearConversationHistory()
	}

	state.Complete = true
	return false, nil
}
