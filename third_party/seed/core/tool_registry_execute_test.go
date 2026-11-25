package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecute_Sequential(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "seq_test", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "seq_result", nil
		},
	})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "seq_test", Arguments: "{}"}}})
	if len(results) != 1 || results[0].Content != "seq_result" {
		t.Errorf("expected 'seq_result', got: %q", results[0].Content)
	}
}

func TestExecute_ArgsErrorUnknown(t *testing.T) {
	recv := make(map[string]interface{})
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "arg_rcv", Parameters: []ParameterConfig{{Name: "x", Type: "string", Required: true}},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			recv = args
			return "ok", nil
		},
	})
	reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "arg_rcv", Arguments: `{"x": "hello"}`}}})
	if recv["x"] != "hello" {
		t.Errorf("expected x='hello', got: %v", recv["x"])
	}
	reg2 := NewToolRegistry(ToolRegistryOptions{})
	reg2.Register(ToolConfig{
		Name: "fail_tool", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "", fmt.Errorf("handler failed")
		},
	})
	results := reg2.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "fail_tool", Arguments: "{}"}}})
	if len(results) != 1 || !strings.Contains(results[0].Content, "handler failed") {
		t.Errorf("expected error in result, got: %q", results[0].Content)
	}
	results2 := reg2.Execute(context.Background(), []ToolCall{{ID: "c2", Type: "function", Function: ToolCallFunction{Name: "unknown", Arguments: "{}"}}})
	if len(results2) != 1 || !strings.Contains(results2[0].Content, "unknown tool") {
		t.Errorf("expected unknown tool error, got: %q", results2[0].Content)
	}
	if results := reg2.Execute(context.Background(), []ToolCall{}); results != nil {
		t.Errorf("expected nil for empty calls")
	}
	if results := reg2.Execute(context.Background(), nil); results != nil {
		t.Errorf("expected nil for nil calls")
	}
}
func TestTimeout(t *testing.T) {
	// Exceeds
	reg := NewToolRegistry(ToolRegistryOptions{DefaultTimeout: 50 * time.Millisecond})
	reg.Register(ToolConfig{
		Name: "slow", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			time.Sleep(200 * time.Millisecond)
			return "done", nil
		},
	})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "slow", Arguments: "{}"}}})
	if len(results) != 1 || !strings.Contains(results[0].Content, "timed out") {
		t.Errorf("expected timeout, got: %q", results[0].Content)
	}
	// Default works
	reg2 := NewToolRegistry(ToolRegistryOptions{DefaultTimeout: 5 * time.Second})
	reg2.Register(ToolConfig{
		Name: "fast", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "fast_result", nil
		},
	})
	results = reg2.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "fast", Arguments: "{}"}}})
	if results[0].Content != "fast_result" {
		t.Errorf("expected 'fast_result', got: %q", results[0].Content)
	}
	// Context cancellation
	reg3 := NewToolRegistry(ToolRegistryOptions{DefaultTimeout: 30 * time.Second})
	reg3.Register(ToolConfig{
		Name: "cancel_me", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results = reg3.Execute(ctx, []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "cancel_me", Arguments: "{}"}}})
	if len(results) != 1 || (!strings.Contains(results[0].Content, "cancel") && !strings.Contains(results[0].Content, "timed out")) {
		t.Errorf("expected cancel error, got: %q", results[0].Content)
	}
	// Per-tool timeout
	reg4 := NewToolRegistry(ToolRegistryOptions{DefaultTimeout: 5 * time.Second})
	reg4.Register(ToolConfig{
		Name: "per_tool", Timeout: 50 * time.Millisecond,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			time.Sleep(200 * time.Millisecond)
			return "done", nil
		},
	})
	results = reg4.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "per_tool", Arguments: "{}"}}})
	if len(results) != 1 || !strings.Contains(results[0].Content, "timed out") {
		t.Errorf("expected timeout, got: %q", results[0].Content)
	}
}
func TestTruncation(t *testing.T) {
	// Exceeds global limit
	reg := NewToolRegistry(ToolRegistryOptions{MaxResultSize: 20})
	reg.Register(ToolConfig{
		Name: "trunc", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "This is a very long result that should be truncated", nil
		},
	})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "trunc", Arguments: "{}"}}})
	if !strings.Contains(results[0].Content, "truncated") {
		t.Errorf("expected truncated, got: %q", results[0].Content)
	}
	// Under limit
	reg2 := NewToolRegistry(ToolRegistryOptions{MaxResultSize: 200})
	reg2.Register(ToolConfig{
		Name: "short", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "short result", nil
		},
	})
	results = reg2.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "short", Arguments: "{}"}}})
	if results[0].Content != "short result" {
		t.Errorf("expected 'short result', got: %q", results[0].Content)
	}
	// Per-tool overrides global
	reg3 := NewToolRegistry(ToolRegistryOptions{MaxResultSize: 20})
	reg3.Register(ToolConfig{
		Name: "per_trunc", MaxResultSize: 50,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "This is a moderately long result that exceeds global but not per-tool", nil
		},
	})
	results = reg3.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "per_trunc", Arguments: "{}"}}})
	if !strings.Contains(results[0].Content, "truncated") {
		t.Errorf("expected per-tool truncation, got: %q", results[0].Content)
	}
}
func TestCircuitBreaker(t *testing.T) {
	// Opens after threshold
	reg := NewToolRegistry(ToolRegistryOptions{})
	failCount := int32(0)
	reg.Register(ToolConfig{
		Name: "breaker_test",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if atomic.AddInt32(&failCount, 1) <= 5 {
				return "", fmt.Errorf("fail %d", failCount)
			}
			return "ok", nil
		},
	})
	for i := 0; i < 5; i++ {
		_ = reg.Execute(context.Background(), []ToolCall{{ID: fmt.Sprintf("c%d", i), Type: "function", Function: ToolCallFunction{Name: "breaker_test", Arguments: "{}"}}})
	}
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c6", Type: "function", Function: ToolCallFunction{Name: "breaker_test", Arguments: "{}"}}})
	if !strings.Contains(results[0].Content, "Circuit breaker") {
		t.Errorf("expected circuit breaker rejection, got: %q", results[0].Content)
	}
	// Rejects when open
	reg2 := NewToolRegistry(ToolRegistryOptions{})
	reg2.Register(ToolConfig{
		Name: "open_cb", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "", fmt.Errorf("always fails")
		},
	})
	for i := 0; i < 6; i++ {
		reg2.Execute(context.Background(), []ToolCall{{ID: fmt.Sprintf("c%d", i), Type: "function", Function: ToolCallFunction{Name: "open_cb", Arguments: "{}"}}})
	}
	results = reg2.Execute(context.Background(), []ToolCall{{ID: "c_fail", Type: "function", Function: ToolCallFunction{Name: "open_cb", Arguments: "{}"}}})
	if !strings.Contains(results[0].Content, "Circuit breaker") {
		t.Errorf("expected rejection, got: %q", results[0].Content)
	}
	// Closes after success in half-open
	reg3 := NewToolRegistry(ToolRegistryOptions{})
	reg3.Register(ToolConfig{
		Name: "recovery_cb", Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "recovered", nil
		},
	})
	for i := 0; i < 5; i++ {
		_ = reg3.Execute(context.Background(), []ToolCall{{ID: fmt.Sprintf("c%d", i), Type: "function", Function: ToolCallFunction{Name: "recovery_cb", Arguments: "{}"}}})
	}
	reg3.circuitBreakers["recovery_cb"].mu.Lock()
	reg3.circuitBreakers["recovery_cb"].state = breakerHalfOpen
	reg3.circuitBreakers["recovery_cb"].mu.Unlock()
	results = reg3.Execute(context.Background(), []ToolCall{{ID: "c_recovery", Type: "function", Function: ToolCallFunction{Name: "recovery_cb", Arguments: "{}"}}})
	if len(results) != 1 || results[0].Content != "recovered" {
		t.Errorf("expected 'recovered', got: %q", results[0].Content)
	}
	results2 := reg3.Execute(context.Background(), []ToolCall{{ID: "c_ok", Type: "function", Function: ToolCallFunction{Name: "recovery_cb", Arguments: "{}"}}})
	if results2[0].Content != "recovered" {
		t.Errorf("expected 'recovered' after circuit closed, got: %q", results2[0].Content)
	}
}
func TestCircuitBreakerUnit(t *testing.T) {
	cb := newCircuitBreaker(3, 100*time.Millisecond)
	for i := 0; i < 3; i++ {
		cb.Allow()
		cb.RecordFailure()
	}
	if cb.Allow() {
		t.Error("expected false when open")
	}
	time.Sleep(150 * time.Millisecond)
	if !cb.Allow() {
		t.Error("expected true in half-open")
	}
	if cb.Allow() {
		t.Error("expected only one request allowed in half-open")
	}
	cb2 := newCircuitBreaker(3, 100*time.Millisecond)
	for i := 0; i < 3; i++ {
		cb2.Allow()
		cb2.RecordFailure()
	}
	time.Sleep(150 * time.Millisecond)
	cb2.Allow()
	cb2.RecordFailure()
	if cb2.Allow() {
		t.Error("expected false after half-open failure")
	}
}

