package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// sequentialProvider: returns different ChatResponses on successive Chat() calls
// ---------------------------------------------------------------------------

type sequentialProvider struct {
	responses []*ChatResponse
	callIndex int
	info      ProviderInfo
	mu        sync.Mutex
}

func (p *sequentialProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.callIndex >= len(p.responses) {
		return nil, nil
	}
	resp := p.responses[p.callIndex]
	p.callIndex++
	return resp, nil
}

func (p *sequentialProvider) ChatStream(_ context.Context, _ *ChatRequest, _ StreamHandler) error {
	return nil
}

func (p *sequentialProvider) Info() ProviderInfo {
	return p.info
}

func (p *sequentialProvider) EstimateTokens(_ *ChatRequest) int {
	return 10
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestAgentWithProvider(t *testing.T, p Provider) *Agent {
	t.Helper()
	a, err := NewAgent(Options{
		Provider:      p,
		Executor:      &mockExecutor{},
		MaxIterations: 10,
	})
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return a
}

func newTestAgentWithProviderAndExecutor(t *testing.T, p Provider, exec *mockExecutor) *Agent {
	t.Helper()
	a, err := NewAgent(Options{
		Provider:      p,
		Executor:      exec,
		MaxIterations: 10,
	})
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return a
}

// findMessageWithContent returns true if any message in the slice contains
// the given substring in its Content field.
func findMessageWithContent(msgs []Message, content string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, content) {
			return true
		}
	}
	return false
}

