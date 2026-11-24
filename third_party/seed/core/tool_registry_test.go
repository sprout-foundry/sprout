package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type mockEventPublisher struct {
	mu     sync.Mutex
	events []struct {
		eventType string
		data      any
	}
}

func (m *mockEventPublisher) Publish(eventType string, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, struct {
		eventType string
		data      any
	}{eventType, data})
}
func (m *mockEventPublisher) GetEvents() []struct {
	eventType string
	data      any
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]struct {
		eventType string
		data      any
	}, len(m.events))
	copy(cp, m.events)
	return cp
}

func dummyHandler(ctx context.Context, args map[string]interface{}) (string, error) {
	return "handled", nil
}

// --- Registration ---

func TestRegister_Success(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	if err := reg.Register(ToolConfig{Name: "echo", Description: "T", Handler: dummyHandler}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reg.HasTool("echo") {
		t.Error("expected echo registered")
	}
}

func TestRegister_HandlerWithImages(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	err := reg.Register(ToolConfig{
		Name: "img", Description: "T",
		HandlerWithImages: func(ctx context.Context, args map[string]interface{}) ([]ImageData, string, error) {
			return []ImageData{{Type: "image/png"}}, "done", nil
		},
	})
	if err != nil || !reg.HasTool("img") {
		t.Fatalf("unexpected: err=%v has=%v", err, reg.HasTool("img"))
	}
}

func TestRegister_FailsAndDuplicate(t *testing.T) {
	tests := []struct {
		name   string
		config ToolConfig
		sub    string
	}{
		{"empty_name", ToolConfig{Name: "", Handler: dummyHandler}, "empty"},
		{"no_handler", ToolConfig{Name: "broken"}, "must have"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewToolRegistry(ToolRegistryOptions{})
			err := reg.Register(tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.sub) {
				t.Errorf("expected %q in error, got: %v", tt.sub, err)
			}
		})
	}
	// Duplicate in same test
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "dup", Handler: dummyHandler})
	if err := reg.Register(ToolConfig{Name: "dup", Handler: dummyHandler}); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestRegisterWithAliases(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "search", Aliases: []string{"find", "query"}, Handler: dummyHandler})
	if !reg.HasTool("search") || !reg.HasTool("find") || !reg.HasTool("query") {
		t.Error("expected all aliases")
	}
	// RegisterAll in same test
	if err := reg.RegisterAll([]ToolConfig{
		{Name: "t1", Handler: dummyHandler}, {Name: "t2", Handler: dummyHandler}, {Name: "t3", Handler: dummyHandler},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.ToolNames()) != 4 {
		t.Errorf("expected 4, got %d", len(reg.ToolNames()))
	}
}

func TestRegisterAll_FirstErrorStops(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	err := reg.RegisterAll([]ToolConfig{
		{Name: "good", Handler: dummyHandler},
		{Name: "", Handler: dummyHandler},
		{Name: "t3", Handler: dummyHandler},
	})
	if err == nil {
		t.Error("expected error")
	}
	if len(reg.ToolNames()) != 1 {
		t.Errorf("expected 1 tool, got %d", len(reg.ToolNames()))
	}
}

func TestUnregister(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "unreg", Handler: dummyHandler})
	if !reg.Unregister("unreg") || reg.HasTool("unreg") {
		t.Error("expected unregister")
	}
	// Non-existent
	if NewToolRegistry(ToolRegistryOptions{}).Unregister("nope") {
		t.Error("expected false for non-existent")
	}
	// By alias
	reg.Register(ToolConfig{Name: "rem", Aliases: []string{"alt"}, Handler: dummyHandler})
	if !reg.Unregister("alt") || reg.HasTool("rem") {
		t.Error("expected unregister via alias")
	}
}

// --- Lookup ---

