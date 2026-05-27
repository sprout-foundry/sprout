package agent

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// OutputMode determines how output is routed
type OutputMode int

const (
	OutputModeTerminal     OutputMode = iota // CLI-only, no event bus
	OutputModeEventSourced                   // EventBus + terminal bridge
)

// OutputRouter is the single routing point for all agent output.
// Instead of dual-writing (publish event + print to terminal), all output
// flows through this router which handles both paths.
//
// Terminal output is ALWAYS produced via the streamingCallback (when set)
// or via fmt.Print (fallback). The streamingCallback is the terminal display
// — it is NOT a WebUI path. The event bus is the WebUI path.
//
// When the event bus is set, events are published for WebUI subscribers AND
// the terminal still receives its output. This is by design: the terminal
// always shows output; the WebUI optionally shows it via events.
type OutputRouter struct {
	mu                       sync.RWMutex
	mode                     OutputMode
	eventBus                 *events.EventBus
	agent                    *Agent
	reasoningTerminalEnabled bool

	// externalWriteHook fires immediately before any non-stream terminal
	// write (tool log, agent message, etc). Used by the CLI's assistant
	// turn renderer to finalize the current prose "segment" so the
	// upcoming non-prose output doesn't get reformatted away at turn-end.
	// May be nil. Caller is responsible for thread-safety inside the
	// hook (renderer uses its own mutex).
	externalWriteHook func()
}

// SetExternalWriteHook registers a callback that fires before every
// writeTerminalMessage emission. Pass nil to clear. Intended for the
// AssistantTurnRenderer in pkg/console to break its prose segment when
// chrome (tool logs / agent messages) interrupts the model's stream.
func (r *OutputRouter) SetExternalWriteHook(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.externalWriteHook = fn
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

// RouteStreamChunk routes a streaming chunk to the event bus and, when allowed,
// to terminal output.
//
// Streaming chunks are special: they represent the character-by-character
// output of the assistant's response. For terminal display, they go through
// the streamingCallback (real-time output). For WebUI, they are published as
// stream_chunk events. Reasoning chunks are published but hidden from terminal
// output unless explicitly enabled.
func (r *OutputRouter) RouteStreamChunk(chunk string, contentType string) {
	// Publish to event bus for WebUI consumption (when active)
	r.publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk, contentType))

	// Reasoning is visible to the event bus by default, but not the terminal.
	if !r.shouldRenderReasoning(contentType) {
		return
	}

	// Terminal: write via streamingCallback if set (real-time character output)
	callback, mu := r.getStreamingCallback()
	if callback != nil {
		// TryLock prevents self-deadlock if callback re-enters the output router
		locked := false
		if mu != nil {
			locked = mu.TryLock()
		}
		if locked {
			defer mu.Unlock()
		}
		callback(chunk)
		return
	}

	// Non-streaming terminal fallback: only write assistant text
	if contentType != "reasoning" {
		_, _ = os.Stdout.Write([]byte(chunk))
	}
}

// RouteAgentMessage routes an agent system message.
// category: "info", "warning", "error", "tool_log", "thought"
// RouteAgentMessage routes a message for display in both the WebUI and terminal.
func (r *OutputRouter) RouteAgentMessage(category, message string, extra map[string]interface{}) {
	// Always publish to event bus for WebUI (when active)
	r.publish(events.EventTypeAgentMessage, events.AgentMessageEvent(category, message, extra))

	// Terminal output: always write to terminal
	r.writeTerminalMessage(message)
}

// RouteTerminalOnly writes a message directly to the terminal without publishing
// to the event bus. Use this for output that is already published via a
// separate, more specific event type (e.g., subagent output lines that are
// published as subagent_activity events).
func (r *OutputRouter) RouteTerminalOnly(message string) {
	r.writeTerminalMessage(message)
}

