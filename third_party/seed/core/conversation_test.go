package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sprout-foundry/seed/events"
)

// --- Conversation tests ---

func TestPrepareMessages_SystemPromptPrepended(t *testing.T) {
	provider := &mockProvider{
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	a, err := NewAgent(Options{
		Provider:     provider,
		Executor:     &mockExecutor{},
		SystemPrompt: "You are helpful.",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add messages to state
	a.State().AddMessage(Message{Role: "user", Content: "hi"})
	a.State().AddMessage(Message{Role: "assistant", Content: "hello"})

	ch := newConversationHandler(a)
	messages := ch.prepareMessages()

	// First message should be system prompt
	if messages[0].Role != "system" || messages[0].Content != "You are helpful." {
		t.Errorf("expected system prompt, got %v", messages[0])
	}
	// Should have 3 messages: system, user, assistant
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}
}

func TestPrepareMessages_SystemMessagesStrippedFromHistory(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{
			info:       ProviderInfo{ContextSize: 10000},
			tokenCount: 100,
		},
		Executor:     &mockExecutor{},
		SystemPrompt: "Current system.",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add old system message to history (simulating imported state)
	a.State().SetMessages([]Message{
		{Role: "system", Content: "Old system prompt"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})

	ch := newConversationHandler(a)
	messages := ch.prepareMessages()

	// Should have: current system + user + assistant (old system stripped)
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}
	if messages[0].Content != "Current system." {
		t.Errorf("expected current system prompt, got %q", messages[0].Content)
	}
}

func TestPrepareMessages_ImagesStrippedForNonVision(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{
			info:       ProviderInfo{ContextSize: 10000, HasVision: false},
			tokenCount: 100,
		},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.State().AddMessage(Message{
		Role:    "user",
		Content: "Look at this",
		Images:  []ImageData{{URL: "http://example.com/img.png", Type: "image/png"}},
	})

	ch := newConversationHandler(a)
	messages := ch.prepareMessages()

	// Find the user message
	for _, msg := range messages {
		if msg.Role == "user" {
			if len(msg.Images) != 0 {
				t.Error("expected images to be stripped for non-vision model")
			}
			return
		}
	}
	t.Error("user message not found")
}

func TestPrepareMessages_ImagesKeptForVision(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{
			info:       ProviderInfo{ContextSize: 10000, HasVision: true},
			tokenCount: 100,
		},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	a.State().AddMessage(Message{
		Role:    "user",
		Content: "Look at this",
		Images:  []ImageData{{URL: "http://example.com/img.png", Type: "image/png"}},
	})

	ch := newConversationHandler(a)
	messages := ch.prepareMessages()

	// Find the user message
	for _, msg := range messages {
		if msg.Role == "user" {
			if len(msg.Images) != 1 {
				t.Error("expected images to be kept for vision model")
			}
			return
		}
	}
	t.Error("user message not found")
}

func TestPrepareMessages_CollapseSystemMessages(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "First system"},
		{Role: "user", Content: "hi"},
		{Role: "system", Content: "Second system"},
		{Role: "assistant", Content: "hello"},
	}

	result := collapseSystemMessages(messages)

	// Should have: merged system + user + assistant
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Error("first message should be system")
	}
	if result[0].Content != "First system\n\nSecond system" {
		t.Errorf("expected merged system content, got %q", result[0].Content)
	}
}

func TestRemoveOrphanedToolResults(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	ch := newConversationHandler(a)
	messages := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "let me search", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{Name: "search"}},
		}},
		{Role: "tool", Content: "results", ToolCallID: "call_1"},
		{Role: "tool", Content: "orphaned", ToolCallID: "call_999"}, // orphaned
	}

	result := ch.removeOrphanedToolResults(messages)

	// Should have 4 messages (orphaned removed)
	if len(result) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result))
	}
	// Verify orphaned is gone
	for _, msg := range result {
		if msg.ToolCallID == "call_999" {
			t.Error("orphaned tool result should be removed")
		}
	}
}

func TestProcessQuery_EmptyChoices(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{},
			Usage:   ChatUsage{TotalTokens: 5},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for zero choices, got nil")
	}
	if !errors.Is(err, ErrZeroChoices) {
		t.Fatalf("expected ErrZeroChoices, got %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for empty choices, got %q", result)
	}
}

