package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// api_client.go — RateLimitExceededError
// ---------------------------------------------------------------------------

func TestRateLimitExceededError_ZC(t *testing.T) {
	t.Parallel()
	t.Run("without_last_error", func(t *testing.T) {
		e := &RateLimitExceededError{Attempts: 3}
		got := e.Error()
		want := "rate limit exceeded after 3 attempt(s)"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
	t.Run("with_last_error", func(t *testing.T) {
		inner := errors.New("too many requests")
		e := &RateLimitExceededError{Attempts: 5, LastError: inner}
		got := e.Error()
		want := "rate limit exceeded after 5 attempt(s): too many requests"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
	t.Run("unwrap", func(t *testing.T) {
		inner := errors.New("inner")
		e := &RateLimitExceededError{LastError: inner}
		if !errors.Is(e.Unwrap(), inner) {
			t.Error("Unwrap() should return LastError")
		}
	})
	t.Run("unwrap_nil", func(t *testing.T) {
		e := &RateLimitExceededError{}
		if e.Unwrap() != nil {
			t.Error("Unwrap() should return nil when LastError is nil")
		}
	})
}

// ---------------------------------------------------------------------------
// api_client.go — stripImagesFromMessages
// ---------------------------------------------------------------------------

func TestStripImagesFromMessages_ZC(t *testing.T) {
	t.Parallel()
	t.Run("no_images", func(t *testing.T) {
		msgs := []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		}
		out, hasImages := stripImagesFromMessages(msgs)
		if hasImages {
			t.Error("should report no images")
		}
		if len(out) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(out))
		}
	})
	t.Run("with_images", func(t *testing.T) {
		t.Parallel()
		msgs := []api.Message{
			{Role: "user", Content: "see this", Images: []api.ImageData{{Base64: "abc"}}},
			{Role: "assistant", Content: "ok"},
		}
		out, hasImages := stripImagesFromMessages(msgs)
		if !hasImages {
			t.Error("should report images present")
		}
		if len(out[0].Images) != 0 {
			t.Error("images should be stripped from first message")
		}
		// Original should not be mutated
		if len(msgs[0].Images) == 0 {
			t.Error("original should still have images (copy was made)")
		}
	})
	t.Run("empty", func(t *testing.T) {
		out, hasImages := stripImagesFromMessages(nil)
		if hasImages {
			t.Error("should report no images for nil")
		}
		if len(out) != 0 {
			t.Error("should return empty for nil input")
		}
	})
}

// ---------------------------------------------------------------------------
// api_client.go — stripLeadingAssistantPrefillFromMessages
// ---------------------------------------------------------------------------

func TestStripLeadingAssistantPrefill_ZC(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		out := stripLeadingAssistantPrefillFromMessages(nil)
		if len(out) != 0 {
			t.Error("empty in → empty out")
		}
	})
	t.Run("no_assistant_messages", func(t *testing.T) {
		msgs := []api.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hi"},
		}
		out := stripLeadingAssistantPrefillFromMessages(msgs)
		if len(out) != 2 {
			t.Fatalf("expected 2, got %d", len(out))
		}
	})
	t.Run("assistant_prefill_stripped", func(t *testing.T) {
		msgs := []api.Message{
			{Role: "system", Content: "sys"},
			{Role: "assistant", Content: "prefill"},
			{Role: "user", Content: "hello"},
		}
		out := stripLeadingAssistantPrefillFromMessages(msgs)
		if len(out) != 2 {
			t.Fatalf("expected 2 (system+user), got %d", len(out))
		}
		if out[1].Content != "hello" {
			t.Errorf("expected 'hello', got %q", out[1].Content)
		}
	})
	t.Run("assistant_with_tool_calls_preserved", func(t *testing.T) {
		msgs := []api.Message{
			{Role: "system", Content: "sys"},
			{Role: "assistant", Content: "prefill", ToolCalls: []api.ToolCall{{ID: "1"}}},
			{Role: "user", Content: "hello"},
		}
		out := stripLeadingAssistantPrefillFromMessages(msgs)
		if len(out) != 3 {
			t.Fatalf("assistant with tool calls should be preserved, got %d", len(out))
		}
	})
	t.Run("only_system_messages", func(t *testing.T) {
		msgs := []api.Message{
			{Role: "system", Content: "sys1"},
			{Role: "system", Content: "sys2"},
		}
		out := stripLeadingAssistantPrefillFromMessages(msgs)
		if len(out) != 2 {
			t.Fatalf("all system messages should be preserved, got %d", len(out))
		}
	})
}

