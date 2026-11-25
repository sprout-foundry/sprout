package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Mock implementations ---

type mockProvider struct {
	chatResp   *ChatResponse
	chatErr    error
	streamErr  error
	info       ProviderInfo
	tokenCount int
}

func (m *mockProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return m.chatResp, m.chatErr
}
func (m *mockProvider) ChatStream(_ context.Context, _ *ChatRequest, _ StreamHandler) error {
	return m.streamErr
}
func (m *mockProvider) Info() ProviderInfo {
	return m.info
}
func (m *mockProvider) EstimateTokens(_ *ChatRequest) int {
	return m.tokenCount
}

type mockExecutor struct {
	tools   []Tool
	results []Message
}

func (m *mockExecutor) GetTools() []Tool {
	return m.tools
}
func (m *mockExecutor) Execute(_ context.Context, calls []ToolCall) []Message {
	return m.results
}

type mockUI struct {
	promptResp   string
	promptErr    error
	confirmResp  bool
	confirmErr   error
	printBuf     strings.Builder
	printLineBuf strings.Builder
}

func (m *mockUI) Prompt(_ string) (string, error) {
	return m.promptResp, m.promptErr
}
func (m *mockUI) Confirm(_ string) (bool, error) {
	return m.confirmResp, m.confirmErr
}
func (m *mockUI) Print(s string) {
	m.printBuf.WriteString(s)
}
func (m *mockUI) PrintLine(s string) {
	m.printLineBuf.WriteString(s + "\n")
}

// --- Agent tests ---

func TestNewAgent_ErrOnNilProvider(t *testing.T) {
	_, err := NewAgent(Options{
		Executor: &mockExecutor{},
	})
	if err == nil {
		t.Fatal("expected error for nil Provider")
	}
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("expected ErrNoProvider, got: %v", err)
	}
}

func TestNewAgent_ErrOnNilExecutor(t *testing.T) {
	_, err := NewAgent(Options{
		Provider: &mockProvider{},
	})
	if err == nil {
		t.Fatal("expected error for nil ToolExecutor")
	}
	if !errors.Is(err, ErrNoExecutor) {
		t.Fatalf("expected ErrNoExecutor, got: %v", err)
	}
}

func TestNewAgent_DefaultSystemPrompt(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.systemPrompt != DefaultSystemPrompt {
		t.Errorf("expected default system prompt, got %q", a.systemPrompt)
	}
}

func TestNewAgent_CustomSystemPrompt(t *testing.T) {
	custom := "You are a coding assistant."
	a, err := NewAgent(Options{
		Provider:     &mockProvider{},
		Executor:     &mockExecutor{},
		SystemPrompt: custom,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.systemPrompt != custom {
		t.Errorf("expected %q, got %q", custom, a.systemPrompt)
	}
}

func TestNewAgent_WhitespaceSystemPrompt(t *testing.T) {
	a, err := NewAgent(Options{
		Provider:     &mockProvider{},
		Executor:     &mockExecutor{},
		SystemPrompt: "   \t\n  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.systemPrompt != DefaultSystemPrompt {
		t.Errorf("expected default system prompt for whitespace, got %q", a.systemPrompt)
	}
}

func TestAgent_SetSystemPrompt(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.SetSystemPrompt("New prompt")
	if a.systemPrompt != "New prompt" {
		t.Errorf("expected 'New prompt', got %q", a.systemPrompt)
	}
}

func TestAgent_PauseAndResume(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.IsPaused() {
		t.Error("expected not paused initially")
	}
	a.Pause()
	if !a.IsPaused() {
		t.Error("expected paused after Pause()")
	}
	a.Resume()
	if a.IsPaused() {
		t.Error("expected not paused after Resume()")
	}
}

func TestAgent_Run_WhenPaused(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Pause()
	_, err = a.Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when paused")
	}
	if !strings.Contains(err.Error(), "paused") {
		t.Errorf("expected 'paused' in error, got: %v", err)
	}
}

func TestAgent_Run_CompleteResponse(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Hello!"}}},
			Usage:   ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	executor := &mockExecutor{}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", result)
	}

	// Check state
	st := a.State()
	if st.TotalTokens() != 15 {
		t.Errorf("expected 15 total tokens, got %d", st.TotalTokens())
	}
}

func TestAgent_Run_WithToolCalls(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "Let me search.",
					ToolCalls: []ToolCall{{
						ID:       "call_1",
						Function: ToolCallFunction{Name: "search", Arguments: `{"query":"test"}`},
					}},
				},
			}},
			Usage: ChatUsage{TotalTokens: 20},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "Search results", ToolCallID: "call_1"}},
	}
	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Search for something")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("expected ErrMaxIterations, got: %v", err)
	}

	st := a.State()
	msgs := st.Messages()
	// Should have: user, assistant (with tool call), tool result
	if len(msgs) < 3 {
		t.Errorf("expected at least 3 messages, got %d", len(msgs))
	}
}

