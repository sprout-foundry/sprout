//go:build !js

// verification_reply_delivery_test.go — the delivery tests for the
// final-reply contract: the verification block must reach the
// user on every path, not just the returned result string. It is written
// into the last assistant message in state (what the query_completed
// response and later turns read) and, when the reply string is suppressed by
// streaming, emitted as a stream chunk (what the streaming CLI and the Web
// UI's live stream consume). The tests pin:
//
//   - a full scripted failing turn shows "Verification: FAILED" in the
//     state's last assistant message AND in the query_completed response
//     (the Web output path) — the item's acceptance test;
//   - a passing turn shows "Verification: passed" through the same paths
//     (the delivery is not failure-only);
//   - a disabled-verification turn (the default) shows nothing: no state
//     append, no stream chunk, byte-identical result — the rule-breaker;
//   - the bare-agent unit tests pin the stream-chunk emission contract: a
//     chunk only when the reply string is suppressed (streaming enabled and
//     a non-empty buffer), none when the buffer is empty or the stored
//     result is nil, and the state write + returned string in every case.
//
// The full-turn cases reuse the fixtures in verification_hook_test.go
// (vhAgent, vhWriteStarterManifest, vhWriteToolCall, ScriptedClient) and
// run the build command through the default executor (sh -c), so they skip
// where sh is absent.

package agent

import (
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// vdFailingTurnVerification is the stored state a failing baseline build
// leaves behind (the build check failed, the test check skipped itself, the
// stopping rule fired at N=2) — the same shape the hook stores.
func vdFailingTurnVerification() turnVerification {
	return turnVerification{
		result:   vhFailedBuildResult(),
		attempts: map[string]int{"build": 2},
		limit:    2,
	}
}

// vdFailingAttachment is the failure report rendered from
// vdFailingTurnVerification: what passes (nothing), what fails (the build),
// and what was tried (2/2 repair attempts).
const vdFailingAttachment = "Verification: FAILED after the stopping rule (2 repair attempts)\n" +
	"Passed: none\n" +
	"Failed: build — command failed\n" +
	"Tried: build: 2/2 repair attempts"

// vdLastAssistantMessage returns the last assistant message (with non-empty
// content) in the agent's state — the target of the verification state write
// and the source of the query_completed response.
func vdLastAssistantMessage(t *testing.T, ag *Agent) string {
	t.Helper()
	messages := ag.state.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && messages[i].Content != "" {
			return messages[i].Content
		}
	}
	t.Fatal("no assistant message with content in state")
	return ""
}

// vdWaitForEvent drains ch, discarding events of other types, until it finds
// an event of type wantType (returned) or the deadline passes (the test
// fails). Other event types (query_progress, tool logs, ...) are ignored:
// the assertion is about the presence of one specific event, not the exact
// sequence.
func vdWaitForEvent(t *testing.T, ch <-chan events.UIEvent, wantType string) events.UIEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == wantType {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q event", wantType)
			return events.UIEvent{}
		}
	}
}

// vdNoEventOfType asserts that no event of type wantType arrives within the
// window — the "no chunk was emitted" checks. Events of other types are
// drained and ignored.
func vdNoEventOfType(t *testing.T, ch <-chan events.UIEvent, wantType string, window time.Duration) {
	t.Helper()
	deadline := time.After(window)
	for {
		select {
		case ev := <-ch:
			if ev.Type == wantType {
				t.Fatalf("unexpected %q event was published", wantType)
			}
		case <-deadline:
			return
		}
	}
}

// vdQueryCompletedResponse extracts the query_completed event's response
// field (the finalContent finalizeConversationPostHooks read from state).
func vdQueryCompletedResponse(t *testing.T, ev events.UIEvent) string {
	t.Helper()
	if ev.Type != events.EventTypeQueryCompleted {
		t.Fatalf("event type = %q, want %q", ev.Type, events.EventTypeQueryCompleted)
	}
	m, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("query_completed payload is %T, want map[string]interface{}", ev.Data)
	}
	resp, _ := m["response"].(string)
	return resp
}

// vdBareAgentWithBus wires a bare agent (state + output managers) to a
// captured EventBus and returns it with its subscriber channel. Streaming is
// enabled with a no-op callback so the suppression condition can be driven.
func vdBareAgentWithBus(t *testing.T, name string) (*Agent, <-chan events.UIEvent) {
	t.Helper()
	ag := NewTestAgent()
	ag.SetSessionID(name)
	bus := events.NewEventBus()
	ch := bus.Subscribe(name)
	t.Cleanup(func() { bus.Unsubscribe(name) })
	ag.SetEventBus(bus)
	ag.EnableStreaming(func(string) {})
	return ag, ch
}

