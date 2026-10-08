// repetition_guard_agent_test.go — agent-level scripted tests for the
// streamed repetition guard: a looping stream is cut and retried once with a
// nudge, legitimate repetition is not flagged, a tool call after repetition is
// kept, and a second loop fails the turn. These drive the real provider
// streaming paths (doChatStream via ProcessQuery, and ChatStream directly)
// with the scripted streaming client.
package agent

import (
	"context"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// loopedStreamResponse builds a scripted streaming response whose chunks are a
// degenerate repetition of the same short line (well past the default
// threshold) with no tool call.
func loopedStreamResponse(line string, count int) *ScriptedResponse {
	chunks := make([]string, 0, count)
	for i := 0; i < count; i++ {
		chunks = append(chunks, line)
	}
	return NewScriptedResponseBuilder().
		Content(strings.Repeat(line, count)).
		FinishReason("stop").
		StreamConfig(&StreamConfig{Chunks: chunks, FinishReason: "stop"}).
		Build()
}

// newRepetitionAgent builds an agent backed by the scripted client with
// streaming enabled and the repetition guard configured. enabled=false
// disables the guard; minRepetitions=0 leaves the default.
func newRepetitionAgent(t *testing.T, enabled bool, minRepetitions int, responses ...*ScriptedResponse) (*Agent, *ScriptedClient) {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	disable := !enabled
	if disable || minRepetitions > 0 {
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.RepetitionGuard = &configuration.RepetitionGuardConfig{
				Enabled:        boolPtr(!disable),
				MinRepetitions: minRepetitions,
			}
			return nil
		}); err != nil {
			t.Fatalf("UpdateConfigNoSave: %v", err)
		}
	}
	client := NewScriptedClient(responses...)
	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(func() { ag.Shutdown() })
	ag.SetStreamingEnabled(true)
	return ag, client
}

// TestRepetitionGuardCutsLoopAndRetriesOnce drives a full scripted turn: the
// first streamed reply is a degenerate repetition loop with no tool call, so
// the guard cuts it, the degenerate text is dropped, and the request is
// retried once with the nudge appended — the clean retry is what the turn
// delivers, and the loop is counted once for the agent's model.
func TestRepetitionGuardCutsLoopAndRetriesOnce(t *testing.T) {
	metrics := NewRepetitionMetrics()
	cleanupMetrics := SetGlobalRepetitionMetricsForTest(metrics)
	defer cleanupMetrics()

	ag, client := newRepetitionAgent(t, true, 0,
		loopedStreamResponse("Let me run.\n", 10),                                 // the turn's reply: degenerate loop
		NewStopResponse("The task is complete. I ran the tests and they passed."), // the retry: clean
	)

	if _, err := ag.ProcessQuery("Please run the tests"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// Two model calls: the looping attempt + the one retry.
	reqs := client.GetSentRequests()
	if len(reqs) != 2 {
		t.Fatalf("model calls = %d, want 2 (the looping attempt + one retry)", len(reqs))
	}
	// The retry carries the nudge as a trailing system message.
	last := reqs[len(reqs)-1]
	if len(last) == 0 || last[len(last)-1].Role != "system" {
		t.Fatalf("the retry request must end with a system nudge; got %+v", last)
	}
	if !strings.Contains(last[len(last)-1].Content, "repeated itself") {
		t.Errorf("the retry nudge %q does not explain the repetition", last[len(last)-1].Content)
	}

	// The degenerate repetition is dropped: only the first (legitimate)
	// occurrence of the line may have streamed before detection; the repeated
	// copies never reach the client buffer.
	buffer := ag.output.GetStreamingBuffer().String()
	if n := strings.Count(buffer, "Let me run."); n > 1 {
		t.Errorf("the degenerate repetition leaked into the client buffer (%d occurrences): %q", n, buffer)
	}
	if !strings.Contains(buffer, "The task is complete") {
		t.Errorf("the retried clean reply did not reach the client buffer: %q", buffer)
	}
	if lastMsg := lastAssistantMessage(t, ag); !strings.Contains(lastMsg.Content, "The task is complete") {
		t.Errorf("state does not carry the retried reply: %q", lastMsg.Content)
	}

	// The loop is counted once for the agent's model.
	stats := metrics.Snapshot()
	total := int64(0)
	for _, s := range stats {
		total += s.Loops
	}
	if total != 1 {
		t.Errorf("repetition loop count = %d, want 1; snapshot=%+v", total, stats)
	}
}

// TestRepetitionGuardSecondLoopFailsTurn pins that a persistently degenerating
// model fails the turn: when the retry also loops, a normal turn error is
// surfaced instead of an unbounded retry.
func TestRepetitionGuardSecondLoopFailsTurn(t *testing.T) {
	ag, client := newRepetitionAgent(t, true, 0,
		loopedStreamResponse("Let me run.\n", 10),
		loopedStreamResponse("Let me do it.\n", 10), // the retry loops too
	)

	_, err := ag.ProcessQuery("Please run the tests")
	if err == nil {
		t.Fatalf("a second repetition loop must fail the turn")
	}
	// Exactly two model calls: the looping attempt + the single retry.
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (no unbounded retry)", calls)
	}
}