// writeTerminalMessage writes a message to the terminal with appropriate formatting.
// It acquires the outputMutex for thread safety, then routes through the
// terminalWriter (if set for subagents), streamingCallback (if set), or prints directly.
// Uses TryLock to prevent self-deadlock when the streaming callback re-enters
// the output router (e.g., callback → PrintLine → writeTerminalMessage).
func (r *OutputRouter) writeTerminalMessage(message string) {
	if message == "" {
		return
	}

	// Fire the external-write hook BEFORE printing so the CLI assistant
	// renderer can finalize its current prose segment (and skip
	// re-rendering content that's about to be interrupted by chrome).
	// Snapshot the hook under the read lock so a concurrent SetExternalWriteHook
	// doesn't race with the call.
	r.mu.RLock()
	hook := r.externalWriteHook
	r.mu.RUnlock()
	if hook != nil {
		hook()
	}

	// Ensure newline
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	agent := r.agent

	// Route through terminalWriter if set (for subagent output).
	// This MUST happen before acquiring outputMutex because the
	// terminalWriter acquires the same mutex internally. Holding
	// outputMutex while calling terminalWriter would deadlock.
	if agent != nil && agent.output.GetTerminalWriter() != nil {
		agent.output.GetTerminalWriter()(message)
		return
	}

	// Acquire output mutex for thread-safe terminal output.
	// TryLock prevents self-deadlock if the streaming callback re-enters
	// this method (reentrant call from same goroutine).
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

	// Route through streamingCallback if available
	if agent != nil && agent.output.IsStreamingEnabled() && agent.output.GetStreamingCallback() != nil {
		agent.output.GetStreamingCallback()(message)
		return
	}

	// Direct terminal output
	if configuration.GetEnvSimple("CI_MODE") == "1" || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		_, _ = os.Stdout.Write([]byte(message))
		return
	}

	_, _ = os.Stdout.Write([]byte("\r\033[K"))
	_, _ = os.Stdout.Write([]byte(message))
}

// RouteToolLog routes a tool execution log message with iteration and context info.
//
// Terminal rendering: a glyph-prefixed dim line. The iter/context info is
// kept on the WebUI event (for the activity feed) but elided from the
// terminal — that data already lives on the status footer, and pulling it
// into every tool-log line just adds noise. Format:
//
//	→ shell_command ls -la /path
func (r *OutputRouter) RouteToolLog(action string, target string) {
	agent := r.agent

	// Calculate context usage percentage (still needed for the WebUI event).
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

	// Always publish structured event for WebUI (even without agent, for robustness).
	// WebUI keeps the full "[iter] action target" because it has the space.
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

	// Terminal output: dim arrow + target only (drop the iter/context prefix).
	// `action` ("executing tool" / "executed") is implied by the arrow glyph
	// and elided to keep the line scannable.
	const dim = "\033[2m"
	const reset = "\033[0m"

	var message string
	if target != "" {
		message = fmt.Sprintf("%s→ %s%s", dim, target, reset)
	} else {
		message = fmt.Sprintf("%s→ %s%s", dim, action, reset)
	}
	r.writeTerminalMessage(message)
}

// RouteToolCompletion emits the inline duration / outcome chip that follows
// a tool-log line. Kept separate from RouteToolLog because tool_start fires
// before the work begins and tool_end fires after — the two are paired by
// toolCallID at the call site (richEventPublisher / tool_executor).
//
// Format: `  ✓ 124ms` (indented under the prior tool-log line, dim green).
// On failure: `  ✗ 124ms — <short error>`.
func (r *OutputRouter) RouteToolCompletion(ok bool, duration time.Duration, errMsg string) {
	const dim = "\033[2m"
	const reset = "\033[0m"
	const greenDim = "\033[2;32m"
	const redDim = "\033[2;31m"

	dur := formatToolDuration(duration)
	var line string
	if ok {
		line = fmt.Sprintf("%s  ✓ %s%s", greenDim, dur, reset)
	} else {
		short := errMsg
		if len(short) > 80 {
			short = short[:77] + "..."
		}
		if short != "" {
			line = fmt.Sprintf("%s  ✗ %s%s %s— %s%s", redDim, dur, reset, dim, short, reset)
		} else {
			line = fmt.Sprintf("%s  ✗ %s%s", redDim, dur, reset)
		}
	}
	r.writeTerminalMessage(line)
}

// formatToolDuration picks a sensible unit for short tool runs: <1s → ms,
// <60s → seconds with one decimal, ≥1m → "<m>m<s>s". Keeps the chip narrow.
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
