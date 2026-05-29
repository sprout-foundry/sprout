package modelprobe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// scriptedClient replays a fixed sequence of assistant turns, one per
// SendChatRequest, so we can drive the probe deterministically without a real
// provider. It embeds ClientInterface to satisfy the (large) interface; only
// SendChatRequest is exercised.
type scriptedClient struct {
	api.ClientInterface
	turns []api.Message
	i     int
}

func (c *scriptedClient) SendChatRequest(_ context.Context, _ []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	if c.i >= len(c.turns) {
		return &api.ChatResponse{Choices: []api.ChatChoice{{Message: api.Message{Content: "done"}}}}, nil
	}
	msg := c.turns[c.i]
	c.i++
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	return &api.ChatResponse{Choices: []api.ChatChoice{{Message: msg, FinishReason: "tool_calls"}}}, nil
}

func toolCall(id, name string, args any) api.ToolCall {
	b, _ := json.Marshal(args)
	return api.ToolCall{ID: id, Type: "function", Function: api.ToolCallFunction{Name: name, Arguments: string(b)}}
}

func turn(calls ...api.ToolCall) api.Message {
	return api.Message{Role: "assistant", ToolCalls: calls}
}

// passingGateTurns is the correct first-response for each fast gate, in order.
func passingGateTurns() []api.Message {
	return []api.Message{
		turn(toolCall("g1", "read_file", map[string]string{"path": "config.json"})),
		turn(toolCall("g2", "repo_map", map[string]string{})),
		turn(toolCall("g3", "search_code", map[string]string{"query": "processPayment"})),
		turn(toolCall("g4", "set_version", map[string]string{"version": "1.5.0"})),
		turn(toolCall("g5", "acknowledge_task", map[string]string{"summary": "remove the deprecated token-refresh path"})),
	}
}

func TestFastGates_AllPass(t *testing.T) {
	c := &scriptedClient{turns: passingGateTurns()}
	o := runFastGates(context.Background(), c)
	if !o.passed {
		t.Fatalf("all gates should pass, got reason %q", o.reason)
	}
	if o.score != 1.0 {
		t.Errorf("score = %.2f, want 1.0", o.score)
	}
	if o.stats.turns != 5 {
		t.Errorf("expected 5 single-turn requests, got %d", o.stats.turns)
	}
}

func TestFastGates_FastFailStopsEarly(t *testing.T) {
	turns := passingGateTurns()
	// Break the repo_map gate (2nd): answer with read_file instead.
	turns[1] = turn(toolCall("x", "read_file", map[string]string{"path": "main.go"}))
	c := &scriptedClient{turns: turns}
	o := runFastGates(context.Background(), c)
	if o.passed {
		t.Fatal("should fast-fail when repo_map gate is missed")
	}
	if !strings.Contains(o.reason, "uses_repo_map") {
		t.Errorf("reason should name the failing gate, got %q", o.reason)
	}
	if o.stats.turns != 2 {
		t.Errorf("fast-fail should stop at the 2nd gate, ran %d", o.stats.turns)
	}
}

func TestFastGates_ProseFails(t *testing.T) {
	// A model that answers the first gate in prose (no tool call) fails immediately.
	c := &scriptedClient{turns: []api.Message{{Role: "assistant", Content: "You should read config.json."}}}
	o := runFastGates(context.Background(), c)
	if o.passed {
		t.Fatal("prose-only response must fail the first gate")
	}
}

func TestFastGates_WrongComputedArgFails(t *testing.T) {
	turns := passingGateTurns()
	turns[3] = turn(toolCall("x", "set_version", map[string]string{"version": "1.4.3"})) // patch bump, wrong
	c := &scriptedClient{turns: turns}
	o := runFastGates(context.Background(), c)
	if o.passed {
		t.Fatal("a wrong semver bump must fail the computes_argument gate")
	}
	if !strings.Contains(o.reason, "computes_argument") {
		t.Errorf("reason should name computes_argument, got %q", o.reason)
	}
}