// ---------------------------------------------------------------------------
// api_client.go — estimateCompletionTokensFromResponse
// ---------------------------------------------------------------------------

func TestEstimateCompletionTokensFromResponse_ZC(t *testing.T) {
	t.Parallel()
	t.Run("nil_response", func(t *testing.T) {
		if got := estimateCompletionTokensFromResponse(nil); got != 0 {
			t.Errorf("nil response should be 0, got %d", got)
		}
	})
	t.Run("no_choices", func(t *testing.T) {
		resp := &api.ChatResponse{}
		if got := estimateCompletionTokensFromResponse(resp); got != 0 {
			t.Errorf("empty choices should be 0, got %d", got)
		}
	})
	t.Run("with_content", func(t *testing.T) {
		t.Parallel()
		resp := &api.ChatResponse{
			Choices: []api.Choice{
				{Message: struct {
					Role             string        `json:"role"`
					Content          string        `json:"content"`
					ReasoningContent string        `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{Content: "Hello world"}},
			},
		}
		got := estimateCompletionTokensFromResponse(resp)
		if got <= 0 {
			t.Errorf("expected positive tokens for non-empty content, got %d", got)
		}
	})
	t.Run("with_reasoning", func(t *testing.T) {
		t.Parallel()
		resp := &api.ChatResponse{
			Choices: []api.Choice{
				{Message: struct {
					Role             string        `json:"role"`
					Content          string        `json:"content"`
					ReasoningContent string        `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{Content: "Hi", ReasoningContent: "thinking deeply"}},
			},
		}
		got := estimateCompletionTokensFromResponse(resp)
		if got <= 0 {
			t.Errorf("expected positive tokens, got %d", got)
		}
	})
}

// ---------------------------------------------------------------------------
// api_client.go — isRetryableError
// ---------------------------------------------------------------------------

func TestIsRetryableError_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		errStr  string
		want    bool
	}{
		{"502", "502 Bad Gateway", true},
		{"upstream_error", "upstream error: connection refused", true},
		{"stream_error", "stream error: internal", true},
		{"internal_error", "INTERNAL_ERROR encountered", true},
		{"connection_reset", "connection reset by peer", true},
		{"eof", "unexpected EOF", true},
		{"timeout", "timeout waiting for response", true},
		{"non_retryable", "invalid API key", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// isRetryableError only depends on errStr string, not on agent state
			ac := &APIClient{}
			if got := ac.isRetryableError(tt.errStr); got != tt.want {
				t.Errorf("isRetryableError(%q) = %v, want %v", tt.errStr, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// api_client.go — isImageNotSupportedError
// ---------------------------------------------------------------------------

func TestIsImageNotSupportedError_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"image_not_supported", errors.New("image input is not supported by this model"), true},
		{"does_not_support", errors.New("model does not support image input"), true},
		{"vision_not_supported", errors.New("vision is not supported"), true},
		{"multimodal_not_supported", errors.New("multimodal is not supported"), true},
		{"other_error", errors.New("something else"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ac := &APIClient{}
			if got := ac.isImageNotSupportedError(tt.err); got != tt.want {
				t.Errorf("isImageNotSupportedError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// conversation_pruner.go — constructor and basic operations
// ---------------------------------------------------------------------------

func TestNewConversationPruner_ZC(t *testing.T) {
	t.Parallel()
	cp := NewConversationPruner(false)
	if cp == nil {
		t.Fatal("NewConversationPruner returned nil")
	}
	if cp.strategy != PruneStrategyAdaptive {
		t.Errorf("expected adaptive strategy, got %s", cp.strategy)
	}
	if cp.debug {
		t.Error("debug should be false")
	}
}

func TestConversationPruner_SetStrategy_ZC(t *testing.T) {
	t.Parallel()
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategySlidingWindow)
	if cp.strategy != PruneStrategySlidingWindow {
		t.Errorf("expected sliding_window, got %s", cp.strategy)
	}
}

func TestConversationPruner_SetThreshold_ZC(t *testing.T) {
	t.Parallel()
	cp := NewConversationPruner(false)
	cp.SetThreshold(0.75)
	if cp.contextThreshold != 0.75 {
		t.Errorf("expected 0.75, got %f", cp.contextThreshold)
	}
}

func TestConversationPruner_ShouldPrune_ZC(t *testing.T) {
	t.Parallel()
	t.Run("none_strategy", func(t *testing.T) {
		cp := NewConversationPruner(false)
		cp.SetStrategy(PruneStrategyNone)
		if cp.ShouldPrune(90000, 100000, "openai", false) {
			t.Error("none strategy should never prune")
		}
	})
	t.Run("zero_max_tokens", func(t *testing.T) {
		cp := NewConversationPruner(false)
		if cp.ShouldPrune(100, 0, "openai", false) {
			t.Error("zero maxTokens should not prune")
		}
	})
	t.Run("below_threshold", func(t *testing.T) {
		cp := NewConversationPruner(false)
		// Default threshold is ~0.85, so 50% should not trigger
		if cp.ShouldPrune(50000, 100000, "openai", false) {
			t.Error("50% usage should not trigger pruning")
		}
	})
	t.Run("above_threshold", func(t *testing.T) {
		cp := NewConversationPruner(false)
		cp.SetThreshold(0.5) // 50% threshold
		if !cp.ShouldPrune(90000, 100000, "openai", false) {
			t.Error("90% usage with 50% threshold should trigger pruning")
		}
	})
}

func TestClampTargetTokens_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		target    int
		maxTokens int
		want      int
	}{
		{"in_range", 50000, 100000, 50000},
		{"above_max", 150000, 100000, 100000},
		{"below_min_clamped", 100, 100000, 25000}, // 25% of 100000 = 25000
		{"small_max", 500, 1000, 1000},            // minTarget = max(250, 1000) = 1000
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clampTargetTokens(tt.target, tt.maxTokens)
			if got != tt.want {
				t.Errorf("clampTargetTokens(%d, %d) = %d, want %d", tt.target, tt.maxTokens, got, tt.want)
			}
		})
	}
}

func TestConversationPruner_CountToolCalls_ZC(t *testing.T) {
	t.Parallel()
	msgs := []api.Message{
		{Role: "user", Content: "do stuff"},
		{Role: "assistant", Content: "calling tool", ToolCalls: []api.ToolCall{{ID: "1"}}},
		{Role: "tool", Content: "result"},
		{Role: "assistant", Content: "done"},
	}
	cp := NewConversationPruner(false)
	got := cp.countToolCalls(msgs)
	if got != 2 {
		t.Errorf("expected 2 tool calls, got %d", got)
	}
}

func TestConversationPruner_HasLargeFileReads_ZC(t *testing.T) {
	t.Parallel()
	t.Run("no_tool_messages", func(t *testing.T) {
		msgs := []api.Message{{Role: "user", Content: "hi"}}
		cp := NewConversationPruner(false)
		if cp.hasLargeFileReads(msgs) {
			t.Error("no tool messages should return false")
		}
	})
	t.Run("small_read", func(t *testing.T) {
		msgs := []api.Message{
			{Role: "tool", Content: "Tool call result for read_file: small content"},
		}
		cp := NewConversationPruner(false)
		if cp.hasLargeFileReads(msgs) {
			t.Error("small read should return false")
		}
	})
	t.Run("large_read", func(t *testing.T) {
		largeContent := "Tool call result for read_file: " + strings.Repeat("x", 6000)
		msgs := []api.Message{
			{Role: "tool", Content: largeContent},
		}
		cp := NewConversationPruner(false)
		if !cp.hasLargeFileReads(msgs) {
			t.Error("large read should return true")
		}
	})
}

func TestConversationPruner_ScoreSingleMessage_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		msg   api.Message
		score float64
	}{
		{"system", api.Message{Role: "system", Content: "sys"}, 1.0},
		{"user", api.Message{Role: "user", Content: "hello"}, 0.6},
		{"user_error", api.Message{Role: "user", Content: "got an error here"}, 0.8},
		{"tool", api.Message{Role: "tool", Content: "result"}, 0.5},
		{"tool_error", api.Message{Role: "tool", Content: "Error: failed"}, 0.8},
		{"assistant", api.Message{Role: "assistant", Content: "reply"}, 0.5},
		{"assistant_with_tools", api.Message{Role: "assistant", Content: "reply", ToolCalls: []api.ToolCall{{ID: "1"}}}, 0.6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cp := NewConversationPruner(false)
			got := cp.scoreSingleMessage(tt.msg)
			if got != tt.score {
				t.Errorf("scoreSingleMessage(%s) = %f, want %f", tt.name, got, tt.score)
			}
		})
	}
}

