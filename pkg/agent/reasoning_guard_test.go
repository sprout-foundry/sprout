// reasoning_guard_test.go — unit tests for the reasoning-channel repetition
// guard: the tighter reasoning thresholds flag a shorter run than the visible
// channel, a nil sink is safe, the history scrub collapses only the degenerate
// lines, and the nudge is armed once and consumed once.
package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestReasoningGuardUsesTighterDefaults pins that a reasoning guard resolves
// the reasoning thresholds (four short lines, 80 chars) when none are set,
// while a visible-text guard keeps the visible defaults.
func TestReasoningGuardUsesTighterDefaults(t *testing.T) {
	reasoning := NewRepetitionGuard(RepetitionGuardConfig{Enabled: true, Reasoning: true}, nil)
	if got := reasoning.cfg.MinRepetitions; got != defaultRepetitionReasoningMinRepetitions {
		t.Errorf("reasoning MinRepetitions = %d, want %d", got, defaultRepetitionReasoningMinRepetitions)
	}
	if got := reasoning.cfg.MaxLineChars; got != defaultRepetitionReasoningMaxLineChars {
		t.Errorf("reasoning MaxLineChars = %d, want %d", got, defaultRepetitionReasoningMaxLineChars)
	}

	visible := NewRepetitionGuard(RepetitionGuardConfig{Enabled: true}, nil)
	if got := visible.cfg.MinRepetitions; got != defaultRepetitionMinRepetitions {
		t.Errorf("visible MinRepetitions = %d, want %d", got, defaultRepetitionMinRepetitions)
	}
}

// TestReasoningGuardFlagsShorterRun pins the practical difference: a run of
// four identical short lines is degenerate in the reasoning channel but not
// in visible prose.
func TestReasoningGuardFlagsShorterRun(t *testing.T) {
	reasoning := NewRepetitionGuard(RepetitionGuardConfig{Enabled: true, Reasoning: true}, nil)
	for i := 0; i < 4; i++ {
		reasoning.Write("Run.\n")
	}
	if !reasoning.Looping() {
		t.Fatalf("a four-line reasoning run must flag with the reasoning thresholds")
	}

	visible := NewRepetitionGuard(RepetitionGuardConfig{Enabled: true}, nil)
	for i := 0; i < 4; i++ {
		visible.Write("Run.\n")
	}
	if visible.Looping() {
		t.Fatalf("a four-line visible run must stay below the visible threshold")
	}
}

// TestReasoningGuardNilSinkIsSafe pins that the observation-only guard (no
// sink) neither panics nor loses detection.
func TestReasoningGuardNilSinkIsSafe(t *testing.T) {
	g := NewRepetitionGuard(RepetitionGuardConfig{Enabled: true, Reasoning: true}, nil)
	for i := 0; i < 6; i++ {
		g.Write("Emitting now.\n")
	}
	if !g.Looping() {
		t.Fatalf("a nil sink must not disable detection")
	}
}

// TestCollapseDegenerateLineRunsKeepsOneCopy pins the scrub: the degenerate
// line survives once, the surrounding reasoning is untouched, and the count
// reports the removed occurrences.
func TestCollapseDegenerateLineRunsKeepsOneCopy(t *testing.T) {
	cfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{Enabled: true, Reasoning: true})
	text := "I need to check the file.\nRun.\nRun.\nRun.\nRun.\nRun.\nRun.\nThe file is missing.\n"

	scrubbed, removed := collapseDegenerateLineRuns(text, cfg)
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	// The first three occurrences are below the reasoning threshold (four)
	// and survive; everything past it is collapsed.
	want := "I need to check the file.\nRun.\nRun.\nRun.\nThe file is missing.\n"
	if scrubbed != want {
		t.Errorf("scrubbed = %q, want %q", scrubbed, want)
	}
}

// TestCollapseDegenerateLineRunsLeavesProse pins the false-positive guard: a
// run below the threshold and repeated long lines are both left alone.
func TestCollapseDegenerateLineRunsLeavesProse(t *testing.T) {
	cfg := resolveRepetitionGuardConfig(RepetitionGuardConfig{Enabled: true, Reasoning: true})
	below := "Run.\nRun.\nRun.\n"
	if got, removed := collapseDegenerateLineRuns(below, cfg); removed != 0 || got != below {
		t.Errorf("a below-threshold run must be untouched; got %q, removed %d", got, removed)
	}

	long := strings.Repeat("This is a long explanatory sentence that is legitimate reasoning, kept because repeated prose is not a stuck token.\n", 8)
	if got, removed := collapseDegenerateLineRuns(long, cfg); removed != 0 || got != long {
		t.Errorf("repeated long lines must be untouched; removed %d", removed)
	}
}

