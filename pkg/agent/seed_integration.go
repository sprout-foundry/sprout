// Package agent provides the seed integration layer — a thin adapter that
// delegates the seed conversation loop to sprout's existing provider,
// executor, and event bus.
//
// seed/core types are the canonical definitions (seed/core/types.go).
// sprout/agent_api/types.go re-exports these via type aliases so sprout
// consumes them directly. The conversion helpers below are identity
// functions now that the types match.
//
// The adapter lives here because it bridges seed/core.Provider and
// seed/core.ToolExecutor interfaces to sprout's ClientInterface and
// ToolExecutor.
package agent

import (
	"context"
	"fmt"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// sproutProvider — implements seed/core.Provider by wrapping api.ClientInterface
// ---------------------------------------------------------------------------

// sproutProvider adapts sprout's ClientInterface to seed's Provider interface.
type sproutProvider struct {
	client api.ClientInterface
}

// NewSproutProvider creates a Provider that wraps a sprout ClientInterface.
func NewSproutProvider(client api.ClientInterface) (core.Provider, error) {
	if client == nil {
		return nil, fmt.Errorf("sprout provider requires a non-nil client")
	}
	return &sproutProvider{client: client}, nil
}

func (sp *sproutProvider) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	sproutReq := seedRequestToSprout(req)
	resp, err := sp.client.SendChatRequest(sproutReq.Messages, sproutReq.Tools, sproutReq.Reasoning, false)
	if err != nil {
		return nil, err
	}
	return sproutResponseToSeed(resp), nil
}

func (sp *sproutProvider) ChatStream(ctx context.Context, req *core.ChatRequest, handler core.StreamHandler) error {
	sproutReq := seedRequestToSprout(req)

	callback := func(content string, contentType string) {
		switch contentType {
		case "reasoning":
			handler.OnReasoning(content)
		default:
			handler.OnContent(content)
		}
	}

	resp, err := sp.client.SendChatRequestStream(
		sproutReq.Messages, sproutReq.Tools,
		sproutReq.Reasoning, false,
		callback,
	)
	if err != nil {
		handler.OnError(err)
		return err
	}
	// Seed expects OnDone to be called after streaming finishes.
	handler.OnDone(sproutResponseToSeed(resp))
	return nil
}

func (sp *sproutProvider) Info() core.ProviderInfo {
	ctxLimit, _ := sp.client.GetModelContextLimit()
	return core.ProviderInfo{
		Model:       sp.client.GetModel(),
		ContextSize: ctxLimit,
		HasVision:   sp.client.SupportsVision(),
	}
}

func (sp *sproutProvider) EstimateTokens(req *core.ChatRequest) int {
	if req == nil || len(req.Messages) == 0 {
		return 0
	}
	// Fallback: ~4 chars per token
	total := 0
	for _, msg := range req.Messages {
		total += len(msg.Content) + len(msg.ReasoningContent)
	}
	return total / 4
}

// ---------------------------------------------------------------------------
// sproutToolExecutor — implements seed/core.ToolExecutor
// ---------------------------------------------------------------------------

// sproutToolExecutor adapts an agent.ToolExecutor to seed's ToolExecutor interface.
type sproutToolExecutor struct {
	executor *ToolExecutor
}

// NewSproutToolExecutor creates a ToolExecutor that wraps a sprout agent ToolExecutor.
func NewSproutToolExecutor(executor *ToolExecutor) core.ToolExecutor {
	return &sproutToolExecutor{executor: executor}
}

func (ste *sproutToolExecutor) GetTools() []core.Tool {
	return api.GetToolDefinitions()
}

func (ste *sproutToolExecutor) Execute(ctx context.Context, calls []core.ToolCall) []core.Message {
	sproutResults := ste.executor.ExecuteTools(calls)
	return sproutResults
}

// ---------------------------------------------------------------------------
// Conversion helpers — identity functions (types are now aliases)
// ---------------------------------------------------------------------------