func TestConversationPruner_EstimateTokens_ZC(t *testing.T) {
	t.Parallel()
	msgs := []api.Message{
		{Role: "user", Content: "short"},
		{Role: "assistant", Content: strings.Repeat("word ", 100), ReasoningContent: "thinking"},
	}
	cp := NewConversationPruner(false)
	got := cp.estimateTokens(msgs)
	if got <= 0 {
		t.Error("estimateTokens should return positive value")
	}
}

func TestConversationPruner_PruneSlidingWindow_ZC(t *testing.T) {
	t.Parallel()
	msgs := make([]api.Message, 10)
	msgs[0] = api.Message{Role: "system", Content: "sys"}
	for i := 1; i < 10; i++ {
		msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("msg %d", i)}
	}
	cp := NewConversationPruner(false)
	cp.slidingWindowSize = 5
	out := cp.pruneSlidingWindow(msgs)
	// Should keep system + last (slidingWindowSize-1) messages
	if len(out) > len(msgs) {
		t.Errorf("pruned should not be longer: got %d, orig %d", len(out), len(msgs))
	}
	if out[0].Role != "system" {
		t.Error("system message should always be kept")
	}
}

func TestConversationPruner_GetTargetTokens_ZC(t *testing.T) {
	t.Parallel()
	cp := NewConversationPruner(false)
	// Small message count should return higher target
	small := cp.getTargetTokens(10, 100000)
	large := cp.getTargetTokens(100, 100000)
	if small <= large {
		t.Errorf("small conversation (%d) should have higher target than large (%d)", small, large)
	}
}

