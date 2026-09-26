//go:build !js

package workflow

// loop_gate.go — the agent workflow loop gate / todo / outcome helpers:
// the gate / triage result types, the TODO-item find / mark-done helpers,
// the gate call + response parsers, the loop-outcome classification
// (loopOutcome + classifyLoopOutcome), and the markdown-fence trimmer.
// Split out of loop.go (both files carry the //go:build !js tag).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// gateResult is the JSON response from the gate LLM call.
type gateResult struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Skip       bool   `json:"skip"`
	SkipReason string `json:"skip_reason"`
}

// maxConsecutiveGateFailures is the circuit breaker for Step 2: after this
// many consecutive gate call/parse failures with no intervening successful
// item, the loop aborts instead of spinning. A healthy gate succeeds on the
// first or second try; a run that cannot get a parseable gate response is
// misconfigured (wrong model, stale prompt, provider outage) and burning
// quota on every retry. Derived from MaxRetries (default 2) — generous
// enough to ride out a transient 429, hard enough to stop a hot loop.
func gateFailureLimit(maxRetries int) int {
	limit := maxRetries + 1
	if limit < 3 {
		return 3
	}
	return limit
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
