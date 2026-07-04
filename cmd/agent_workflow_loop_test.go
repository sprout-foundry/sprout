//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// findNextTodoItem tests
// ---------------------------------------------------------------------------

func TestFindNextTodoItem(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `# Project TODO

## Section 1
- [ ] item one
- [x] already done
- [ ] item two

## Section 2
- [x] done item
- [ ] item three
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lineNum, sectionText, err := findNextTodoItem(todoFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lineNum != 4 {
		t.Errorf("expected line 4, got %d", lineNum)
	}
	// The section should be everything under "## Section 1"
	if !strings.Contains(sectionText, "## Section 1") {
		t.Errorf("section text should contain '## Section 1', got: %s", sectionText)
	}
	if !strings.Contains(sectionText, "- [ ] item one") {
		t.Errorf("section text should contain '- [ ] item one', got: %s", sectionText)
	}
	if !strings.Contains(sectionText, "- [ ] item two") {
		t.Errorf("section text should contain '- [ ] item two', got: %s", sectionText)
	}
	// Section should NOT include Section 2 content
	if strings.Contains(sectionText, "## Section 2") {
		t.Errorf("section text should NOT contain '## Section 2', got: %s", sectionText)
	}
}

func TestFindNextTodoItem_NoUnchecked(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `# Done
## Section
- [x] all done
- [x] everything checked
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := findNextTodoItem(todoFile)
	if err == nil {
		t.Fatal("expected error for no unchecked items")
	}
	if !strings.Contains(err.Error(), "no unchecked") {
		t.Errorf("expected 'no unchecked' error, got: %v", err)
	}
}