// TestRepetitionGuardLegitimateCodeNotFlagged pins a code-heavy reply with
// repeated lines: it is legal output, so no cut, no retry, and the full reply
// is delivered.
func TestRepetitionGuardLegitimateCodeNotFlagged(t *testing.T) {
	code := "```python\n" + strings.Repeat("print('hello')\n", 12) + "```\n"
	ag, client := newRepetitionAgent(t, true, 0,
		newStreamingResponse(code, strings.SplitAfter(code, "\n")),
	)

	if _, err := ag.ProcessQuery("Show me a loop"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (a code block must not be flagged)", calls)
	}
	buffer := ag.output.GetStreamingBuffer().String()
	if !strings.Contains(buffer, "print('hello')") {
		t.Errorf("the code reply did not reach the client buffer: %q", buffer)
	}
}

// TestRepetitionAgentSimilarListNotFlagged pins a list of similar-but-different
// items: not a degenerate run, so the full reply is delivered in one call.
func TestRepetitionAgentSimilarListNotFlagged(t *testing.T) {
	list := "- Install dependencies\n- Run tests\n- Build binary\n- Update changelog\n- Tag release\n- Push tag\n- Publish package\n"
	ag, client := newRepetitionAgent(t, true, 0,
		newStreamingResponse(list, strings.SplitAfter(list, "\n")),
	)

	if _, err := ag.ProcessQuery("What are the steps?"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (a list of different items must not be flagged)", calls)
	}
	buffer := ag.output.GetStreamingBuffer().String()
	if !strings.Contains(buffer, "Publish package") {
		t.Errorf("the full list did not reach the client buffer: %q", buffer)
	}
}

// TestRepetitionAgentDisabledIsPassthrough pins the opt-out: with the guard
// disabled, a degenerate looping stream is NOT cut — it streams through and
// only one model call is made.
func TestRepetitionAgentDisabledIsPassthrough(t *testing.T) {
	ag, client := newRepetitionAgent(t, false, 0,
		loopedStreamResponse("Let me run.\n", 10),
	)

	if _, err := ag.ProcessQuery("Please run the tests"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (a disabled guard never retries)", calls)
	}
	buffer := ag.output.GetStreamingBuffer().String()
	if !strings.Contains(buffer, "Let me run.") {
		t.Errorf("with the guard disabled the loop must stream through; buffer = %q", buffer)
	}
}