func TestParallel(t *testing.T) {
	// SafeForParallel concurrent
	var executed int64
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "par_safe", SafeForParallel: true,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			atomic.AddInt64(&executed, 1)
			time.Sleep(50 * time.Millisecond)
			return "safe", nil
		},
	})
	results := reg.Execute(context.Background(), []ToolCall{
		{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "par_safe", Arguments: "{}"}},
		{ID: "c2", Type: "function", Function: ToolCallFunction{Name: "par_safe", Arguments: "{}"}},
	})
	if len(results) != 2 || atomic.LoadInt64(&executed) != 2 {
		t.Errorf("expected 2 results & executions, got %d results, %d executed", len(results), executed)
	}
	// Sequential non-safe
	var mu sync.Mutex
	order := []string{}
	reg2 := NewToolRegistry(ToolRegistryOptions{})
	reg2.Register(ToolConfig{
		Name: "par_unsafe",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			mu.Lock()
			order = append(order, "start")
			time.Sleep(50 * time.Millisecond)
			order = append(order, "end")
			mu.Unlock()
			return "unsafe", nil
		},
	})
	reg2.Execute(context.Background(), []ToolCall{
		{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "par_unsafe", Arguments: "{}"}},
		{ID: "c2", Type: "function", Function: ToolCallFunction{Name: "par_unsafe", Arguments: "{}"}},
	})
	mu.Lock()
	defer mu.Unlock()
	if order[0] != "start" || order[1] != "end" || order[2] != "start" || order[3] != "end" {
		t.Errorf("expected [start,end,start,end], got: %v", order)
	}
	// Result ordering
	reg3 := NewToolRegistry(ToolRegistryOptions{})
	reg3.Register(ToolConfig{
		Name: "ordered", SafeForParallel: true,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			time.Sleep(50 * time.Millisecond)
			return "result", nil
		},
	})
	calls := []ToolCall{
		{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "ordered", Arguments: "{}"}},
		{ID: "call_2", Type: "function", Function: ToolCallFunction{Name: "ordered", Arguments: "{}"}},
		{ID: "call_3", Type: "function", Function: ToolCallFunction{Name: "ordered", Arguments: "{}"}},
	}
	results = reg3.Execute(context.Background(), calls)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ToolCallID != "call_1" || results[1].ToolCallID != "call_2" || results[2].ToolCallID != "call_3" {
		t.Errorf("expected correct ordering, got: %s, %s, %s",
			results[0].ToolCallID, results[1].ToolCallID, results[2].ToolCallID)
	}
}

