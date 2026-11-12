//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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
// Credential model: WASM never holds real API keys. Native hosts (Studio
// iOS/Android) attach the real key at the same-origin proxy hop
// (/api/proxy/llm/{provider}), replacing whatever Authorization the
// browser sent. To satisfy GenericProvider's pre-flight auth validation
// (GetAuthToken → buildHTTPRequestCtx), an init-time placeholder is seeded
// for every bearer/api_key provider env var; the proxy is the real
// credential boundary. See seedWasmCredentialPlaceholders below.

func init() { seedWasmCredentialPlaceholders() }

// seedWasmCredentialPlaceholders seeds non-empty placeholder values for
// known provider API-key env vars when they are unset. This lets provider
// config validation and per-request auth checks pass inside WASM; the
// placeholder never reaches a real provider — the host's proxy replaces
// the Authorization header server-side (native Keychain/Keystore lookup).
func seedWasmCredentialPlaceholders() {
	for _, envVar := range []string{
		"OPENAI_API_KEY", "OPENROUTER_API_KEY", "DEEPINFRA_API_KEY",
		"DEEPSEEK_API_KEY", "ZAI_API_KEY", "ZAI_CODING_API_KEY",
		"OLLAMA_API_KEY", "MINIMAX_API_KEY", "CHUTES_API_KEY",
		"MISTRAL_API_KEY", "CEREBRAS_API_KEY", "ANTHROPIC_API_KEY",
	} {
		if os.Getenv(envVar) == "" {
			os.Setenv(envVar, "wasm-proxy-placeholder")
		}
	}
}

//
// Tool execution under WASM is constrained: shell tools, MCP, and other
// process-spawning tools no-op or error out. SP-045-4e tracks the work
// to route shell-like tools through SproutWasm.executeCommand so the
// agent can edit files and run a curated set of commands inside MEMFS.

// chatAgents caches one Agent per chat so that each conversation keeps its
// own history across turns (multi-turn chat) and separate chats never see
// each other's messages. Keyed by the chat id the UI stamps on each query;
// "" is the single implicit chat of callers that send none.
//
// A cached agent is rebuilt when the caller switches providers. The cache is
// bounded: the least recently used agent is dropped past maxChatAgents (its
// chat then starts a fresh agent — history shown in the UI is unaffected).
//
// Access is guarded by chatAgentsMu to prevent races when multiple runAgent
// calls arrive concurrently (rapid user messages, steer, several chats).
type chatAgent struct {
	ag       *agent.Agent
	provider string // provider name the agent was built for
	options  agentRunOptions
	// personaGeneration is the runtime's host-persona generation the agent
	// was built with. A persona defined (or removed) after that invalidates
	// the cached agent, so the next run picks the change up (SP-175).
	personaGeneration uint64
	errCnt            int // consecutive ProcessQuery errors; reset on success
	lastUsed          time.Time
}

var (
	chatAgentsMu sync.Mutex
	chatAgents   = map[string]*chatAgent{}
	lastChatID   string // most recently run chat, for callers that name none
)

const maxChatAgents = 8

// maxConsecutiveErrors is the threshold at which a cached agent is
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

// lookupChatAgent returns the cached agent for a chat, or nil.
func lookupChatAgent(chatID string) *agent.Agent {
	chatAgentsMu.Lock()
	defer chatAgentsMu.Unlock()
	if entry := chatAgents[chatID]; entry != nil {
		return entry.ag
	}
	return nil
}

// storeChatAgent caches a freshly built agent and evicts the least recently
// used one past the bound. Caller must not hold chatAgentsMu.
func storeChatAgent(chatID string, ag *agent.Agent, provider string, options agentRunOptions) {
	chatAgentsMu.Lock()
	defer chatAgentsMu.Unlock()
	chatAgents[chatID] = &chatAgent{
		ag:                ag,
		provider:          provider,
		options:           options,
		personaGeneration: hostPersonaGeneration(),
		lastUsed:          time.Now(),
	}
	for len(chatAgents) > maxChatAgents {
		oldestID, oldest := "", time.Time{}
		for id, entry := range chatAgents {
			if id != chatID && (oldest.IsZero() || entry.lastUsed.Before(oldest)) {
				oldestID, oldest = id, entry.lastUsed
			}
		}
		delete(chatAgents, oldestID)
	}
}

// resetChatAgents clears one chat's agent, or every chat's when chatID is
// nil — the JS side's "start a fresh conversation".
func resetChatAgents(chatID *string) {
	chatAgentsMu.Lock()
	defer chatAgentsMu.Unlock()
	if chatID == nil {
		chatAgents = map[string]*chatAgent{}
		return
	}
	delete(chatAgents, *chatID)
}

