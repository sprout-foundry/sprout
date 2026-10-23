package agent

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/validation"
)

// publishEvent publishes an event to the event bus if available
func (a *Agent) publishEvent(eventType string, data interface{}) {
	if a.eventBus != nil {
		a.eventBus.Publish(eventType, a.decorateEventPayload(data))
	}
}

// GetChatID returns the chat session this agent is bound to (WebUI event
// metadata "chat_id"), or "". Shell tools use it to scope hidden and
// background PTY sessions per conversation so multi-chat daemons don't
// share one shell. Nil-safe for bare Agents constructed in tests.
func (a *Agent) GetChatID() string {
	if a == nil || a.output == nil {
		return ""
	}
	mu := a.output.GetEventMetadataMutex()
	if mu == nil {
		return ""
	}
	mu.RLock()
	defer mu.RUnlock()
	meta := a.output.GetEventMetadataUnlocked()
	if v, ok := meta["chat_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// decorateEventPayload merges event metadata into the payload if present.
func (a *Agent) decorateEventPayload(data interface{}) interface{} {
	if data == nil {
		return data
	}

	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadataUnlocked()
	if len(eventMetadata) == 0 {
		return data
	}

	payload, ok := data.(map[string]interface{})
	if !ok {
		return data
	}

	cloned := make(map[string]interface{}, len(payload)+len(eventMetadata))
	for k, v := range payload {
		cloned[k] = v
	}
	for k, v := range eventMetadata {
		if _, exists := cloned[k]; !exists {
			// Resolve function values to their return value
			if fn, ok := v.(func() string); ok {
				resolved := fn()
				cloned[k] = resolved
			} else {
				cloned[k] = v
			}
		}
	}
	return cloned
}

// PublishQueryProgress publishes query progress for real-time updates
func (a *Agent) PublishQueryProgress(message string, iteration int, tokensUsed int) {
	a.publishEvent(events.EventTypeQueryProgress, events.QueryProgressEvent(message, iteration, tokensUsed))
}

// PublishToolStart publishes a rich tool start event
func (a *Agent) PublishToolStart(toolName, toolCallID, arguments, displayName, persona string, isSubagent bool, subagentType string, toolIndex int) {
	a.publishEvent(events.EventTypeToolStart, events.ToolStartEvent(toolName, toolCallID, arguments, displayName, persona, isSubagent, subagentType, toolIndex))
}

// PublishToolEnd publishes a rich tool end event
func (a *Agent) PublishToolEnd(toolCallID, toolName, status, result, errorMessage string, duration time.Duration) {
	a.publishEvent(events.EventTypeToolEnd, events.ToolEndEvent(toolCallID, toolName, status, result, errorMessage, duration))
}

// PublishTodoUpdate publishes a structured todo update event
func (a *Agent) PublishTodoUpdate(todos []map[string]interface{}) {
	a.publishEvent(events.EventTypeTodoUpdate, events.TodoUpdateEvent(todos))
}

// PublishFileChange emits a file_changed event for ChangeTracker-detected mutations.
func (a *Agent) PublishFileChange(filePath, action, content string) {
	a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(filePath, action, content))
}

// PublishAgentMessage publishes a structured agent system message event.
func (a *Agent) PublishAgentMessage(category, message string, extra map[string]interface{}) {
	a.publishEvent(events.EventTypeAgentMessage, events.AgentMessageEvent(category, message, extra))
}

// PublishCompactStarted emits a compact_started event with diagnostic
// fields describing the conversation state at the moment compaction
// begins. source is the path: "manual" (slash command) or
// "auto_llm_summary" (seed structural compaction).
func (a *Agent) PublishCompactStarted(source string, messageCount, checkpointCount int) {
	a.publishEvent(events.EventTypeCompactStarted, events.CompactStartedEvent(source, messageCount, checkpointCount))
}

// PublishCompactCompleted emits a compact_completed event with the
// result of the compaction. Pass nil err on success.
func (a *Agent) PublishCompactCompleted(source string, beforeCount, afterCount, summaryChars int, err error) {
	a.publishEvent(events.EventTypeCompactCompleted, events.CompactCompletedEvent(source, beforeCount, afterCount, summaryChars, err))
}

// PublishContextManagementDiagnostic emits the per-iteration context-budget snapshot.
// Emits both the effective max (post-cap) and the native max (pre-cap).
func (a *Agent) PublishContextManagementDiagnostic(currentTokens, maxTokens, iteration, messageCount, cachedTokens, promptTokens, cacheWriteTokens int) {
	nativeMax := a.getNativeModelContextLimit()
	a.publishEvent(
		events.EventTypeContextManagementDiagnostic,
		events.ContextManagementDiagnosticEvent(
			currentTokens,
			maxTokens,
			nativeMax,
			a.compactionTriggerFractionFor(a.getModelContextLimit()), reservedForResponseFraction,
			reservedForThinkingFraction,
			reservedForToolIOFraction,
			iteration,
			messageCount,
			cachedTokens,
			promptTokens,
			cacheWriteTokens,
		),
	)
}

// PublishRateLimited emits a rate_limited event so the WebUI can show
// "rate-limited, retrying…" and gate the input until the backoff elapses.
func (a *Agent) PublishRateLimited(ev *events.RateLimitedEvent) {
	if ev != nil {
		a.publishEvent(events.EventTypeRateLimited, ev)
	}
}

// publishRetryEvent emits metrics updates for retry events with the
// error category label. This is called from the provider retry loop
// in seed_provider.go on each retry attempt. willRetry reports whether
// another attempt will actually follow (false on the final failure).
func (a *Agent) publishRetryEvent(err error, attempt, maxRetries int, provider string, willRetry bool) {
	if err == nil || a.eventBus == nil {
		return
	}
	// Determine the error category for the metrics label.
	category := "unknown"
	if te := agenterrors.AsTypedError(err); te != nil {
		category = string(te.Code)
	} else if cat, ok := agenterrors.GetCategory(err); ok {
		category = cat.String()
	}

	// Emit a metrics update with the category so the status footer can
	// show "rate-limited, retrying…" vs "provider error, retrying…"
	a.publishEvent(
		events.EventTypeMetricsUpdate,
		events.MetricsUpdateEventWithCategory(
			provider,
			a.GetModel(),
			a.state.GetTotalTokens(),
			a.state.GetCurrentContextTokens(),
			a.getModelContextLimit(),
			a.state.GetCurrentIteration(),
			a.state.GetTotalCost(),
			category,
		),
	)

	// Also publish a rate_limited event if this is a rate limit error.
	if agenterrors.IsRateLimited(err) {
		a.PublishRateLimited(&events.RateLimitedEvent{
			Provider:    provider,
			Attempt:     attempt,
			MaxAttempts: maxRetries,
			Message:     err.Error(),
		})
	}

	// Visible per-attempt notice. The metrics event above only reaches the
	// status footer; during subagent runs a retry loop can otherwise churn
	// for many minutes with no terminal output at all. The terminal
	// subscriber renders non-tool_log categories mid-turn via PrintExternal
	// (steer-panel safe); tool_log is suppressed there.
	msg := retryNoticeMessage(err)
	status := "retrying"
	if !willRetry {
		status = "giving up"
	}
	a.publishEvent(
		events.EventTypeAgentMessage,
		events.AgentMessageEvent("provider_retry",
			fmt.Sprintf("LLM request failed (%s), %s attempt %d/%d: %s", category, status, attempt+1, maxRetries+1, msg), nil),
	)
}

// retryNoticeMessage condenses a provider error for the one-line retry notice.
//
// A provider failure arrives as a chain of nested TypedErrors, each repeating
// its own "[code] message:" prefix, so the raw text spends most of a line on
// boilerplate and the leaf cause — the part that says what actually broke —
// falls past the truncation point. The notice keeps the deepest cause and as
// much of the outermost message as fits, which is the pair a reader needs, and
// drops the local address from a net.OpError since the ephemeral port
// identifies nothing.
func retryNoticeMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// The leaf cause carries the actionable detail ("connection reset by
	// peer", "no such host"); the outer messages only name the layer.
	var leaf string
	for e := errors.Unwrap(err); e != nil; e = errors.Unwrap(e) {
		leaf = e.Error()
	}
	if leaf != "" && !strings.Contains(msg, leaf) {
		msg = msg + ": " + leaf
	}

	// "read tcp 192.168.1.134:43440->104.21.37.56:443" — the local half is
	// noise; the remote address is what identifies the peer.
	msg = localAddrPattern.ReplaceAllString(msg, "$1 ")

	return truncateKeepingCause(msg, leaf)
}