func TestProcessQuery_ContextCompaction(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}}},
			Usage:   ChatUsage{TotalTokens: 10},
		},
		info:       ProviderInfo{ContextSize: 50}, // Very small context
		tokenCount: 100,                           // Over context limit
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add many messages to trigger compaction
	for i := 0; i < 30; i++ {
		a.State().AddMessage(Message{Role: "user", Content: "user message " + string(rune('a'+i%26))})
		a.State().AddMessage(Message{Role: "assistant", Content: "assistant response " + string(rune('a'+i%26))})
	}

	_, err = a.Run(context.Background(), "final query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessQuery_ProviderErrorPublishesErrorEvent(t *testing.T) {
	provider := &mockProvider{
		chatErr: fmt.Errorf("provider down"),
		info:    ProviderInfo{ContextSize: 10000},
	}
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	a, err := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from provider failure")
	}

	// With retry logic: 3 retry error events (from chatFn) + 1 final error event (from runLoop) = 4 error events
	// First event is query_started
	evt1 := <-ch
	if evt1.Type != events.EventTypeQueryStarted {
		t.Fatalf("expected first event %q, got %q", events.EventTypeQueryStarted, evt1.Type)
	}

	// Collect all error events
	var errorEvents []events.UIEvent
	for {
		select {
		case evt := <-ch:
			if evt.Type == events.EventTypeError {
				errorEvents = append(errorEvents, evt)
			}
		default:
			goto done
		}
	}
done:

	if len(errorEvents) == 0 {
		t.Fatal("expected at least 1 error event, got 0")
	}

	// The first error event is from the first retry attempt (chatFn)
	// It should contain the original error message
	data, ok := errorEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", errorEvents[0].Data)
	}
	if data["message"] != "chat failed" {
		t.Errorf("expected message 'chat failed', got %v", data["message"])
	}
	errStr, ok := data["error"].(string)
	if !ok {
		t.Fatalf("expected error string, got %T", data["error"])
	}
	if !strings.Contains(errStr, "provider down") {
		t.Errorf("expected error to contain 'provider down', got %v", errStr)
	}
}

func TestProcessQuery_ProviderErrorNoEventBus(t *testing.T) {
	provider := &mockProvider{
		chatErr: fmt.Errorf("provider down"),
		info:    ProviderInfo{ContextSize: 10000},
	}

	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
		// No EventBus — should not panic
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from provider failure")
	}
}

func TestProcessQuery_MetricsUpdateEvent(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}}},
			Usage:   ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	a, err := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Event 1: query_started
	evt1 := <-ch
	if evt1.Type != events.EventTypeQueryStarted {
		t.Fatalf("expected first event %q, got %q", events.EventTypeQueryStarted, evt1.Type)
	}

	// Event 2: metrics_update
	evt := <-ch
	if evt.Type != events.EventTypeMetricsUpdate {
		t.Fatalf("expected event type %q, got %q", events.EventTypeMetricsUpdate, evt.Type)
	}
	data, ok := evt.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", evt.Data)
	}
	if data["total_tokens"] != 15 {
		t.Errorf("expected total_tokens=15, got %v", data["total_tokens"])
	}
	if data["context_tokens"] != 10 {
		t.Errorf("expected context_tokens=10, got %v", data["context_tokens"])
	}
	if data["max_context_tokens"] != 10000 {
		t.Errorf("expected max_context_tokens=10000, got %v", data["max_context_tokens"])
	}
	if data["iteration"] != 0 {
		t.Errorf("expected iteration=0, got %v", data["iteration"])
	}
	if data["total_cost"] != 0.0 {
		t.Errorf("expected total_cost=0.0, got %v", data["total_cost"])
	}

	// Event 3: query_completed
	evt3 := <-ch
	if evt3.Type != events.EventTypeQueryCompleted {
		t.Errorf("expected event type %q, got %q", events.EventTypeQueryCompleted, evt3.Type)
	}

	// Event 4: agent_message
	evt4 := <-ch
	if evt4.Type != events.EventTypeAgentMessage {
		t.Errorf("expected event type %q, got %q", events.EventTypeAgentMessage, evt4.Type)
	}
	data4, ok := evt4.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", evt4.Data)
	}
	if data4["category"] != "info" {
		t.Errorf("expected category 'info', got %v", data4["category"])
	}
	if data4["message"] != "done" {
		t.Errorf("expected message 'done', got %v", data4["message"])
	}

	// No more events
	select {
	case extra := <-ch:
		t.Errorf("unexpected extra event: %v", extra.Type)
	default:
	}
}

