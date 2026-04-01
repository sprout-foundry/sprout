package agent

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/alantheprice/ledit/pkg/events"
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
	if agent.streamingEnabled && agent.streamingCallback != nil {
		return agent.streamingCallback, agent.outputMutex
	}
	return nil, agent.outputMutex
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
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		callback(chunk)
		return
	}

	// Non-streaming terminal fallback: only write assistant text
	if contentType != "reasoning" {
		fmt.Print(chunk)
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
// streamingCallback (if set) or prints directly.
func (r *OutputRouter) writeTerminalMessage(message string) {
	if message == "" {
		return
	}

	// Ensure newline
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	// Acquire output mutex for thread-safe terminal output
	agent := r.agent
	var mu *sync.Mutex
	if agent != nil {
		mu = agent.outputMutex
	}
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}

	// Route through streamingCallback if available (still under mutex for ordering)
	if agent != nil && agent.streamingEnabled && agent.streamingCallback != nil {
		agent.streamingCallback(message)
		return
	}

	// Direct terminal output
	if os.Getenv("LEDIT_CI_MODE") == "1" || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		fmt.Print(message)
		return
	}

	fmt.Print("\r\033[K")
	fmt.Print(message)
}

// RouteToolLog routes a tool execution log message with iteration and context info.
func (r *OutputRouter) RouteToolLog(action string, target string) {
	agent := r.agent

	// Calculate context usage percentage
	var contextPercent string
	var currentIter int
	if agent != nil {
		currentIter = agent.currentIteration
		if agent.maxContextTokens > 0 && agent.currentContextTokens > 0 {
			percentage := float64(agent.currentContextTokens) / float64(agent.maxContextTokens) * 100
			contextPercent = fmt.Sprintf(" - %.0f%%", percentage)
		}
	}
	iterInfo := fmt.Sprintf("[%d%s]", currentIter, contextPercent)

	// Always publish structured event for WebUI (even without agent, for robustness)
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

	// Terminal output: format with ANSI colors
	const darkGray = "\033[90m"
	const slightlyLighterGray = "\033[38;5;246m"
	const reset = "\033[0m"

	var message string
	if target != "" {
		message = fmt.Sprintf("%s%s %s%s %s%s%s", darkGray, iterInfo, action, reset, slightlyLighterGray, target, reset)
	} else {
		message = fmt.Sprintf("%s%s %s%s", darkGray, iterInfo, action, reset)
	}
	r.writeTerminalMessage(message)
}

// isEventSourced returns true if the router is in event-sourced mode
func (m OutputMode) isEventSourced() bool {
	return m == OutputModeEventSourced
}
