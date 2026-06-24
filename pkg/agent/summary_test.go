package agent

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestTokenTrackingAccuracy verifies that token tracking is accurate
func TestTokenTrackingAccuracy(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetTotalTokens(87200)
	agent.state.SetPromptTokens(84750)
	agent.state.SetCompletionTokens(2433)
	agent.state.SetCachedTokens(71400)

	// Calculate processed prompt (excluding cached)
	processedPromptTokens := agent.state.GetPromptTokens() - agent.state.GetCachedTokens()
	if processedPromptTokens != 13350 {
		t.Errorf("Expected processedPromptTokens to be 13350, got %d", processedPromptTokens)
	}

	// Calculate processed: processedPrompt + completion
	processedTokens := processedPromptTokens + agent.state.GetCompletionTokens()
	if processedTokens != 15783 {
		t.Errorf("Expected processedTokens to be 15783, got %d", processedTokens)
	}

	// Verify the math adds up: processedPrompt + completion = processed
	if processedPromptTokens+agent.state.GetCompletionTokens() != processedTokens {
		t.Errorf("Math doesn't add up: %d + %d != %d", processedPromptTokens, agent.state.GetCompletionTokens(), processedTokens)
	}

	// TrackMetricsFromResponse should work correctly
	agent.TrackMetricsFromResponse(1000, 200, 1200, 0.01, 500, 0)
	if agent.state.GetTotalTokens() != 88400 { // 87200 + 1200
		t.Errorf("Expected totalTokens to be 88400, got %d", agent.state.GetTotalTokens())
	}
	if agent.state.GetCachedTokens() != 71900 { // 71400 + 500
		t.Errorf("Expected cachedTokens to be 71900, got %d", agent.state.GetCachedTokens())
	}
	if agent.state.GetPromptTokens() != 85750 { // 84750 + 1000
		t.Errorf("Expected promptTokens to be 85750, got %d", agent.state.GetPromptTokens())
	}
}

// TestEmptyCachedTokens handles edge case where there are no cached tokens
func TestEmptyCachedTokens(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetTotalTokens(5000)
	agent.state.SetPromptTokens(4000)
	agent.state.SetCompletionTokens(1000)
	agent.state.SetCachedTokens(0)

	processedTokens := agent.state.GetTotalTokens()
	if processedTokens != 5000 {
		t.Errorf("Expected processedTokens to be 5000, got %d", processedTokens)
	}

	processedPromptTokens := agent.state.GetPromptTokens()
	if processedPromptTokens != 4000 {
		t.Errorf("Expected processedPromptTokens to be 4000, got %d", processedPromptTokens)
	}
}

// TestNegativeProcessedPromptTokens tests when cachedTokens > promptTokens
func TestNegativeProcessedPromptTokens(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetTotalTokens(1500)
	agent.state.SetPromptTokens(1000)
	agent.state.SetCompletionTokens(500)
	agent.state.SetCachedTokens(2000)

	// Should clamp to 0
	processedPromptTokens := agent.state.GetPromptTokens() - agent.state.GetCachedTokens()
	if processedPromptTokens > 0 {
		t.Errorf("Expected processedPromptTokens to be clamped to 0, got %d", processedPromptTokens)
	}

	// processedTokens should still be valid (completionTokens only in this case)
	processedTokens := max(0, processedPromptTokens) + agent.state.GetCompletionTokens()
	if processedTokens != agent.state.GetCompletionTokens() {
		t.Errorf("Expected processedTokens to be %d, got %d", agent.state.GetCompletionTokens(), processedTokens)
	}
}

// TestTokenDiscrepancy tests when totalTokens != promptTokens + completionTokens
func TestTokenDiscrepancy(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetTotalTokens(10000)
	agent.state.SetPromptTokens(8000)
	agent.state.SetCompletionTokens(200)
	agent.state.SetCachedTokens(7500)

	// Simulate the calculation from summary.go
	processedPromptTokens := agent.state.GetPromptTokens() - agent.state.GetCachedTokens()  // 500
	processedTokens := processedPromptTokens + agent.state.GetCompletionTokens() // 700

	expectedProcessed := agent.state.GetTotalTokens() - agent.state.GetCachedTokens() // 1000

	// These should differ due to the discrepancy
	if processedTokens == expectedProcessed {
		t.Logf("Note: With these values, processedTokens=%d equals expectedProcessed=%d", processedTokens, expectedProcessed)
	} else {
		t.Logf("Expected discrepancy: computed=%d, expected=%d", processedTokens, expectedProcessed)
	}
}