func TestGetTools(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "one", Handler: dummyHandler})
	reg.Register(ToolConfig{Name: "two", Handler: dummyHandler})
	if len(reg.GetTools()) != 2 {
		t.Errorf("expected 2, got %d", len(reg.GetTools()))
	}
	if got := NewToolRegistry(ToolRegistryOptions{}).GetTools(); got == nil || len(got) != 0 {
		t.Errorf("expected empty non-nil, got: %v", got)
	}
}

func TestGetTool(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "lookup", Description: "T", Handler: dummyHandler})
	tool := reg.GetTool("lookup")
	if tool == nil || tool.Function.Name != "lookup" {
		t.Errorf("expected lookup, got: %v", tool)
	}
	reg.Register(ToolConfig{Name: "canonical", Aliases: []string{"canon"}, Handler: dummyHandler})
	if tool = reg.GetTool("canon"); tool == nil || tool.Function.Name != "canonical" {
		t.Errorf("expected canonical by alias, got: %v", tool)
	}
	if NewToolRegistry(ToolRegistryOptions{}).GetTool("nope") != nil {
		t.Error("expected nil")
	}
	// Copy test
	reg.Register(ToolConfig{Name: "cm", Description: "Original", Handler: dummyHandler})
	reg.GetTool("cm").Function.Description = "Modified"
	if reg.GetTool("cm").Function.Description != "Original" {
		t.Error("expected copy")
	}
}

func TestHasTool(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "ht", Aliases: []string{"alt"}, Handler: dummyHandler})
	if !reg.HasTool("ht") || !reg.HasTool("alt") || reg.HasTool("nope") {
		t.Error("expected true/false")
	}
}

func TestToolNames(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "a", Handler: dummyHandler})
	reg.Register(ToolConfig{Name: "b", Handler: dummyHandler})
	names := reg.ToolNames()
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
	seen := map[string]bool{"a": false, "b": false}
	for _, n := range names {
		seen[n] = true
	}
	for n, v := range seen {
		if !v {
			t.Errorf("missing %q", n)
		}
	}
}

// --- Name resolution ---

func TestStripChannelSuffix(t *testing.T) {
	tests := []struct{ input, want string }{
		{"search<|channel|>0", "search"}, {"search<|channel|>1", "search"},
		{"search<|channel|>123", "search"}, {"search", "search"}, {"no channel", "no channel"},
	}
	for _, tt := range tests {
		if got := stripChannelSuffix(tt.input); got != tt.want {
			t.Errorf("strip(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestChannelSuffixResolved(t *testing.T) {
	recv := new(string)
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "ch_test",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			*recv = "called"
			return *recv, nil
		},
	})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "ch_test<|channel|>0", Arguments: "{}"}}})
	if *recv != "called" || results[0].Content != "called" {
		t.Errorf("expected called, recv=%q result=%q", *recv, results[0].Content)
	}
}

func TestCaseInsensitive(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "ct", Aliases: []string{"CT", "Ct"}, Description: "T", Handler: dummyHandler})
	if !reg.HasTool("CT") || !reg.HasTool("Ct") || !reg.HasTool("ct") {
		t.Error("expected case-insensitive alias")
	}
	reg.Register(ToolConfig{Name: "lower", Description: "T", Handler: dummyHandler})
	if !reg.HasTool("LOWER") {
		t.Error("expected case-insensitive name")
	}
}

// --- Argument parsing ---

func TestArgs_ValidJSON(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "args_test", Parameters: []ParameterConfig{{Name: "name", Type: "string", Required: true}}, Handler: dummyHandler,
	})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "args_test", Arguments: `{"name": "test"}`}}})
	if len(results) != 1 || strings.Contains(results[0].Content, "Failed to parse") {
		t.Errorf("unexpected error: %s", results[0].Content)
	}
}

func TestArgs_Repaired(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "comma", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) { return "ok", nil }})
	if results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "comma", Arguments: `{"k": "v",}`}}}); results[0].Content != "ok" {
		t.Errorf("trailing comma: expected 'ok', got %q", results[0].Content)
	}
	reg.Register(ToolConfig{Name: "sq", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) { return "repaired", nil }})
	if results := reg.Execute(context.Background(), []ToolCall{{ID: "c2", Type: "function", Function: ToolCallFunction{Name: "sq", Arguments: `{'k': 'v'}`}}}); results[0].Content != "repaired" {
		t.Errorf("single quote: expected 'repaired', got %q", results[0].Content)
	}
}

