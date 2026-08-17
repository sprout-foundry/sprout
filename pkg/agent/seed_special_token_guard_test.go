package agent

import (
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"
)

func TestEndsWithSpecialToken(t *testing.T) {
	cases := []struct {
		name, in string
		want     bool
	}{
		{"exact im_end", "the marker is <|im_end|>", true},
		{"trailing newline", "cut here <|im_end|>\n\n", true},
		{"im_start", "begins <|im_start|>", true},
		{"endoftext", "stop <|endoftext|>", true},
		{"quoted with backtick — not failure", "write `<|im_end|>` to close", false},
		{"quoted with paren", "call (<|im_end|>) after", false},
		{"mid-text", "<|im_end|> is the eos", false},
		{"clean", "all done here", false},
		{"empty", "", false},
		{"whitespace only", "   ", false},
	}
	for _, c := range cases {
		if got := endsWithSpecialToken(c.in); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestObserveAndHintAppendsHint(t *testing.T) {
	sp := &sproutProvider{}
	msgs := []core.Message{
		{Role: "user", Content: "explain the template"},
		{Role: "assistant", Content: "The template uses <|im_end|>"},
		{Role: "user", Content: "Please continue your response from where you left off."},
	}
	out := sp.observeAndHint(msgs)
	if len(out) != len(msgs)+1 {
		t.Fatalf("expected hint appended, len %d -> %d", len(msgs), len(out))
	}
	last := out[len(out)-1]
	if last.Role != "user" {
		t.Errorf("hint must be user role, got %s", last.Role)
	}
	if strings.Contains(last.Content, "<|") {
		t.Errorf("hint must not contain raw special-token bytes: %q", last.Content)
	}
	// Input slice must not be mutated.
	if len(msgs) != 3 {
		t.Errorf("input slice mutated")
	}
}

func TestObserveAndHintCapAndReset(t *testing.T) {
	sp := &sproutProvider{}
	cut1 := []core.Message{
		{Role: "assistant", Content: "first attempt cut <|im_end|>"},
		{Role: "user", Content: "Please continue your response from where you left off."},
	}
	cut2 := []core.Message{
		{Role: "assistant", Content: "second attempt also cut <|im_end|>"},
		{Role: "user", Content: "Please continue your response from where you left off."},
	}
	cut3 := []core.Message{
		{Role: "assistant", Content: "third attempt cut <|im_end|>"},
		{Role: "user", Content: "Please continue your response from where you left off."},
	}
	if out := sp.observeAndHint(cut1); len(out) != 3 {
		t.Fatalf("fire 1: expected append, got %d", len(out))
	}
	// Retries of the SAME call (identical content) never consume budget.
	if out := sp.observeAndHint(cut1); len(out) != 2 {
		t.Fatalf("retry dedupe: no append expected, got %d", len(out))
	}
	if out := sp.observeAndHint(cut2); len(out) != 3 {
		t.Fatalf("fire 2: expected append, got %d", len(out))
	}
	if out := sp.observeAndHint(cut3); len(out) != 2 {
		t.Fatalf("fire 3: expected cap (no append), got %d", len(out))
	}
	// Clean response resets the budget.
	clean := []core.Message{
		{Role: "assistant", Content: "Here is the complete answer, properly finished."},
		{Role: "user", Content: "thanks"},
	}
	if out := sp.observeAndHint(clean); len(out) != 2 {
		t.Fatalf("clean: no append expected, got %d", len(out))
	}
	if out := sp.observeAndHint(cut1); len(out) != 3 {
		t.Fatalf("post-reset: expected append again, got %d", len(out))
	}
}

func TestObserveAndHintNoHintWhenAlreadyPresent(t *testing.T) {
	sp := &sproutProvider{}
	msgs := []core.Message{
		{Role: "assistant", Content: "cut <|im_end|>"},
		{Role: "user", Content: specialTokenHint},
	}
	if out := sp.observeAndHint(msgs); len(out) != 2 {
		t.Fatalf("expected no stacking, got %d", len(out))
	}
}

func TestObserveAndHintCleanHistoryNoop(t *testing.T) {
	sp := &sproutProvider{}
	msgs := []core.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello, fully finished answer."},
		{Role: "user", Content: "ok"},
	}
	if out := sp.observeAndHint(msgs); len(out) != 3 {
		t.Fatalf("clean history must pass through, got %d", len(out))
	}
	if sp.specialTokenGuard.fired != 0 {
		t.Errorf("guard must not fire on clean history, fired=%d", sp.specialTokenGuard.fired)
	}
}

func TestIsSeedContinuationNudge(t *testing.T) {
	nudges := []string{
		"Please continue your response from where you left off.",
		"Please continue.",
		"Your previous response was filtered. Please rephrase your response.",
		"Your previous response appears incomplete. Please provide your final answer.",
	}
	for _, n := range nudges {
		if !isSeedContinuationNudge(n) {
			t.Errorf("expected nudge match: %q", n)
		}
	}
	nonNudges := []string{
		"",
		"please continue.", // case differs (exact-match by design)
		"What's the status?",
		specialTokenHint,
	}
	for _, n := range nonNudges {
		if isSeedContinuationNudge(n) {
			t.Errorf("unexpected nudge match: %q", n)
		}
	}
}

func TestRecordContinuationNudges(t *testing.T) {
	a := newTestAgent(t)
	sp := &sproutProvider{agent: a}
	msgs := []core.Message{
		{Role: "assistant", Content: "partial <|im_end|>"},
		{Role: "user", Content: "Please continue your response from where you left off."},
	}
	sp.recordContinuationNudges(msgs)
	if got := a.state.GetContinuationNudges(); got != 1 {
		t.Errorf("expected 1 recorded nudge, got %d", got)
	}
	sp.recordContinuationNudges([]core.Message{{Role: "user", Content: "ok"}})
	if got := a.state.GetContinuationNudges(); got != 1 {
		t.Errorf("non-nudge must not increment, got %d", got)
	}
}