// optionalChatID reads an optional chat id argument; nil when absent.
func optionalChatID(args []js.Value, i int) *string {
	if len(args) > i && args[i].Type() == js.TypeString {
		id := args[i].String()
		return &id
	}
	return nil
}

func agentJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"runAgent":          js.FuncOf(runAgentFunc),
		"runPlan":           js.FuncOf(runPlanFunc),
		"clearConversation": js.FuncOf(clearConversationFunc),
		"stopAgent":         js.FuncOf(stopAgentFunc),
		"steerAgent":        js.FuncOf(steerAgentFunc),
		"retractSteer":      js.FuncOf(retractSteerFunc),
	}
}

// clearConversationFunc resets a chat's agent so its next runAgent call
// starts a fresh conversation. args[0] (string, optional) names the chat;
// without it every chat is reset.
func clearConversationFunc(_ js.Value, args []js.Value) interface{} {
	resetChatAgents(optionalChatID(args, 0))
	return nil
}

// stopAgentFunc interrupts a running agent loop — the cloud-mode stop
// button. It cancels the agent's interrupt context so in-flight HTTP
// requests and tool executions abort promptly. args[0] (string, optional)
// names the chat; without it every chat's agent is interrupted.
func stopAgentFunc(_ js.Value, args []js.Value) interface{} {
	chatID := optionalChatID(args, 0)
	chatAgentsMu.Lock()
	var targets []*agent.Agent
	for id, entry := range chatAgents {
		if chatID == nil || id == *chatID {
			targets = append(targets, entry.ag)
		}
	}
	chatAgentsMu.Unlock()
	for _, ag := range targets {
		ag.TriggerInterrupt()
	}
	return nil
}

// steerAgentFunc injects a steering message into a chat's agent. If the
// agent is mid-turn, the message is queued and delivered as a follow-up
// prompt after the current turn completes — the cloud-mode steer input.
// args[1] (string, optional) names the chat; without it the most recently
// run chat is steered.
func steerAgentFunc(_ js.Value, args []js.Value) interface{} {
	message := argString(args, 0, "")
	if message == "" {
		return map[string]interface{}{"steered": false, "error": "message is required"}
	}
	chatID := optionalChatID(args, 1)
	if chatID == nil {
		chatAgentsMu.Lock()
		last := lastChatID
		chatAgentsMu.Unlock()
		chatID = &last
	}
	ag := lookupChatAgent(*chatID)
	if ag == nil {
		return map[string]interface{}{"steered": false, "error": "no active agent"}
	}
	if err := ag.InjectInputContext(message); err != nil {
		return map[string]interface{}{"steered": false, "error": err.Error()}
	}
	return map[string]interface{}{"steered": true}
}

// retractSteerFunc takes back the newest steering message a chat's agent has
// not read yet: one sent as its run ended, which would otherwise wait for the
// chat's next run. args[0] (string, optional) names the chat; without it the
// most recently run chat. Returns {retracted, message}.
func retractSteerFunc(_ js.Value, args []js.Value) interface{} {
	chatID := optionalChatID(args, 0)
	if chatID == nil {
		chatAgentsMu.Lock()
		last := lastChatID
		chatAgentsMu.Unlock()
		chatID = &last
	}
	ag := lookupChatAgent(*chatID)
	if ag == nil {
		return map[string]interface{}{"retracted": false}
	}
	message, ok := ag.RetractLatestSteer()
	return map[string]interface{}{"retracted": ok, "message": message}
}