func TestConversationPruner_GetTargetTokensForProvider_ZC(t *testing.T) {
	t.Parallel()
	cp := NewConversationPruner(false)
	got := cp.getTargetTokensForProvider(10, "openai", 100000)
	if got <= 0 {
		t.Error("should return positive value")
	}
}

// ---------------------------------------------------------------------------
// conversation_pruner.go — ensureRequiredHeadroom
// ---------------------------------------------------------------------------

func TestConversationPruner_EnsureRequiredHeadroom_ZC(t *testing.T) {
	t.Parallel()
	t.Run("nil_messages", func(t *testing.T) {
		cp := NewConversationPruner(false)
		out := cp.ensureRequiredHeadroom(nil, 100000, 10000)
		if len(out) != 0 {
			t.Error("nil input should return nil/empty")
		}
	})
	t.Run("zero_max", func(t *testing.T) {
		cp := NewConversationPruner(false)
		msgs := []api.Message{{Role: "user", Content: "hi"}}
		out := cp.ensureRequiredHeadroom(msgs, 0, 10000)
		if len(out) != 1 {
			t.Error("zero maxTokens should return input unchanged")
		}
	})
	t.Run("trims_to_headroom", func(t *testing.T) {
		t.Parallel()
		cp := NewConversationPruner(false)
		msgs := make([]api.Message, 20)
		msgs[0] = api.Message{Role: "system", Content: "sys"}
		for i := 1; i < 20; i++ {
			msgs[i] = api.Message{Role: "user", Content: strings.Repeat("x", 30000)}
		}
		// Each message ~7500 tokens, 20 msgs = ~150K tokens, maxTokens=100K
		// Need 40K headroom → should trim ~6+ messages
		out := cp.ensureRequiredHeadroom(msgs, 100000, 40000)
		if len(out) >= len(msgs) {
			t.Errorf("should have trimmed some messages: got %d, orig %d", len(out), len(msgs))
		}
		if out[0].Role != "system" {
			t.Error("system message should be preserved")
		}
	})
}

// ---------------------------------------------------------------------------
// secret_prompter.go — isSecretSensitiveTool
// ---------------------------------------------------------------------------

