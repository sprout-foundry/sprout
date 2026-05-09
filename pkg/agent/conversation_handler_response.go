package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// responseContext carries shared state between processResponse sub-methods
type responseContext struct {
	turn             TurnEvaluation
	contentUsed      string
	reasoningContent string
	toolCalls        []api.ToolCall
	parserErrors     []string
	fallbackUsed     bool
	fallbackOutput   string
}

// responseDecision indicates whether a sub-method made a stop/continue decision,
// or whether control should fall through to the next handler.
type responseDecision int

const (
	responseDecideStop responseDecision = iota // orchestrator should call finalizeTurn(turn, true)
	responseDecideContinue                     // orchestrator should call finalizeTurn(turn, false)
	responseDecideFallThrough                  // orchestrator should NOT call finalizeTurn, continue to next handler
)

// extractResponseContent populates ctx from the API response.
// Returns true if choices are empty (orchestrator handles empty-choices path).
func (ch *ConversationHandler) extractResponseContent(resp *api.ChatResponse, ctx *responseContext) bool {
	ctx.turn = TurnEvaluation{
		Iteration: ch.agent.state.GetCurrentIteration(),
		Timestamp: time.Now(),
		UserInput: ch.pendingUserMessage,
	}
	ctx.turn.TokenUsage = TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		EstimatedCost:    resp.Usage.EstimatedCost,
	}

	if len(resp.Choices) == 0 {
		return true
	}

	choice := resp.Choices[0]

	// Determine the content to record and validate. Prefer the streaming buffer if streaming was used
	contentUsed := choice.Message.Content
	if ch.agent.output.IsStreamingEnabled() && len(ch.agent.output.GetStreamingBuffer().String()) > 0 {
		contentUsed = ch.agent.output.GetStreamingBuffer().String()
	}

	if ch.agent.debug {
		if strings.Contains(contentUsed, "\x1b[") || strings.Contains(contentUsed, "\x1b(") {
			ch.agent.debugLog("[!!] ANSI DETECTED in conversation content: %q\n", contentUsed)
		}
	}
	contentUsed = ch.sanitizeContent(contentUsed)

	ctx.contentUsed = contentUsed
	ctx.turn.AssistantContent = contentUsed
	ctx.turn.FinishReason = choice.FinishReason

	reasoningContent := choice.Message.ReasoningContent
	if ch.agent.output.IsStreamingEnabled() && len(ch.agent.output.GetReasoningBuffer().String()) > 0 {
		reasoningContent = ch.agent.output.GetReasoningBuffer().String()
	}
	ctx.turn.ReasoningSnippet = abbreviate(reasoningContent, 280)
	ctx.reasoningContent = reasoningContent

	return false
}