func TestProcessQuery_MetricsUpdateNotPublishedWhenNoTokens(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}}},
			Usage:   ChatUsage{TotalTokens: 0},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	a, err := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Event 1: query_started
	evt1 := <-ch
	if evt1.Type != events.EventTypeQueryStarted {
		t.Fatalf("expected first event %q, got %q", events.EventTypeQueryStarted, evt1.Type)
	}

	// Event 2: query_completed (no metrics_update since TotalTokens == 0)
	evt2 := <-ch
	if evt2.Type != events.EventTypeQueryCompleted {
		t.Errorf("expected event type %q, got %q", events.EventTypeQueryCompleted, evt2.Type)
	}

	// Event 3: agent_message (should follow query_completed)
	evt3 := <-ch
	if evt3.Type != events.EventTypeAgentMessage {
		t.Errorf("expected event type %q, got %q", events.EventTypeAgentMessage, evt3.Type)
	}
	data, ok := evt3.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", evt3.Data)
	}
	if data["category"] != "info" {
		t.Errorf("expected category 'info', got %v", data["category"])
	}
	if data["message"] != "done" {
		t.Errorf("expected message 'done', got %v", data["message"])
	}

	// No more events
	select {
	case extra := <-ch:
		t.Errorf("unexpected extra event: %v", extra.Type)
	default:
	}
}

func TestProcessQuery_AgentMessageEvent(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "final answer"}}},
			Usage:   ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	a, err := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect all events to verify ordering
	var evts []events.UIEvent
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				// Channel closed (unsubscribe was called)
				goto done
			}
			evts = append(evts, evt)
		default:
			goto done
		}
	}
done:

	// Verify EventTypeQueryCompleted comes before EventTypeAgentMessage
	queryCompletedIdx := -1
	agentMessageIdx := -1
	for i, evt := range evts {
		if evt.Type == events.EventTypeQueryCompleted {
			queryCompletedIdx = i
		}
		if evt.Type == events.EventTypeAgentMessage {
			agentMessageIdx = i
		}
	}
	if queryCompletedIdx < 0 {
		t.Error("expected EventTypeQueryCompleted in events")
	}
	if agentMessageIdx < 0 {
		t.Error("expected EventTypeAgentMessage in events")
	}
	if agentMessageIdx < queryCompletedIdx {
		t.Errorf("EventTypeAgentMessage should come after EventTypeQueryCompleted (indices: agent_message=%d, query_completed=%d)", agentMessageIdx, queryCompletedIdx)
	}

	// Verify EventTypeAgentMessage data
	if agentMessageIdx >= 0 {
		evt := evts[agentMessageIdx]
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map data for agent_message, got %T", evt.Data)
		}
		if data["category"] != "info" {
			t.Errorf("expected category 'info', got %v", data["category"])
		}
		if data["message"] != "final answer" {
			t.Errorf("expected message 'final answer', got %v", data["message"])
		}
	}
}

func TestProcessQuery_AgentMessageNotPublishedForEmptyContent(t *testing.T) {
	provider := &mockProvider{
		chatResp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: ""}}},
			Usage:   ChatUsage{TotalTokens: 0},
		},
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	a, err := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Event 1: query_started
	evt1 := <-ch
	if evt1.Type != events.EventTypeQueryStarted {
		t.Fatalf("expected first event %q, got %q", events.EventTypeQueryStarted, evt1.Type)
	}

	// Event 2: query_completed
	evt2 := <-ch
	if evt2.Type != events.EventTypeQueryCompleted {
		t.Errorf("expected event type %q, got %q", events.EventTypeQueryCompleted, evt2.Type)
	}

	// No agent_message should be published for empty content
	select {
	case extra := <-ch:
		t.Errorf("unexpected extra event: %v (expected no agent_message for empty content)", extra.Type)
	default:
	}
}

// --- isBlankIteration tests ---

