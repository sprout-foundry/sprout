//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// Tier 2b agent bridge. Where runChat issues a single LLM call, runAgent
// runs the full sprout agent loop: multi-turn conversation, tool calls,
// system prompt, persona, etc. — the same loop native `sprout agent`
// drives. The only WASM-specific accommodation is that we skip the
// interactive provider-resolution dance and let the JS host pick
// provider/model directly (matching runChat's contract).
//
// Tool execution under WASM is constrained: shell tools, MCP, and other
// process-spawning tools no-op or error out. SP-045-4e tracks the work
// to route shell-like tools through SproutWasm.executeCommand so the
// agent can edit files and run a curated set of commands inside MEMFS.

// persistentAgent caches a single Agent instance across runAgent calls so
// that conversation history accumulates between turns (multi-turn chat).
// Without this, every call to runAgentFunc creates a fresh Agent and the
// model has no memory of previous messages in the conversation.
//
// The cache key is the provider name — if the caller switches providers,
// a new agent is created and the old one is replaced.
//
// Access is guarded by persistentAgentMu to prevent races when multiple
// runAgent calls arrive concurrently (rapid user messages, steer, etc.).
var (
	persistentAgentMu sync.Mutex
	persistentAgent   *agent.Agent
	persistentAgentPv string // provider name the cached agent was built for
	persistentErrCnt  int    // consecutive ProcessQuery errors; reset on success
)

// maxConsecutiveErrors is the threshold at which the cached agent is
// invalidated. Transient errors (network, rate-limit) are fine to retry
// on the same agent, but repeated failures suggest state corruption.
const maxConsecutiveErrors = 3

// agentTimeout is the maximum duration for a single runAgent call.
// The default is 20 minutes for complex multi-tool-call workflows.
// Can be overridden via SPROUT_WASM_AGENT_TIMEOUT env var (in minutes).
var agentTimeout = func() time.Duration {
	if m := os.Getenv("SPROUT_WASM_AGENT_TIMEOUT"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return 20 * time.Minute
}()

// resetPersistentAgent clears the cached agent. Called when the JS side
// wants to start a fresh conversation (new chat session).
func resetPersistentAgent() {
	persistentAgentMu.Lock()
	defer persistentAgentMu.Unlock()
	persistentAgent = nil
	persistentAgentPv = ""
}

func agentJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"runAgent":          js.FuncOf(runAgentFunc),
		"runPlan":           js.FuncOf(runPlanFunc),
		"clearConversation": js.FuncOf(clearConversationFunc),
		"stopAgent":         js.FuncOf(stopAgentFunc),
		"steerAgent":        js.FuncOf(steerAgentFunc),
	}
}

// clearConversationFunc resets the persistent agent so the next runAgent
// call starts a fresh conversation with no history. Called from JS when
// the user starts a new chat session or clears the conversation.
func clearConversationFunc(_ js.Value, _ []js.Value) interface{} {
	resetPersistentAgent()
	return nil
}

// stopAgentFunc interrupts the currently running agent loop (if any).
// This is the cloud-mode equivalent of the stop button — it cancels the
// agent's interrupt context so any in-flight HTTP requests and tool
// executions abort promptly.
func stopAgentFunc(_ js.Value, _ []js.Value) interface{} {
	persistentAgentMu.Lock()
	ag := persistentAgent
	persistentAgentMu.Unlock()
	if ag != nil {
		ag.TriggerInterrupt()
	}
	return nil
}

// steerAgentFunc injects a steering message into the persistent agent's
// steering channel. If the agent is mid-turn, the message is queued and
// delivered as a follow-up prompt after the current turn completes.
// This is the cloud-mode equivalent of the steer input field.
func steerAgentFunc(_ js.Value, args []js.Value) interface{} {
	message := argString(args, 0, "")
	if message == "" {
		return map[string]interface{}{"steered": false, "error": "message is required"}
	}
	persistentAgentMu.Lock()
	ag := persistentAgent
	persistentAgentMu.Unlock()
	if ag == nil {
		return map[string]interface{}{"steered": false, "error": "no active agent"}
	}
	ag.InjectInputContext(message)
	return map[string]interface{}{"steered": true}
}