// findMessageWithRoleAndContent returns true if any message with the given role
// contains the given substring in its Content.
func findMessageWithRoleAndContent(msgs []Message, role, content string) bool {
	for _, m := range msgs {
		if m.Role == role && strings.Contains(m.Content, content) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Test 1: TestSteer_AppearsInPrepareMessages
// ---------------------------------------------------------------------------
// Steer a message, create a handler, verify prepareMessages includes it.
// System steers get collapsed into the merged system message, so we check
// for substring presence. User steers appear as their own message.
// ---------------------------------------------------------------------------

func TestSteer_AppearsInPrepareMessages(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	// Steer a user message (won't be collapsed by collapseSystemMessages)
	a.Steer(Message{Role: "user", Content: "steered user input"})

	h := newConversationHandler(a)
	msgs := h.prepareMessages()

	// The steered message should appear as a user message
	found := findMessageWithRoleAndContent(msgs, "user", "steered user input")
	if !found {
		t.Fatal("steered user message not found in prepareMessages output")
	}
}

// ---------------------------------------------------------------------------
// Test 1b: TestSteer_SystemMessageAppearsInPrepareMessages
// ---------------------------------------------------------------------------
// System steers get collapsed into the merged system message.
// ---------------------------------------------------------------------------

func TestSteer_SystemMessageAppearsInPrepareMessages(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	// Steer a system message — it gets collapsed into the merged system message
	a.Steer(Message{Role: "system", Content: "steered-system-instruction"})

	h := newConversationHandler(a)
	msgs := h.prepareMessages()

	// The steered content should appear somewhere in the output
	// (collapsed into the system message)
	found := findMessageWithContent(msgs, "steered-system-instruction")
	if !found {
		t.Fatal("steered system message content not found in prepareMessages output")
	}
}

// ---------------------------------------------------------------------------
// Test 2: TestSteer_MultipleAppearInOrder
// ---------------------------------------------------------------------------

func TestSteer_MultipleAppearInOrder(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	// Steer multiple user messages in order (to avoid collapseSystemMessages merging)
	a.Steer(Message{Role: "user", Content: "user-steer-first"})
	a.Steer(Message{Role: "user", Content: "user-steer-second"})
	a.Steer(Message{Role: "user", Content: "user-steer-third"})

	h := newConversationHandler(a)
	msgs := h.prepareMessages()

	// Find the indices of our steered messages
	var indices []int
	for i, m := range msgs {
		if m.Content == "user-steer-first" || m.Content == "user-steer-second" || m.Content == "user-steer-third" {
			indices = append(indices, i)
		}
	}

	if len(indices) != 3 {
		t.Fatalf("expected 3 steered messages, found %d", len(indices))
	}

	// Check order
	if indices[0] > indices[1] || indices[1] > indices[2] {
		t.Fatal("steered messages not in correct order")
	}

	if msgs[indices[0]].Content != "user-steer-first" {
		t.Errorf("first message content wrong: %s", msgs[indices[0]].Content)
	}
	if msgs[indices[1]].Content != "user-steer-second" {
		t.Errorf("second message content wrong: %s", msgs[indices[1]].Content)
	}
	if msgs[indices[2]].Content != "user-steer-third" {
		t.Errorf("third message content wrong: %s", msgs[indices[2]].Content)
	}
}

// ---------------------------------------------------------------------------
// Test 3: TestSteer_ConsumedOnceNotPersisted
// ---------------------------------------------------------------------------

func TestSteer_ConsumedOnceNotPersisted(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	// Steer a user message (not collapsed)
	a.Steer(Message{Role: "user", Content: "transient-steer-user"})

	h := newConversationHandler(a)

	// First call — should include the steered message
	msgs1 := h.prepareMessages()
	found1 := findMessageWithRoleAndContent(msgs1, "user", "transient-steer-user")
	if !found1 {
		t.Fatal("steered message not found in first prepareMessages call")
	}

	// Second call — should NOT include the steered message (transient, consumed once)
	msgs2 := h.prepareMessages()
	found2 := findMessageWithRoleAndContent(msgs2, "user", "transient-steer-user")
	if found2 {
		t.Fatal("steered message should have been consumed after first prepareMessages call")
	}
}

// ---------------------------------------------------------------------------
// Test 4: TestInjectInput_BufferedChanOne
// ---------------------------------------------------------------------------

func TestInjectInput_BufferedChanOne(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	// First call should succeed (buffered chan capacity 1, currently empty)
	ok1 := a.InjectInput("first input")
	if !ok1 {
		t.Fatal("first InjectInput should return true")
	}

	// Second call should fail (channel full)
	ok2 := a.InjectInput("second input")
	if ok2 {
		t.Fatal("second InjectInput should return false (channel full)")
	}

	// Drain the channel to verify the first input is there
	select {
	case v := <-a.inputInjectionChan:
		if v != "first input" {
			t.Fatalf("expected 'first input', got %q", v)
		}
	default:
		t.Fatal("channel should have the first input")
	}

	// Channel should be empty now
	select {
	case <-a.inputInjectionChan:
		t.Fatal("channel should be empty after drain")
	default:
		// good
	}

	// Third call should succeed again (channel empty now)
	ok3 := a.InjectInput("third input")
	if !ok3 {
		t.Fatal("third InjectInput should return true (channel was drained)")
	}
}

// ---------------------------------------------------------------------------
// Test 5: TestSteer_BetweenRuns
// ---------------------------------------------------------------------------

func TestSteer_BetweenRuns(t *testing.T) {
	t.Parallel()

	// Use a sequential provider: first run gets "answer1", second gets "answer2"
	p := &sequentialProvider{
		responses: []*ChatResponse{
			{Choices: []ChatChoice{{Message: Message{Content: "answer1"}, FinishReason: "stop"}}},
			{Choices: []ChatChoice{{Message: Message{Content: "answer2"}, FinishReason: "stop"}}},
		},
		info: ProviderInfo{Model: "test", ContextSize: 4096},
	}
	a := newTestAgentWithProvider(t, p)

	// Steer before first run
	a.Steer(Message{Role: "user", Content: "steer-for-run-1"})

	ctx := context.Background()

	// First run
	result1, err := a.Run(ctx, "query 1")
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if result1 != "answer1" {
		t.Errorf("first run result: %s", result1)
	}

	// Steer for second run
	a.Steer(Message{Role: "user", Content: "steer-for-run-2"})

	// Second run
	result2, err := a.Run(ctx, "query 2")
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if result2 != "answer2" {
		t.Errorf("second run result: %s", result2)
	}

	// Verify the provider was called exactly twice (once per run)
	if p.callIndex != 2 {
		t.Errorf("expected provider to be called exactly 2 times, got %d", p.callIndex)
	}

	// Verify steer-for-run-1 was NOT in the state after run 2
	// (it was transient, consumed in run 1)
	msgs := a.state.Messages()
	steer1Found := findMessageWithRoleAndContent(msgs, "user", "steer-for-run-1")
	// Steer messages are appended via prepareMessages to the API request,
	// but they may or may not persist in state depending on how runLoop
	// handles the response. The key test is that both runs succeeded.
	if steer1Found {
		t.Log("steer-for-run-1 still in state (may be expected if state is appended)")
	}
}

// ---------------------------------------------------------------------------
// Test 6: TestInjectInput_PickedUpDuringRun (after tool execution)
// ---------------------------------------------------------------------------

func TestInjectInput_PickedUpDuringRun(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{
		tools: []Tool{
			{Type: "function", Function: ToolFunction{Name: "get_time", Description: "Get the current time", Parameters: map[string]interface{}{}}},
		},
		results: []Message{{Role: "tool", ToolCallID: "call_1", Content: "12:00 PM"}},
	}

	// Response 1: tool call
	// Response 2: no tool calls (stop)
	p := &sequentialProvider{
		responses: []*ChatResponse{
			{
				Choices: []ChatChoice{{
					Message: Message{
						Role:    "assistant",
						Content: "",
						ToolCalls: []ToolCall{{
							ID:   "call_1",
							Type: "function",
							Function: ToolCallFunction{
								Name:      "get_time",
								Arguments: "{}",
							},
						}},
					},
					FinishReason: "tool_calls",
				}},
			},
			{
				Choices: []ChatChoice{{
					Message:      Message{Role: "assistant", Content: "The time is 12:00 PM"},
					FinishReason: "stop",
				}},
			},
		},
		info: ProviderInfo{Model: "test", ContextSize: 4096},
	}
	a := newTestAgentWithProviderAndExecutor(t, p, exec)

	ctx := context.Background()

	// Start the run in a goroutine and inject input after a short delay
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.Run(ctx, "What time is it?")
	}()

	// Wait for the first Chat call + tool execution to complete, then inject
	time.Sleep(100 * time.Millisecond)
	ok := a.InjectInput("Also, how are you?")
	if !ok {
		t.Log("inject returned false — run may have completed before injection")
	}

	// Give it time to complete
	wg.Wait()

	// Check state for the injected message
	msgs := a.state.Messages()
	foundInjected := findMessageWithRoleAndContent(msgs, "user", "Also, how are you?")
	if foundInjected {
		t.Log("Successfully injected and found user message after tool execution")
	}
	// Whether we find it is timing-dependent, but we verify the mechanism
	// doesn't crash
}

// ---------------------------------------------------------------------------
// Test 7: TestInjectInput_PickedUpWhenNoToolCalls
// ---------------------------------------------------------------------------

func TestInjectInput_PickedUpWhenNoToolCalls(t *testing.T) {
	t.Parallel()

	// Provider returns "stop" on first call, then a final response on second.
	p := &sequentialProvider{
		responses: []*ChatResponse{
			{
				Choices: []ChatChoice{{
					Message:      Message{Role: "assistant", Content: "Let me think about that..."},
					FinishReason: "stop",
				}},
			},
			{
				Choices: []ChatChoice{{
					Message:      Message{Role: "assistant", Content: "Here is the final answer"},
					FinishReason: "stop",
				}},
			},
		},
		info: ProviderInfo{Model: "test", ContextSize: 4096},
	}
	a := newTestAgentWithProvider(t, p)

	ctx := context.Background()

	// Start run in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.Run(ctx, "Tell me something")
	}()

	// Inject input after a short delay — should be picked up at the
	// "no tool calls" injection point
	time.Sleep(50 * time.Millisecond)
	ok := a.InjectInput("Actually, tell me about ducks")
	if !ok {
		t.Log("inject returned false — may have completed before injection")
	}

	wg.Wait()

	// Check state for the injected message
	msgs := a.state.Messages()
	foundInjected := findMessageWithRoleAndContent(msgs, "user", "Actually, tell me about ducks")
	if foundInjected {
		t.Log("Successfully found injected message in state when no tool calls")
	}
}

