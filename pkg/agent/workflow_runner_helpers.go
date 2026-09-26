// workflow_runner_helpers.go — the in-process workflow runner helpers:
// the result / gate / triage types (WorkflowResult, workflowGateResult,
// workflowGateTriageResult), the workflowOutcome type + outcome constants,
// the session-ID generator, the heartbeat starter, the TODO-file operations
// (findNextTodoItemInFile, markTodoDoneInFile), the gate / triage response
// parsers, the markdown-fence trimmer, the outcome classifier, and the
// workflow-file parser. Split out of workflow_runner.go.

package agent

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// Config types — lightweight subset of cmd/AgentWorkflowConfig, parsed
// directly from the workflow JSON so the agent package has no import cycle
// with cmd/.
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
func parseWorkflowFile(path string) (*workflowFileConfig, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, agenterrors.NewAgent("workflow_runner", fmt.Sprintf("failed to read %q", path), err)
	}
	var cfg workflowFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse %q", path), err)
	}
	return &cfg, nil
}