// seedRequestToSprout returns the request unchanged since
// api.ChatRequest and core.ChatRequest are the same type.
func seedRequestToSprout(req *core.ChatRequest) *core.ChatRequest {
	return req
}

// seedMessagesToSprout returns messages unchanged.
func seedMessagesToSprout(msgs []core.Message) []core.Message {
	return msgs
}

// sproutMessagesToSeed returns messages unchanged.
func sproutMessagesToSeed(msgs []api.Message) []core.Message {
	return msgs
}

// seedToolCallsToSprout returns tool calls unchanged.
func seedToolCallsToSprout(calls []core.ToolCall) []core.ToolCall {
	return calls
}

// coreToolCallToSprout returns the tool call unchanged.
func coreToolCallToSprout(tc core.ToolCall) api.ToolCall {
	return tc
}

// sproutToolCallsToSeed returns tool calls unchanged.
func sproutToolCallsToSeed(calls []api.ToolCall) []core.ToolCall {
	return calls
}

// seedToolsToSprout returns tools unchanged.
func seedToolsToSprout(tools []core.Tool) []core.Tool {
	return tools
}

// sproutToolsToSeed returns tools unchanged.
func sproutToolsToSeed(tools []api.Tool) []core.Tool {
	return tools
}

// seedMessageToSprout returns the message unchanged.
func seedMessageToSprout(m core.Message) api.Message {
	return m
}

// sproutMessageToSeed returns the message unchanged.
func sproutMessageToSeed(m api.Message) core.Message {
	return m
}

// sproutResponseToSeed returns the response unchanged since
// api.ChatResponse and core.ChatResponse are the same type.
func sproutResponseToSeed(resp *api.ChatResponse) *core.ChatResponse {
	return resp
}

// ---------------------------------------------------------------------------
// Integration entry point
// ---------------------------------------------------------------------------

// processQueryWithSeed runs the conversation loop through seed's core.Agent
// instead of sprout's native ConversationHandler.
func (a *Agent) processQueryWithSeed(userQuery string) (string, error) {
	a.initSubManagers()

	// 1. Create seed provider adapter wrapping sprout's ClientInterface
	provider, err := NewSproutProvider(a.client)
	if err != nil {
		return "", fmt.Errorf("failed to create seed provider adapter: %w", err)
	}

	// 2. Create seed tool executor adapter wrapping sprout's ToolExecutor
	toolExec := NewSproutToolExecutor(NewToolExecutor(a))

	// 3. Build seed Agent options
	opts := core.Options{
		Provider:       provider,
		Executor:       toolExec,
		MaxIterations:  a.maxIterations,
		Debug:          a.debug,
		EventPublisher: a.eventBus, // sprout's EventBus implements Publish(string, any)
	}

	if a.systemPrompt != "" {
		opts.SystemPrompt = a.systemPrompt
	}

	// 4. Create seed Agent
	seedAgent, err := core.NewAgent(opts)
	if err != nil {
		return "", fmt.Errorf("failed to create seed agent: %w", err)
	}

	// 5. Run the query through seed's conversation loop
	result, err := seedAgent.Run(context.Background(), userQuery)
	if err != nil {
		return "", err
	}

	// 6. Sync state back to sprout's agent state manager
	a.syncSeedStateToSprout(seedAgent)

	return result, nil
}

// syncSeedStateToSprout copies conversation state from the seed agent back to
// sprout's state manager so that subsequent queries have up-to-date history.
func (a *Agent) syncSeedStateToSprout(seedAgent *core.Agent) {
	if a.state == nil {
		return
	}

	seedState := seedAgent.State()

	// Convert seed messages to sprout types (identity — types are aliases)
	sproutMsgs := seedState.Messages()
	a.state.SetMessages(sproutMsgs)

	// Sync token counts
	a.state.SetTotalTokens(seedState.TotalTokens())
}

// UseSeedLoop returns true if the agent should use seed's conversation loop
// instead of the native sprout ConversationHandler.
func UseSeedLoop() bool {
	return configuration.GetEnvSimple("SEED_LOOP") == "1"
}