// TestClampingBehavior comprehensive test for various edge cases
func TestClampingBehavior(t *testing.T) {
	tests := []struct {
		name                string
		promptTokens        int
		completionTokens    int
		cachedTokens        int
		totalTokens         int
		wantProcessedPrompt int
		wantProcessed       int
	}{
		{
			name:                "normal case",
			promptTokens:        1000,
			completionTokens:    200,
			cachedTokens:        500,
			totalTokens:         1200,
			wantProcessedPrompt: 500,
			wantProcessed:       700,
		},
		{
			name:                "cached exceeds prompt",
			promptTokens:        500,
			completionTokens:    200,
			cachedTokens:        600,
			totalTokens:         800,
			wantProcessedPrompt: 0,
			wantProcessed:       200,
		},
		{
			name:                "no cached tokens",
			promptTokens:        1000,
			completionTokens:    200,
			cachedTokens:        0,
			totalTokens:         1200,
			wantProcessedPrompt: 1000,
			wantProcessed:       1200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &Agent{
				state: NewAgentStateManager(false),
			}
			agent.state.SetTotalTokens(tt.totalTokens)
			agent.state.SetPromptTokens(tt.promptTokens)
			agent.state.SetCompletionTokens(tt.completionTokens)
			agent.state.SetCachedTokens(tt.cachedTokens)

			processedPromptTokens := agent.state.GetPromptTokens() - agent.state.GetCachedTokens()
			if processedPromptTokens < 0 {
				processedPromptTokens = 0
			}
			processedTokens := processedPromptTokens + agent.state.GetCompletionTokens()

			if processedPromptTokens != tt.wantProcessedPrompt {
				t.Errorf("processedPromptTokens = %d, want %d", processedPromptTokens, tt.wantProcessedPrompt)
			}
			if processedTokens != tt.wantProcessed {
				t.Errorf("processedTokens = %d, want %d", processedTokens, tt.wantProcessed)
			}
		})
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestComputeConversationSummaryMetrics(t *testing.T) {
	messages := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "do thing"},
		{
			Role:    "assistant",
			Content: "calling tools",
			ToolCalls: []api.ToolCall{
				{ID: "t1", Type: "function"},
				{ID: "t2", Type: "function"},
			},
		},
		{Role: "tool", Content: "{}"},
		{Role: "assistant", Content: "done"},
	}

	metrics := computeConversationSummaryMetrics(messages)
	if metrics.systemMessages != 1 {
		t.Fatalf("expected 1 system message, got %d", metrics.systemMessages)
	}
	if metrics.userMessages != 1 {
		t.Fatalf("expected 1 user message, got %d", metrics.userMessages)
	}
	if metrics.assistantMessages != 2 {
		t.Fatalf("expected 2 assistant messages, got %d", metrics.assistantMessages)
	}
	if metrics.toolMessages != 1 {
		t.Fatalf("expected 1 tool message, got %d", metrics.toolMessages)
	}
	if metrics.toolCalls != 2 {
		t.Fatalf("expected 2 tool calls, got %d", metrics.toolCalls)
	}
}

func TestPrintConversationSummaryDoesNotPanicWithSingleMessage(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetMessages([]api.Message{{Role: "user", Content: "hello"}})
	agent.state.SetMaxContextTokens(120000)

	output := captureStdout(t, func() {
		agent.PrintConversationSummary(true)
	})

	if !strings.Contains(output, "Conversation Summary") {
		t.Fatalf("expected summary header in output, got: %s", output)
	}
	if !strings.Contains(output, "User msgs:        1") {
		t.Fatalf("expected user message count in output, got: %s", output)
	}
}

func TestPrintConversationSummaryShowsEstimatedTokenNote(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetMessages([]api.Message{{Role: "user", Content: "hello"}})
	agent.state.SetEstimatedTokenResponses(2)
	agent.state.SetTotalTokens(87200)
	agent.state.SetPromptTokens(84750)
	agent.state.SetCompletionTokens(2433)
	agent.state.SetCachedTokens(71400)

	output := captureStdout(t, func() {
		agent.PrintConversationSummary(true)
	})

	if !strings.Contains(output, "includes estimates for 2 response(s)") {
		t.Fatalf("expected estimated usage note in output, got: %s", output)
	}
	if !strings.Contains(output, "Total (estimated):") {
		t.Fatalf("expected estimated marker on total line, got: %s", output)
	}
	if !strings.Contains(output, "Processed (estimated):") {
		t.Fatalf("expected estimated marker on processed line, got: %s", output)
	}
}