func TestFastGates_OrderingConstraint(t *testing.T) {
	turns := passingGateTurns()
	// 5th gate: call read_file first instead of acknowledge_task.
	turns[4] = turn(
		toolCall("a", "read_file", map[string]string{"path": "auth.go"}),
		toolCall("b", "acknowledge_task", map[string]string{"summary": "x"}),
	)
	c := &scriptedClient{turns: turns}
	o := runFastGates(context.Background(), c)
	if o.passed {
		t.Fatal("acknowledge_task must be the FIRST call; ignoring order should fail")
	}
}

// --- complex stage ---

const goodTodos = "1. In internal/users/service.go DeleteAccount, call subs.RemoveForUser(id) so the deleted user's notification subscription is removed.\n" +
	"2. Add a regression test asserting a deleted user gets no email."

func TestComplex_PassWithRootCause(t *testing.T) {
	c := &scriptedClient{turns: []api.Message{
		turn(toolCall("1", "list_dir", map[string]string{"path": "."})),
		turn(toolCall("2", "read_file", map[string]string{"path": "internal/users/service.go"})),
		turn(toolCall("3", "read_file", map[string]string{"path": "internal/notify/subscriptions.go"})),
		turn(toolCall("4", "submit_todos", map[string]string{"summary": "deleting an account never removes the user's notification subscription", "todos": goodTodos})),
	}}
	o := runComplex(context.Background(), c)
	if !o.passed {
		t.Fatalf("complex should pass, got reason %q (score %.2f)", o.reason, o.score)
	}
	if o.todos == "" {
		t.Error("todos should be captured for review")
	}
}

func TestComplex_DistractedMissesCause(t *testing.T) {
	// Lured by the email machinery; never identifies the subscription cleanup.
	todos := "1. In internal/notify/mailer.go, stop SendDaily from emailing.\n2. Edit internal/notify/templates.go."
	c := &scriptedClient{turns: []api.Message{
		turn(toolCall("1", "list_dir", map[string]string{"path": "internal/notify"})),
		turn(toolCall("2", "read_file", map[string]string{"path": "internal/notify/mailer.go"})),
		turn(toolCall("3", "read_file", map[string]string{"path": "internal/notify/templates.go"})),
		turn(toolCall("4", "submit_todos", map[string]string{"summary": "the mailer sends too many emails", "todos": todos})),
	}}
	o := runComplex(context.Background(), c)
	if o.passed {
		t.Fatalf("should fail when distracted from the real cause (reason %q)", o.reason)
	}
}

func TestComplex_TrapWrongFixSiteFails(t *testing.T) {
	// Fooled into "fixing" the deprecated legacy file; no real fix site.
	todos := "1. In internal/legacy/old_delete.go, make DeleteUserAndData also clear subscriptions."
	c := &scriptedClient{turns: []api.Message{
		turn(toolCall("1", "list_dir", map[string]string{"path": "."})),
		turn(toolCall("2", "read_file", map[string]string{"path": "internal/legacy/old_delete.go"})),
		turn(toolCall("3", "read_file", map[string]string{"path": "internal/users/store.go"})),
		turn(toolCall("4", "submit_todos", map[string]string{"summary": "subscriptions aren't cleared on delete", "todos": todos})),
	}}
	o := runComplex(context.Background(), c)
	if o.passed {
		t.Fatalf("should fail when the only fix site is the deprecated legacy file (reason %q)", o.reason)
	}
}

func TestComplex_NoTodosFails(t *testing.T) {
	c := &scriptedClient{turns: []api.Message{
		turn(toolCall("1", "list_dir", map[string]string{"path": "."})),
		turn(toolCall("2", "read_file", map[string]string{"path": "internal/users/service.go"})),
	}}
	o := runComplex(context.Background(), c)
	if o.passed {
		t.Fatal("must fail when no todos are submitted")
	}
}

// --- orchestration ---

func TestRun_GateFailSkipsComplex(t *testing.T) {
	// First gate answered in prose → fast fail, complex never runs.
	c := &scriptedClient{turns: []api.Message{{Role: "assistant", Content: "Sure, I'll read it."}}}
	r, err := Run(context.Background(), c, "test", "m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Passed || r.Complex {
		t.Errorf("expected fail, got passed=%v complex=%v", r.Passed, r.Complex)
	}
	if r.Score >= 0.5 {
		t.Errorf("a gate fast-fail should score < 0.5, got %.2f", r.Score)
	}
	if !strings.HasPrefix(r.Reason, "fast-fail:") {
		t.Errorf("reason should mark a fast-fail, got %q", r.Reason)
	}
}