func TestAgent_Run_MaxIterations(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:       "call_1",
						Function: ToolCallFunction{Name: "loop", Arguments: "{}"},
					}},
				},
			}},
			Usage: ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "loop", ToolCallID: "call_1"}},
	}
	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Loop")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("expected ErrMaxIterations, got: %v", err)
	}
	// Should stop after 3 iterations
}

func TestAgent_Run_ProviderError(t *testing.T) {
	provider := &mockProvider{
		chatErr: fmt.Errorf("provider down"),
		info:    ProviderInfo{ContextSize: 10000},
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "chat failed") {
		t.Errorf("expected 'chat failed' in error, got: %v", err)
	}
}

func TestAgent_ExportImportState(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.State().AddMessage(Message{Role: "user", Content: "hello"})
	a.State().SetSessionID("test-session")
	a.State().AddTokens(100, 50, 150)

	data, err := a.ExportState()
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	a2, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a2.ImportState(data); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	st := a2.State()
	if st.SessionID() != "test-session" {
		t.Errorf("expected session 'test-session', got %q", st.SessionID())
	}
	if st.TotalTokens() != 150 {
		t.Errorf("expected 150 tokens, got %d", st.TotalTokens())
	}
	msgs := st.Messages()
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("expected 1 message 'hello', got %d messages", len(msgs))
	}
}

func TestAgent_ProviderAccess(t *testing.T) {
	provider := &mockProvider{
		info: ProviderInfo{Model: "test-model", ContextSize: 128000},
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	info := a.Provider().Info()
	if info.Model != "test-model" {
		t.Errorf("expected 'test-model', got %q", info.Model)
	}
}

func TestAgent_SetProvider(t *testing.T) {
	original := &mockProvider{
		info: ProviderInfo{Model: "original-model", ContextSize: 10000},
	}
	a, err := NewAgent(Options{
		Provider: original,
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial provider
	info := a.Provider().Info()
	if info.Model != "original-model" {
		t.Errorf("expected 'original-model', got %q", info.Model)
	}

	// Swap to new provider
	newProvider := &mockProvider{
		info: ProviderInfo{Model: "swapped-model", ContextSize: 128000},
	}
	a.SetProvider(newProvider)

	// Verify the swap took effect
	info = a.Provider().Info()
	if info.Model != "swapped-model" {
		t.Errorf("expected 'swapped-model', got %q", info.Model)
	}
}

func TestAgent_SetProvider_NilPanics(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on SetProvider(nil)")
		}
	}()
	a.SetProvider(nil)
}

func TestAgent_SetProvider_AffectsSubsequentRuns(t *testing.T) {
	providerA := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Response A"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{Model: "model-a", ContextSize: 10000},
		tokenCount: 50,
	}
	providerB := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Response B"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{Model: "model-b", ContextSize: 128000},
		tokenCount: 60,
	}
	a, err := NewAgent(Options{
		Provider: providerA,
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	// First run uses providerA
	result, err := a.Run(context.Background(), "query 1")
	if err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	if result != "Response A" {
		t.Errorf("expected 'Response A', got %q", result)
	}

	// Swap provider
	a.SetProvider(providerB)

	// Second run uses providerB
	result, err = a.Run(context.Background(), "query 2")
	if err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	if result != "Response B" {
		t.Errorf("expected 'Response B', got %q", result)
	}

	// Verify providerB was actually called
	if providerB.EstimateTokens(&ChatRequest{}) != 60 {
		t.Error("providerB token estimate mismatch")
	}
}

func TestAgent_SteerSystem(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.SteerSystem("Focus on performance.")

	msgs := a.drainSteerMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 steer message, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected role 'system', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "Focus on performance." {
		t.Errorf("expected 'Focus on performance.', got %q", msgs[0].Content)
	}
}

func TestAgent_SteerSystem_Multiple(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.SteerSystem("First directive.")
	a.SteerSystem("Second directive.")

	msgs := a.drainSteerMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 steer messages, got %d", len(msgs))
	}
	if msgs[0].Content != "First directive." {
		t.Errorf("expected 'First directive.', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "Second directive." {
		t.Errorf("expected 'Second directive.', got %q", msgs[1].Content)
	}
}

func TestAgent_SteerSystem_AfterDrainIsEmpty(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.SteerSystem("Temporary guidance.")
	msgs := a.drainSteerMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 steer message, got %d", len(msgs))
	}

	// Drain again — should be empty
	msgs = a.drainSteerMessages()
	if msgs != nil {
		t.Errorf("expected nil after second drain, got %d messages", len(msgs))
	}
}