// runAgentFunc invokes one ProcessQuery turn through a persistent
// Agent. Inputs:
//
//	args[0] (string)  — provider name (matches runChat's argument 0)
//	args[1] (string)  — model id (pass "" for the provider's default)
//	args[2] (string)  — user query / prompt
//	args[3] (func?)   — onEvent(jsonString) callback for streamed UI events
//
// Returns a Promise resolving to:
//
//	{
//	  response: string,    // the agent's final response text
//	  provider: string,
//	  model:    string,    // model actually used (after factory substitution)
//	}
//
// When args[3] is a function, the agent's EventBus is wired up and every
// event flowing through it (tool_start, tool_end, query_progress,
// stream_chunk, agent_message, error, etc.) is forwarded to the callback
// as a JSON-stringified UIEvent. The callback is invoked from a worker
// goroutine — JS callbacks under Go-WASM are themselves synchronous, so
// no extra plumbing is needed, but heavy work should be deferred to a
// microtask on the JS side.
//
// The agent is cached across calls (keyed by provider) so conversation
// history accumulates between turns. Call clearConversation() to reset.
//
// Timeout: 10 minutes per call. Long agent loops with many tool calls
// will hit this — open an issue if it bites and we'll make it
// configurable.
func runAgentFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	query := argString(args, 2, "")

	var onEvent js.Value
	if len(args) > 3 && args[3].Type() == js.TypeFunction {
		onEvent = args[3]
	}

	return asPromiseWithTimeout(agentTimeout, func(ctx context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required (first arg)")
		}
		if query == "" {
			return nil, fmt.Errorf("query is required (third arg)")
		}

		// Reuse the cached agent when the provider matches, so the
		// conversation history carries over turn-to-turn. A provider
		// change (or nil cache) forces a rebuild.
		persistentAgentMu.Lock()
		ag := persistentAgent
		needsRebuild := ag == nil || persistentAgentPv != provider
		persistentAgentMu.Unlock()

		if needsRebuild {
			var err error
			client, err := factory.CreateProviderClient(api.ClientType(provider), model)
			if err != nil {
				return nil, fmt.Errorf("create client: %w", err)
			}
			injectWasmStreamingClient(client)

			configMgr, err := configuration.NewManagerSilent()
			if err != nil {
				return nil, fmt.Errorf("init configuration: %w", err)
			}

			ag, err = agent.NewAgentWithClient(client, api.ClientType(provider), configMgr)
			if err != nil {
				return nil, fmt.Errorf("init agent: %w", err)
			}

			// Enable streaming so doChatOnce takes the streaming path
			// (doChatStream), which publishes stream_chunk events through
			// the EventBus. Without this, the agent uses doChatNonStream
			// which buffers the entire response and only publishes it at
			// query_completed — the browser sees nothing until the full
			// response is ready.
			ag.SetStreamingEnabled(true)

			persistentAgentMu.Lock()
			persistentAgent = ag
			persistentAgentPv = provider
			persistentAgentMu.Unlock()
		}

		// Wire the event bus only when JS provided a sink — saves the
		// channel-and-goroutine plumbing for callers that just want the
		// final response.
		var unsubscribe func()
		if !onEvent.IsUndefined() && !onEvent.IsNull() {
			unsubscribe = wireAgentEventForwarding(ag, onEvent)
			defer unsubscribe()
		}

		response, err := ag.ProcessQuery(query)
		if err != nil {
			// Track consecutive errors. After maxConsecutiveErrors,
			// invalidate the cached agent so the next call starts fresh
			// instead of looping on a potentially corrupted state.
			persistentAgentMu.Lock()
			persistentErrCnt++
			if persistentErrCnt >= maxConsecutiveErrors {
				persistentAgent = nil
				persistentAgentPv = ""
				persistentErrCnt = 0
			}
			persistentAgentMu.Unlock()
			return nil, fmt.Errorf("process query: %w", err)
		}

		// Success — reset error counter.
		persistentAgentMu.Lock()
		persistentErrCnt = 0
		persistentAgentMu.Unlock()

		return map[string]interface{}{
			"response": response,
			"provider": provider,
			"model":    ag.GetModel(),
		}, nil
	})
}

