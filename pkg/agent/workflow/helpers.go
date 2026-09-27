// helpers.go — gate/triage types and parsers, outcome classification,
// TODO-file operations, the session-ID generator, and the budget heartbeat.

package workflow

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
// loop-scoped gate types (mirror cmd versions)
// ---------------------------------------------------------------------------

// GateResult is the JSON response from the gate LLM call.
type GateResult struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Skip       bool   `json:"skip"`
	SkipReason string `json:"skip_reason"`
}

// TriageResult is the JSON response from the triage gate call.
type TriageResult struct {
	Action string `json:"action"` // "retry" or "skip"
	Reason string `json:"reason"`
}

// Outcome classifies a single TODO item's result.
type Outcome int

const (
	OutcomeProcessed Outcome = iota
	OutcomeFailed
	OutcomeIncomplete
	OutcomeSkipped
)

// ClassifyOutcome is the pure decision logic for categorizing a TODO item's
// result. It maps the four boolean-like signals into a single outcome.
func ClassifyOutcome(buildFailed bool, processErr error, retrySucceeded, triageSkipped bool) Outcome {
	if triageSkipped {
		return OutcomeSkipped
	}
	if buildFailed {
		return OutcomeFailed
	}
	if processErr != nil && !retrySucceeded {
		return OutcomeIncomplete
	}
	return OutcomeProcessed
}

// ParseGateResponse extracts a GateResult from the LLM's text response,
// stripping markdown fences if present.
func ParseGateResponse(text string) (GateResult, error) {
	text = trimWorkflowMarkdownFence(text)
	var result GateResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return GateResult{}, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse gate JSON (text: %s)", text), err)
	}
	return result, nil
}

// ParseTriageResponse extracts a TriageResult from the LLM's text response.
func ParseTriageResponse(text string) (TriageResult, error) {
	text = trimWorkflowMarkdownFence(text)
	var result TriageResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return TriageResult{}, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse triage JSON (text: %s)", text), err)
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

// MarkTodoDone changes "- [ ]" to "- [x]" at the given 1-based line
// number in the specified markdown file.
func MarkTodoDone(todoFile string, lineNum int) error {
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
	return os.WriteFile(filepath.Clean(todoFile), bytes.Join(lines, []byte("\n")), 0o644)
}

// ---------------------------------------------------------------------------
// Session ID
// ---------------------------------------------------------------------------

// NewSessionID generates a workflow session ID.
func NewSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("wf-inproc-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("wf-inproc-%s", hex.EncodeToString(b))
}

// ---------------------------------------------------------------------------
// Heartbeat (lightweight version for budget visibility during long runs)
// ---------------------------------------------------------------------------

// startHeartbeat starts a goroutine that prints a one-line budget/progress
// heartbeat on stderr every interval. Returns a stop function that
// terminates it.
func startHeartbeat(agent Agent, budget Budget, reporter HeartbeatReporter, interval time.Duration) func() {
	if agent == nil || interval <= 0 {
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
				var spent, limit float64
				if b := budget; b != nil {
					spent, limit = b.Snapshot()
				} else if reporter != nil {
					spent = reporter.GetTotalCost()
				}
				iter := 0
				if reporter != nil {
					iter = reporter.GetCurrentIteration()
				}
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