const retryNoticeMaxLen = 160

// truncateKeepingCause bounds msg to retryNoticeMaxLen bytes without cutting
// the cause out of it. The cause is the reason the notice exists, so when the
// two cannot both fit, the leading context is shortened instead.
func truncateKeepingCause(msg, cause string) string {
	if len(msg) <= retryNoticeMaxLen {
		return msg
	}
	if cause == "" || len(cause) >= retryNoticeMaxLen {
		return truncateRunes(msg, retryNoticeMaxLen)
	}
	// Reserve room for the cause plus the "…: " joiner.
	head := retryNoticeMaxLen - len(cause) - len("…: ")
	if head < 0 {
		return truncateRunes(msg, retryNoticeMaxLen)
	}
	prefix := truncateRunes(msg, head)
	return prefix + "…: " + cause
}

// truncateRunes cuts s to at most n bytes without splitting a UTF-8 rune.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// localAddrPattern matches the "<proto> <local>->" half of a net.OpError
// address pair, keeping the protocol word and the remote address.
var localAddrPattern = regexp.MustCompile(`\b(\w+) [\d.]+:\d+->`)

// PublishEvent publishes an event through the agent's event bus with metadata decoration.
func (a *Agent) PublishEvent(eventType string, data interface{}) {
	a.publishEvent(eventType, data)
}

