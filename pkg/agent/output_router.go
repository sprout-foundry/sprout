package agent

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// OutputMode determines how output is routed
type OutputMode int

const (
	OutputModeTerminal     OutputMode = iota // CLI-only, no event bus
	OutputModeEventSourced                   // EventBus + terminal bridge
)

// OutputRouter is the single routing point for all agent output. Routes to event bus (WebUI) and/or terminal.
type OutputRouter struct {
	mu                       sync.RWMutex
	mode                     OutputMode
	eventBus                 *events.EventBus
	agent                    *Agent
	reasoningTerminalEnabled bool

	// externalWriteHook fires before non-stream terminal writes to finalize the prose segment. May be nil.
	externalWriteHook func()

	// terminalSubscriberActive: when true, a terminal subscriber owns agent_message rendering, so skip the raw write fallback.
	terminalSubscriberActive bool

	// reasoningCallback: dedicated sink for reasoning chunks so the CLI can render a collapsed header.
	reasoningCallback func(string)
}

// SetExternalWriteHook registers a callback that fires before every writeTerminalMessage emission. Pass nil to clear.
func (r *OutputRouter) SetExternalWriteHook(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.externalWriteHook = fn
}

// FlushExternalWrite fires the external-write hook if one is registered. Used by the terminal subscriber to flush prose before tool chrome.
func (r *OutputRouter) FlushExternalWrite() {
	r.mu.RLock()
	hook := r.externalWriteHook
	r.mu.RUnlock()
	if hook != nil {
		hook()
	}
}

// SetTerminalSubscriberActive marks whether a terminal subscriber owns agent_message rendering. When true, skip the raw write fallback.
func (r *OutputRouter) SetTerminalSubscriberActive(active bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.terminalSubscriberActive = active
}

// TerminalSubscriberActive reports whether a terminal subscriber owns terminal rendering. Used to suppress duplicate output.
func (r *OutputRouter) TerminalSubscriberActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.terminalSubscriberActive
}

// SetReasoningCallback registers a dedicated sink for reasoning chunks so the CLI can render a collapsed header. Pass nil to clear.
func (r *OutputRouter) SetReasoningCallback(fn func(string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reasoningCallback = fn
}

// getReasoningCallback returns the current reasoning callback under a
// read lock. Separated from SetReasoningCallback so RouteStreamChunk
// stays lock-free on the hot path.
func (r *OutputRouter) getReasoningCallback() func(string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.reasoningCallback
}

// NewOutputRouter creates an output router.
// If eventBus is nil, operates in terminal-only mode.
// agent may be nil during early initialization; set it later via the field directly.
func NewOutputRouter(agent *Agent, eventBus *events.EventBus) *OutputRouter {
	mode := OutputModeTerminal
	if eventBus != nil {
		mode = OutputModeEventSourced
	}
	return &OutputRouter{
		mode:     mode,
		eventBus: eventBus,
		agent:    agent,
	}
}

// SetReasoningTerminalEnabled controls whether reasoning chunks are rendered in the terminal.
// It is disabled by default so reasoning stays available to the event bus/WebUI without
// polluting normal CLI output.
func (r *OutputRouter) SetReasoningTerminalEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reasoningTerminalEnabled = enabled
}

// SetEventBus updates the event bus (called when webui connects/disconnects).
// The streamingCallback on the agent is NOT affected — it always routes to
// the terminal regardless of WebUI state.
func (r *OutputRouter) SetEventBus(eventBus *events.EventBus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if eventBus != nil {
		r.mode = OutputModeEventSourced
	} else {
		r.mode = OutputModeTerminal
	}
	r.eventBus = eventBus
}

// Mode returns the current output mode
func (r *OutputRouter) Mode() OutputMode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mode
}

// Write implements io.Writer so OutputRouter can be used directly as the
// OutputWriter in tools.ToolEnv. It buffers partial lines and flushes them
// on newline boundaries via the agent's PrintLineAsync, avoiding the need
// to allocate a separate outputRouter wrapper per tool call.
func (r *OutputRouter) Write(p []byte) (int, error) {
	if r == nil {
		return os.Stdout.Write(p)
	}
	agent := r.agent
	if agent == nil {
		return os.Stdout.Write(p)
	}
	buf := bytes.NewBuffer(p)
	for {
		line, err := buf.ReadString('\n')
		if err == nil {
			agent.PrintLineAsync(strings.TrimRight(line, "\n"))
		} else {
			// Partial line remaining — discard; it'll arrive on the next Write
			break
		}
	}
	return len(p), nil
}

// hasEventBus returns true if the router has an active event bus for publishing.
func (r *OutputRouter) hasEventBus() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.eventBus != nil
}

// getStreamingCallback returns the current streaming callback and its mutex (if any).
func (r *OutputRouter) getStreamingCallback() (func(string), *sync.Mutex) {
	agent := r.agent
	if agent == nil {
		return nil, nil
	}
	if agent.output.IsStreamingEnabled() && agent.output.GetStreamingCallback() != nil {
		return agent.output.GetStreamingCallback(), agent.output.GetOutputMutex()
	}
	return nil, agent.output.GetOutputMutex()
}

// publish publishes an event to the event bus (no-op if nil/bus unavailable)
func (r *OutputRouter) publish(eventType string, data interface{}) {
	r.mu.RLock()
	bus := r.eventBus
	agent := r.agent
	r.mu.RUnlock()
	if bus == nil {
		return
	}
	if agent != nil {
		data = agent.decorateEventPayload(data)
	}
	bus.Publish(eventType, data)
}