// prepareToolCalls processes tool calls from a choice (strip channel suffix,
// generate IDs, deduplicate, normalize, handle malformed). Populates ctx.toolCalls.
func (ch *ConversationHandler) prepareToolCalls(choice api.Choice, ctx *responseContext) {
	if len(choice.Message.ToolCalls) == 0 {
		ctx.toolCalls = nil
		return
	}

	// Strip channel suffix and generate missing IDs
	for i := range choice.Message.ToolCalls {
		choice.Message.ToolCalls[i].Function.Name = strings.Split(choice.Message.ToolCalls[i].Function.Name, "<|channel|>")[0]

		if choice.Message.ToolCalls[i].ID == "" {
			choice.Message.ToolCalls[i].ID = ch.toolExecutor.GenerateToolCallID(choice.Message.ToolCalls[i].Function.Name)
			ch.agent.debugLog("[tool] Generated missing tool call ID: %s for tool: %s\n",
				choice.Message.ToolCalls[i].ID, choice.Message.ToolCalls[i].Function.Name)
		}
	}

	// Deduplicate by ID AND arguments
	deduped := make([]api.ToolCall, 0, len(choice.Message.ToolCalls))
	seenIDs := make(map[string]struct{}, len(choice.Message.ToolCalls))
	seenArgs := make(map[string]struct{}, len(choice.Message.ToolCalls))
	for _, tc := range choice.Message.ToolCalls {
		isDup := false
		if tc.ID != "" {
			if _, exists := seenIDs[tc.ID]; exists {
				isDup = true
			} else {
				seenIDs[tc.ID] = struct{}{}
			}
		}

		argsKey := fmt.Sprintf("%s|%s", tc.Function.Name, strings.TrimSpace(tc.Function.Arguments))
		if !isDup {
			if _, exists := seenArgs[argsKey]; exists {
				isDup = true
			} else {
				seenArgs[argsKey] = struct{}{}
			}
		}

		if isDup {
			sampleArgs := argsKey
			if len(sampleArgs) > 120 {
				sampleArgs = sampleArgs[:117] + "..."
			}
			ch.agent.debugLog("[recycle] Skipping duplicate tool call id=%s name=%s args=%s\n",
				tc.ID, tc.Function.Name, sampleArgs)
			continue
		}

		deduped = append(deduped, tc)
	}
	if len(deduped) != len(choice.Message.ToolCalls) {
		ch.agent.debugLog("[recycle] Deduplicated tool calls: kept %d of %d\n", len(deduped), len(choice.Message.ToolCalls))
	}
	choice.Message.ToolCalls = deduped

	// Normalize tool calls
	normalizedToolCalls, malformedToolCalls := normalizeToolCallsForExecution(choice.Message.ToolCalls)
	choice.Message.ToolCalls = normalizedToolCalls
	if len(malformedToolCalls) > 0 {
		names := make([]string, 0, len(malformedToolCalls))
		for _, tc := range malformedToolCalls {
			names = append(names, tc.Function.Name)
		}
		ch.agent.debugLog("[WARN] Received %d malformed structured tool call(s): %s\n", len(malformedToolCalls), strings.Join(names, ", "))
		ch.enqueueTransientMessage(api.Message{
			Role: "user",
			Content: "Your previous tool call arguments were incomplete or invalid JSON. " +
				"Re-emit the intended tool call(s) with complete valid JSON arguments only.",
		})
		choice.Message.ToolCalls = nil
		ctx.turn.GuardrailTrigger = "malformed structured tool call"
		ctx.parserErrors = append(ctx.parserErrors, fmt.Sprintf("malformed tool calls: %s", strings.Join(names, ", ")))
	}

	// Reset OCR enforcement attempts if analyze_image_content is used
	for _, tc := range choice.Message.ToolCalls {
		if strings.Split(tc.Function.Name, "<|channel|>")[0] == "analyze_image_content" {
			ch.ocrEnforcementAttempts = 0
			break
		}
	}

	ctx.toolCalls = choice.Message.ToolCalls
}

// handleToolCalls executes tool calls from the response.
// Returns responseDecideContinue (orchestrator calls finalizeTurn(turn, false)).
// The orchestrator MUST call finalizeTurn after this returns.
func (ch *ConversationHandler) handleToolCalls(ctx *responseContext) responseDecision {
	// Log raw tool calls as received from the model for debugging
	for _, tc := range ctx.toolCalls {
		ch.agent.LogToolCall(tc, "received")
	}
	ch.agent.debugLog("[tool] Executing %d tool calls\n", len(ctx.toolCalls))

	// Flush any buffered streaming content before tool execution
	if ch.agent.output.GetFlushCallback() != nil {
		ch.agent.output.GetFlushCallback()()
	}

	ch.displayIntermediateResponse(ctx.contentUsed)
	toolResults := ch.toolExecutor.ExecuteTools(ctx.toolCalls)

	// Add tool results immediately after the assistant message with tool calls
	for _, result := range toolResults {
		ch.agent.state.AddMessage(result)
	}
	ch.agent.debugLog("[ok] Added %d tool results to conversation\n", len(toolResults))

	// The model made concrete progress by executing tools, so reset
	// the tentative rejection counter — prior rejections are now stale.
	ch.tentativeRejectionCount = 0

	// Additional debugging for DeepSeek tool call format
	if strings.EqualFold(ch.agent.GetProvider(), "deepseek") {
		ch.agent.debugLog("[search] DeepSeek conversation flow check:\n")
		messages := ch.agent.state.GetMessages()
		for i, msg := range messages {
			if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
				ch.agent.debugLog("  [%d] Assistant with %d tool_calls\n", i, len(msg.ToolCalls))
			} else if msg.Role == "tool" {
				ch.agent.debugLog("  [%d] Tool response for tool_call_id: %s\n", i, msg.ToolCallId)
			}
		}
	}

	ctx.turn.ToolResults = append(ctx.turn.ToolResults, toolResults...)

	// NOTE: finalizeTurn is called by the orchestrator after this method returns.
	// updateTurnRecord is called here to record response data before finalizeTurn.
	ch.updateTurnRecord(ctx.contentUsed, ctx.toolCalls, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)

	return responseDecideContinue
}