func TestIsSecretSensitiveTool_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool string
		want bool
	}{
		{"shell_command", true},
		{"read_file", true},
		{"search_files", true},
		{"write_file", true},
		{"edit_file", true},
		{"write_structured_file", true},
		{"patch_structured_file", true},
		{"web_search", false},
		{"fetch_url", false},
		{"", false},
		{"analyze_image_content", false},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()
			if got := isSecretSensitiveTool(tt.tool); got != tt.want {
				t.Errorf("isSecretSensitiveTool(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// tool_executor_todo_events.go — todoStatusSymbol
// ---------------------------------------------------------------------------

func TestTodoStatusSymbol_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status string
		want   string
	}{
		{"pending", "[ ]"},
		{"in_progress", "[~]"},
		{"completed", "[x]"},
		{"cancelled", "[-]"},
		{"unknown", "[?]"},
		{"", "[?]"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			if got := todoStatusSymbol(tt.status); got != tt.want {
				t.Errorf("todoStatusSymbol(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// tool_handlers_search.go — bytesIndexByte
// ---------------------------------------------------------------------------

func TestBytesIndexByte_ZC(t *testing.T) {
	t.Parallel()
	t.Run("found", func(t *testing.T) {
		if got := bytesIndexByte([]byte("hello"), 'l'); got != 2 {
			t.Errorf("expected 2, got %d", got)
		}
	})
	t.Run("not_found", func(t *testing.T) {
		if got := bytesIndexByte([]byte("hello"), 'z'); got != -1 {
			t.Errorf("expected -1, got %d", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := bytesIndexByte(nil, 'a'); got != -1 {
			t.Errorf("expected -1 for nil, got %d", got)
		}
	})
	t.Run("first_byte", func(t *testing.T) {
		if got := bytesIndexByte([]byte("abc"), 'a'); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})
}

// ---------------------------------------------------------------------------
// submanager_mcp.go — AgentMCPManager
// ---------------------------------------------------------------------------

func TestNewAgentMCPManager_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentMCPManager()
	if m == nil {
		t.Fatal("NewAgentMCPManager returned nil")
	}
	if m.IsInitialized() {
		t.Error("new manager should not be initialized")
	}
	if m.GetInitError() != nil {
		t.Error("new manager should have no init error")
	}
}

func TestAgentMCPManager_SetGet_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentMCPManager()

	// Tools cache
	tool := api.Tool{}
	tool.Function.Name = "test_tool"
	m.SetToolsCache([]api.Tool{tool})
	if got := m.GetToolsCache(); len(got) != 1 || got[0].Function.Name != "test_tool" {
		t.Errorf("tools cache mismatch: %+v", got)
	}

	// Initialized
	m.SetInitialized(true)
	if !m.IsInitialized() {
		t.Error("should be initialized after SetInitialized(true)")
	}

	// Init error
	err := errors.New("test error")
	m.SetInitError(err)
	if m.GetInitError() == nil {
		t.Error("should have init error")
	}
}

func TestAgentMCPManager_LockInit_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentMCPManager()
	// Basic lock/unlock doesn't panic
	m.LockInit()
	m.UnlockInit()
}

// ---------------------------------------------------------------------------
// submanager_output.go — AgentOutputManager
// ---------------------------------------------------------------------------

func TestNewAgentOutputManager_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	if m == nil {
		t.Fatal("NewAgentOutputManager returned nil")
	}
	if m.IsStreamingEnabled() {
		t.Error("new manager should not have streaming enabled")
	}
	if m.GetStreamingBuffer() == nil {
		t.Error("streaming buffer should not be nil")
	}
	if m.GetReasoningBuffer() == nil {
		t.Error("reasoning buffer should not be nil")
	}
}

func TestAgentOutputManager_Streaming_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	m.SetStreamingEnabled(true)
	if !m.IsStreamingEnabled() {
		t.Error("streaming should be enabled")
	}

	called := false
	m.SetStreamingCallback(func(s string) { called = true })
	cb := m.GetStreamingCallback()
	if cb == nil {
		t.Fatal("callback should not be nil")
	}
	cb("test")
	if !called {
		t.Error("callback should have been called")
	}
}

func TestAgentOutputManager_Reasoning_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	called := false
	m.SetReasoningCallback(func(s string) { called = true })
	cb := m.GetReasoningCallback()
	cb("test")
	if !called {
		t.Error("reasoning callback should have been called")
	}
}

func TestAgentOutputManager_FlushCallback_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	called := false
	m.SetFlushCallback(func() { called = true })
	cb := m.GetFlushCallback()
	cb()
	if !called {
		t.Error("flush callback should have been called")
	}
}

func TestAgentOutputManager_OutputMutex_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	var mu sync.Mutex
	m.SetOutputMutex(&mu)
	if m.GetOutputMutex() != &mu {
		t.Error("mutex should be the same instance")
	}
}