func TestAgent_SteerSystem_MixedWithSteer(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.Steer(Message{Role: "user", Content: "User directive."})
	a.SteerSystem("System directive.")
	a.Steer(Message{Role: "user", Content: "Another user directive."})

	msgs := a.drainSteerMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 steer messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "User directive." {
		t.Errorf("expected first message user, got %+v", msgs[0])
	}
	if msgs[1].Role != "system" || msgs[1].Content != "System directive." {
		t.Errorf("expected second message system, got %+v", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "Another user directive." {
		t.Errorf("expected third message user, got %+v", msgs[2])
	}
}

func TestAgent_OnIteration_CallbackFired(t *testing.T) {
	var calls []struct {
		iter          int
		messages      int
		tokenEstimate int
		contextSize   int
	}
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Done"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 50,
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
		OnIteration: func(iter int, messages int, tokenEstimate int, contextSize int) {
			calls = append(calls, struct {
				iter          int
				messages      int
				tokenEstimate int
				contextSize   int
			}{iter, messages, tokenEstimate, contextSize})
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 OnIteration call, got %d", len(calls))
	}
	if calls[0].iter != 0 {
		t.Errorf("expected iteration 0, got %d", calls[0].iter)
	}
	// After adding the user message, state has 1 message
	if calls[0].messages != 1 {
		t.Errorf("expected 1 message, got %d", calls[0].messages)
	}
	// tokenEstimate comes from provider.EstimateTokens (mock returns 50)
	if calls[0].tokenEstimate != 50 {
		t.Errorf("expected tokenEstimate 50, got %d", calls[0].tokenEstimate)
	}
	// contextSize comes from provider.Info().ContextSize (set to 10000)
	if calls[0].contextSize != 10000 {
		t.Errorf("expected contextSize 10000, got %d", calls[0].contextSize)
	}
}

func TestAgent_OnIteration_MultipleIterations(t *testing.T) {
	var calls []int
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:       "call_1",
						Function: ToolCallFunction{Name: "loop", Arguments: "{}"},
					}},
				},
			}},
			Usage: ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 50,
	}
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "ok", ToolCallID: "call_1"}},
	}
	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 3,
		OnIteration: func(iter int, messages int, tokenEstimate int, contextSize int) {
			calls = append(calls, iter)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Loop")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("expected ErrMaxIterations, got: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("expected 3 OnIteration calls, got %d", len(calls))
	}
	if calls[0] != 0 || calls[1] != 1 || calls[2] != 2 {
		t.Errorf("expected iterations [0,1,2], got %v", calls)
	}
}