// handleFinishReasonDispatch processes finish reason logic for both empty and non-empty finish reasons.
func (ch *ConversationHandler) handleFinishReasonDispatch(ctx *responseContext, choice api.Choice) responseDecision {
	if choice.FinishReason == "" {
		// Empty finish reason - model expects to continue
		if ch.isBlankIteration(ctx.contentUsed, ctx.toolCalls) {
			ch.agent.debugLog("[search] Blank response with no finish reason - falling through to blank iteration handling\n")
			return responseDecideFallThrough
		}
		// Not blank - check if truly incomplete
		isIncomplete := ch.responseValidator.IsIncomplete(ctx.contentUsed)
		ch.agent.debugLog("[search] IsIncomplete() result: %v\n", isIncomplete)
		if !isIncomplete {
			// Response looks complete despite no finish_reason
			if handled, stop := ch.handleOCRCompletionGate(&ctx.turn); handled {
				ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
				if stop {
					return responseDecideStop
				}
				return responseDecideContinue
			}
			ch.agent.debugLog("[OK] No finish_reason but response appears complete - accepting\n")
			ch.displayFinalResponse(ctx.contentUsed)
			ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
			return responseDecideStop
		}
		// Not blank, but incomplete
		ch.agent.debugLog("[~] No finish reason and response appears incomplete - asking model to continue\n")
		ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
		return responseDecideContinue
	}
	// Non-empty finish reason
	if shouldStop, stopReason := ch.handleFinishReason(choice.FinishReason, ctx.contentUsed); shouldStop {
		ctx.turn.GuardrailTrigger = stopReason
		if stopReason == "completion" || stopReason == "implicit completion" {
			ctx.turn.CompletionReached = true
		}
		if handled, stop := ch.handleOCRCompletionGate(&ctx.turn); handled {
			ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
			if stop {
				return responseDecideStop
			}
			return responseDecideContinue
		}
		ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
		return responseDecideStop
	}
	return responseDecideFallThrough
}

// handleBlankOrRepetitiveIteration handles blank and repetitive content checks.
func (ch *ConversationHandler) handleBlankOrRepetitiveIteration(ctx *responseContext) responseDecision {
	isBlankIteration := ch.isBlankIteration(ctx.contentUsed, ctx.toolCalls)
	isRepetitiveContent := ch.isRepetitiveContent(ctx.contentUsed)

	if isBlankIteration || isRepetitiveContent {
		ch.consecutiveBlankIterations++
		if isBlankIteration {
			ch.agent.debugLog("[WARN] Blank iteration detected (%d consecutive)\n", ch.consecutiveBlankIterations)
		} else {
			ch.agent.debugLog("[WARN] Repetitive content detected (%d consecutive)\n", ch.consecutiveBlankIterations)
		}

		if ch.consecutiveBlankIterations == 1 {
			ch.agent.debugLog("[bell] Sending reminder about next action\n")
			var reminderContent string
			if isRepetitiveContent {
				reminderContent = "You appear to be stuck in a repetitive loop. Please break out of this pattern and either:\n" +
					"1. If you are finished, provide a final summary or result\n" +
					"2. If not finished, take a concrete action (use tools) or provide a specific result\n" +
					"3. Avoid repeating the same phrases and move forward with the actual task.\n" +
					"- Focus on making actual changes or providing specific findings."
			} else {
				reminderContent = "You provided no content. If you are finished, provide a final summary or result. If not finished, continue now with your next concrete action/output.\n" +
					"- If you intend to use tools, emit valid tool_calls with proper JSON arguments.\n" +
					"- Otherwise, proceed with the actual result (not a plan)."
			}
			ch.enqueueTransientMessage(api.Message{
				Role:    "user",
				Content: reminderContent,
			})

			ctx.turn.GuardrailTrigger = "blank iteration reminder"
			ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
			return responseDecideContinue
		} else if ch.consecutiveBlankIterations >= 2 {
			ch.agent.debugLog("[FAIL] Too many consecutive blank iterations, stopping with error\n")
			errorMessage := "Error: The agent provided two consecutive blank responses and appears to be stuck. Please try rephrasing your request or break it into smaller tasks."
			ch.displayFinalResponse(errorMessage)
			ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
			return responseDecideStop
		}
	} else {
		ch.consecutiveBlankIterations = 0
	}

	return responseDecideFallThrough
}