func TestConversationHandler_isBlankIteration(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "empty string",
			content: "",
			want:    true,
		},
		{
			name:    "spaces only",
			content: "   ",
			want:    true,
		},
		{
			name:    "newlines and tabs",
			content: "\n\t  \n",
			want:    true,
		},
		{
			name:    "non-empty content",
			content: "hello",
			want:    false,
		},
		{
			name:    "content with leading and trailing whitespace",
			content: "  hello  ",
			want:    false,
		},
		{
			name:    "unicode whitespace only",
			content: "\u00A0\u3000\u2003",
			want:    true,
		},
	}

	agent, err := NewAgent(Options{
		Provider: &mockProvider{
			info:       ProviderInfo{ContextSize: 10000},
			tokenCount: 100,
		},
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := newConversationHandler(agent)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ch.isBlankIteration(tt.content)
			if got != tt.want {
				t.Errorf("isBlankIteration(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

// --- normalizeForComparison tests ---

func TestNormalizeForComparison_Lowercases(t *testing.T) {
	input := "HELLO WORLD"
	got := normalizeForComparison(input)
	want := "hello world"
	if got != want {
		t.Errorf("normalizeForComparison(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeForComparison_TrimWhitespace(t *testing.T) {
	input := "  hello world  "
	got := normalizeForComparison(input)
	want := "hello world"
	if got != want {
		t.Errorf("normalizeForComparison(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeForComparison_StripsTrailingPunctuation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"trailing period", "hello.", "hello"},
		{"trailing exclamation", "hello!", "hello"},
		{"trailing comma", "hello,", "hello"},
		{"trailing semicolon", "hello;", "hello"},
		{"trailing colon", "hello:", "hello"},
		{"trailing question mark", "hello?", "hello"},
		{"multiple trailing punctuations", "hello...!!??", "hello"},
		{"trailing dash should not strip", "hello--", "hello--"},
		{"dash inside should not strip", "well-known", "well-known"},
		{"apostrophe inside should not strip", "it's", "it's"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeForComparison(tt.input)
			if got != tt.want {
				t.Errorf("normalizeForComparison(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeForComparison_PreservesInternalPunctuation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"internal period", "e.g. hello", "e.g. hello"},
		{"URL", "http://example.com", "http://example.com"},
		{"math expression", "x > 5", "x > 5"},
		{"quoted text", "say \"hello\"", "say \"hello\""},
		{"parentheses", "(see note)", "(see note)"},
		{"hyphenated word", "state-of-the-art", "state-of-the-art"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeForComparison(tt.input)
			if got != tt.want {
				t.Errorf("normalizeForComparison(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeForComparison_EmptyInput(t *testing.T) {
	got := normalizeForComparison("")
	if got != "" {
		t.Errorf("normalizeForComparison(\"\") = %q, want empty string", got)
	}
}

// --- contentSimilar tests ---

func TestContentSimilar_ExactMatchAfterNormalization(t *testing.T) {
	// Same text, different case — should be similar after normalization
	a := "Hello World"
	b := "hello world"
	if !contentSimilar(a, b) {
		t.Errorf("contentSimilar(%q, %q) = false, want true", a, b)
	}
}

func TestContentSimilar_PunctuationDifferences(t *testing.T) {
	// Trailing punctuation differences should be handled
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"extra trailing period", "hello world.", "hello world", true},
		{"extra exclamation", "hello world!", "hello world", true},
		{"extra question mark", "hello world?", "hello world", true},
		{"extra comma", "hello world,", "hello world", true},
		{"multiple trailing punct", "hello world.!", "hello world", true},
		{"different trailing punct", "hello world.", "hello world?", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentSimilar(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("contentSimilar(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestContentSimilar_HighOverlapAboveThreshold(t *testing.T) {
	// Long messages with high word overlap (>80% and >=10 words in shorter)
	// 18 of 21 words match → ~85.7% overlap (>=10 overlap AND ratio > 0.8)
	a := "The quick brown fox jumps over the lazy dog today and tomorrow"
	b := "The quick brown fox jumps over the lazy dog yesterday and nextweek"
	if !contentSimilar(a, b) {
		t.Errorf("contentSimilar(high overlap) = false, want true")
	}
}

func TestContentSimilar_LowOverlapBelowThreshold(t *testing.T) {
	// Long messages with low word overlap
	a := "The cat sat on the mat and ate a fish"
	b := "The dog ran in the park and chased a ball"
	if contentSimilar(a, b) {
		t.Errorf("contentSimilar(low overlap) = true, want false")
	}
}

func TestContentSimilar_ShortMessagesNoFalsePositive(t *testing.T) {
	// Short messages with HIGH overlap: exact match is true (exact-match path,
	// bypasses word-overlap guard). NEAR-matches with <10 overlap words are
	// correctly NOT flagged via the word-overlap heuristic.
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		// Exact matches always return true (bypass word-overlap guard)
		{"exact single word", "yes", "yes", true},
		{"exact few words", "hello world", "hello world", true},
		// Near-matches: word-overlap heuristic used; overlap <10 → false
		{"two words different", "yes no", "yes maybe", false},
		{"five words different", "the cat sat on the mat", "the cat sat on the rug", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentSimilar(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("contentSimilar(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestContentSimilar_EmptyInput(t *testing.T) {
	if contentSimilar("", "anything") {
		t.Error("contentSimilar(\"\", \"anything\") = true, want false")
	}
	if contentSimilar("anything", "") {
		t.Error("contentSimilar(\"anything\", \"\") = true, want false")
	}
	if contentSimilar("", "") {
		t.Error("contentSimilar(\"\", \"\") = true, want false")
	}
}

func TestContentSimilar_NoOverlap(t *testing.T) {
	// Completely different content should return false
	a := "The sky is blue and the ocean is vast"
	b := "Programming requires patience and practice"
	if contentSimilar(a, b) {
		t.Errorf("contentSimilar(completely different) = true, want false")
	}
}

func TestContentSimilar_SubstantialExpansionOfSameContent(t *testing.T) {
	// One message restates context but the longer message adds significantly
	// new content such that the overlap ratio drops below the 80% threshold.
	// The shorter text has enough words (12) to pass repetitionMinOverlapCount,
	// but only 7 of 12 words appear in the longer text (58.3% < 80%).
	a := "The answer to the question involves multiple factors that must be considered."
	b := "The answer to the question involves multiple factors. These include timing complexity and resource availability which were not previously addressed in this discussion."

	if contentSimilar(a, b) {
		t.Errorf("contentSimilar(expanded content) = true, want false (overlap ~58%% below 80%% threshold)")
	}
}

func TestContentSimilar_CompletelyOverlapping(t *testing.T) {
	// Identical long messages should be similar
	a := "This is a comprehensive answer that covers all the important details that were requested in the original query."
	b := "This is a comprehensive answer that covers all the important details that were requested in the original query."
	if !contentSimilar(a, b) {
		t.Errorf("contentSimilar(identical long) = false, want true")
	}
}

func TestContentSimilar_OneWordDiffersInLongMessage(t *testing.T) {
	// Two long messages where only one word differs — overlap should be very high
	a := "The configuration file contains database cache and logging settings for the application."
	b := "The configuration file contains database memory and logging settings for the application."

	// a: 12 words, b: 12 words. 11 of 12 match → 91.7% overlap ≥ 80%
	if !contentSimilar(a, b) {
		t.Errorf("contentSimilar(one word diff) = false, want true (high overlap)")
	}
}

func TestContentSimilar_BarelyBelowThreshold(t *testing.T) {
	// Messages that barely miss the overlap threshold — should NOT be similar
	// Build messages with exactly 10 words in the shorter, and 8/10 = 80% overlap.
	// The code uses `>` (strictly greater than) for the ratio check, so 80% exactly
	// is NOT enough — needs to be > 0.8.
	shorter := "alpha bravo charlie delta echo foxtrot golf hotel india juliet" // 10 words
	// Change 3 of 10 words → 7/10 = 70% overlap → below threshold
	longer := "alpha bravo zulu delta echo foxtrot golf hotel india juliet kilo" // 11 words

	if contentSimilar(shorter, longer) {
		t.Errorf("contentSimilar(70%% overlap) = true, want false (below 80%% threshold)")
	}
}

func TestContentSimilar_ShorterIsSecond(t *testing.T) {
	// Verify that contentSimilar handles the case where the second argument is shorter
	// The algorithm should swap so that wordsA is always the shorter set.
	a := "This is a longer message with many more words that should not be flagged as repetitive"
	b := "This is a longer message with many more words that should not be flagged as repetitive."
	if !contentSimilar(a, b) {
		t.Errorf("contentSimilar(longer first) = false, want true")
	}
}

// --- isRepetitiveContent tests ---

// buildHandlerForRepetition creates a ConversationHandler with the given
// message history so that isRepetitiveContent can be tested directly.
// The provider is mocked to return an empty response; the executor is noop.
// The handler's agent points at the provided state.
func buildHandlerForRepetition(t *testing.T, messages []Message) *ConversationHandler {
	provider := &mockProvider{
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
	}
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.state.SetMessages(messages)
	ch := newConversationHandler(a)
	return ch
}

func TestIsRepetitiveContent_NoPreviousAssistantMessage(t *testing.T) {
	// Empty state — no previous assistant message exists
	ch := buildHandlerForRepetition(t, []Message{})
	if ch.isRepetitiveContent("anything") {
		t.Error("isRepetitiveContent(empty state) = true, want false (no previous assistant message)")
	}
}

func TestIsRepetitiveContent_NoAssistantMessages(t *testing.T) {
	// State with only user/tool messages, no assistant message
	msgs := []Message{
		{Role: "system", Content: "You are a test assistant."},
		{Role: "user", Content: "hello"},
	}
	ch := buildHandlerForRepetition(t, msgs)
	if ch.isRepetitiveContent("anything") {
		t.Error("isRepetitiveContent(no assistant msg) = true, want false")
	}
}

func TestIsRepetitiveContent_VeryDifferentContent(t *testing.T) {
	// Previous assistant message is very different from current content
	prevAssistant := "The answer is forty two and it requires careful consideration."
	currentContent := "I cannot help you with that request at this time."

	msgs := []Message{
		{Role: "system", Content: "You are a test assistant."},
		{Role: "user", Content: "What is the answer?"},
		{Role: "assistant", Content: prevAssistant},
		{Role: "assistant", Content: currentContent}, // current (already in state)
	}
	ch := buildHandlerForRepetition(t, msgs)
	if ch.isRepetitiveContent(currentContent) {
		t.Error("isRepetitiveContent(different content) = true, want false")
	}
}

func TestIsRepetitiveContent_ExactMatchWithPrevious(t *testing.T) {
	// Current content exactly matches previous assistant message
	repeatedContent := "This is the complete answer to your question with all the details."

	msgs := []Message{
		{Role: "system", Content: "You are a test assistant."},
		{Role: "user", Content: "Tell me more."},
		{Role: "assistant", Content: repeatedContent}, // previous
		{Role: "assistant", Content: repeatedContent}, // current (already in state)
	}
	ch := buildHandlerForRepetition(t, msgs)
	if !ch.isRepetitiveContent(repeatedContent) {
		t.Error("isRepetitiveContent(identical) = false, want true")
	}
}

func TestIsRepetitiveContent_MinorPunctuationDifference(t *testing.T) {
	// Current content matches previous assistant message except for trailing punctuation.
	// normalizeForComparison strips trailing punctuation, so the word-overlap
	// heuristic should detect the repetition.
	previous := "The file contains the expected configuration data that was requested in the query."
	current := "The file contains the expected configuration data that was requested in the query" // no trailing period

	msgs := []Message{
		{Role: "system", Content: "You are a test assistant."},
		{Role: "user", Content: "What's in the file?"},
		{Role: "assistant", Content: previous},
		{Role: "assistant", Content: current},
	}
	ch := buildHandlerForRepetition(t, msgs)
	if !ch.isRepetitiveContent(current) {
		t.Error("isRepetitiveContent(minor punctuation diff) = false, want true")
	}
}

func TestIsRepetitiveContent_WithToolResultInBetween(t *testing.T) {
	// The previous assistant message before the current one should skip over
	// the current assistant (already in state) and the tool result.
	previous := "I need to check the configuration file for the database settings."
	current := "I need to check the configuration file for the database settings" // no trailing period

	msgs := []Message{
		{Role: "system", Content: "You are a test assistant."},
		{Role: "user", Content: "Check config."},
		{Role: "assistant", Content: previous},
		{Role: "tool", Content: "config data", ToolCallID: "call_1"},
		{Role: "assistant", Content: current}, // current (already in state)
	}
	ch := buildHandlerForRepetition(t, msgs)
	if !ch.isRepetitiveContent(current) {
		t.Error("isRepetitiveContent(with tool result) = false, want true")
	}
}

func TestIsRepetitiveContent_UserMessageInBetween(t *testing.T) {
	// Previous assistant message separated by user/tool messages.
	// The function walks back past the current assistant, finds the previous one.
	previous := "Let me search the codebase for the relevant function definitions."
	current := "Let me search the codebase for the relevant function definitions" // no trailing period

	msgs := []Message{
		{Role: "system", Content: "You are a test assistant."},
		{Role: "user", Content: "What does this do?"},
		{Role: "assistant", Content: previous},
		{Role: "user", Content: "Also check the tests."},
		{Role: "assistant", Content: current}, // current
	}
	ch := buildHandlerForRepetition(t, msgs)
	if !ch.isRepetitiveContent(current) {
		t.Error("isRepetitiveContent(user between) = false, want true")
	}
}

func TestIsRepetitiveContent_DifferentPreviousFound(t *testing.T) {
	// There are multiple assistant messages in history. The function should
	// compare against the immediately preceding one (walking back from end).
	firstAssistant := "First answer with different content and words."
	secondAssistant := "Second answer completely different topic here."
	repeated := "Second answer completely different topic here."

	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: firstAssistant}, // index 1
		{Role: "user", Content: "more"},
		{Role: "assistant", Content: secondAssistant}, // index 3 — previous to current
		{Role: "assistant", Content: repeated},        // index 4 — current
	}
	ch := buildHandlerForRepetition(t, msgs)
	if !ch.isRepetitiveContent(repeated) {
		t.Error("isRepetitiveContent(multiple assistant msgs, matches previous) = false, want true")
	}
}

func TestIsRepetitiveContent_RepeatedButTooShort(t *testing.T) {
	// Near-identical short content (<10 overlap words) should NOT be flagged because
	// the word-overlap guard requires >= repetitionMinOverlapCount words. Exact matches of
	// short text ARE flagged (bypass word-overlap), so we test a near-match
	// where the overlap heuristic is used and correctly returns false.
	shortRepeated := "yes that is correct" // 4 words

	msgs := []Message{
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: shortRepeated},
		{Role: "assistant", Content: "yes that is correct with adjustment"},
	}
	ch := buildHandlerForRepetition(t, msgs)
	if ch.isRepetitiveContent(msgs[2].Content) {
		t.Error("isRepetitiveContent(short near-repeat) = true, want false (below min word threshold)")
	}
}

func TestIsRepetitiveContent_MultipleToolMessagesBetween(t *testing.T) {
	// Previous assistant message separated by multiple tool messages.
	previous := "I will read the file and analyze its contents carefully."
	current := "I will read the file and analyze its contents carefully" // no trailing period

	msgs := []Message{
		{Role: "user", Content: "analyze this."},
		{Role: "assistant", Content: previous},
		{Role: "tool", Content: "file contents 1", ToolCallID: "call_1"},
		{Role: "tool", Content: "file contents 2", ToolCallID: "call_2"},
		{Role: "assistant", Content: current}, // current
	}
	ch := buildHandlerForRepetition(t, msgs)
	if !ch.isRepetitiveContent(current) {
		t.Error("isRepetitiveContent(multiple tools between) = false, want true")
	}
}

// --- sanitizeANSI tests ---

func TestSanitizeANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no ANSI codes",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "simple color codes",
			input: "\x1b[31mred\x1b[0m",
			want:  "red",
		},
		{
			name:  "multiple codes stripped",
			input: "\x1b[31mred\x1b[0m and \x1b[32mgreen\x1b[0m",
			want:  "red and green",
		},
		{
			name:  "OSC sequence stripped",
			input: "\x1b]0;title\x07content",
			want:  "content",
		},
		{
			name:  "complex sequence",
			input: "\x1b[1;32mbold green\x1b[0m",
			want:  "bold green",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only ANSI codes",
			input: "\x1b[31m\x1b[0m\x1b[1m",
			want:  "",
		},
		{
			name:  "CSI cursor movement",
			input: "\x1b[2K\x1b[1;1Hcontent\x1b[0K",
			want:  "content",
		},
		{
			name:  "single char control",
			input: "hello\x1b(B world",
			want:  "hello world",
		},
		{
			name:  "mixed content and ANSI",
			input: "\x1b[33mWarning:\x1b[0m file not found",
			want:  "Warning: file not found",
		},
		{
			name:  "ANSI codes only - whitespace",
			input: "\x1b[2K\n\x1b[2K",
			want:  "\n",
		},
		{
			name:  "real world shell output",
			input: "\x1b[1;34m$ \x1b[0m\x1b[1mgit\x1b[0m \x1b[33mstatus\x1b[0m\n\x1b[32mon\x1b[0m \x1b[33mmain\x1b[0m \x1b[36m~1\x1b[0m\x1b[31m!\x1b[0m",
			want:  "$ git status\non main ~1!",
		},
		{
			name:  "DEC private mode sequence",
			input: "\x1b[?25lhidden\x1b[?25h",
			want:  "hidden",
		},
		{
			name:  "ST-terminated OSC sequence",
			input: "\x1b]0;title\x1b\\content",
			want:  "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeANSI(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
