package agent

import (
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func (ch *ConversationHandler) handleOCRCompletionGate(turn *TurnEvaluation) (handled bool, shouldStop bool) {
	if !ch.shouldRequireOCRBeforeCompletion() {
		ch.ocrEnforcementAttempts = 0
		return false, false
	}

	ch.ocrEnforcementAttempts++
	if ch.ocrEnforcementAttempts >= 3 {
		if turn != nil {
			turn.GuardrailTrigger = "ocr policy blocked completion"
		}
		ch.displayFinalResponse("Error: OCR policy requires at least one analyze_image_content tool call before completion when image/PDF menu signals are present.")
		return true, true
	}

	if turn != nil {
		turn.GuardrailTrigger = "ocr policy reminder"
	}

	ch.enqueueTransientMessage(api.Message{
		Role:    "user",
		Content: "OCR policy requirement: before finishing, call analyze_image_content at least once for menu/offer image or PDF sources you discovered. If OCR fails, include the failure reason in errors/missing_data and then finalize.",
	})
	return true, false
}

func (ch *ConversationHandler) shouldRequireOCRBeforeCompletion() bool {
	if ch.agent != nil && ch.agent.shouldUseDirectMultimodalImageReasoning(ch.agent.messages) {
		return false
	}
	if !ch.isOCRPolicyRequested() {
		return false
	}
	if ch.hasToolCallInHistory("analyze_image_content") {
		return false
	}
	return ch.fetchResultsSuggestImageOrPDFMenu()
}

func (ch *ConversationHandler) isOCRPolicyRequested() bool {
	for _, msg := range ch.agent.messages {
		if msg.Role != "user" {
			continue
		}
		text := strings.ToLower(msg.Content)
		if strings.Contains(text, "ocr trigger policy") {
			return true
		}
		if strings.Contains(text, "analyze_image_content") &&
			(strings.Contains(text, "menu") || strings.Contains(text, "offer") || strings.Contains(text, "pdf")) {
			return true
		}
	}
	return false
}

func (ch *ConversationHandler) hasToolCallInHistory(toolName string) bool {
	for _, msg := range ch.agent.messages {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			name := strings.Split(tc.Function.Name, "<|channel|>")[0]
			if name == toolName {
				return true
			}
		}
	}
	return false
}

func (ch *ConversationHandler) fetchResultsSuggestImageOrPDFMenu() bool {
	fetchIDs := make(map[string]struct{})
	for _, msg := range ch.agent.messages {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			name := strings.Split(tc.Function.Name, "<|channel|>")[0]
			if name == "fetch_url" && tc.ID != "" {
				fetchIDs[tc.ID] = struct{}{}
			}
		}
	}
	if len(fetchIDs) == 0 {
		return false
	}

	imageHints := []string{".pdf", ".jpg", ".jpeg", ".png", "image 1", "image 2", "image 3", "image 4", "flyer", "images.squarespace-cdn"}
	contextHints := []string{"menu", "offer", "event", "happy hour", "promotion"}
	for _, msg := range ch.agent.messages {
		if msg.Role != "tool" {
			continue
		}
		if _, ok := fetchIDs[msg.ToolCallId]; !ok {
			continue
		}
		content := strings.ToLower(msg.Content)
		hasImageHint := false
		for _, hint := range imageHints {
			if strings.Contains(content, hint) {
				hasImageHint = true
				break
			}
		}
		if !hasImageHint {
			continue
		}
		for _, hint := range contextHints {
			if strings.Contains(content, hint) {
				return true
			}
		}
	}

	return false
}