// shouldRenderReasoning reports whether reasoning chunks should reach terminal output.
func (r *OutputRouter) shouldRenderReasoning(contentType string) bool {
	if contentType != "reasoning" {
		return true
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.reasoningTerminalEnabled
}

// RouteStreamChunk routes a streaming chunk to the event bus and, when allowed, to terminal output.
func (r *OutputRouter) RouteStreamChunk(chunk string, contentType string) {
	r.publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk, contentType))

	// Reasoning chunks: route to the dedicated reasoning callback first, regardless of reasoningTerminalEnabled.
	if contentType == "reasoning" {
		if reasoningCb := r.getReasoningCallback(); reasoningCb != nil {
			reasoningCb(chunk)
			return
		}
	}

	if !r.shouldRenderReasoning(contentType) {
		return
	}

	// Terminal: write via streamingCallback if set. Callback acquires its own mutex — don't hold ours (self-deadlock).
	callback, _ := r.getStreamingCallback()
	if callback != nil {
		callback(chunk)
		return
	}

	// Non-streaming terminal fallback: only write assistant text
	if contentType != "reasoning" {
		_, _ = os.Stdout.Write([]byte(chunk))
	}
}

// RouteAgentMessage routes an agent system message to both WebUI (via event bus) and terminal.
func (r *OutputRouter) RouteAgentMessage(category, message string, extra map[string]interface{}) {
	r.publish(events.EventTypeAgentMessage, events.AgentMessageEvent(category, message, extra))

	// When a browser is connected, suppress tool_log and thought on the terminal to avoid duplication.
	if category == "tool_log" || category == "thought" {
		if r.agent != nil && !r.agent.IsSubagent() && r.agent.HasActiveWebUIClients() {
			return
		}
	}

	// When a terminal subscriber owns rendering, skip the raw write to avoid double-printing.
	r.mu.RLock()
	subscriberActive := r.terminalSubscriberActive
	r.mu.RUnlock()
	if subscriberActive && category != "tool_log" {
		return
	}

	r.writeTerminalMessage(message)
}

// RouteTerminalOnly writes a message directly to the terminal without publishing to the event bus.
func (r *OutputRouter) RouteTerminalOnly(message string) {
	r.writeTerminalMessage(message)
}

// writeTerminalMessage writes a message to the terminal. Uses TryLock to prevent self-deadlock on reentrant calls.
func (r *OutputRouter) writeTerminalMessage(message string) {
	if message == "" {
		return
	}

	// Fire the external-write hook BEFORE printing so the CLI assistant renderer can finalize its prose segment.
	r.mu.RLock()
	hook := r.externalWriteHook
	r.mu.RUnlock()
	if hook != nil {
		hook()
	}

	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	agent := r.agent

	// Route through terminalWriter if set (for subagent output). Must happen before acquiring outputMutex (deadlock).
	if agent != nil && agent.output.GetTerminalWriter() != nil {
		agent.output.GetTerminalWriter()(message)
		return
	}

	// TryLock prevents self-deadlock if the streaming callback re-enters this method.
	var mu *sync.Mutex
	if agent != nil {
		mu = agent.output.GetOutputMutex()
	}
	locked := false
	if mu != nil {
		locked = mu.TryLock()
	}
	if locked {
		defer mu.Unlock()
	}

	// Direct terminal output. No \r\033[K prefix — the externalWriteHook already reset the renderer.
	_, _ = os.Stdout.Write([]byte(message))
}

// RouteToolLog routes a tool execution log message. Terminal output is handled by the terminal subscriber; this publishes the WebUI event.
func (r *OutputRouter) RouteToolLog(action string, target string) {
	agent := r.agent

	var contextPercent string
	var currentIter int
	if agent != nil {
		currentIter = agent.state.GetCurrentIteration()
		if agent.state.GetMaxContextTokens() > 0 && agent.state.GetCurrentContextTokens() > 0 {
			percentage := float64(agent.state.GetCurrentContextTokens()) / float64(agent.state.GetMaxContextTokens()) * 100
			contextPercent = fmt.Sprintf(" - %.0f%%", percentage)
		}
	}
	iterInfo := fmt.Sprintf("[%d%s]", currentIter, contextPercent)

	// Publish structured event for WebUI.
	extra := map[string]interface{}{
		"action":    action,
		"target":    target,
		"iteration": currentIter,
		"context":   contextPercent,
	}
	if target != "" {
		r.publish(events.EventTypeAgentMessage, events.AgentMessageEvent("tool_log", fmt.Sprintf("%s %s %s", iterInfo, action, target), extra))
	} else {
		r.publish(events.EventTypeAgentMessage, events.AgentMessageEvent("tool_log", fmt.Sprintf("%s %s", iterInfo, action), extra))
	}
}

// RouteToolCompletion emits the inline duration/outcome chip for the WebUI. Terminal output is handled by the subscriber.
func (r *OutputRouter) RouteToolCompletion(ok bool, duration time.Duration, errMsg string) {
	dur := formatToolDuration(duration)
	var msg string
	if ok {
		msg = fmt.Sprintf("✓ %s", dur)
	} else {
		short := errMsg
		if len(short) > 80 {
			short = short[:77] + "..."
		}
		if short != "" {
			msg = fmt.Sprintf("✗ %s — %s", dur, short)
		} else {
			msg = fmt.Sprintf("✗ %s", dur)
		}
	}
	r.publish(events.EventTypeAgentMessage, events.AgentMessageEvent("tool_log", msg, nil))
}

// formatToolDuration picks a sensible unit: <1s → ms, <60s → seconds, ≥1m → "m:ss".
func formatToolDuration(d time.Duration) string {
	if d < time.Second {
		ms := d.Milliseconds()
		if ms < 1 {
			return "<1ms"
		}
		return fmt.Sprintf("%dms", ms)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", m, s)
}

// isEventSourced returns true if the router is in event-sourced mode
func (m OutputMode) isEventSourced() bool {
	return m == OutputModeEventSourced
}