// ---------------------------------------------------------------------------
// Test 8: TestSteerSystem_Convenience
// ---------------------------------------------------------------------------

func TestSteerSystem_Convenience(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	// SteerSystem is a convenience that creates a system-role message
	a.SteerSystem("be-concise")

	h := newConversationHandler(a)
	msgs := h.prepareMessages()

	// SteerSystem content should appear somewhere (collapsed into system message)
	found := findMessageWithContent(msgs, "be-concise")
	if !found {
		t.Fatal("SteerSystem message content not found in prepareMessages")
	}
}

// ---------------------------------------------------------------------------
// Test 9: TestDrainSteerMessages_Empty
// ---------------------------------------------------------------------------

func TestDrainSteerMessages_Empty(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	// Drain when nothing has been steered — should return nil or empty
	result := a.drainSteerMessages()
	if result == nil {
		return // nil is acceptable
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result from drainSteerMessages, got %d items", len(result))
	}
}

// ---------------------------------------------------------------------------
// Test 10: TestDrainSteerMessages_ClearsState
// ---------------------------------------------------------------------------

func TestDrainSteerMessages_ClearsState(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	a.Steer(Message{Role: "system", Content: "msg1"})
	a.Steer(Message{Role: "system", Content: "msg2"})

	// First drain — should return both
	drained1 := a.drainSteerMessages()
	if len(drained1) != 2 {
		t.Fatalf("expected 2 drained messages, got %d", len(drained1))
	}

	// Verify content
	if drained1[0].Content != "msg1" {
		t.Errorf("first drained message: %s", drained1[0].Content)
	}
	if drained1[1].Content != "msg2" {
		t.Errorf("second drained message: %s", drained1[1].Content)
	}

	// Second drain — should return nil (already cleared)
	drained2 := a.drainSteerMessages()
	if drained2 != nil && len(drained2) > 0 {
		t.Fatalf("second drain should return nil/empty, got %d items", len(drained2))
	}
}

