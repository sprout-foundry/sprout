//go:build darwin && arm64 && cgo

package localmodel

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// fakeBudgetModel is a minimal llm.Model stand-in for budget tests. Only
// TokenizerEncode and ContextLength are exercised by localMaxOutputTokens;
// the embedded nil everything else is fine because the helper never touches
// inference surfaces.
type fakeBudgetModel struct {
	promptTokens int
	ctxLength    int
}

func (f *fakeBudgetModel) TokenizerEncode(_ string) []int {
	return make([]int, f.promptTokens)
}

func (f *fakeBudgetModel) ContextLength() int { return f.ctxLength }

// Regression guard for the "ends at a random spot" bug: the budget must be
// a real function of the context window, never the 512 default that
// sinter's DefaultGenerateConfig would otherwise impose.
func TestLocalMaxOutputTokens_NotCappedAtDefault512(t *testing.T) {
	m := &fakeBudgetModel{promptTokens: 4000, ctxLength: 128_000}
	got := localMaxOutputTokens(m, "prompt")
	if got <= 512 {
		t.Fatalf("budget must exceed the old 512 truncation point for a modest prompt, got %d", got)
	}
}

// A small context still budgets correctly (window minus input, cushioned),
// and never collapses below the minimum viable output.
func TestLocalMaxOutputTokens_SmallContext(t *testing.T) {
	m := &fakeBudgetModel{promptTokens: 3000, ctxLength: 8192}
	got := localMaxOutputTokens(m, "prompt")
	if got < api.MinOutputTokens {
		t.Fatalf("budget below MinOutputTokens (%d): %d", api.MinOutputTokens, got)
	}
	if got > 8192-3000 {
		t.Fatalf("budget exceeds physical window remainder: %d", got)
	}
}

// A nearly-full window floors at MinOutputTokens rather than zero.
func TestLocalMaxOutputTokens_NearlyFullWindow(t *testing.T) {
	m := &fakeBudgetModel{promptTokens: 8000, ctxLength: 8192}
	got := localMaxOutputTokens(m, "prompt")
	if got != api.MinOutputTokens {
		t.Fatalf("nearly-full window must floor at MinOutputTokens, got %d", got)
	}
}

// The runaway-generation ceiling applies regardless of window size.
func TestLocalMaxOutputTokens_RunawayCap(t *testing.T) {
	m := &fakeBudgetModel{promptTokens: 100, ctxLength: 128_000}
	got := localMaxOutputTokens(m, "prompt")
	if got != localMaxOutputCap {
		t.Fatalf("large-window budget must be capped at %d, got %d", localMaxOutputCap, got)
	}
}

// finish_reason must distinguish truncation from a natural stop.
func TestLocalFinishReason(t *testing.T) {
	cases := []struct {
		name      string
		generated int
		maxTokens int
		tools     []api.ToolCall
		want      string
	}{
		{"natural stop", 120, 16384, nil, "stop"},
		{"cap hit", 16384, 16384, nil, "length"},
		{"cap hit with tools", 16384, 16384, []api.ToolCall{{}}, "tool_calls"},
		{"tools before cap", 50, 16384, []api.ToolCall{{}}, "tool_calls"},
		{"zero max treated as stop", 50, 0, nil, "stop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := localFinishReason(tc.generated, tc.maxTokens, tc.tools); got != tc.want {
				t.Fatalf("localFinishReason(%d, %d, %d tools) = %q, want %q",
					tc.generated, tc.maxTokens, len(tc.tools), got, tc.want)
			}
		})
	}
}

// The budget helper's signature takes the localBudgetModel interface, so
// the fake above exercises the real math.
func TestLocalBudgetConstants(t *testing.T) {
	if localMaxOutputCap <= 512 {
		t.Fatalf("runaway cap %d must exceed the old 512 default", localMaxOutputCap)
	}
	if localMaxOutputCap >= 128_000 {
		t.Fatalf("runaway cap %d must stay under the context ceiling", localMaxOutputCap)
	}
}