// TestScrubDegenerateReasoningTouchesOnlyReasoning pins that the scrub edits
// reasoning_content and leaves visible content, tool calls, and non-assistant
// messages exactly as they were.
func TestScrubDegenerateReasoningTouchesOnlyReasoning(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	a.state.SetMessages([]api.Message{
		{Role: "user", Content: "do the thing"},
		{
			Role:             "assistant",
			Content:          "Checking.",
			ReasoningContent: "Run.\nRun.\nRun.\nRun.\nRun.\n",
			ToolCalls: []api.ToolCall{{
				ID:       "call_1",
				Type:     "function",
				Function: api.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "contents"},
	})

	a.scrubDegenerateReasoning(resolveRepetitionGuardConfig(RepetitionGuardConfig{Enabled: true, Reasoning: true}))

	msgs := a.state.GetMessages()
	if got := msgs[1].ReasoningContent; got != "Run.\nRun.\nRun.\n" {
		t.Errorf("reasoning = %q, want the three below-threshold lines kept", got)
	}
	if got := msgs[1].Content; got != "Checking." {
		t.Errorf("visible content changed: %q", got)
	}
	if len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("tool calls changed: %+v", msgs[1].ToolCalls)
	}
	if got := msgs[2].Content; got != "contents" {
		t.Errorf("tool result changed: %q", got)
	}
}

// TestReasoningLoopNudgePersistsForTurn pins the nudge lifecycle: arming is
// idempotent, every request of the turn sees it, and the next turn's clear
// drops it along with the steered marker.
func TestReasoningLoopNudgePersistsForTurn(t *testing.T) {
	a := &Agent{}
	a.armReasoningLoopNudge(reasoningLoopNudge)
	a.armReasoningLoopNudge(reasoningLoopNudge)

	if got := a.reasoningLoopNudgeForTurn(); got != reasoningLoopNudge {
		t.Fatalf("nudge = %q, want it armed", got)
	}
	if got := a.reasoningLoopNudgeForTurn(); got != reasoningLoopNudge {
		t.Errorf("nudge = %q, want it to persist for the turn", got)
	}

	if !a.markReasoningLoopSteered() {
		t.Fatalf("the first steer must count")
	}
	if a.markReasoningLoopSteered() {
		t.Errorf("a second steer in the same turn must not count again")
	}

	a.clearReasoningLoopNudge()
	if got := a.reasoningLoopNudgeForTurn(); got != "" {
		t.Errorf("nudge = %q, want it cleared at turn start", got)
	}
	if !a.markReasoningLoopSteered() {
		t.Errorf("a new turn must be steerable again")
	}
}

// TestWithReasoningLoopNudgeAppendsToSystem pins that the nudge reaches the
// model through the system prompt, and that a request with no system message
// gets one rather than losing the instruction.
func TestWithReasoningLoopNudgeAppendsToSystem(t *testing.T) {
	a := &Agent{}
	sp := &sproutProvider{agent: a}

	a.armReasoningLoopNudge(reasoningLoopNudge)
	got := sp.withReasoningLoopNudge(&api.ChatRequest{Messages: []api.Message{
		{Role: "system", Content: "base prompt"},
		{Role: "user", Content: "hi"},
	}})
	if want := "base prompt\n\n" + reasoningLoopNudge; got.Messages[0].Content != want {
		t.Errorf("system = %q, want %q", got.Messages[0].Content, want)
	}
	if len(got.Messages) != 2 {
		t.Errorf("message count = %d, want 2", len(got.Messages))
	}

	a.armReasoningLoopNudge(reasoningLoopNudge)
	got = sp.withReasoningLoopNudge(&api.ChatRequest{Messages: []api.Message{
		{Role: "user", Content: "hi"},
	}})
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[0].Content != reasoningLoopNudge {
		t.Errorf("a request without a system message must get one carrying the nudge; got %+v", got.Messages)
	}
}

// TestWithReasoningLoopNudgeNoOpWhenDisarmed pins the passthrough: with no
// armed nudge the request is returned unchanged.
func TestWithReasoningLoopNudgeNoOpWhenDisarmed(t *testing.T) {
	sp := &sproutProvider{agent: &Agent{}}
	req := &api.ChatRequest{Messages: []api.Message{{Role: "user", Content: "hi"}}}
	got := sp.withReasoningLoopNudge(req)
	if len(got.Messages) != 1 || got.Messages[0].Content != "hi" {
		t.Errorf("a disarmed nudge must leave the request unchanged; got %+v", got.Messages)
	}
}

// TestScrubResponseReasoningCleansTheTriggeringReply pins the second half of
// the steering: the reply that triggered the loop is scrubbed on its way into
// history, and its visible content and tool calls are untouched.
func TestScrubResponseReasoningCleansTheTriggeringReply(t *testing.T) {
	sp := &sproutProvider{agent: &Agent{}}
	resp := &api.ChatResponse{Choices: []api.Choice{{
		Message: api.Message{
			Role:             "assistant",
			Content:          "Reading the file.",
			ReasoningContent: "Run.\nRun.\nRun.\nRun.\nRun.\nRun.\n",
			ToolCalls: []api.ToolCall{{
				ID:       "call_1",
				Type:     "function",
				Function: api.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`},
			}},
		},
	}}}

	sp.scrubResponseReasoning(resp)

	msg := resp.Choices[0].Message
	if want := "Run.\nRun.\nRun.\n"; msg.ReasoningContent != want {
		t.Errorf("reasoning = %q, want %q", msg.ReasoningContent, want)
	}
	if msg.Content != "Reading the file." {
		t.Errorf("visible content changed: %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Errorf("tool calls changed: %+v", msg.ToolCalls)
	}
}

// TestScrubResponseReasoningNilSafe pins that an empty or nil response is a
// no-op rather than a panic.
func TestScrubResponseReasoningNilSafe(t *testing.T) {
	sp := &sproutProvider{agent: &Agent{}}
	sp.scrubResponseReasoning(nil)
	sp.scrubResponseReasoning(&api.ChatResponse{})
}