// runAgentFunc invokes one ProcessQuery turn through the chat's cached
// Agent. Inputs:
//
//	args[0] (string)  — provider name (matches runChat's argument 0)
//	args[1] (string)  — model id (pass "" for the provider's default)
//	args[2] (string)  — user query / prompt
//	args[3] (func?)   — onEvent(jsonString) callback for streamed UI events
//	args[4] (string?) — chat id; each chat keeps its own agent and history
//	args[5] (string?) — JSON [{role, content}] history, used only when the
//	                    chat's agent is created fresh (e.g. after a page
//	                    reload) so the conversation continues where it was
//	args[6] (string?) — reserved; hosts pass "" (no mode skills today)
//	args[7] (string?) — JSON agent options, {persona?, context?} (see
//	                    agentRunOptions); applied when the chat's agent is
//	                    built, and a change rebuilds it
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
// Call clearConversation(chatId) to reset a chat's history.
func runAgentFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	query := argString(args, 2, "")
	chatID := ""
	if id := optionalChatID(args, 4); id != nil {
		chatID = *id
	}
	historyJSON := argString(args, 5, "")
	optionsJSON := argString(args, 7, "")

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
		options, err := parseAgentRunOptions(optionsJSON)
		if err != nil {
			return nil, err
		}

		// Reuse the chat's cached agent when the provider matches, so the
		// conversation history carries over turn-to-turn. A provider
		// change (or nil cache) forces a rebuild.
		chatAgentsMu.Lock()
		lastChatID = chatID
		var ag *agent.Agent
		if entry := chatAgents[chatID]; entry != nil && entry.provider == provider &&
			entry.options.equal(options) && entry.personaGeneration == hostPersonaGeneration() {
			ag = entry.ag
			entry.lastUsed = time.Now()
		}
		chatAgentsMu.Unlock()

		if ag == nil {
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

			// Inject the WASM-owned AskUserManager so ask_user routes through
			// the event bus instead of falling back to stdin (which doesn't
			// exist in a browser). The approval manager is left nil — WASM
			// security paths nil-check before use and fall through to the
			// non-interactive auto-approve path.
			ag.InjectWebUIManagers(nil, wasmAskUserMgr)
			// In WASM the browser IS always the interactive surface.
			// Call sites guard with the specific manager they need:
			// askUserMgr for ask_user, GetSecurityApprovalMgr() for
			// approval paths, and the package-level edit broker needs
			// no manager. Returning true is safe because the approval
			// manager is nil in WASM and the edit-approval path has its
			// own package broker with the cloud response route.
			ag.SetHasActiveWebUIClients(func() bool { return true })

			// Host personas defined with definePersona belong to every
			// agent the runtime builds (SP-175).
			for _, def := range definedHostPersonas() {
				if err := ag.DefinePersona(def); err != nil {
					return nil, err
				}
			}
			if err := applyAgentRunOptions(ag, options); err != nil {
				return nil, err
			}
			// Stamp the chat on the agent's events and tool env, so a host
			// running several chats can route events and host tool calls.
			if chatID != "" {
				ag.MergeEventMetadata(map[string]interface{}{"chat_id": chatID})
			}

			if history := parseSeedHistory(historyJSON); len(history) > 0 {
				ag.SetMessages(history)
			}

			storeChatAgent(chatID, ag, provider, options)
		}

		// Wire the event bus only when JS provided a sink — saves the
		// channel-and-goroutine plumbing for callers that just want the
		// final response.
		var unsubscribe func()
		if !onEvent.IsUndefined() && !onEvent.IsNull() {
			unsubscribe = wireAgentEventForwarding(ag, onEvent)
			defer unsubscribe()
		}

		// The agent is cached across turns; a host-side changeDir between
		// turns (studio: picking another repo) must reach its tool paths.
		ag.SetWorkspaceRoot(workspaceRoot)

		response, err := ag.ProcessQuery(query)
		chatAgentsMu.Lock()
		if entry := chatAgents[chatID]; entry != nil && entry.ag == ag {
			if err != nil {
				// After maxConsecutiveErrors, drop the chat's agent so the
				// next call starts fresh instead of looping on a
				// potentially corrupted state.
				entry.errCnt++
				if entry.errCnt >= maxConsecutiveErrors {
					delete(chatAgents, chatID)
				}
			} else {
				entry.errCnt = 0
			}
		}
		chatAgentsMu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("process query: %w", err)
		}

		if strings.TrimSpace(response) == "" {
			response = lastTurnReply(ag.GetMessages())
		}

		return map[string]interface{}{
			"response": response,
			"provider": provider,
			"model":    ag.GetModel(),
		}, nil
	})
}

// lastTurnReply returns the final assistant text of the latest turn. The
// streaming path delivers the answer only as stream_chunk events and
// ProcessQuery returns "", but the host still needs the text to record the
// turn for a chat that finished off screen.
func lastTurnReply(messages []api.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		switch messages[i].Role {
		case "user":
			return ""
		case "assistant":
			if text := strings.TrimSpace(messages[i].Content); text != "" {
				return messages[i].Content
			}
		}
	}
	return ""
}

// parseSeedHistory decodes the history a fresh chat agent is seeded with.
// Only user and assistant turns with content are kept; anything malformed
// yields no history (the chat simply starts without prior context).
func parseSeedHistory(raw string) []api.Message {
	if raw == "" {
		return nil
	}
	var turns []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &turns); err != nil {
		return nil
	}
	history := make([]api.Message, 0, len(turns))
	for _, t := range turns {
		if (t.Role == "user" || t.Role == "assistant") && strings.TrimSpace(t.Content) != "" {
			history = append(history, api.Message{Role: t.Role, Content: t.Content})
		}
	}
	return history
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
