// repetition_recovery_test.go — unit tests for the recovery-compaction
// helpers: the de-loop collapses degenerate runs out of both channels (and is
// index-stable), the summarizer builder is nil-safe, and the effort cap never
// raises a configured effort.
package agent

import (
	"context"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestCappedRecoveryReasoning pins the effort cap: it never raises a
// configured effort (low/medium stay), caps xhigh/max to high, and leaves an
// unset (empty) effort to the provider default. Unknown levels pass through
// (the provider decides whether they are valid).
func TestCappedRecoveryReasoning(t *testing.T) {
	cases := map[string]string{
		"low":     "low",    // never raised
		"medium":  "medium", // never raised
		"high":    "high",   // at the cap
		"xhigh":   "high",   // above the cap → capped
		"max":     "high",   // above the cap → capped
		"":        "",       // unset → provider default (unchanged)
		"minimal": "high",   // unrecognized → clamped (fail-safe, not fail-open)
		"none":    "high",   // unrecognized → clamped
		"highest": "high",   // misspelled above-cap level → clamped, not passed
		" HIGH ":  " HIGH ", // at the cap after normalization; original returned verbatim
	}
	for in, want := range cases {
		if got := cappedRecoveryReasoning(in); got != want {
			t.Errorf("cappedRecoveryReasoning(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCollapseDegenerateRunsInMessagesVide pins the durable de-loop: repeated
// short lines in the VISIBLE content are collapsed (one copy survives) while
// the message identity (role, tool calls, length) is preserved.
func TestCollapseDegenerateRunsInMessagesVisible(t *testing.T) {
	line := "Let me run.\n"
	msgs := []api.Message{
		{Role: "assistant", Content: strings.Repeat(line, 8) + "Now the build passes."},
		{Role: "tool", Content: "build ok"}, // tool results are never de-looped
	}
	visibleCfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{})
	reasoningCfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{Reasoning: true})

	out, changed := collapseDegenerateRunsInMessages(msgs, visibleCfg, reasoningCfg)
	if !changed {
		t.Fatalf("expected the degenerate run to be collapsed")
	}
	if len(out) != len(msgs) {
		t.Fatalf("de-loop must be index-stable: len(out)=%d want %d", len(out), len(msgs))
	}
	// Only one copy of the degenerate line survives (the first), the rest
	// are gone, and the trailing real prose is intact.
	if n := strings.Count(out[0].Content, "Let me run."); n != 1 {
		t.Errorf("expected 1 surviving copy of the loop line, got %d: %q", n, out[0].Content)
	}
	if !strings.Contains(out[0].Content, "Now the build passes.") {
		t.Errorf("the legitimate trailing prose must survive: %q", out[0].Content)
	}
	// Tool results are untouched.
	if out[1].Content != "build ok" {
		t.Errorf("tool results must not be de-looped: %q", out[1].Content)
	}
}

// TestCollapseDegenerateRunsInMessagesReasoning pins that the reasoning
// channel is de-looped with the (tighter) reasoning thresholds, independent
// of the visible channel.
func TestCollapseDegenerateRunsInMessagesReasoning(t *testing.T) {
	line := "Run.\n"
	msgs := []api.Message{
		// No visible degeneration, but the reasoning has a tight-run loop.
		{Role: "assistant", Content: "Emitting the call.", ReasoningContent: strings.Repeat(line, 6) + "done."},
	}
	visibleCfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{})
	reasoningCfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{Reasoning: true})

	out, changed := collapseDegenerateRunsInMessages(msgs, visibleCfg, reasoningCfg)
	if !changed {
		t.Fatalf("expected the reasoning run to be collapsed")
	}
	if n := strings.Count(out[0].ReasoningContent, "Run."); n != 1 {
		t.Errorf("expected 1 surviving copy in reasoning, got %d: %q", n, out[0].ReasoningContent)
	}
	if out[0].Content != "Emitting the call." {
		t.Errorf("the visible content must be untouched when only reasoning degenerated: %q", out[0].Content)
	}
}

// TestCollapseDegenerateRunsInMessagesNoChange pins the no-op: a history with
// no degenerate runs is returned unchanged (the same slice) and changed=false.
func TestCollapseDegenerateRunsInMessagesNoChange(t *testing.T) {
	msgs := []api.Message{
		{Role: "assistant", Content: "One thing. Another thing."},
		{Role: "user", Content: "hi"},
	}
	visibleCfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{})
	reasoningCfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{Reasoning: true})

	out, changed := collapseDegenerateRunsInMessages(msgs, visibleCfg, reasoningCfg)
	if changed {
		t.Fatalf("no degenerate runs present; changed must be false")
	}
	// The input slice is returned as-is (no copy) when nothing changed.
	if len(out) != len(msgs) || out[0].Content != msgs[0].Content {
		t.Errorf("an unchanged history must be returned as the input")
	}
}

// TestBuildRecoverySummarizerNil pins that a nil client yields a nil
// summarizer (the caller then falls back to the de-loop-only recovery).
func TestBuildRecoverySummarizerNil(t *testing.T) {
	if s := buildRecoverySummarizer(nil, "test", "low"); s != nil {
		t.Errorf("a nil client must yield a nil summarizer, got %v", s)
	}
}

// TestSummarizeMiddleTooShort pins that a conversation below seed's
// structural minimum is returned unchanged (ok=false), so a short session
// never pays for a lossy summary.
func TestSummarizeMiddleTooShort(t *testing.T) {
	ctx := context.Background()
	msgs := []api.Message{{Role: "user", Content: "hi"}}
	// A summarizer that would panic if ever called.
	summarizer := func(context.Context, []core.Message, core.SummarizerHint) (string, error) {
		t.Fatalf("the summarizer must not be called for a too-short conversation")
		return "", nil
	}
	out, ok, summarized := summarizeMiddle(ctx, msgs, summarizer)
	if ok {
		t.Errorf("a too-short conversation must not be summarized")
	}
	if summarized != 0 {
		t.Errorf("a too-short conversation must report 0 summarized, got %d", summarized)
	}
	if len(out) != 1 {
		t.Errorf("the input must be returned unchanged when too short")
	}
}

// TestPerformRecoveryCompactionNilAgent pins the guard: a provider with no
// agent cannot build a re-seed (returns nil, zero detail), so the caller
// falls back to the terminal failure rather than panicking.
func TestPerformRecoveryCompactionNilAgent(t *testing.T) {
	sp := &sproutProvider{}
	final, detail := sp.performRecoveryCompaction(context.Background(), []api.Message{{Role: "user", Content: "hi"}})
	if final != nil {
		t.Errorf("a nil-agent provider must not build a re-seed, got %v", final)
	}
	if detail.SummaryApplied || detail.DurableChanged {
		t.Errorf("a nil-agent provider must report a zero detail, got %+v", detail)
	}
}