// TestRepetitionGuardChatStreamCutsAndRetriesOnce drives the handler-based
// ChatStream entry point directly (the shared stream the CLI, subagents, and
// the web UI route through): a looping reply is cut, retried once, and the
// handler receives the clean reply.
func TestRepetitionGuardChatStreamCutsAndRetriesOnce(t *testing.T) {
	mgr, cleanupMgr := configuration.NewTestManager(t)
	t.Cleanup(cleanupMgr)
	client := NewScriptedClient(
		loopedStreamResponse("Let me run.\n", 10),
		NewStopResponse("Done: the tests passed."),
	)
	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(func() { ag.Shutdown() })
	provider, err := NewSproutProvider(ag, client)
	if err != nil {
		t.Fatalf("NewSproutProvider: %v", err)
	}
	sp := provider.(*sproutProvider)

	var content strings.Builder
	handler := &recordingStreamHandler{onContent: func(s string) { content.WriteString(s) }}
	req := &core.ChatRequest{Messages: []api.Message{{Role: "user", Content: "Please run the tests"}}}
	if err := sp.ChatStream(context.Background(), req, handler); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if handler.err != nil {
		t.Fatalf("handler error: %v", handler.err)
	}
	if !handler.done {
		t.Fatalf("handler did not receive completion")
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (the looping attempt + one retry)", calls)
	}
	delivered := content.String()
	if n := strings.Count(delivered, "Let me run."); n > 1 {
		t.Errorf("the degenerate repetition reached the handler (%d occurrences): %q", n, delivered)
	}
	if !strings.Contains(delivered, "Done: the tests passed.") {
		t.Errorf("the retried reply did not reach the handler: %q", delivered)
	}
}

// TestRepetitionGuardLoopWithToolCallResponseKept pins the completion-time
// tool-call check: a streamed reply whose text loops at the threshold but whose
// response carries a tool call acts rather than degenerating, so it is kept
// (the turn continues with the tool) instead of being cut and retried.
func TestRepetitionGuardLoopWithToolCallResponseKept(t *testing.T) {
	line := "Let me run.\n"
	chunks := []string{line, line, line, line, line, line} // exactly the default threshold
	ag, client := newRepetitionAgent(t, true, 0,
		NewScriptedResponseBuilder().
			Content(strings.Repeat(line, len(chunks))).
			ToolCalls([]api.ToolCall{{
				ID:   "call_kept_1",
				Type: "function",
				Function: api.ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path":"main.go"}`,
				},
			}}).
			FinishReason("tool_calls").
			StreamConfig(&StreamConfig{Chunks: chunks, FinishReason: "tool_calls"}).
			Build(),
		NewStopResponse("The file reads cleanly."),
	)

	if _, err := ag.ProcessQuery("Please inspect main.go"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	// Two calls: the tool-call preamble (kept) + the final answer — no retry.
	reqs := client.GetSentRequests()
	if len(reqs) != 2 {
		t.Errorf("model calls = %d, want 2 (the tool-call reply kept + final answer, no retry)", len(reqs))
	}
	// The reply is KEPT, not cut-then-retried: the second request must not
	// carry the repetition nudge.
	for _, req := range reqs {
		if len(req) == 0 {
			continue
		}
		if last := req[len(req)-1]; last.Role == "system" && strings.Contains(last.Content, "repeated itself") {
			t.Errorf("the tool-call reply was cut and retried (nudge present); it must be kept: %+v", req)
		}
	}
	if lastMsg := lastAssistantMessage(t, ag); !strings.Contains(lastMsg.Content, "reads cleanly") {
		t.Errorf("state does not carry the final answer: %q", lastMsg.Content)
	}
}