func TestRun_FullPass(t *testing.T) {
	turns := passingGateTurns()
	turns = append(turns,
		turn(toolCall("c1", "list_dir", map[string]string{"path": "."})),
		turn(toolCall("c2", "read_file", map[string]string{"path": "internal/users/service.go"})),
		turn(toolCall("c3", "read_file", map[string]string{"path": "internal/notify/subscriptions.go"})),
		turn(toolCall("c4", "submit_todos", map[string]string{"summary": "subscription not removed on delete", "todos": goodTodos})),
	)
	c := &scriptedClient{turns: turns}
	r, err := Run(context.Background(), c, "test", "m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Passed || !r.Complex {
		t.Fatalf("expected pass+complex, got passed=%v complex=%v reason=%q", r.Passed, r.Complex, r.Reason)
	}
	if r.Score <= 0.5 {
		t.Errorf("a full pass should score > 0.5, got %.2f", r.Score)
	}
	if r.ProbeVersion != ProbeVersion {
		t.Errorf("probe version = %q, want %q", r.ProbeVersion, ProbeVersion)
	}
}

// erroringClient replays gateTurns, then returns a transport error on the next
// request — used to simulate a 5xx/timeout mid-probe.
type erroringClient struct {
	api.ClientInterface
	gateTurns []api.Message
	i         int
}

func (c *erroringClient) SendChatRequest(_ context.Context, _ []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	if c.i < len(c.gateTurns) {
		msg := c.gateTurns[c.i]
		c.i++
		if msg.Role == "" {
			msg.Role = "assistant"
		}
		return &api.ChatResponse{Choices: []api.ChatChoice{{Message: msg, FinishReason: "tool_calls"}}}, nil
	}
	return nil, errors.New("simulated transport error")
}

func TestRun_GateTransportErrorIsInconclusive(t *testing.T) {
	c := &erroringClient{} // errors on the very first request
	r, err := Run(context.Background(), c, "test", "m")
	if err == nil {
		t.Fatal("expected a transport error to be returned")
	}
	if !r.Errored {
		t.Error("a transport failure must mark the result Errored (inconclusive)")
	}
	if r.Passed {
		t.Error("an inconclusive run must not report Passed")
	}
}

func TestRun_ComplexTransportErrorIsInconclusive(t *testing.T) {
	// Gates all pass, then the complex stage's first request errors.
	c := &erroringClient{gateTurns: passingGateTurns()}
	r, err := Run(context.Background(), c, "test", "m")
	if err != nil {
		t.Fatalf("gate result is valid, Run should not return an error: %v", err)
	}
	if !r.Errored {
		t.Error("a complex-stage transport failure must mark the result Errored")
	}
	if !r.Passed {
		t.Error("gates passed, so Passed should be true even when complex errored")
	}
	if r.Complex {
		t.Error("complex must not be reported true when it couldn't be assessed")
	}
}

func TestWithinCostBudget(t *testing.T) {
	cases := []struct {
		name    string
		in, out float64
		known   bool
		max     float64
		wantOK  bool
	}{
		{"no budget", 100, 300, true, 0, true},
		{"cheap under budget", 0.5, 1.5, true, 0.10, true},
		{"premium over budget", 3, 15, true, 0.10, false},
		{"unknown price with budget", 0, 0, false, 0.10, false},
		{"unknown price no budget", 0, 0, false, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, reason := WithinCostBudget(c.in, c.out, c.known, c.max)
			if ok != c.wantOK {
				t.Errorf("ok = %v, want %v (reason: %s)", ok, c.wantOK, reason)
			}
			if !ok && reason == "" {
				t.Error("rejection should carry a reason")
			}
		})
	}
}

func TestComplexScenarioHasClues(t *testing.T) {
	files := complexFiles()
	subs := files["internal/notify/subscriptions.go"]
	if !strings.Contains(subs, "RemoveForUser") || !strings.Contains(subs, "not called") {
		t.Error("subscriptions.go must surface the unused RemoveForUser cleanup")
	}
	if !strings.Contains(files["internal/legacy/old_delete.go"], "DEPRECATED") {
		t.Error("legacy trap file must be marked DEPRECATED")
	}
}