// SetEventBus sets the event bus for real-time UI updates and initializes the validator
func (a *Agent) SetEventBus(eventBus *events.EventBus) {
	a.eventBus = eventBus
	if a.OutputRouter() != nil {
		a.OutputRouter().SetEventBus(eventBus)
	} else {
		a.output.SetOutputRouter(NewOutputRouter(a, eventBus))
	}
	// Initialize validator for syntax checking and async diagnostics
	a.validator = validation.NewValidator(eventBus)
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	em := a.output.GetEventMetadataUnlocked()
	if len(em) > 0 {
		a.validator.SetEventMetadata(em)
	}
	mu.RUnlock()
}

// GetEventBus returns the current event bus
func (a *Agent) GetEventBus() *events.EventBus {
	return a.eventBus
}

// SetEventMetadata attaches metadata that should be merged into all emitted UI events.
func (a *Agent) SetEventMetadata(metadata map[string]interface{}) {
	mu := a.output.GetEventMetadataMutex()
	mu.Lock()
	defer mu.Unlock()
	if len(metadata) == 0 {
		a.output.SetEventMetadataUnlocked(nil)
		if a.validator != nil {
			a.validator.SetEventMetadata(nil)
		}
		return
	}
	cloned := make(map[string]interface{}, len(metadata))
	for k, v := range metadata {
		cloned[k] = v
	}
	a.output.SetEventMetadataUnlocked(cloned)
	if a.validator != nil {
		a.validator.SetEventMetadata(cloned)
	}
}

// MergeEventMetadata adds extras to the current event metadata without discarding existing keys.
func (a *Agent) MergeEventMetadata(extras map[string]interface{}) {
	if len(extras) == 0 {
		return
	}
	existing := a.output.GetEventMetadata()
	merged := make(map[string]interface{}, len(existing)+len(extras))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range extras {
		merged[k] = v
	}
	a.SetEventMetadata(merged)
}

// GetEventClientID returns the bound client_id from event metadata, if present.
func (a *Agent) GetEventClientID() string {
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadataUnlocked()
	if len(eventMetadata) == 0 {
		return ""
	}
	if clientID, ok := eventMetadata["client_id"].(string); ok {
		return strings.TrimSpace(clientID)
	}
	return ""
}

// GetEventChatID returns the bound chat_id from event metadata, if present.
func (a *Agent) GetEventChatID() string {
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadataUnlocked()
	if len(eventMetadata) == 0 {
		return ""
	}
	if chatID, ok := eventMetadata["chat_id"].(string); ok {
		return strings.TrimSpace(chatID)
	}
	return ""
}

// GetEventUserID returns the bound user_id from event metadata, if present.
func (a *Agent) GetEventUserID() string {
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadataUnlocked()
	if len(eventMetadata) == 0 {
		return ""
	}
	if userID, ok := eventMetadata["user_id"].(string); ok {
		return strings.TrimSpace(userID)
	}
	return ""
}
