package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"
)

// A turn whose substance lives in reasoning must reach the summarizer: an
// assistant message with empty Content but non-empty ReasoningContent would
// otherwise render as nothing at all, while seed's token estimator still
// charges the summarizer budget for the reasoning it carries.
func TestBuildSummarizerTranscript_IncludesReasoningExcerpt(t *testing.T) {
	reasoning := "I should read main.go before editing it."
	msgs := []core.Message{
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", Content: "", ReasoningContent: reasoning},
	}

	got := buildSummarizerTranscript(msgs, llmSummarizerMaxInputChars)

	if !strings.Contains(got, reasoning) {
		t.Errorf("transcript is missing the reasoning excerpt:\n%s", got)
	}
	if !strings.Contains(got, summarizerReasoningTag) {
		t.Errorf("transcript is missing the %q tag:\n%s", summarizerReasoningTag, got)
	}
}

// The excerpt is bounded: a long reasoning block contributes at most
// summarizerReasoningExcerptChars characters, so one turn's deliberation
// cannot crowd out the rest of the transcript.
func TestBuildSummarizerTranscript_ReasoningExcerptBounded(t *testing.T) {
	reasoning := strings.Repeat("x", 10000)
	msgs := []core.Message{
		{Role: "assistant", Content: "done", ReasoningContent: reasoning},
	}

	got := buildSummarizerTranscript(msgs, llmSummarizerMaxInputChars)

	idx := strings.Index(got, summarizerReasoningTag)
	if idx < 0 {
		t.Fatalf("reasoning tag missing from transcript:\n%s", got)
	}
	rest := got[idx+len(summarizerReasoningTag):]
	excerpt := rest
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		excerpt = rest[:nl]
	}
	if len(excerpt) > summarizerReasoningExcerptChars {
		t.Errorf("excerpt is %d chars, want at most %d", len(excerpt), summarizerReasoningExcerptChars)
	}
	if len(excerpt) == 0 {
		t.Error("excerpt is empty; the reasoning was dropped rather than bounded")
	}
	if strings.Count(got, "x") > summarizerReasoningExcerptChars {
		t.Errorf("transcript carries %d reasoning chars, want at most %d",
			strings.Count(got, "x"), summarizerReasoningExcerptChars)
	}
	if !strings.Contains(got, "done") {
		t.Errorf("the assistant reply was lost:\n%s", got)
	}
}

// The excerpt is written into the transcript before truncation, so it is
// inside the llmSummarizerMaxInputChars budget: a transcript carrying a huge
// reasoning block still comes back within the cap.
func TestBuildSummarizerTranscript_ReasoningCountsAgainstBudget(t *testing.T) {
	msgs := []core.Message{
		{Role: "user", Content: strings.Repeat("u", 5000)},
		{Role: "assistant", Content: "reply", ReasoningContent: strings.Repeat("r", 5000)},
	}
	// A cap smaller than the reasoning block alone.
	const cap = 2000

	got := buildSummarizerTranscript(msgs, cap)
	if len(got) > cap {
		t.Errorf("transcript is %d chars, want at most the %d-char cap", len(got), cap)
	}
}

// Only assistant messages carry reasoning into the summary: a non-assistant
// message with ReasoningContent set contributes none of it.
func TestBuildSummarizerTranscript_IgnoresNonAssistantReasoning(t *testing.T) {
	secret := "user-side reasoning that must not be summarized"
	msgs := []core.Message{
		{Role: "user", Content: "hello", ReasoningContent: secret},
		{Role: "tool", Content: "result", ReasoningContent: secret},
	}

	got := buildSummarizerTranscript(msgs, llmSummarizerMaxInputChars)
	if strings.Contains(got, secret) {
		t.Errorf("non-assistant reasoning leaked into the transcript:\n%s", got)
	}
	if strings.Contains(got, summarizerReasoningTag) {
		t.Errorf("reasoning tag emitted for a non-assistant message:\n%s", got)
	}
}