func TestAgentOutputManager_AsyncOutput_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	ch := make(chan string, 10)
	m.SetAsyncOutput(ch)
	if m.GetAsyncOutput() != ch {
		t.Error("async output channel mismatch")
	}
}

func TestAgentOutputManager_AsyncBufferSize_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	m.SetAsyncBufferSize(42)
	if m.GetAsyncBufferSize() != 42 {
		t.Errorf("expected 42, got %d", m.GetAsyncBufferSize())
	}
}

func TestAgentOutputManager_EnsureAsyncOutputWorker_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	count := 0
	fn := func() { count++ }
	m.EnsureAsyncOutputWorker(fn)
	m.EnsureAsyncOutputWorker(fn) // Should only run once
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

func TestAgentOutputManager_EventMetadata_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	meta := map[string]interface{}{"key": "value"}
	m.SetEventMetadata(meta)
	got := m.GetEventMetadata()
	if got["key"] != "value" {
		t.Errorf("metadata mismatch: %+v", got)
	}
}

func TestAgentOutputManager_OutputRouter_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	if m.GetOutputRouter() != nil {
		t.Error("new manager should have nil router")
	}
}

// ---------------------------------------------------------------------------
// submanager_security.go — AgentSecurityManager
// ---------------------------------------------------------------------------

func TestNewAgentSecurityManager_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentSecurityManager()
	if m == nil {
		t.Fatal("NewAgentSecurityManager returned nil")
	}
	if m.GetUnsafeMode() {
		t.Error("new manager should not be in unsafe mode")
	}
	if m.IsSecurityBypassApproved() {
		t.Error("new manager should not have bypass approved")
	}
}

func TestAgentSecurityManager_UnsafeMode_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentSecurityManager()
	m.SetUnsafeMode(true)
	if !m.GetUnsafeMode() {
		t.Error("unsafe mode should be true after set")
	}
	m.SetUnsafeMode(false)
	if m.GetUnsafeMode() {
		t.Error("unsafe mode should be false after set")
	}
}

func TestAgentSecurityManager_Bypass_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentSecurityManager()
	m.SetSecurityBypassApproved()
	if !m.IsSecurityBypassApproved() {
		t.Error("bypass should be approved after set")
	}
}

func TestAgentSecurityManager_ConcernIgnored_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentSecurityManager()
	if m.IsConcernIgnored("file.go", "issue") {
		t.Error("unregistered concern should not be ignored")
	}
	m.SetConcernIgnored("file.go", "issue")
	if !m.IsConcernIgnored("file.go", "issue") {
		t.Error("registered concern should be ignored")
	}
	if m.IsConcernIgnored("other.go", "issue") {
		t.Error("different file should not be ignored")
	}
}

func TestAgentSecurityManager_HasActiveWebUIClients_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentSecurityManager()
	m.SetHasActiveWebUIClients(func() bool { return true })
	if !m.HasActiveWebUIClients() {
		t.Error("should return true when function returns true")
	}
}

func TestAgentSecurityManager_ElevationGate_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentSecurityManager()
	if m.GetElevationGate() == nil {
		t.Error("new manager should have initialized elevation gate")
	}
}

// ---------------------------------------------------------------------------
// submanager_state.go — AgentStateManager
// ---------------------------------------------------------------------------

func TestNewAgentStateManager_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	if s == nil {
		t.Fatal("NewAgentStateManager returned nil")
	}
	if len(s.GetMessages()) != 0 {
		t.Error("new manager should have no messages")
	}
}

func TestAgentStateManager_Messages_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	msgs := []api.Message{{Role: "user", Content: "hello"}}
	s.SetMessages(msgs)
	if len(s.GetMessages()) != 1 {
		t.Fatal("expected 1 message")
	}
	s.AddMessage(api.Message{Role: "assistant", Content: "hi"})
	if len(s.GetMessages()) != 2 {
		t.Fatal("expected 2 messages after AddMessage")
	}
}

func TestAgentStateManager_SessionID_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	s.SetSessionID("test-123")
	if s.GetSessionID() != "test-123" {
		t.Errorf("expected test-123, got %s", s.GetSessionID())
	}
}

