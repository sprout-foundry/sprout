package agent_audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
)

func TestDigest(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	gotHash, gotBytes := Digest(body)

	sum := sha256.Sum256(body)
	wantHash := hex.EncodeToString(sum[:])
	if gotHash != wantHash {
		t.Errorf("Digest hash = %q, want %q", gotHash, wantHash)
	}
	if gotBytes != len(body) {
		t.Errorf("Digest bytes = %d, want %d", gotBytes, len(body))
	}
}

func TestEndpointHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.openai.com/v1/chat/completions", "api.openai.com"},
		{"https://api.example.com:8443/v1", "api.example.com:8443"},
		{"http://127.0.0.1:18081/v1/chat/completions", "127.0.0.1:18081"},
		{"", ""},
		{"not a url", "not a url"},
		{"api.example.com/v1/chat/completions", "api.example.com"},
	}
	for _, tc := range cases {
		if got := EndpointHost(tc.in); got != tc.want {
			t.Errorf("EndpointHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveTrigger(t *testing.T) {
	// Tool-call follow-up: last message is a tool result.
	if got := ResolveTrigger(context.Background(), "tool"); got != TriggerToolFollowUp {
		t.Errorf("ResolveTrigger(tool) = %q, want %q", got, TriggerToolFollowUp)
	}
	// Fresh user turn.
	if got := ResolveTrigger(context.Background(), "user"); got != TriggerUserTurn {
		t.Errorf("ResolveTrigger(user) = %q, want %q", got, TriggerUserTurn)
	}
	// Subagent overrides the message-role heuristic.
	ctx := WithCallContext(context.Background(), CallContext{Subagent: true})
	if got := ResolveTrigger(ctx, "tool"); got != TriggerSubagent {
		t.Errorf("ResolveTrigger(subagent) = %q, want %q", got, TriggerSubagent)
	}
}

func TestCallContextRoundTrip(t *testing.T) {
	ctx := WithCallContext(context.Background(), CallContext{ChatID: "chat-1", SessionID: "sess-1"})
	cc, ok := CallContextFrom(ctx)
	if !ok {
		t.Fatal("expected call context on ctx")
	}
	if cc.ChatID != "chat-1" || cc.SessionID != "sess-1" {
		t.Errorf("call context = %+v, want chat-1/sess-1", cc)
	}
	if _, ok := CallContextFrom(context.Background()); ok {
		t.Error("background ctx should carry no call context")
	}
}

// recordingSink captures emitted events for assertions.
type recordingSink struct {
	mu    sync.Mutex
	calls []CallEvent
	tools []ToolEvent
}

func (s *recordingSink) EmitCall(ev CallEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, ev)
}

func (s *recordingSink) EmitTool(ev ToolEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, ev)
}

func (s *recordingSink) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func TestEmitCallNilSinkIsNoOp(t *testing.T) {
	SetSink(nil)
	defer SetSink(nil)
	// Must not panic with no sink installed.
	EmitCall(context.Background(), CallEvent{Provider: "p"})
	EmitTool(context.Background(), ToolEvent{Tool: "t"})
}

func TestEmitCallEnrichesFromContext(t *testing.T) {
	sink := &recordingSink{}
	SetSink(sink)
	defer SetSink(nil)

	ctx := WithCallContext(context.Background(), CallContext{ChatID: "chat-9", SessionID: "sess-9"})
	EmitCall(ctx, CallEvent{Provider: "openai", Model: "gpt-x", Outcome: OutcomeOK})

	if sink.callCount() != 1 {
		t.Fatalf("expected 1 call event, got %d", sink.callCount())
	}
	ev := sink.calls[0]
	if ev.ChatID != "chat-9" || ev.SessionID != "sess-9" {
		t.Errorf("event not enriched from ctx: %+v", ev)
	}
	if ev.Trigger != TriggerUserTurn {
		t.Errorf("trigger = %q, want default %q", ev.Trigger, TriggerUserTurn)
	}
	if ev.Time.IsZero() {
		t.Error("event timestamp not set")
	}
	if ev.Kind != KindModelCall {
		t.Errorf("kind = %q, want %q", ev.Kind, KindModelCall)
	}
}

func TestEmitToolEnrichesFromContext(t *testing.T) {
	sink := &recordingSink{}
	SetSink(sink)
	defer SetSink(nil)

	ctx := WithCallContext(context.Background(), CallContext{ChatID: "chat-3", SessionID: "sess-3"})
	EmitTool(ctx, ToolEvent{Tool: "read_file", Status: "ok"})

	if len(sink.tools) != 1 {
		t.Fatalf("expected 1 tool event, got %d", len(sink.tools))
	}
	ev := sink.tools[0]
	if ev.ChatID != "chat-3" || ev.SessionID != "sess-3" {
		t.Errorf("tool event not enriched from ctx: %+v", ev)
	}
	if ev.Kind != KindToolCall {
		t.Errorf("kind = %q, want %q", ev.Kind, KindToolCall)
	}
}

func TestSetSinkLastWriterWins(t *testing.T) {
	s1 := &recordingSink{}
	s2 := &recordingSink{}
	SetSink(s1)
	SetSink(s2)
	defer SetSink(nil)

	EmitCall(context.Background(), CallEvent{Provider: "p"})
	if s1.callCount() != 0 {
		t.Error("first sink should not receive events after replacement")
	}
	if s2.callCount() != 1 {
		t.Error("second sink should receive events")
	}
}

func TestCurrentSinkNilAfterClear(t *testing.T) {
	SetSink(&recordingSink{})
	SetSink(nil)
	if CurrentSink() != nil {
		t.Error("expected nil sink after clearing")
	}
}

func BenchmarkEmitCallNoSink(b *testing.B) {
	SetSink(nil)
	defer SetSink(nil)
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		EmitCall(ctx, CallEvent{Provider: "p"})
	}
}

func TestTriggerConstantsAreStable(t *testing.T) {
	// The event shape is a host contract; these strings are documented.
	for _, want := range []string{"user_turn", "tool_call_follow_up", "subagent"} {
		if !strings.Contains(TriggerUserTurn+TriggerToolFollowUp+TriggerSubagent, want) {
			t.Errorf("trigger constant %q missing", want)
		}
	}
}