// ---------------------------------------------------------------------------
// Full scripted turn (web/CLI convergence)
// ---------------------------------------------------------------------------

// TestVerificationDelivery_FailingTurnReachesStateAndWebOutput is the item's
// acceptance test: a failing turn shows "Verification: FAILED" in the
// state's last assistant message (the state write — what query_completed and
// later turns read) and in the query_completed event's response (the web
// output path).
func TestVerificationDelivery_FailingTurnReachesStateAndWebOutput(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const (
		turnAnswer     = "I made the change."
		repairOne      = "Repair one: I tried to fix the build."
		repairTwoFinal = "The build still fails; the remaining failure is fixture-broken-build."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse(repairOne),
		NewScriptedTextResponse(repairTwoFinal),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	bus := events.NewEventBus()
	ch := bus.Subscribe("vd-failing")
	t.Cleanup(func() { bus.Unsubscribe("vd-failing") })
	ag.SetEventBus(bus)

	if _, err := ag.ProcessQuery("Implement the app entry point."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// (a) State write: the last assistant message in state carries the block.
	lastAssistant := vdLastAssistantMessage(t, ag)
	if !strings.Contains(lastAssistant, "Verification: FAILED") {
		t.Errorf("last assistant message in state = %q, want it to contain %q (the state write)",
			lastAssistant, "Verification: FAILED")
	}

	// (b) Web output path: the query_completed response carries the block.
	ev := vdWaitForEvent(t, ch, events.EventTypeQueryCompleted)
	resp := vdQueryCompletedResponse(t, ev)
	if !strings.Contains(resp, "Verification: FAILED") {
		t.Errorf("query_completed response = %q, want it to contain %q (the web output path)",
			resp, "Verification: FAILED")
	}
}

// TestVerificationDelivery_PassingTurnReachesStateAndWebOutput pins that the
// delivery is not failure-only: a passing turn shows "Verification: passed"
// through the same two paths (state and the query_completed response).
func TestVerificationDelivery_PassingTurnReachesStateAndWebOutput(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-build-ok")

	const turnAnswer = "Done, the build passes."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	bus := events.NewEventBus()
	ch := bus.Subscribe("vd-passing")
	t.Cleanup(func() { bus.Unsubscribe("vd-passing") })
	ag.SetEventBus(bus)

	if _, err := ag.ProcessQuery("Implement the app entry point."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	lastAssistant := vdLastAssistantMessage(t, ag)
	if !strings.Contains(lastAssistant, "Verification: passed") {
		t.Errorf("last assistant message in state = %q, want it to contain %q",
			lastAssistant, "Verification: passed")
	}

	ev := vdWaitForEvent(t, ch, events.EventTypeQueryCompleted)
	resp := vdQueryCompletedResponse(t, ev)
	if !strings.Contains(resp, "Verification: passed") {
		t.Errorf("query_completed response = %q, want it to contain %q", resp, "Verification: passed")
	}
}

// TestVerificationDelivery_DisabledTurnShowsNothing is the rule-breaker: a
// disabled-verification turn (the default) must show nothing — no state
// append, no stream chunk, and a byte-identical result. The manifest's build
// is deliberately broken so that if the hook ran (a bug) the block would
// appear.
func TestVerificationDelivery_DisabledTurnShowsNothing(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const turnAnswer = "Done."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	// No verification section: the default-off path.
	ag := vhAgent(t, client, root, nil)

	bus := events.NewEventBus()
	ch := bus.Subscribe("vd-disabled")
	t.Cleanup(func() { bus.Unsubscribe("vd-disabled") })
	ag.SetEventBus(bus)

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q (byte-identical)", result, turnAnswer)
	}
	lastAssistant := vdLastAssistantMessage(t, ag)
	if strings.Contains(lastAssistant, "Verification:") {
		t.Errorf("last assistant message in state = %q, must NOT contain a verification block (verification disabled)",
			lastAssistant)
	}
	vdNoEventOfType(t, ch, events.EventTypeStreamChunk, 300*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Bare-agent unit tests: the stream-chunk emission contract
// ---------------------------------------------------------------------------

// TestDeliverVerificationResult_EmitsStreamChunkWhenSuppressed pins the CLI
// output path: with streaming enabled and a pre-seeded buffer (so the reply
// string will be suppressed), a stored failing result drives a stream_chunk
// carrying the attachment, updates the state's last assistant message, and
// returns the reply with the attachment.
func TestDeliverVerificationResult_EmitsStreamChunkWhenSuppressed(t *testing.T) {
	ag, ch := vdBareAgentWithBus(t, "vd-chunk-suppressed")
	// Pre-seed the buffer so the suppression condition holds.
	_, _ = ag.output.GetStreamingBuffer().WriteString("pre-seeded streamed text")
	ag.setTurnVerification(vdFailingTurnVerification())
	ag.state.SetMessages([]api.Message{{Role: "assistant", Content: "The build is broken."}})

	const reply = "The build is broken."
	const want = reply + "\n\n" + vdFailingAttachment

	got := ag.deliverVerificationResult(reply)
	if got != want {
		t.Errorf("deliverVerificationResult = %q,\nwant %q", got, want)
	}

	// The state's last assistant message was updated with the attachment.
	msgs := ag.state.GetMessages()
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "assistant" {
		t.Fatalf("state messages = %+v, want a last assistant message", msgs)
	}
	if !strings.HasSuffix(msgs[len(msgs)-1].Content, vdFailingAttachment) {
		t.Errorf("state last assistant message = %q, want it to end with the attachment", msgs[len(msgs)-1].Content)
	}

	// A stream_chunk with the attachment was published (the CLI terminal
	// write and the Web UI's live stream both consume it).
	ev := vdWaitForEvent(t, ch, events.EventTypeStreamChunk)
	data, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("stream_chunk payload is %T, want map[string]interface{}", ev.Data)
	}
	chunk, _ := data["chunk"].(string)
	if chunk != "\n\n"+vdFailingAttachment {
		t.Errorf("stream_chunk chunk = %q, want %q", chunk, "\n\n"+vdFailingAttachment)
	}
}

// TestDeliverVerificationResult_NoChunkWhenBufferEmpty pins the
// non-suppressed case: streaming is enabled but the buffer is empty, so the
// result string is NOT suppressed — the returned string carries the
// attachment and NO chunk is emitted (double display would be a bug). The
// state write still happens (it is not gated on suppression).
func TestDeliverVerificationResult_NoChunkWhenBufferEmpty(t *testing.T) {
	ag, ch := vdBareAgentWithBus(t, "vd-chunk-empty")
	// Streaming enabled, but the buffer stays EMPTY: the suppression
	// condition (non-empty buffer) does not hold.
	ag.setTurnVerification(vdFailingTurnVerification())
	ag.state.SetMessages([]api.Message{{Role: "assistant", Content: "The build is broken."}})

	const reply = "The build is broken."
	const want = reply + "\n\n" + vdFailingAttachment

	got := ag.deliverVerificationResult(reply)
	if got != want {
		t.Errorf("deliverVerificationResult = %q,\nwant %q (the returned string carries the attachment)", got, want)
	}

	// The state write is not gated on suppression: it still happened.
	msgs := ag.state.GetMessages()
	if len(msgs) == 0 || !strings.HasSuffix(msgs[len(msgs)-1].Content, vdFailingAttachment) {
		t.Errorf("state last assistant message = %q, want it to end with the attachment (the state write is unconditional)",
			msgs[len(msgs)-1].Content)
	}

	// No chunk: the non-suppressed case carries the block in the string only.
	vdNoEventOfType(t, ch, events.EventTypeStreamChunk, 300*time.Millisecond)
}

// TestDeliverVerificationResult_NoOpWhenNoStoredResult pins the no-op: with
// no stored verification result (the hook never ran) the delivery is a
// complete no-op — byte-identical reply, no state write, no chunk. This
// guards the rule that a disabled / no-code-change / subagent / setup-error
// turn never surfaces a verification block.
func TestDeliverVerificationResult_NoOpWhenNoStoredResult(t *testing.T) {
	ag, ch := vdBareAgentWithBus(t, "vd-chunk-noop")
	// Streaming enabled + a pre-seeded buffer: the suppression condition
	// WOULD hold, but there is no stored result, so nothing may be emitted.
	_, _ = ag.output.GetStreamingBuffer().WriteString("pre-seeded")
	ag.state.SetMessages([]api.Message{{Role: "assistant", Content: "Just a normal answer."}})

	const reply = "Just a normal answer."
	got := ag.deliverVerificationResult(reply)
	if got != reply {
		t.Errorf("deliverVerificationResult = %q, want the untouched reply %q (byte-identical)", got, reply)
	}

	msgs := ag.state.GetMessages()
	if len(msgs) == 0 || msgs[len(msgs)-1].Content != reply {
		t.Errorf("state last assistant message = %q, want the untouched reply %q (no state write)",
			msgs[len(msgs)-1].Content, reply)
	}

	vdNoEventOfType(t, ch, events.EventTypeStreamChunk, 300*time.Millisecond)
}