// recordAssistantMessage appends tool calls to the turn record and
// constructs an assistant message, snapshotting tool calls so that
// tool outputs remain properly linked.
func (ch *ConversationHandler) recordAssistantMessage(ctx *responseContext) {
	ctx.turn.ToolCalls = append(ctx.turn.ToolCalls, ctx.toolCalls...)

	// Preserve tool calls (with generated IDs if needed) so tool outputs remain linked
	var toolCallsSnapshot []api.ToolCall
	if len(ctx.toolCalls) > 0 {
		toolCallsSnapshot = make([]api.ToolCall, len(ctx.toolCalls))
		copy(toolCallsSnapshot, ctx.toolCalls)
	}

	assistantMsg := api.Message{
		Role:             "assistant",
		Content:          ctx.contentUsed,
		ReasoningContent: ctx.reasoningContent,
		ToolCalls:        toolCallsSnapshot,
	}
	if assistantMsg.Role != "" {
		ch.agent.state.AddMessage(assistantMsg)
	}
}

// handleNoToolContent handles the path when the LLM response contained no tool calls.
// It delegates to the malformed handler, finish reason dispatcher, blank iteration
// handler, and final incomplete-response check. Returns the finalizeTurn result.
func (ch *ConversationHandler) handleNoToolContent(ctx *responseContext, choice api.Choice) bool {
	// If no tool_calls came back but the content suggests attempted tool usage,
	// try to parse and execute them using fallback parser
	if !ch.responseValidator.ValidateToolCalls(ctx.contentUsed) {
		return ch.handleMalformedToolCalls(ctx.contentUsed, &ctx.turn, ctx.parserErrors)
	}

	// Handle finish reason (empty or non-empty)
	if decision := ch.handleFinishReasonDispatch(ctx, choice); decision != responseDecideFallThrough {
		return ch.finalizeTurn(ctx.turn, decision == responseDecideStop)
	}

	// Handle blank/repetitive iterations
	if decision := ch.handleBlankOrRepetitiveIteration(ctx); decision != responseDecideFallThrough {
		return ch.finalizeTurn(ctx.turn, decision == responseDecideStop)
	}

	// Final check for incomplete responses
	if ch.responseValidator.IsIncomplete(ctx.contentUsed) {
		ch.agent.debugLog("[WARN] Response appears incomplete, asking model to continue\n")
		ch.handleIncompleteResponse()
		ctx.turn.GuardrailTrigger = "incomplete response reminder"
		ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
		return ch.finalizeTurn(ctx.turn, false)
	}

	// Response doesn't look incomplete — respect the model's judgment
	ch.agent.debugLog("[...] Model response continuing conversation\n")
	ch.updateTurnRecord(ctx.contentUsed, nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
	return ch.finalizeTurn(ctx.turn, false)
}

// normalizeToolCallsForExecution normalizes tool calls for execution: parses
// and repairs arguments, normalizes the Type field to "function".
func normalizeToolCallsForExecution(toolCalls []api.ToolCall) ([]api.ToolCall, []api.ToolCall) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	normalized := make([]api.ToolCall, 0, len(toolCalls))
	malformed := make([]api.ToolCall, 0)

	for _, tc := range toolCalls {
		args, repaired, err := parseToolArgumentsWithRepair(tc.Function.Arguments)
		if err != nil {
			malformed = append(malformed, tc)
			continue
		}
		if repaired {
			if encoded, marshalErr := json.Marshal(args); marshalErr == nil {
				tc.Function.Arguments = string(encoded)
			}
		}
		// Normalize Type field to "function" (required by OpenAI-compatible API schema)
		if tc.Type != "function" {
			tc.Type = "function"
		}
		normalized = append(normalized, tc)
	}

	return normalized, malformed
}