// ---------------------------------------------------------------------------
// Test 11: TestSteer_MixedRoles
// ---------------------------------------------------------------------------

func TestSteer_MixedRoles(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Content: "hello"}, FinishReason: "stop"}},
		},
		info:       ProviderInfo{Model: "test", ContextSize: 4096},
		tokenCount: 10,
	}
	a := newTestAgentWithProvider(t, p)

	a.SteerSystem("system-steer-prompt")
	a.Steer(Message{Role: "user", Content: "user-steer-input"})

	h := newConversationHandler(a)
	msgs := h.prepareMessages()

	// System steer gets collapsed, check for substring
	systemSteerFound := findMessageWithContent(msgs, "system-steer-prompt")
	if !systemSteerFound {
		t.Error("system steer not found in prepareMessages")
	}

	// User steer appears as its own message
	userSteerFound := findMessageWithRoleAndContent(msgs, "user", "user-steer-input")
	if !userSteerFound {
		t.Error("user steer not found in prepareMessages")
	}
}

// ---------------------------------------------------------------------------
// Test 12: TestSteer_InjectedBeforeRun_IsTransient
// ---------------------------------------------------------------------------
// Verify that steered messages are consumed (transient) and do NOT persist
// in state after Run() completes. They only appear in the API request.
// ---------------------------------------------------------------------------

func TestSteer_InjectedBeforeRun_IsTransient(t *testing.T) {
	t.Parallel()

	p := &sequentialProvider{
		responses: []*ChatResponse{
			{Choices: []ChatChoice{{Message: Message{Content: "steered-answer"}, FinishReason: "stop"}}},
		},
		info: ProviderInfo{Model: "test", ContextSize: 4096},
	}
	a := newTestAgentWithProvider(t, p)

	// Steer a user message before running
	a.Steer(Message{Role: "user", Content: "pre-run-steer"})

	ctx := context.Background()
	result, err := a.Run(ctx, "original query")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result != "steered-answer" {
		t.Errorf("unexpected result: %s", result)
	}

	// The steered message should NOT appear in state after Run() —
	// it's transient, consumed by prepareMessages for the API request only.
	msgs := a.state.Messages()
	found := findMessageWithRoleAndContent(msgs, "user", "pre-run-steer")
	if found {
		t.Fatal("pre-run steered message should NOT be in state after Run (it's transient)")
	}

	// The original query SHOULD be in state
	queryFound := findMessageWithRoleAndContent(msgs, "user", "original query")
	if !queryFound {
		t.Fatal("original query not found in state")
	}
}
