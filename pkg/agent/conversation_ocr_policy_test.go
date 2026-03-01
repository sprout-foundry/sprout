package agent

import (
	"context"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestShouldRequireOCRBeforeCompletion(t *testing.T) {
	agent := &Agent{
		streamingEnabled: true,
		interruptCtx:     context.Background(),
		outputMutex:      &sync.Mutex{},
		messages: []api.Message{
			{Role: "user", Content: "OCR Trigger Policy (MANDATORY): use analyze_image_content for menu images/PDFs."},
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					{
						ID:   "fetch_1",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "fetch_url",
							Arguments: `{"url":"https://example.com/menu"}`,
						},
					},
				},
			},
			{Role: "tool", ToolCallId: "fetch_1", Content: "Menu page includes Image 4: https://cdn.example.com/menu.jpg"},
		},
	}
	ch := NewConversationHandler(agent)

	if !ch.shouldRequireOCRBeforeCompletion() {
		t.Fatalf("expected OCR requirement when policy is requested and fetch results indicate menu images")
	}
}

func TestShouldRequireOCRBeforeCompletion_FalseWhenOCRAlreadyAttempted(t *testing.T) {
	agent := &Agent{
		streamingEnabled: true,
		interruptCtx:     context.Background(),
		outputMutex:      &sync.Mutex{},
		messages: []api.Message{
			{Role: "user", Content: "OCR Trigger Policy (MANDATORY): use analyze_image_content for menu images/PDFs."},
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					{
						ID:   "ocr_1",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "analyze_image_content",
							Arguments: `{"image_path":"https://cdn.example.com/menu.jpg"}`,
						},
					},
				},
			},
		},
	}
	ch := NewConversationHandler(agent)

	if ch.shouldRequireOCRBeforeCompletion() {
		t.Fatalf("expected OCR requirement to be satisfied when analyze_image_content has already been called")
	}
}

func TestHandleOCRCompletionGate_RemindsThenStops(t *testing.T) {
	agent := &Agent{
		streamingEnabled: true,
		interruptCtx:     context.Background(),
		outputMutex:      &sync.Mutex{},
		messages: []api.Message{
			{Role: "user", Content: "OCR Trigger Policy (MANDATORY): use analyze_image_content for menu images/PDFs."},
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					{
						ID:   "fetch_1",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "fetch_url",
							Arguments: `{"url":"https://example.com/menu"}`,
						},
					},
				},
			},
			{Role: "tool", ToolCallId: "fetch_1", Content: "Menu page includes Image 3 and PDF download menu.pdf"},
		},
	}
	ch := NewConversationHandler(agent)
	turn := &TurnEvaluation{}

	handled, shouldStop := ch.handleOCRCompletionGate(turn)
	if !handled || shouldStop {
		t.Fatalf("expected first OCR gate hit to continue with reminder, got handled=%v stop=%v", handled, shouldStop)
	}
	if len(ch.transientMessages) != 1 {
		t.Fatalf("expected one transient reminder message, got %d", len(ch.transientMessages))
	}

	ch.ocrEnforcementAttempts = 2
	handled, shouldStop = ch.handleOCRCompletionGate(turn)
	if !handled || !shouldStop {
		t.Fatalf("expected OCR gate to stop after repeated misses, got handled=%v stop=%v", handled, shouldStop)
	}
}