func TestFindNextTodoItem_FirstLine(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `- [ ] first item`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lineNum, sectionText, err := findNextTodoItem(todoFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lineNum != 1 {
		t.Errorf("expected line 1, got %d", lineNum)
	}
	if !strings.Contains(sectionText, "- [ ] first item") {
		t.Errorf("section text should contain '- [ ] first item', got: %s", sectionText)
	}
}

func TestFindNextTodoItem_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	if err := os.WriteFile(todoFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := findNextTodoItem(todoFile)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestFindNextTodoItem_NoHeading(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `- [ ] item without heading
- [ ] another item
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lineNum, sectionText, err := findNextTodoItem(todoFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lineNum != 1 {
		t.Errorf("expected line 1, got %d", lineNum)
	}
	// Without a heading, the section should start from the first line.
	if !strings.Contains(sectionText, "- [ ] item without heading") {
		t.Errorf("section text should contain the item, got: %s", sectionText)
	}
	if !strings.Contains(sectionText, "- [ ] another item") {
		t.Errorf("section text should contain second item, got: %s", sectionText)
	}
}

// ---------------------------------------------------------------------------
// markTodoDone tests
// ---------------------------------------------------------------------------

func TestMarkTodoDone(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `## Section
- [ ] item one
- [ ] item two
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := markTodoDone(todoFile, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(todoFile)
	if err != nil {
		t.Fatal(err)
	}
	expected := `## Section
- [x] item one
- [ ] item two
`
	if string(data) != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, string(data))
	}
}

func TestMarkTodoDone_AlreadyDone(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `## Section
- [x] already done
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := markTodoDone(todoFile, 2)
	if err == nil {
		t.Fatal("expected error for already-done item")
	}
	if !strings.Contains(err.Error(), "does not contain '- [ ]'") {
		t.Errorf("expected 'does not contain' error, got: %v", err)
	}
}

func TestMarkTodoDone_OutOfRange(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `## Section
- [ ] item
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := markTodoDone(todoFile, 99)
	if err == nil {
		t.Fatal("expected error for out-of-range line number")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected 'out of range' error, got: %v", err)
	}

	// Also test 0 (should be invalid since 1-based)
	err = markTodoDone(todoFile, 0)
	if err == nil {
		t.Fatal("expected error for line 0")
	}
}

func TestMarkTodoDone_NegativeLine(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `- [ ] item`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := markTodoDone(todoFile, -1)
	if err == nil {
		t.Fatal("expected error for negative line number")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected 'out of range' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseGateResponse tests
// ---------------------------------------------------------------------------

func TestParseGateResponse(t *testing.T) {
	input := `{"title": "Implement auth", "prompt": "Add user login", "skip": false, "skip_reason": ""}`
	res, err := parseGateResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Title != "Implement auth" {
		t.Errorf("expected title 'Implement auth', got %q", res.Title)
	}
	if res.Prompt != "Add user login" {
		t.Errorf("expected prompt 'Add user login', got %q", res.Prompt)
	}
	if res.Skip {
		t.Errorf("expected skip=false, got true")
	}
	if res.SkipReason != "" {
		t.Errorf("expected empty skip_reason, got %q", res.SkipReason)
	}
}

func TestParseGateResponse_Fenced(t *testing.T) {
	input := "```json\n{\"title\": \"Fix tests\", \"skip\": true, \"skip_reason\": \"not needed\"}\n```"
	res, err := parseGateResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Title != "Fix tests" {
		t.Errorf("expected title 'Fix tests', got %q", res.Title)
	}
	if !res.Skip {
		t.Errorf("expected skip=true, got false")
	}
	if res.SkipReason != "not needed" {
		t.Errorf("expected skip_reason 'not needed', got %q", res.SkipReason)
	}
}

func TestParseGateResponse_FencedNoLang(t *testing.T) {
	input := "```\n{\"title\": \"Refactor\", \"skip\": false}\n```"
	res, err := parseGateResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Title != "Refactor" {
		t.Errorf("expected title 'Refactor', got %q", res.Title)
	}
	if res.Skip {
		t.Errorf("expected skip=false, got true")
	}
}

func TestParseGateResponse_Invalid(t *testing.T) {
	input := "this is not json at all"
	_, err := parseGateResponse(input)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// parseTriageResponse tests
// ---------------------------------------------------------------------------

func TestParseTriageResponse(t *testing.T) {
	input := `{"action": "retry", "reason": "transient build error"}`
	res, err := parseTriageResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Action != "retry" {
		t.Errorf("expected action 'retry', got %q", res.Action)
	}
	if res.Reason != "transient build error" {
		t.Errorf("expected reason 'transient build error', got %q", res.Reason)
	}
}

func TestParseTriageResponse_Skip(t *testing.T) {
	input := `{"action": "skip", "reason": "fundamental issue"}`
	res, err := parseTriageResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Action != "skip" {
		t.Errorf("expected action 'skip', got %q", res.Action)
	}
	if res.Reason != "fundamental issue" {
		t.Errorf("expected reason 'fundamental issue', got %q", res.Reason)
	}
}

func TestParseTriageResponse_Fenced(t *testing.T) {
	input := "```json\n{\"action\": \"retry\", \"reason\": \"fixable\"}\n```"
	res, err := parseTriageResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Action != "retry" {
		t.Errorf("expected action 'retry', got %q", res.Action)
	}
	if res.Reason != "fixable" {
		t.Errorf("expected reason 'fixable', got %q", res.Reason)
	}
}

func TestParseTriageResponse_Invalid(t *testing.T) {
	input := "garbage input"
	_, err := parseTriageResponse(input)
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}

// ---------------------------------------------------------------------------
// classifyLoopOutcome tests — the decision tree that was previously inline
// and had a double-counting bug. These verify every path through the logic.
// ---------------------------------------------------------------------------

func TestClassifyLoopOutcome_Processed(t *testing.T) {
	// build passes, no agent error → outcomeProcessed
	outcome := classifyLoopOutcome(false, nil, false, false)
	if outcome != outcomeProcessed {
		t.Errorf("expected outcomeProcessed, got %d", outcome)
	}
}

func TestClassifyLoopOutcome_Failed(t *testing.T) {
	// build fails, no triage skip → outcomeFailed
	outcome := classifyLoopOutcome(true, nil, false, false)
	if outcome != outcomeFailed {
		t.Errorf("expected outcomeFailed, got %d", outcome)
	}
}

func TestClassifyLoopOutcome_Incomplete(t *testing.T) {
	// build passes, but agent returned error (max iterations), no retry success
	err := fmt.Errorf("max iterations reached")
	outcome := classifyLoopOutcome(false, err, false, false)
	if outcome != outcomeIncomplete {
		t.Errorf("expected outcomeIncomplete, got %d", outcome)
	}
}

func TestClassifyLoopOutcome_RetrySucceeded(t *testing.T) {
	// build passes after retry, retry succeeded (no error), no triage skip
	err := fmt.Errorf("original error")
	outcome := classifyLoopOutcome(false, err, true, false)
	if outcome != outcomeProcessed {
		t.Errorf("expected outcomeProcessed (retry succeeded), got %d", outcome)
	}
}

func TestClassifyLoopOutcome_RetryFailed(t *testing.T) {
	// build passes after retry, but retry still returned an error, no triage skip
	processErr := fmt.Errorf("original error")
	outcome := classifyLoopOutcome(false, processErr, true, false)
	if outcome != outcomeProcessed {
		t.Errorf("expected outcomeProcessed (retry succeeded overrides processErr), got %d", outcome)
	}
}

func TestClassifyLoopOutcome_Skipped(t *testing.T) {
	// triage said skip → outcomeSkipped regardless of other signals
	outcome := classifyLoopOutcome(false, nil, false, true)
	if outcome != outcomeSkipped {
		t.Errorf("expected outcomeSkipped, got %d", outcome)
	}
}

func TestClassifyLoopOutcome_SkippedOverridesFailure(t *testing.T) {
	// triage said skip AND build failed AND process error → outcomeSkipped
	// This was the double-counting bug: triageSkipped sets itemsSkipped++,
	// but then fallthrough would also set itemsFailed++.
	processErr := fmt.Errorf("agent error")
	outcome := classifyLoopOutcome(true, processErr, false, true)
	if outcome != outcomeSkipped {
		t.Errorf("expected outcomeSkipped (triage skip overrides everything), got %d", outcome)
	}
}

// ---------------------------------------------------------------------------
// Additional findNextTodoItem edge cases
// ---------------------------------------------------------------------------

func TestFindNextTodoItem_SecondItemChecked(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `## Section
- [x] already done
- [ ] second item
- [ ] third item
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lineNum, sectionText, err := findNextTodoItem(todoFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should find the second line (1-based line 3: "- [ ] second item")
	if lineNum != 3 {
		t.Errorf("expected line 3 (first unchecked), got %d", lineNum)
	}
	if !strings.Contains(sectionText, "- [ ] second item") {
		t.Errorf("section text should contain '- [ ] second item', got: %s", sectionText)
	}
	if !strings.Contains(sectionText, "- [ ] third item") {
		t.Errorf("section text should contain '- [ ] third item', got: %s", sectionText)
	}
}

// ---------------------------------------------------------------------------
// Additional markTodoDone edge cases
// ---------------------------------------------------------------------------

func TestMarkTodoDone_PreservesOtherLines(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "TODO.md")

	content := `line 1
line 2
- [ ] target
line 4
line 5
line 6
line 7
line 8
line 9
line 10
`
	if err := os.WriteFile(todoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := markTodoDone(todoFile, 3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(todoFile)
	if err != nil {
		t.Fatal(err)
	}

	// Only line 3 should change from "- [ ] target" to "- [x] target".
	expected := `line 1
line 2
- [x] target
line 4
line 5
line 6
line 7
line 8
line 9
line 10
`
	if string(data) != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, string(data))
	}
}

// ---------------------------------------------------------------------------
// trimMarkdownFence tests
// ---------------------------------------------------------------------------

func TestTrimMarkdownFence_NoFence(t *testing.T) {
	input := `{"title": "plain json", "skip": false}`
	result := trimMarkdownFence(input)
	if result != input {
		t.Errorf("expected no change, got: %s", result)
	}
}

func TestTrimMarkdownFence_Nested(t *testing.T) {
	// Backticks inside fenced content (not at line start) must be preserved.
	input := "```json\n{\n  \"code\": \"text with ``` inside\"\n}\n```"
	result := trimMarkdownFence(input)
	expected := "{\n  \"code\": \"text with ``` inside\"\n}"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestTrimMarkdownFence_MultipleBlocks(t *testing.T) {
	// Two fenced blocks — the function strips all fence delimiter lines,
	// keeping content between and after them.
	input := "```json\n{\"first\": true}\n```\nsome text\n```\n{\"second\": false}\n```"
	result := trimMarkdownFence(input)
	expected := "{\"first\": true}\nsome text\n{\"second\": false}"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}