func TestAgent_OnIteration_NilNoop(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "OK"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 50,
	}
	a, err := NewAgent(Options{
		Provider:    provider,
		Executor:    &mockExecutor{},
		OnIteration: nil, // explicitly nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic
	_, err = a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgent_OnIteration_CallbackPanicRecovered(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "OK"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 50,
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
		OnIteration: func(iter int, messages int, tokenEstimate int, contextSize int) {
			panic("telemetry error")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should not crash; the panic is recovered and the agent continues
	result, err := a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error despite callback panic: %v", err)
	}
	if result != "OK" {
		t.Errorf("expected 'OK', got %q", result)
	}
}

// --- OnCheckpoint tests ---

func TestAgent_OnCheckpoint_CallbackFired(t *testing.T) {
	var checkpoints []TurnCheckpoint
	var mu sync.Mutex

	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Hello!"}}},
			Usage:   ChatUsage{TotalTokens: 15},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
		OnCheckpoint: func(cp TurnCheckpoint) {
			mu.Lock()
			defer mu.Unlock()
			checkpoints = append(checkpoints, cp)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// OnCheckpoint fires asynchronously via RecordTurnCheckpointAsync.
	// Poll until we have the expected checkpoint count (or timeout).
	start := time.Now()
	for time.Since(start) < 3*time.Second {
		mu.Lock()
		n := len(checkpoints)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 OnCheckpoint call, got %d", len(checkpoints))
	}
	cp := checkpoints[0]
	if cp.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if cp.UserMessage != "test query" {
		t.Errorf("expected user message 'test query', got %q", cp.UserMessage)
	}
	if cp.StartIndex != 0 {
		t.Errorf("expected StartIndex 0, got %d", cp.StartIndex)
	}
	if cp.EndIndex < 0 {
		t.Errorf("expected non-negative EndIndex, got %d", cp.EndIndex)
	}
	// The turn has 2 messages (user + assistant), so EndIndex should be 1
	if cp.EndIndex != 1 {
		t.Errorf("expected EndIndex 1, got %d", cp.EndIndex)
	}
	if cp.ActionableSummary == "" {
		t.Error("expected non-empty ActionableSummary")
	}
}

func TestAgent_OnCheckpoint_NilNoop(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "OK"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 50,
	}
	a, err := NewAgent(Options{
		Provider:     provider,
		Executor:     &mockExecutor{},
		OnCheckpoint: nil, // explicitly nil — should be a no-op
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic even when OnCheckpoint is nil.
	_, err = a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgent_OnCheckpoint_PanicRecovered(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Response"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 50,
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
		OnCheckpoint: func(cp TurnCheckpoint) {
			_ = cp
			panic("telemetry error")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should not crash; the panic inside the callback is recovered
	// synchronously in finalize() and the agent continues normally.
	result, err := a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error despite callback panic: %v", err)
	}
	if result != "Response" {
		t.Errorf("expected 'Response', got %q", result)
	}
}

func TestAgent_OnCheckpoint_OnlyOnCompletedTurns(t *testing.T) {
	var checkpoints []TurnCheckpoint
	var mu sync.Mutex

	// Provider always returns tool calls, so the loop never reaches a
	// content-only final response. With MaxIterations it will hit the
	// iteration cap without ever setting turnCompleted=true.
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:       "call_1",
						Function: ToolCallFunction{Name: "loop", Arguments: "{}"},
					}},
				},
			}},
			Usage: ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "loop result", ToolCallID: "call_1"}},
	}
	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 3,
		OnCheckpoint: func(cp TurnCheckpoint) {
			mu.Lock()
			defer mu.Unlock()
			checkpoints = append(checkpoints, cp)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Loop")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("expected ErrMaxIterations, got: %v", err)
	}

	// OnCheckpoint fires synchronously; with turnCompleted=false no callback
	// should have been invoked.
	mu.Lock()
	defer mu.Unlock()

	if len(checkpoints) != 0 {
		t.Fatalf("expected 0 OnCheckpoint calls for incomplete turn, got %d", len(checkpoints))
	}
}

func TestAgent_OnCheckpoint_MultipleTurns(t *testing.T) {
	var checkpoints []TurnCheckpoint
	var mu sync.Mutex

	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Done"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 50,
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
		OnCheckpoint: func(cp TurnCheckpoint) {
			mu.Lock()
			defer mu.Unlock()
			checkpoints = append(checkpoints, cp)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// First query
	_, err = a.Run(context.Background(), "first query")
	if err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}

	// Second query on the same agent (state persists)
	_, err = a.Run(context.Background(), "second query")
	if err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}

	// OnCheckpoint fires asynchronously; poll until both callbacks arrive.
	start := time.Now()
	for time.Since(start) < 3*time.Second {
		mu.Lock()
		n := len(checkpoints)
		mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(checkpoints) != 2 {
		t.Fatalf("expected 2 OnCheckpoint calls, got %d", len(checkpoints))
	}

	// Async checkpoints can arrive in any order; sort by StartIndex for deterministic assertions.
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].StartIndex < checkpoints[j].StartIndex
	})

	if checkpoints[0].UserMessage != "first query" {
		t.Errorf("expected checkpoint[0].UserMessage 'first query', got %q", checkpoints[0].UserMessage)
	}
	if checkpoints[1].UserMessage != "second query" {
		t.Errorf("expected checkpoint[1].UserMessage 'second query', got %q", checkpoints[1].UserMessage)
	}

	// The second turn appends to the same state, so its EndIndex must be
	// higher than the first turn's EndIndex.
	if checkpoints[0].EndIndex >= checkpoints[1].EndIndex {
		t.Errorf("expected checkpoint[1].EndIndex > checkpoint[0].EndIndex (%d >= %d)",
			checkpoints[1].EndIndex, checkpoints[0].EndIndex)
	}

	// StartIndex for the second turn should be >= StartIndex for the first.
	if checkpoints[1].StartIndex < checkpoints[0].StartIndex {
		t.Errorf("expected checkpoint[1].StartIndex >= checkpoint[0].StartIndex (%d < %d)",
			checkpoints[1].StartIndex, checkpoints[0].StartIndex)
	}
}