func TestArgs_MissingRequired(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "req_test", Parameters: []ParameterConfig{{Name: "must", Type: "string", Required: true}}, Handler: dummyHandler})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "req_test", Arguments: "{}"}}})
	if !strings.Contains(results[0].Content, "missing required") {
		t.Errorf("expected missing required, got: %q", results[0].Content)
	}
}

func TestArgs_AlternativeNames(t *testing.T) {
	recv := make(map[string]interface{})
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "alt", Parameters: []ParameterConfig{{Name: "file", Type: "string", Required: true, Alternatives: []string{"filename"}}},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) { recv = args; return "ok", nil },
	})
	reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "alt", Arguments: `{"filename": "/tmp/x"}`}}})
	if v, ok := recv["file"]; !ok || v != "/tmp/x" {
		t.Errorf("expected alt resolved to 'file'='/tmp/x', got: %v", recv)
	}
}

func TestArgs_TypeCoercion(t *testing.T) {
	testCases := []struct {
		name, arg, key string
		want           interface{}
	}{
		{"string_to_number", `{"count": "42"}`, "count", float64(42)},
		{"string_to_bool", `{"flag": "true"}`, "flag", true},
		{"float_to_int", `{"val": 3.14}`, "val", int64(3)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recv := make(map[string]interface{})
			pType := "number"
			if tc.name == "string_to_bool" {
				pType = "boolean"
			} else if tc.name == "float_to_int" {
				pType = "integer"
			}
			reg := NewToolRegistry(ToolRegistryOptions{})
			reg.Register(ToolConfig{
				Name: "coerce", Parameters: []ParameterConfig{{Name: tc.key, Type: pType}},
				Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
					recv = args
					return "ok", nil
				},
			})
			reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "coerce", Arguments: tc.arg}}})
			if recv[tc.key] != tc.want {
				t.Errorf("expected %v, got %v", tc.want, recv[tc.key])
			}
		})
	}
}

// --- Hooks ---

func TestHooks(t *testing.T) {
	// Block
	reg := NewToolRegistry(ToolRegistryOptions{PreExecuteHook: func(string, map[string]interface{}) error { return context.Canceled }})
	reg.Register(ToolConfig{Name: "hook_block", Handler: dummyHandler})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "hook_block", Arguments: "{}"}}})
	if !strings.Contains(results[0].Content, "hook rejected") {
		t.Errorf("expected hook rejection, got: %q", results[0].Content)
	}
	// Allow
	called := false
	reg = NewToolRegistry(ToolRegistryOptions{PreExecuteHook: func(string, map[string]interface{}) error { called = true; return nil }})
	reg.Register(ToolConfig{Name: "hook_allow", Handler: dummyHandler})
	reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "hook_allow", Arguments: "{}"}}})
	if !called {
		t.Error("expected PreExecuteHook called")
	}
	// Post-execute
	reg = NewToolRegistry(ToolRegistryOptions{PostExecuteHook: func(name string, result string) string { return "[wrapped] " + result }})
	reg.Register(ToolConfig{
		Name: "post_hook", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) { return "original", nil },
	})
	results = reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "post_hook", Arguments: "{}"}}})
	if results[0].Content != "[wrapped] original" {
		t.Errorf("expected wrapped, got: %q", results[0].Content)
	}
	// Nil hooks
	reg = NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "nil_hooks", Handler: dummyHandler})
	results = reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "nil_hooks", Arguments: "{}"}}})
	if results[0].Content != "handled" {
		t.Errorf("expected handled with nil hooks, got: %q", results[0].Content)
	}
}

// --- Events ---

