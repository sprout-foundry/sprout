package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// capturingMP is testMP that keeps each request, to see what the model was
// told between its replies.
type capturingMP struct {
	testMP
	requests []*ChatRequest
}

func (m *capturingMP) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	m.requests = append(m.requests, req)
	return m.testMP.Chat(ctx, req)
}

// callingExecutor records the calls it runs.
type callingExecutor struct {
	tools []Tool
	calls []string
}

func (e *callingExecutor) GetTools() []Tool { return e.tools }
func (e *callingExecutor) Execute(_ context.Context, calls []ToolCall) []Message {
	var out []Message
	for _, c := range calls {
		e.calls = append(e.calls, c.Function.Name)
		out = append(out, Message{Role: "tool", ToolCallID: c.ID, Content: "ok"})
	}
	return out
}

func assistantReply(content string) *ChatResponse {
	return &ChatResponse{Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: content}}}, Usage: ChatUsage{TotalTokens: 10}}
}

// stopReply is a reply the way providers send one: finish reason "stop",
// which is the path the loop's blank, repetition and substance checks run on.
func stopReply(content string) *ChatResponse {
	r := assistantReply(content)
	r.Choices[0].FinishReason = "stop"
	return r
}

// qwenUnknownCall is a Qwen-style tool call written as text, to a tool that
// does not exist (check_background is a parameter of a shell tool, not a tool).
const qwenUnknownCall = "<tool_call>\n<function=check_background>\n<parameter=session_id>\nbg-1\n</parameter>\n</function>\n</tool_call>"

func shellTools() []Tool {
	return []Tool{{Function: ToolFunction{Name: "shell", Description: "Run a command", Parameters: ToolParameters{Type: "object"}}}}
}

// A reply that only calls a tool that does not exist is answered with the
// tools there are, and the turn goes on instead of ending on the call's text.
func TestFallback_UnknownToolIsAnsweredAndTheTurnGoesOn(t *testing.T) {
	p := &capturingMP{testMP: testMP{responses: []*ChatResponse{
		stopReply(qwenUnknownCall),
		stopReply("<tool_call>\n<function=shell>\n<parameter=command>\nls\n</parameter>\n</function>\n</tool_call>"),
		stopReply("Done."),
	}}}
	e := &callingExecutor{tools: shellTools()}
	a, err := NewAgent(Options{Provider: p, Executor: e})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := a.Run(context.Background(), "check the background job")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Done." {
		t.Fatalf("reply = %q, want the final answer after the real call", reply)
	}
	if len(e.calls) != 1 || e.calls[0] != "shell" {
		t.Fatalf("calls = %v, want one shell call", e.calls)
	}
	if len(p.requests) < 2 {
		t.Fatalf("requests = %d, want the turn to go on", len(p.requests))
	}
	var told string
	for _, m := range p.requests[1].Messages {
		if m.Role == "user" && strings.Contains(m.Content, "There is no tool named") {
			told = m.Content
		}
	}
	if !strings.Contains(told, "`check_background`") || !strings.Contains(told, "shell") {
		t.Fatalf("the model was told %q, want the unknown name and the real tools", told)
	}
}

// A model that keeps calling tools that do not exist is answered at most
// maxUnknownToolNudges times in a turn; after that the turn ends as it did
// before (the loop's other checks may still ask once more).
func TestFallback_UnknownToolNudgesAreBounded(t *testing.T) {
	var responses []*ChatResponse
	for i := 0; i < 12; i++ {
		responses = append(responses, assistantReply(qwenUnknownCall))
	}
	p := &capturingMP{testMP: testMP{responses: responses}}
	e := &callingExecutor{tools: shellTools()}
	a, err := NewAgent(Options{Provider: p, Executor: e})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "check the background job"); err != nil {
		t.Fatal(err)
	}
	last := p.requests[len(p.requests)-1]
	nudges := 0
	for _, m := range last.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "There is no tool named") {
			nudges++
		}
	}
	if nudges > maxUnknownToolNudges {
		t.Fatalf("the model was answered %d times, want at most %d", nudges, maxUnknownToolNudges)
	}
	if len(p.requests) >= len(responses) {
		t.Fatalf("requests = %d, want the turn to end before the replies run out", len(p.requests))
	}
	if len(e.calls) != 0 {
		t.Fatalf("calls = %v, want none", e.calls)
	}
}

// A model polling a background session writes the same call as text each
// time. Each one is a call to run, not a repeated reply: the loop's
// repetition check must not end the turn on it.
func TestFallback_RepeatedTextCallIsRunEachTime(t *testing.T) {
	poll := "<tool_call>\n<function=shell>\n<parameter=check_background>\nbg-1\n</parameter>\n</function>\n</tool_call>"
	p := &capturingMP{testMP: testMP{responses: []*ChatResponse{
		stopReply(poll),
		stopReply(poll),
		stopReply(poll),
		stopReply("The job finished and its tests pass."),
	}}}
	e := &callingExecutor{tools: shellTools()}
	a, err := NewAgent(Options{Provider: p, Executor: e})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := a.Run(context.Background(), "wait for the background job")
	if err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	if len(e.calls) != 3 {
		t.Fatalf("calls = %v, want the poll run three times", e.calls)
	}
	if reply != "The job finished and its tests pass." {
		t.Fatalf("reply = %q, want the final answer", reply)
	}
}

// The parser reports the names it refused.
func TestFallbackParser_ReportsUnknownToolNames(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{KnownToolNames: func(n string) bool { return n == "shell" }})
	r := fp.Parse(qwenUnknownCall)
	if len(r.ToolCalls) != 0 {
		t.Fatalf("tool calls = %v, want none", r.ToolCalls)
	}
	if fmt.Sprint(r.UnknownToolNames) != "[check_background]" {
		t.Fatalf("unknown = %v, want [check_background]", r.UnknownToolNames)
	}
}