func TestHandlerWithImages(t *testing.T) {
	// Returns text
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "img_tool",
		HandlerWithImages: func(ctx context.Context, args map[string]interface{}) ([]ImageData, string, error) {
			return []ImageData{{Type: "image/png", Base64: "abc"}}, "text result", nil
		},
	})
	results := reg.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "img_tool", Arguments: "{}"}}})
	if results[0].Content != "text result" {
		t.Errorf("expected 'text result', got: %q", results[0].Content)
	}
	// Images must ride on the tool-result Message, not be discarded.
	// Regression: runWithTimeout historically executed HandlerWithImages as
	// `_, text, err := ...`, dropping the images — the tool promised
	// multimodal results (read_file on a PDF) that never reached the model.
	if len(results[0].Images) != 1 || results[0].Images[0].Base64 != "abc" {
		t.Errorf("expected 1 image (base64 'abc') on the result message, got %+v", results[0].Images)
	}
	if results[0].Status != ToolStatusCompleted {
		t.Errorf("expected completed status, got %q", results[0].Status)
	}
	// Error
	reg2 := NewToolRegistry(ToolRegistryOptions{})
	reg2.Register(ToolConfig{
		Name: "img_err", HandlerWithImages: func(ctx context.Context, args map[string]interface{}) ([]ImageData, string, error) {
			return nil, "", fmt.Errorf("image error")
		},
	})
	results = reg2.Execute(context.Background(), []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "img_err", Arguments: "{}"}}})
	if !strings.Contains(results[0].Content, "image error") {
		t.Errorf("expected error, got: %q", results[0].Content)
	}
}

func TestDefaults(t *testing.T) {
	reg := NewToolRegistry(ToolRegistryOptions{})
	if reg.defaultTimeout != 5*time.Minute || reg.maxResultSize != 50*1024 {
		t.Errorf("expected defaults: timeout=%v size=%d", reg.defaultTimeout, reg.maxResultSize)
	}
}