// ToolRegistry no longer publishes tool_start / tool_end events directly —
// those are published by the chat loop (see conversation.go around
// executor.Execute) so the API contract holds for any Executor
// implementation, not just ToolRegistry. The integration tests under
// TestFallback_* exercise the end-to-end event path through Agent.Run.

func TestRegistry_DoesNotPublishToolEvents(t *testing.T) {
	ep := &mockEventPublisher{}
	reg := NewToolRegistry(ToolRegistryOptions{EventPublisher: ep})
	reg.Register(ToolConfig{Name: "evt_test", Handler: dummyHandler})
	calls := []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "evt_test", Arguments: "{}"}}}
	reg.Execute(context.Background(), calls)
	if events := ep.GetEvents(); len(events) != 0 {
		t.Fatalf("expected 0 events (registry should not publish tool_start/tool_end), got %d: %v", len(events), events)
	}
}

func TestRegistry_ErrorPathDoesNotPublishToolEvents(t *testing.T) {
	ep := &mockEventPublisher{}
	reg := NewToolRegistry(ToolRegistryOptions{EventPublisher: ep})
	reg.Register(ToolConfig{
		Name: "err_evt",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "", context.Canceled
		},
	})
	reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "err_evt", Arguments: "{}"}}})
	if events := ep.GetEvents(); len(events) != 0 {
		t.Fatalf("expected 0 events on error path, got %d: %v", len(events), events)
	}
}

// --- Message.Status round-trip from Executor through to the chat loop ---
//
// These tests pin the contract that ToolRegistry sets Message.Status on the
// returned Message so the chat loop can publish the correct status field on
// tool_end events. ToolStatusCompleted on success, ToolStatusError on every
// failure mode (parse, circuit breaker, pre-execute hook, handler error,
// execution timeout).

func TestRegistry_Status_SuccessIsCompleted(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{Name: "ok", Handler: dummyHandler})
	out := reg.Execute(context.Background(), []ToolCall{
		{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "ok", Arguments: "{}"}},
	})
	if len(out) != 1 {
		t.Fatalf("expected 1 result, got %d", len(out))
	}
	if out[0].Status != ToolStatusCompleted {
		t.Errorf("expected Status=%q on success, got %q", ToolStatusCompleted, out[0].Status)
	}
}

func TestRegistry_Status_HandlerErrorIsError(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "boom",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "", errors.New("boom")
		},
	})
	out := reg.Execute(context.Background(), []ToolCall{
		{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "boom", Arguments: "{}"}},
	})
	if out[0].Status != ToolStatusError {
		t.Errorf("expected Status=%q on handler error, got %q", ToolStatusError, out[0].Status)
	}
}

func TestRegistry_Status_ParseErrorIsError(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name:    "needs_args",
		Handler: dummyHandler,
		Parameters: []ParameterConfig{
			{Name: "required_field", Type: "string", Required: true},
		},
	})
	out := reg.Execute(context.Background(), []ToolCall{
		// Arguments deliberately missing the required field.
		{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "needs_args", Arguments: "{}"}},
	})
	if out[0].Status != ToolStatusError {
		t.Errorf("expected Status=%q on parse/validation error, got %q (content=%q)", ToolStatusError, out[0].Status, out[0].Content)
	}
}

func TestRegistry_Status_PreExecuteHookRejectIsError(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{
		PreExecuteHook: func(name string, args map[string]interface{}) error {
			return errors.New("blocked by policy")
		},
	})
	reg.Register(ToolConfig{Name: "blocked", Handler: dummyHandler})
	out := reg.Execute(context.Background(), []ToolCall{
		{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "blocked", Arguments: "{}"}},
	})
	if out[0].Status != ToolStatusError {
		t.Errorf("expected Status=%q on pre-execute hook reject, got %q", ToolStatusError, out[0].Status)
	}
}

func TestNoopPublisher(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{EventPublisher: nil})
	reg.Register(ToolConfig{Name: "np_evt", Handler: dummyHandler})
	_ = reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "np_evt", Arguments: "{}"}}})
}