// TestRepetitionGuardLoopThenToolCallRetryKeepsToolCall pins the delayed
// tool-call shape: a streamed reply that loops to the threshold mid-stream (its
// tool call would arrive after the flagging chunk, which the streaming callback
// cannot see) is cut and retried once; the retry emits the tool call, which the
// turn then acts on — so the turn still ends with the tool call, not a dead
// loop.
func TestRepetitionGuardLoopThenToolCallRetryKeepsToolCall(t *testing.T) {
	line := "Let me run.\n"
	loopingChunks := []string{line, line, line, line, line, line, line, line} // flags mid-stream
	ag, client := newRepetitionAgent(t, true, 0,
		NewScriptedResponseBuilder().
			Content(strings.Repeat(line, len(loopingChunks))).
			FinishReason("stop").
			StreamConfig(&StreamConfig{Chunks: loopingChunks, FinishReason: "stop"}).
			Build(),
		// The retry emits the tool call the nudge asked for.
		NewScriptedResponseBuilder().
			Content("Let me inspect the file.").
			ToolCalls([]api.ToolCall{{
				ID:   "call_retry_1",
				Type: "function",
				Function: api.ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path":"main.go"}`,
				},
			}}).
			FinishReason("tool_calls").
			Build(),
		NewStopResponse("The file reads cleanly."),
	)

	if _, err := ag.ProcessQuery("Please inspect main.go"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	// The looping attempt + the retry (which acts) + the final answer.
	reqs := client.GetSentRequests()
	if len(reqs) != 3 {
		t.Fatalf("model calls = %d, want 3 (loop + retry tool call + final answer)", len(reqs))
	}
	// The retry carried the nudge.
	retry := reqs[1]
	if len(retry) == 0 || retry[len(retry)-1].Role != "system" || !strings.Contains(retry[len(retry)-1].Content, "repeated itself") {
		t.Errorf("the retry must carry the nudge; got %+v", retry)
	}
	// The retried tool call was kept and the turn completed.
	if lastMsg := lastAssistantMessage(t, ag); !strings.Contains(lastMsg.Content, "reads cleanly") {
		t.Errorf("state does not carry the final answer: %q", lastMsg.Content)
	}
}

// recordingStreamHandler is a minimal core.StreamHandler that records delivered
// content for the ChatStream tests.
type recordingStreamHandler struct {
	onContent func(string)
	done      bool
	err       error
}

func (h *recordingStreamHandler) OnContent(s string) {
	if h.onContent != nil {
		h.onContent(s)
	}
}
func (h *recordingStreamHandler) OnReasoning(string) {}
func (h *recordingStreamHandler) OnDone(*core.ChatResponse) {
	h.done = true
}
func (h *recordingStreamHandler) OnError(err error) {
	h.err = err
}

// TestRepetitionAgentToolCallAfterSomeRepetitionKept pins the acceptance case
// that a reply which acts is never cut: a streamed preamble with some (below
// threshold) repetition followed by a tool call is kept, the tool executes,
// and the turn completes with the final answer — no retry, no cut.
func TestRepetitionAgentToolCallAfterSomeRepetitionKept(t *testing.T) {
	preamble := "Let me check the file.\nLet me check the file.\nLet me check the file.\n"
	ag, client := newRepetitionAgent(t, true, 0,
		NewScriptedResponseBuilder().
			Content(preamble).
			ToolCalls([]api.ToolCall{{
				ID:   "call_rep_1",
				Type: "function",
				Function: api.ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path":"main.go"}`,
				},
			}}).
			FinishReason("tool_calls").
			StreamConfig(&StreamConfig{Chunks: []string{preamble}, FinishReason: "tool_calls"}).
			Build(),
		NewStopResponse("The file reads cleanly; nothing to change."),
	)

	if _, err := ag.ProcessQuery("Please inspect main.go"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// Two model calls: the preamble (with the tool call) + the final answer —
	// the repetition never triggered a cut/retry.
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (preamble + final answer, no repetition retry)", calls)
	}
	// The turn reaches the final answer (the tool call was kept and executed).
	if lastMsg := lastAssistantMessage(t, ag); !strings.Contains(lastMsg.Content, "nothing to change") {
		t.Errorf("state does not carry the final answer: %q", lastMsg.Content)
	}
}