func TestAgentStateManager_TurnCheckpoints_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	cps := []TurnCheckpoint{{Summary: "q1 summary", StartIndex: 0, EndIndex: 1}}
	s.SetTurnCheckpoints(cps)
	if len(s.GetTurnCheckpoints()) != 1 {
		t.Fatal("expected 1 checkpoint")
	}
	s.AddTurnCheckpoint(TurnCheckpoint{Summary: "q2 summary", StartIndex: 1, EndIndex: 2})
	if len(s.GetTurnCheckpoints()) != 2 {
		t.Fatal("expected 2 checkpoints")
	}
	if s.GetCheckpointMutex() == nil {
		t.Error("checkpoint mutex should not be nil")
	}
}

func TestAgentStateManager_Summary_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	s.SetPreviousSummary("prev summary")
	if s.GetPreviousSummary() != "prev summary" {
		t.Errorf("expected 'prev summary', got %q", s.GetPreviousSummary())
	}
}

func TestAgentStateManager_ContextTokens_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	s.SetCurrentContextTokens(5000)
	if s.GetCurrentContextTokens() != 5000 {
		t.Errorf("expected 5000, got %d", s.GetCurrentContextTokens())
	}
	s.SetMaxContextTokens(100000)
	if s.GetMaxContextTokens() != 100000 {
		t.Errorf("expected 100000, got %d", s.GetMaxContextTokens())
	}
}

func TestAgentStateManager_ContextWarning_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	if s.IsContextWarningIssued() {
		t.Error("should start false")
	}
	s.SetContextWarningIssued(true)
	if !s.IsContextWarningIssued() {
		t.Error("should be true after set")
	}
}

func TestAgentStateManager_Cost_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	s.SetTotalCost(1.5)
	if s.GetTotalCost() != 1.5 {
		t.Errorf("expected 1.5, got %f", s.GetTotalCost())
	}
	s.AddCost(0.5)
	if s.GetTotalCost() != 2.0 {
		t.Errorf("expected 2.0, got %f", s.GetTotalCost())
	}
}

func TestAgentStateManager_Tokens_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	s.SetTotalTokens(150)
	if s.GetTotalTokens() != 150 {
		t.Errorf("expected 150, got %d", s.GetTotalTokens())
	}
}

func TestAgentStateManager_Iteration_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	s.SetCurrentIteration(5)
	if s.GetCurrentIteration() != 5 {
		t.Errorf("expected 5, got %d", s.GetCurrentIteration())
	}
}

func TestAgentStateManager_TaskActions_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(false)
	s.SetTaskActions([]TaskAction{{Type: "test", Description: "test action"}})
	if len(s.GetTaskActions()) != 1 {
		t.Fatal("expected 1 task action")
	}
	s.AddTaskAction(TaskAction{Type: "new", Description: "new action"})
	if len(s.GetTaskActions()) != 2 {
		t.Fatal("expected 2 task actions")
	}
	if s.GetTaskActionsMutex() == nil {
		t.Error("task actions mutex should not be nil")
	}
}

func TestAgentStateManager_DebugMode_ZC(t *testing.T) {
	t.Parallel()
	s := NewAgentStateManager(true)
	// Just verify constructor doesn't panic with debug=true
	if s == nil {
		t.Fatal("NewAgentStateManager returned nil")
	}
}

// ---------------------------------------------------------------------------
// ui_types.go — structs and PublishModel
// ---------------------------------------------------------------------------

func TestUITypesStructs_ZC(t *testing.T) {
	t.Parallel()
	di := DropdownItem{Label: "test", Value: "val"}
	if di.Label != "test" {
		t.Error("DropdownItem label mismatch")
	}

	do := DropdownOptions{Prompt: "pick"}
	if do.Prompt != "pick" {
		t.Error("DropdownOptions prompt mismatch")
	}

	qo := QuickOption{Label: "opt", Value: "v"}
	if qo.Label != "opt" {
		t.Error("QuickOption label mismatch")
	}

	si := SessionItem{Label: "sess1", Value: "Session 1"}
	if si.Label != "sess1" {
		t.Error("SessionItem label mismatch")
	}

	mi := ModelItem{Label: "model1", Value: "Model One"}
	if mi.Label != "model1" {
		t.Error("ModelItem label mismatch")
	}
}

func TestPublishModel_ZC(t *testing.T) {
	t.Parallel()
	PublishModel("test-model-v1")
	// Verify the global was set (read-only check)
	// Just ensure it doesn't panic
}