// The summarizer call's prompt/cached token counts must reach the callback —
// the summary prompt is the largest in a session, so its cache-hit rate is
// the number worth recording.
func TestNewLLMSummarizer_ReportsUsage(t *testing.T) {
	resp := NewScriptedResponseBuilder().
		Content("a recap").
		Usage(1200, 40, 1240, 0).
		Build()
	resp.Usage.PromptTokensDetails.CachedTokens = 900
	client := NewScriptedClient(resp)

	var gotPrompt, gotCached int
	calls := 0
	summarizer := newLLMSummarizer(client, "test-provider", func(promptTokens, cachedTokens int) {
		calls++
		gotPrompt, gotCached = promptTokens, cachedTokens
	})
	if summarizer == nil {
		t.Fatal("newLLMSummarizer returned nil for a non-nil client")
	}

	summary, err := summarizer(context.Background(), []core.Message{
		{Role: "user", Content: "what happened?"},
	}, core.SummarizerHint{})
	if err != nil {
		t.Fatalf("summarizer: %v", err)
	}
	if summary != "a recap" {
		t.Errorf("summary = %q, want %q", summary, "a recap")
	}
	if calls != 1 {
		t.Fatalf("usage callback fired %d times, want 1", calls)
	}
	if gotPrompt != 1200 || gotCached != 900 {
		t.Errorf("usage = (prompt %d, cached %d), want (1200, 900)", gotPrompt, gotCached)
	}
}

// A nil callback must not panic: the summarizer is constructed on paths that
// have no run logger to report to.
func TestNewLLMSummarizer_NilUsageCallbackIsSafe(t *testing.T) {
	client := NewScriptedClient(
		NewScriptedResponseBuilder().Content("a recap").Usage(100, 10, 110, 0).Build(),
	)

	summarizer := newLLMSummarizer(client, "test-provider", nil)
	if summarizer == nil {
		t.Fatal("newLLMSummarizer returned nil for a non-nil client")
	}
	if _, err := summarizer(context.Background(), []core.Message{
		{Role: "user", Content: "what happened?"},
	}, core.SummarizerHint{}); err != nil {
		t.Fatalf("summarizer with nil callback: %v", err)
	}
}

// logCompactionSummaryUsage emits the prompt/cached split with a
// cached/prompt ratio, and reports a zero ratio for a zero prompt rather than
// dividing by zero.
//
// The run logger is a process-wide singleton whose log path is fixed at first
// use, so this test must not change the working directory: doing so would
// point it at a different file than the one the logger is actually writing
// (and break the other runlog test in this package). Instead it reads back
// every runlog file under the working directory and looks for the event.
func TestLogCompactionSummaryUsage_EmitsRatioEvent(t *testing.T) {
	logCompactionSummaryUsage(1000, 250)

	events := readRunlogEvents(t)
	var found map[string]any
	for _, ev := range events {
		if ev["type"] == "compaction_summary_usage" {
			found = ev
		}
	}
	if found == nil {
		t.Fatalf("no compaction_summary_usage event in %d runlog event(s)", len(events))
	}
	if got := found["prompt_tokens"]; got != float64(1000) {
		t.Errorf("prompt_tokens = %v, want 1000", got)
	}
	if got := found["cached_tokens"]; got != float64(250) {
		t.Errorf("cached_tokens = %v, want 250", got)
	}
	if got := found["cache_hit_ratio"]; got != 0.25 {
		t.Errorf("cache_hit_ratio = %v, want 0.25", got)
	}

	// A zero prompt must not divide by zero.
	logCompactionSummaryUsage(0, 0)
	events = readRunlogEvents(t)
	last := events[len(events)-1]
	if last["type"] != "compaction_summary_usage" || last["cache_hit_ratio"] != float64(0) {
		t.Errorf("zero-prompt event = %v, want ratio 0", last)
	}
}

// readRunlogEvents returns every event across the runlog files under the
// working directory, oldest file first so the last element is the most
// recently written event. It fails the test rather than skipping: the run
// logger is initialized by this test's own call when no earlier test in the
// package has done so.
func readRunlogEvents(t *testing.T) []map[string]any {
	t.Helper()
	dir := filepath.Join(".sprout", "runlogs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read runlog dir: %v", err)
	}
	type fileInfo struct {
		path string
		mod  time.Time
	}
	var files []fileInfo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(dir, e.Name()), mod: info.ModTime()})
	}
	if len(files) == 0 {
		t.Fatal("no .jsonl runlog file found")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })

	var events []map[string]any
	for _, f := range files {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("read runlog %s: %v", f.path, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("runlog line is not JSON (%q): %v", line, err)
			}
			events = append(events, ev)
		}
	}
	return events
}