// runPlanFunc is runAgent with the planning-specific system prompt
// installed. Inputs mirror runAgent's. Returns the same Promise shape.
//
//	args[0] (string)  — provider name
//	args[1] (string)  — model id (or "" for default)
//	args[2] (string)  — initial planning query
//	args[3] (func?)   — onEvent callback
//
// Matches the behavior of the native `sprout plan` command's
// createPlanningAgent → ProcessQuery flow, minus the interactive readline
// loop (which doesn't apply in a browser). Host pages drive the
// multi-turn flow by calling runPlan again with the user's follow-up
// response, treating it like a chat thread.
//
// The planning prompt embeds the "create todos as you plan" behavior;
// disabling that knob isn't exposed here yet (always enabled to match
// the native default).
func runPlanFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	query := argString(args, 2, "")

	var onEvent js.Value
	if len(args) > 3 && args[3].Type() == js.TypeFunction {
		onEvent = args[3]
	}

	return asPromiseWithTimeout(agentTimeout, func(ctx context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required (first arg)")
		}
		if query == "" {
			return nil, fmt.Errorf("query is required (third arg)")
		}

		client, err := factory.CreateProviderClient(api.ClientType(provider), model)
		if err != nil {
			return nil, fmt.Errorf("create client: %w", err)
		}
		injectWasmStreamingClient(client)

		configMgr, err := configuration.NewManagerSilent()
		if err != nil {
			return nil, fmt.Errorf("init configuration: %w", err)
		}

		ag, err := agent.NewAgentWithClient(client, api.ClientType(provider), configMgr)
		if err != nil {
			return nil, fmt.Errorf("init agent: %w", err)
		}

		// Enable streaming so tokens appear in real-time (same as runAgentFunc).
		ag.SetStreamingEnabled(true)

		planningPrompt, err := agent.GetEmbeddedPlanningPrompt(true)
		if err != nil {
			return nil, fmt.Errorf("load planning prompt: %w", err)
		}
		ag.SetSystemPrompt(planningPrompt)

		var unsubscribe func()
		if !onEvent.IsUndefined() && !onEvent.IsNull() {
			unsubscribe = wireAgentEventForwarding(ag, onEvent)
			defer unsubscribe()
		}

		response, err := ag.ProcessQuery(query)
		if err != nil {
			return nil, fmt.Errorf("process query: %w", err)
		}

		return map[string]interface{}{
			"response": response,
			"provider": provider,
			"model":    ag.GetModel(),
			"mode":     "plan",
		}, nil
	})
}

// wireAgentEventForwarding attaches an EventBus to the agent and starts
// a goroutine that forwards every published UIEvent to onEvent as a
// JSON-stringified payload. The returned unsubscribe function tears down
// the subscription and waits for the forwarding goroutine to drain.
//
// EventBus.Unsubscribe closes the channel, so the goroutine exits when
// the range loop sees the closed channel. We use done to make the
// teardown synchronous from the caller's perspective.
func wireAgentEventForwarding(ag *agent.Agent, onEvent js.Value) func() {
	bus := events.NewEventBus()
	ag.SetEventBus(bus)

	const subName = "wasm-runagent"
	ch := bus.Subscribe(subName)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			// Use try-style guarded invocation — if the JS side has torn
			// down the callback (page unload, etc.), invoking will throw
			// inside the JS runtime and the goroutine will panic. Catch
			// here so a flaky host page can't crash the WASM module.
			func() {
				defer func() { _ = recover() }()
				onEvent.Invoke(string(payload))
			}()
		}
	}()

	return func() {
		bus.Unsubscribe(subName)
		<-done
	}
}