// ---------------------------------------------------------------------------
// ui_choice.go — choiceDropdownItem
// ---------------------------------------------------------------------------

func TestChoiceDropdownItem_ZC(t *testing.T) {
	t.Parallel()
	opt := ChoiceOption{Label: "Option A", Value: "a"}
	item := choiceDropdownItem{opt: opt}

	if item.Display() != "Option A" {
		t.Errorf("Display() = %q, want %q", item.Display(), "Option A")
	}
	if item.SearchText() != "Option A" {
		t.Errorf("SearchText() = %q, want %q", item.SearchText(), "Option A")
	}
	if item.Value() != "a" {
		t.Errorf("Value() = %v, want %q", item.Value(), "a")
	}
}

// ---------------------------------------------------------------------------
// streaming.go — Agent streaming methods
// ---------------------------------------------------------------------------

func TestAgentStreaming_ZC(t *testing.T) {
	t.Parallel()
	// Test via AgentOutputManager since Agent delegates to it
	m := NewAgentOutputManager()
	m.SetStreamingEnabled(true)
	if !m.IsStreamingEnabled() {
		t.Error("streaming should be enabled")
	}
	m.SetStreamingEnabled(false)
	if m.IsStreamingEnabled() {
		t.Error("streaming should be disabled")
	}
}

func TestAgentStreamingCallback_ZC(t *testing.T) {
	t.Parallel()
	m := NewAgentOutputManager()
	m.SetStreamingEnabled(true)
	if !m.IsStreamingEnabled() {
		t.Error("EnableStreaming should enable streaming")
	}
	m.SetStreamingEnabled(false)
	if m.IsStreamingEnabled() {
		t.Error("DisableStreaming should disable streaming")
	}
}

func TestAgentSetFlushCallback_ZC(t *testing.T) {
	t.Parallel()
	// Agent streaming methods require initialized submanagers - test via AgentOutputManager instead
	m := NewAgentOutputManager()
	called := false
	m.SetFlushCallback(func() { called = true })
	if m.GetFlushCallback() == nil {
		t.Fatal("flush callback should not be nil")
	}
	m.GetFlushCallback()()
	if !called {
		t.Error("flush callback should have been called")
	}
}

func TestAgentSetOutputMutex_ZC(t *testing.T) {
	t.Parallel()
	// Test via AgentOutputManager since Agent streaming methods delegate to it
	m := NewAgentOutputManager()
	var mu sync.Mutex
	m.SetOutputMutex(&mu)
	if m.GetOutputMutex() != &mu {
		t.Error("mutex should be same instance")
	}
}

// ---------------------------------------------------------------------------
// conversation_pruner.go — PruneConversation (integration)
// ---------------------------------------------------------------------------

func TestConversationPruner_PruneConversation_ZC(t *testing.T) {
	t.Parallel()
	t.Run("not_triggered", func(t *testing.T) {
		cp := NewConversationPruner(false)
		msgs := []api.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hi"},
		}
		out := cp.PruneConversation(msgs, 100, 100000, nil, "openai", false)
		if len(out) != len(msgs) {
			t.Error("should not prune when not triggered")
		}
	})
	t.Run("none_strategy_preserves_all", func(t *testing.T) {
		cp := NewConversationPruner(false)
		cp.SetStrategy(PruneStrategyNone)
		msgs := make([]api.Message, 20)
		msgs[0] = api.Message{Role: "system", Content: "sys"}
		for i := 1; i < 20; i++ {
			msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("msg %d", i)}
		}
		ut := cp.estimateTokens(msgs)
		out := cp.PruneConversation(msgs, ut+int(float64(ut)*0.9), ut+ut+100, nil, "openai", false)
		// None strategy → ShouldPrune returns false → returns input
		if len(out) != len(msgs) {
			t.Error("none strategy should preserve all messages")
		}
	})
}

// ---------------------------------------------------------------------------
// conversation_pruner.go — scoreMessages
// ---------------------------------------------------------------------------

func TestConversationPruner_ScoreMessages_ZC(t *testing.T) {
	t.Parallel()
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	cp := NewConversationPruner(false)
	scored := cp.scoreMessages(msgs)
	if len(scored) != 3 {
		t.Fatalf("expected 3 scored messages, got %d", len(scored))
	}
	if scored[0].ImportanceScore != 1.0 {
		t.Errorf("system should score 1.0, got %f", scored[0].ImportanceScore)
	}
}
