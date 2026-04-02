package agent

import (
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/trace"
)

// handleMalformedToolCalls attempts to parse and execute tool calls from malformed content
func (ch *ConversationHandler) handleMalformedToolCalls(content string, turn TurnEvaluation, parserErrors []string) bool {
	ch.agent.debugLog("[tool] Attempting to parse malformed tool calls from content\n")

	// Defensive nil check for fallbackParser
	if ch.fallbackParser == nil {
		ch.agent.debugLog("[WARN] Fallback parser is nil, cannot parse malformed tool calls\n")
		turn.GuardrailTrigger = "fallback parser unavailable"

		// Update turn record without fallback usage
		ch.updateTurnRecord(content, nil, append(parserErrors, "fallback parser unavailable"), false, "")
		return false // Continue conversation to allow model to issue proper tool_calls
	}

	fallbackResult := ch.fallbackParser.Parse(content)
	if fallbackResult == nil || len(fallbackResult.ToolCalls) == 0 {
		ch.agent.debugLog("[WARN] Fallback parser could not extract valid tool calls\n")
		turn.GuardrailTrigger = "fallback parser failed"

		// Update turn record without fallback success
		ch.updateTurnRecord(content, nil, append(parserErrors, "fallback parser failed"), false, "")
		return false // Continue conversation to allow model to issue proper tool_calls
	}

	ch.agent.debugLog("[tool] Successfully parsed %d tool calls from malformed content\n", len(fallbackResult.ToolCalls))

	// Generate IDs for parsed tool calls
	for i := range fallbackResult.ToolCalls {
		if fallbackResult.ToolCalls[i].ID == "" {
			fallbackResult.ToolCalls[i].ID = ch.toolExecutor.GenerateToolCallID(fallbackResult.ToolCalls[i].Function.Name)
		}
	}

	// Update the assistant message with cleaned content and parsed tool calls
	if len(ch.agent.messages) > 0 {
		ch.agent.messages[len(ch.agent.messages)-1].Content = fallbackResult.CleanedContent
		ch.agent.messages[len(ch.agent.messages)-1].ToolCalls = fallbackResult.ToolCalls
	}

	// Execute the parsed tool calls
	toolResults := ch.toolExecutor.ExecuteTools(fallbackResult.ToolCalls)
	ch.agent.messages = append(ch.agent.messages, toolResults...)
	ch.agent.debugLog("[ok] Executed %d fallback-parsed tool calls\n", len(toolResults))

	// The model made concrete progress by executing tools via fallback, so reset
	// the tentative rejection counter — prior rejections are now stale.
	ch.tentativeRejectionCount = 0

	turn.ToolCalls = append(turn.ToolCalls, fallbackResult.ToolCalls...)
	turn.ToolResults = append(turn.ToolResults, toolResults...)
	turn.GuardrailTrigger = "fallback parser success"

	// Update turn record with fallback parser usage
	fallbackOutputStr := ""
	if fallbackResult.CleanedContent != "" {
		fallbackOutputStr = fallbackResult.CleanedContent
	}
	ch.updateTurnRecord(content, fallbackResult.ToolCalls, parserErrors, true, fallbackOutputStr)

	return false // Continue conversation
}

// updateTurnRecord updates the current turn record with response data and persists it
func (ch *ConversationHandler) updateTurnRecord(rawResponse string, parsedToolCalls []api.ToolCall, parserErrors []string, fallbackUsed bool, fallbackOutput string) {
	// Only proceed if tracing is enabled and we have a current turn record
	if ch.traceSession == nil || ch.currentTurnRecord == nil {
		return
	}

	// Type assert to trace session with RecordTurn method
	type traceSessionInterface interface {
		RecordTurn(record trace.TurnRecord) error
	}

	traceSession, ok := ch.traceSession.(traceSessionInterface)
	if !ok {
		// Not a valid trace session, skip recording
		ch.agent.debugLog("DEBUG: traceSession is not a valid trace session, skipping turn update\n")
		return
	}

	// Update the turn record with response data
	ch.currentTurnRecord.RawResponse = rawResponse
	if parsedToolCalls != nil {
		ch.currentTurnRecord.ParsedToolCalls = parsedToolCalls
	}
	if len(parserErrors) > 0 {
		ch.currentTurnRecord.ParserErrors = parserErrors
	}
	ch.currentTurnRecord.FallbackUsed = fallbackUsed
	if fallbackOutput != "" {
		ch.currentTurnRecord.FallbackOutput = fallbackOutput
	}

	// Record the updated turn
	if err := traceSession.RecordTurn(*ch.currentTurnRecord); err != nil {
		ch.agent.debugLog("DEBUG: Failed to update turn record: %v\n", err)
	}
}
