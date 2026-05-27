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
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/sprout-foundry/seed/core"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/spec"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ---------------------------------------------------------------------------
// Default retry configuration (mimics old APIClient defaults)
// ---------------------------------------------------------------------------

const (
	defaultMaxRetries   = 3
	defaultBaseRetryDelay = 1 * time.Second
)

// ---------------------------------------------------------------------------
// sproutProvider — implements seed/core.Provider by wrapping api.ClientInterface
// ---------------------------------------------------------------------------

// sproutProvider adapts sprout's ClientInterface to seed's Provider interface.
type sproutProvider struct {
	agent          *Agent
	client         api.ClientInterface
	pastedImages   map[string][]api.ImageData // path → image data (for multimodal attachment)
	pastedImagesMu sync.RWMutex
}

// NewSproutProvider creates a Provider that wraps a sprout ClientInterface.
func NewSproutProvider(agent *Agent, client api.ClientInterface) (core.Provider, error) {
	if client == nil {
		return nil, fmt.Errorf("sprout provider requires a non-nil client")
	}
	return &sproutProvider{
		agent:        agent,
		client:       client,
		pastedImages: make(map[string][]api.ImageData),
	}, nil
}

// RegisterPastedImages associates extracted image data with file paths so
// they can be attached to the first user message in each Chat request.
func (sp *sproutProvider) RegisterPastedImages(images map[string][]api.ImageData) {
	if images == nil {
		return
	}
	sp.pastedImagesMu.Lock()
	for k, v := range images {
		sp.pastedImages[k] = v
	}
	sp.pastedImagesMu.Unlock()
}

// isRetryableError checks whether an error is transient and should be retried.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	retryKeywords := []string{
		"stream error",
		"INTERNAL_ERROR",
		"connection reset",
		"EOF",
		"timeout",
		"502",
		"upstream error",
	}
	for _, kw := range retryKeywords {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// extractHTTPStatusCode parses common HTTP error patterns to extract the status code.
// Handles formats like "HTTP 400: msg", "HTTP 400", "400 Bad Request", "error 429", etc.
func extractHTTPStatusCode(msg string) int {
	lower := strings.ToLower(msg)
	// "HTTP 400: ..." or "HTTP 400"
	if idx := strings.Index(lower, "http "); idx >= 0 {
		rest := msg[idx+5:]
		if i, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && i >= 100 && i < 1000 {
			return i
		}
	}
	// "error 429" or "response error: 400" — look for a standalone 3-digit number
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return !((r >= '0' && r <= '9') || r == '_') }) {
		if len(word) == 3 {
			if i, err := strconv.Atoi(word); err == nil && i >= 100 && i < 1000 {
				return i
			}
		}
	}
	return 0
}

// recordProviderError stores error info in the agent's state for observability.
func (sp *sproutProvider) recordProviderError(err error, retries int) {
	if sp.agent == nil || err == nil {
		return
	}
	msg := err.Error()
	sp.agent.state.SetLastProviderError(&ProviderErrorInfo{
		Timestamp:  time.Now().Format(time.RFC3339),
		Provider:   sp.agent.GetProvider(),
		Model:      sp.agent.GetModel(),
		StatusCode: extractHTTPStatusCode(msg),
		Message:    msg,
		Retries:    retries,
	})
}

// clearProviderError clears the last provider error (on success).
func (sp *sproutProvider) clearProviderError() {
	if sp.agent == nil {
		return
	}
	sp.agent.state.SetLastProviderError(nil)
}

// doChatWithRetry executes a chat request with exponential backoff retry.
func (sp *sproutProvider) doChatWithRetry(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			delay := defaultBaseRetryDelay * time.Duration(1<<(attempt-1))
			// Add jitter (0 to 500ms)
			jitter := time.Duration(time.Now().UnixNano()%500000000)
			if delay+jitter > 0 {
				select {
				case <-time.After(delay + jitter):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}

		resp, err := sp.doChatOnce(ctx, req)
		if err == nil {
			// Clear any previous provider error on success
			sp.clearProviderError()
			// Fleet budget tracking: debit tokens after each LLM call.
			// If budget is exceeded, propagate the error so the conversation loop stops.
			if budgetErr := sp.trackFleetBudgetForResponse(resp); budgetErr != nil {
				return nil, budgetErr
			}
			return resp, nil
		}
		lastErr = err

		if !isRetryableError(err) {
			// Non-retryable error — record and return immediately
			sp.recordProviderError(err, attempt)
			return nil, err
		}
	}

	// Max retries exhausted — record the error
	sp.recordProviderError(lastErr, defaultMaxRetries)
	return nil, fmt.Errorf("transient error during chat (%s): %w", sp.GetModel(), lastErr)
}

// doChatOnce performs a single chat request, attaching pasted images to the
// first user message if the client supports vision.
func (sp *sproutProvider) doChatOnce(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	if sp.agent != nil && sp.agent.output.IsStreamingEnabled() {
		return sp.doChatStream(ctx, req)
	}
	return sp.doChatNonStream(ctx, req)
}

// doChatNonStream performs a non-streaming chat request.
func (sp *sproutProvider) doChatNonStream(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	// Attach pasted images to the first user message
	messages := sp.attachPastedImages(req.Messages)

	sproutReq := seedRequestToSprout(req)
	resp, err := sp.client.SendChatRequest(ctx, messages, sproutReq.Tools, sproutReq.Reasoning, false)
	if err != nil {
		return nil, err
	}
	return sproutResponseToSeed(resp), nil
}

// doChatStream performs a streaming chat request.
func (sp *sproutProvider) doChatStream(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	// Attach pasted images to the first user message
	messages := sp.attachPastedImages(req.Messages)

	// Create a stream handler that writes to the agent's output manager
	handler := &streamHandler{}

	sproutReq := seedRequestToSprout(req)
	callback := func(content string, contentType string) {
		switch contentType {
		case "reasoning":
			handler.reasoning = true
			if sp.agent != nil && sp.agent.output.GetReasoningCallback() != nil {
				sp.agent.output.GetReasoningCallback()(content)
			}
			sp.agent.output.GetReasoningBuffer().WriteString(content)
		default:
			handler.reasoning = false
			if sp.agent != nil && sp.agent.output.GetStreamingCallback() != nil {
				sp.agent.output.GetStreamingCallback()(content)
			}
			sp.agent.output.GetStreamingBuffer().WriteString(content)
		}
	}

	resp, err := sp.client.SendChatRequestStream(ctx, messages, sproutReq.Tools, sproutReq.Reasoning, false, callback)
	if err != nil {
		return nil, err
	}
	return sproutResponseToSeed(resp), nil
}

// streamHandler implements core.StreamHandler
type streamHandler struct {
	reasoning bool
}

func (h *streamHandler) OnContent(content string) {
	// Already handled in the callback
}

func (h *streamHandler) OnReasoning(content string) {
	// Already handled in the callback
}

func (h *streamHandler) OnDone(resp *core.ChatResponse) {
	// Already handled in the callback
}

func (h *streamHandler) OnError(err error) {
	// Already handled in the callback
}

// attachPastedImages attaches previously registered image data to the first
// user message in the request. This makes pasted images available to the
// vision model through the seed request pipeline.
func (sp *sproutProvider) attachPastedImages(messages []core.Message) []core.Message {
	sp.pastedImagesMu.RLock()
	defer sp.pastedImagesMu.RUnlock()

	if len(sp.pastedImages) == 0 {
		return messages
	}

	if !sp.client.SupportsVision() {
		return messages
	}

	out := make([]core.Message, len(messages))
	copy(out, messages)

	for i := range out {
		if out[i].Role == "user" {
			// Collect all registered image data
			var allImages []api.ImageData
			for _, imgs := range sp.pastedImages {
				allImages = append(allImages, imgs...)
			}
			if len(allImages) > 0 {
				// Append to any existing images
				out[i].Images = append(out[i].Images, allImages...)
			}
			break // Only attach to the first user message
		}
	}

	return out
}

// trackFleetBudgetForResponse debits the tokens from this LLM response to
// the shared fleet budget tracker, if one is configured on the agent.  If
// the budget is exceeded, it sets the truncation flag so the conversation
// loop can stop gracefully.
//
// Returns FleetBudgetExceededError if the budget was just exceeded by this
// call (i.e. the cumulative total went from under-limit to at/over-limit).
func (sp *sproutProvider) trackFleetBudgetForResponse(resp *api.ChatResponse) error {
	if sp.agent == nil {
		return nil
	}
	tracker := sp.agent.fleetBudgetTracker
	limit := sp.agent.fleetBudgetLimit
	if tracker == nil || limit <= 0 {
		return nil
	}
	tokens := int64(resp.Usage.TotalTokens)
	if tokens <= 0 {
		return nil
	}
	newTotal := tracker.Add(tokens)
	// Budget is exceeded when cumulative tokens reach or exceed the limit.
	if newTotal >= limit && !sp.agent.fleetBudgetTrunc.Load() {
		sp.agent.fleetBudgetTrunc.Store(true)
		return FleetBudgetExceededError
	}
	return nil
}

// FleetBudgetExceededError is returned by the seed provider when the shared
// fleet token budget has been exceeded mid-conversation.  It is caught by
// processQueryWithSeed to truncate gracefully rather than surfacing as a
// generic API error.
var FleetBudgetExceededError = errors.New("fleet token budget exceeded")

// Chat implements core.Provider
func (sp *sproutProvider) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return sp.doChatWithRetry(ctx, req)
}

func (sp *sproutProvider) ChatStream(ctx context.Context, req *core.ChatRequest, handler core.StreamHandler) error {
	sproutReq := seedRequestToSprout(req)

	// Attach pasted images to the first user message
	messages := sp.attachPastedImages(req.Messages)

	callback := func(content string, contentType string) {
		switch contentType {
		case "reasoning":
			handler.OnReasoning(content)
		default:
			handler.OnContent(content)
		}
	}

	// Use doChatWithRetry for streaming too, but wrap it to deliver through the handler
	resp, err := sp.doChatWithRetryStreaming(ctx, messages, sproutReq.Tools, sproutReq.Reasoning, callback)
	if err != nil {
		handler.OnError(err)
		return err
	}
	handler.OnDone(sproutResponseToSeed(resp))
	return nil
}

// doChatWithRetryStreaming performs a streaming chat request with retry.
func (sp *sproutProvider) doChatWithRetryStreaming(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if attempt > 0 {
			delay := defaultBaseRetryDelay * time.Duration(1<<(attempt-1))
			jitter := time.Duration(time.Now().UnixNano()%500000000)
			if delay+jitter > 0 {
				select {
				case <-time.After(delay + jitter):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}

		resp, err := sp.client.SendChatRequestStream(ctx, messages, tools, reasoning, false, callback)
		if err == nil {
			// Clear any previous provider error on success
			sp.clearProviderError()
			// Fleet budget tracking: debit tokens after each LLM call
			if budgetErr := sp.trackFleetBudgetForResponse(resp); budgetErr != nil {
				return nil, budgetErr
			}
			return resp, nil
		}
		lastErr = err

		if !isRetryableError(err) {
			// Non-retryable error — record and return immediately
			sp.recordProviderError(err, attempt)
			return nil, err
		}
	}

	// Max retries exhausted — record the error
	sp.recordProviderError(lastErr, defaultMaxRetries)
	return nil, fmt.Errorf("transient error during chat (%s): %w", sp.GetModel(), lastErr)
}

func (sp *sproutProvider) Info() core.ProviderInfo {
	ctxLimit, _ := sp.client.GetModelContextLimit()
	return core.ProviderInfo{
		Model:       sp.client.GetModel(),
		ContextSize: ctxLimit,
		HasVision:   sp.client.SupportsVision(),
	}
}

func (sp *sproutProvider) GetModel() string {
	if sp.client != nil {
		return sp.client.GetModel()
	}
	return "unknown"
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
	agent *Agent
	exec  *ToolExecutor
}

// NewSproutToolExecutor creates a ToolExecutor that wraps a sprout agent ToolExecutor.
func NewSproutToolExecutor(agent *Agent, exec *ToolExecutor) core.ToolExecutor {
	return &sproutToolExecutor{agent: agent, exec: exec}
}

func (ste *sproutToolExecutor) GetTools() []core.Tool {
	// Call agent's getOptimizedToolDefinitions to get dynamic tool set
	// including MCP tools, persona filtering, and custom provider filtering
	if ste.agent != nil {
		tools := ste.agent.getOptimizedToolDefinitions(nil)
		return tools
	}
	return api.GetToolDefinitions()
}

func (ste *sproutToolExecutor) Execute(ctx context.Context, calls []core.ToolCall) []core.Message {
	sproutResults := ste.exec.ExecuteTools(calls)
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
// Error wrapping (mimics old ErrorHandler.HandleAPIFailure)
// ---------------------------------------------------------------------------

// wrapError converts a non-retryable error into a user-friendly string.
// This is called when seed.Run() returns an error that should be shown
// to the user rather than propagated as a Go error.
func wrapError(err error) string {
	msg := err.Error()

	// If the error is already wrapped with "chat failed:" prefix, strip it
	// to avoid double-wrapping
	chatFailedRe := regexp.MustCompile(`^chat failed: (.+)$`)
	if matches := chatFailedRe.FindStringSubmatch(msg); len(matches) > 1 {
		msg = matches[1]
	}

	// Use typed error classification first, then fall back to string matching
	// for errors that aren't AgentError types (backward compat bridge).
	if agenterrors.IsProviderError(err) {
		return fmt.Sprintf("Authentication failed: %s. Please check your API key and configuration.", msg)
	}
	if agenterrors.IsRateLimited(err) {
		return fmt.Sprintf("Rate limit exceeded: %s. Please wait before making more requests.", msg)
	}
	if agenterrors.IsTransient(err) {
		return fmt.Sprintf("The AI service encountered a temporary error and could not recover after several attempts: %s", msg)
	}

	// Fallback: string matching for errors that aren't typed AgentErrors.
	// This maintains backward compatibility with legacy error sources.
	if strings.Contains(msg, "authentication") || strings.Contains(msg, "invalid API key") || strings.Contains(msg, "unauthorized") {
		return fmt.Sprintf("Authentication failed: %s. Please check your API key and configuration.", msg)
	}
	if strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate_limit") || strings.Contains(msg, "429") {
		return fmt.Sprintf("Rate limit exceeded: %s. Please wait before making more requests.", msg)
	}
	if strings.Contains(msg, "transient error") || strings.Contains(msg, "max retries exhausted") {
		return fmt.Sprintf("The AI service encountered a temporary error and could not recover after several attempts: %s", msg)
	}
	if strings.Contains(msg, "context deadline") || strings.Contains(msg, "timeout") {
		return fmt.Sprintf("The request timed out: %s. Please try again.", msg)
	}

	// Default: return the error message as-is
	return fmt.Sprintf("An error occurred: %s", msg)
}

// ---------------------------------------------------------------------------
// Integration entry point
// ---------------------------------------------------------------------------

// injectInputMsg carries a user-steer message from the forwarder goroutine
// into the injector goroutine in processQueryWithSeed.
type injectInputMsg struct {
	content string
}

// processQueryWithSeed runs the conversation loop through seed's core.Agent
// instead of sprout's native ConversationHandler.
func (a *Agent) processQueryWithSeed(userQuery string) (string, error) {
	a.initSubManagers()

	// ---- Pre-loop hooks (moved from old ConversationHandler.ProcessQuery) ----

	// Reset termination reason for fresh query
	a.state.SetLastRunTerminationReason("")

	// Reset interrupt context so a Stop from the previous query doesn't
	// instantly cancel this one. Per SP-034-1e we now plumb interruptCtx
	// all the way into http.NewRequestWithContext, so leaving a cancelled
	// ctx around would make the next ProcessQuery fail before the first
	// LLM call lands.
	a.resetInterruptForNewQuery()

	// Publish query started event
	a.publishEvent(events.EventTypeQueryStarted, events.QueryStartedEvent(userQuery, a.GetProvider(), a.GetModel()))

	// Reset streaming buffers for new query
	a.output.GetStreamingBuffer().Reset()
	a.output.GetReasoningBuffer().Reset()

	// Enable change tracking
	a.EnableChangeTracking(userQuery)

	// Reset circuit breaker history for a fresh query
	if a.state.GetCircuitBreaker() != nil {
		a.state.GetCircuitBreaker().mu.Lock()
		for key := range a.state.GetCircuitBreaker().Actions {
			delete(a.state.GetCircuitBreaker().Actions, key)
		}
		a.state.GetCircuitBreaker().mu.Unlock()
		if a.debug {
			a.Logger().Debug("DEBUG: Reset circuit breaker for new query\n")
		}
	}

	// Process images if present — multimodal support
	images, processedQuery, err := a.processImagesInQuery(userQuery)
	if err != nil {
		a.publishEvent(events.EventTypeError, events.ErrorEvent("Image processing failed", err))
		return "", fmt.Errorf("failed to process images in query: %w", err)
	}

	// Set conversation start time for duration calculation
	a.conversationStartTime = time.Now()

	// Proactive context injection: retrieve relevant past work on first turn
	// or cold session restore. Only inject when the conversation is new (no
	// prior user messages) or the session was just restored from persistence
	// AND proactive context has not already been injected this session.
	existingSupplement := a.state.GetPendingSystemSupplement()
	// Match the current header from FormatProactiveContext. Kept as a
	// distinctive substring so cosmetic edits to the wording don't break
	// the dedup guard (the prior literal drifted and re-injected context
	// on every cold restore).
	alreadyInjected := strings.Contains(existingSupplement, "Previous Work (Read-Only Reference)")
	shouldInjectProactiveContext := !alreadyInjected &&
		(len(a.state.GetMessages()) == 0 || a.state.GetPreviousSummary() != "")
	if shouldInjectProactiveContext {
		injectCtx, injectCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.InjectProactiveContext(injectCtx, processedQuery); err != nil {
			a.Logger().Debug("[proactive-context] injection failed: %v\n", err)
		}
		injectCancel()
	}

	// Build the user message with processed (cleaned) query and images
	userMessage := api.Message{
		Role:    "user",
		Content: processedQuery,
		Images:  images,
	}

	// Register pasted images with the provider for attachment during Chat requests
	// The map key is the file path so the provider can match them up.
	pastedImageMap := make(map[string][]api.ImageData)
	if len(images) > 0 {
		// All images are from the same query — group them under a single key
		pastedImageMap["_current"] = images
	}

	// Save pre-seed message count and user message for later merge
	preSeedMsgCount := len(a.state.GetMessages())
	preSeedUserMsg := userMessage

	// Create seed provider adapter wrapping sprout's ClientInterface
	prov, err := NewSproutProvider(a, a.client)
	if err != nil {
		return "", fmt.Errorf("failed to create seed provider adapter: %w", err)
	}

	_ = prov // provider ready for seed agent construction

	// Use seed's ToolRegistry — registers all 30 sprout tools with
	// PreExecuteHook (security classification + subagent nesting prevention)
	// and handles channel stripping, alias resolution, arg parsing/repair,
	// type coercion, timeouts, truncation, circuit breakers, parallel exec.
	//
	// Create a single richEventPublisher for both the ToolRegistry and the
	// seed core agent, so ALL events (tool_start, tool_end, errors, metrics,
	// compaction, agent_message) carry the agent's event metadata (client_id,
	// chat_id, user_id). Without this, events from the seed core conversation
	// loop are published directly to the raw EventBus and lack the metadata
	// needed by the WebSocket forwarding logic (shouldForwardEventToConnection)
	// to route them to the correct browser tab.
	var seedPublisher core.EventPublisher
	if a.eventBus != nil {
		seedPublisher = newRichEventPublisher(a.eventBus, a)
	}
	seedRegistry := newSeedToolRegistryWithPublisher(a, seedPublisher)

	// Build seed Agent options
	opts := core.Options{
		Provider:       prov,
		Executor:       seedRegistry,
		MaxIterations:  a.maxIterations,
		Debug:          a.debug,
		EventPublisher: seedPublisher,
	}

	// Context-management wiring: hand seed the optimizer, summarizer, and
	// pruner so the chat loop's compaction cascade (proactive threshold +
	// recovery-on-overflow) uses sprout's configuration end-to-end. All three
	// fields are nil-tolerant on the seed side, so any that aren't configured
	// here simply fall back to seed's defaults.
	if optWrap := a.state.GetOptimizer(); optWrap != nil {
		opts.Optimizer = optWrap.Inner()
	}
	if pruner := a.state.GetConversationPruner(); pruner != nil {
		opts.Pruner = pruner
	}
	opts.LLMSummarizer = newLLMSummarizer(a.client, a.GetProvider())

	if a.systemPrompt != "" {
		opts.SystemPrompt = a.systemPrompt
	}

	// Consume any pending system supplement (previous session context,
	// proactive context) and append to the system prompt so the seed agent
	// includes it in its first message.
	if supplement := a.consumePendingSystemSupplement(); supplement != "" {
		opts.SystemPrompt = opts.SystemPrompt + "\n\n" + supplement
	}

	// OnIteration callback: sync per-iteration context token estimates
	// back to sprout's state so the UI can show real-time token usage.
	opts.OnIteration = func(iteration, messages, tokenEstimate, contextSize int) {
		a.state.SetCurrentIteration(iteration)
		a.state.SetCurrentContextTokens(tokenEstimate)
		a.state.SetMaxContextTokens(contextSize)
	}

	// Seed the agent with the existing conversation history so that
	// multi-turn continuity is preserved across queries.
	if msgs := a.state.GetMessages(); len(msgs) > 0 {
		opts.InitialMessages = msgs
	}

	// Restore turn checkpoints so that the message pipeline can apply
	// checkpoint compaction before sending to the provider. Without this,
	// restored sessions send the entire raw history (potentially hundreds of
	// messages with tool calls) instead of the compacted summary, causing
	// provider 400 errors due to mismatched tool calls/responses.
	if cps := a.state.GetTurnCheckpoints(); len(cps) > 0 {
		seedCPs := make([]core.TurnCheckpoint, len(cps))
		for i, cp := range cps {
			seedCPs[i] = core.TurnCheckpoint{
				StartIndex:        cp.StartIndex,
				EndIndex:          cp.EndIndex,
				Summary:           cp.Summary,
				ActionableSummary: cp.ActionableSummary,
			}
		}
		opts.InitialCheckpoints = seedCPs
	}

	// Create seed Agent
	seedAgent, err := core.NewAgent(opts)
	if err != nil {
		return "", fmt.Errorf("failed to create seed agent: %w", err)
	}

	// Run the query through seed's conversation loop.
	// Use the processed (cleaned) query so image placeholders are replaced.
	// ctx is the agent's interrupt context so TriggerInterrupt() — wired to
	// the webui Stop button at pkg/webui/api_query.go::handleAPIQueryStop —
	// actually aborts the in-flight HTTP request, not just the agent loop
	// after the next iteration boundary. See SP-034-1e.
	ctx := a.interruptCtx
	if ctx == nil {
		ctx = context.Background()
	}

	// Bridge sprout's user-facing inputInjectionChan to seed's InjectInput.
	// Callers (CLI prompt goroutine, webui /api/query/steer) push into the
	// sprout channel via InjectInputContext; this forwarder drains it and
	// hands the message to seed, which consumes it at the next natural
	// break point in its loop (between iterations, before deciding to
	// terminate the turn). Without this bridge the sprout channel buffers
	// forever and "steering" silently no-ops.
	//
	// runCtx is scoped to this query (separate from a.interruptCtx, which
	// outlives a single Run) so the forwarder exits cleanly when the
	// model returns. seed.InjectInput is buffered size 1; if full we
	// briefly sleep before retrying so we don't lose the user's steer to
	// a transient collision with seed's own consumer.
	runCtx, runCancel := context.WithCancel(ctx)
	injectChan := make(chan injectInputMsg, 8)
	steerDone := make(chan struct{})

	// Forwarder: reads from sprout's input channel and sends to injectChan
	go func() {
		defer close(injectChan)
		ch := a.GetInputInjectionContext()
		for {
			select {
			case <-runCtx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				select {
				case injectChan <- injectInputMsg{content: msg}:
				case <-runCtx.Done():
					return
				}
			}
		}
	}()

	// Injector: reads from injectChan and applies to seed agent
	go func() {
		defer close(steerDone)
		for msg := range injectChan {
			for !seedAgent.InjectInput(msg.content) {
				select {
				case <-runCtx.Done():
					return
				case <-time.After(25 * time.Millisecond):
				}
			}
		}
	}()
	defer func() {
		runCancel()
		<-steerDone
	}()

	result, err := seedAgent.Run(ctx, processedQuery)
	if err != nil {
		// Check if the fleet budget was exceeded mid-run
		if errors.Is(err, FleetBudgetExceededError) {
			// Extract the last assistant response as the truncated result
			a.syncSeedStateToSprout(seedAgent, preSeedUserMsg, preSeedMsgCount)

			var truncatedResult string
			messages := a.state.GetMessages()
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "assistant" && messages[i].Content != "" {
					truncatedResult = messages[i].Content
					break
				}
			}
			if truncatedResult == "" {
				truncatedResult = result
			}

			a.state.SetLastRunTerminationReason(RunTerminationFleetBudgetExceeded)
			a.finalizeConversationPostHooks(truncatedResult, processedQuery, preSeedMsgCount)

			return truncatedResult, nil
		}

		// Classify the error to provide a user-friendly message.
		// For permanent errors (auth, client error, context overflow), return
		// the error directly so both CLI and webui display it properly.
		classifiedErr := core.ClassifyError(err, a.GetModel())

		// Build a user-friendly message for the event and response
		wrapped := wrapError(classifiedErr)
		a.state.SetLastRunTerminationReason(RunTerminationCompleted)

		// Sync whatever state we can before returning
		a.syncSeedStateToSprout(seedAgent, preSeedUserMsg, preSeedMsgCount)

		// Finalize — publish the user-friendly message as the response
		a.finalizeConversationPostHooks(wrapped, processedQuery, preSeedMsgCount)

		// Return the classified error so CLI/webui display it properly.
		// The wrapped message is published via events above for display.
		return wrapped, classifiedErr
	}

	// Sync state back to sprout's agent manager
	a.syncSeedStateToSprout(seedAgent, preSeedUserMsg, preSeedMsgCount)

	// ---- Post-loop hooks (moved from old ConversationHandler.finalizeConversation) ----

	// Commit tracked changes
	if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
		if commitErr := a.CommitChanges("Task completed"); commitErr != nil {
			a.Logger().Debug("Warning: Failed to commit changes: %v\n", commitErr)
		}
	}

	// Run self-review gate if changes were tracked
	if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
		if err := a.runSelfReviewGate(); err != nil {
			a.publishEvent(events.EventTypeError, events.ErrorEvent("Self-review gate failed", err))
			return "", fmt.Errorf("failed self-review gate: %w", err)
		}
	}

	// Finalize post-loop tasks
	a.finalizeConversationPostHooks(result, processedQuery, preSeedMsgCount)

	// If streaming was enabled and content was streamed, return empty string
	// to avoid duplicate display in the top-level CLI console.
	// Subagents are exempt: their streaming callback writes prefixed lines to
	// stderr for the human, but the orchestrator LLM only sees what we return
	// here via SubagentResult.Output — returning "" would make the orchestrator
	// think the subagent did nothing and re-attempt the task.
	if !a.IsSubagent() && a.output.IsStreamingEnabled() && len(a.output.GetStreamingBuffer().String()) > 0 {
		return "", nil
	}

	return result, nil
}

// finalizeConversationPostHooks runs post-loop hooks shared by success and error paths.
func (a *Agent) finalizeConversationPostHooks(result string, processedQuery string, preSeedMsgCount int) {
	// Maybe checkpoint completed turn
	a.maybeCheckpointCompletedTurn(processedQuery, preSeedMsgCount, len(a.state.GetMessages()))

	// Publish query completed event
	var finalContent string
	messages := a.state.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			finalContent = messages[i].Content
			break
		}
	}
	// Fallback to the result string
	if finalContent == "" {
		finalContent = result
	}

	duration := time.Since(a.conversationStartTime)
	completedEvent := events.QueryCompletedEvent(
		processedQuery,
		finalContent,
		a.GetTotalTokens(),
		a.GetTotalCost(),
		duration,
	)
	if reason := a.GetLastRunTerminationReason(); reason != "" {
		completedEvent["status"] = reason
	}
	a.publishEvent(events.EventTypeQueryCompleted, completedEvent)
}

// maybeCheckpointCompletedTurn checks if a turn checkpoint should be recorded
// for a completed or max-iterations conversation turn.
func (a *Agent) maybeCheckpointCompletedTurn(processedQuery string, queryStartIndex, numMessages int) {
	if a.state == nil {
		return
	}
	messages := a.state.GetMessages()
	if queryStartIndex < 0 || queryStartIndex >= numMessages {
		return
	}

	reason := a.GetLastRunTerminationReason()
	if reason != RunTerminationCompleted && reason != RunTerminationMaxIterations {
		return
	}

	endIndex := numMessages - 1
	hasAssistant := false
	for i := queryStartIndex; i <= endIndex; i++ {
		if messages[i].Role == "assistant" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		return
	}

	a.RecordTurnCheckpointAsync(queryStartIndex, endIndex)
}

// syncSeedStateToSprout merges seed's state back into sprout's state manager.
// Since the seed agent is now created with InitialMessages (the existing
// conversation history), seed's final state contains the complete message
// sequence: [historical msgs, new user msg, assistant msg, tool msgs, ...].
// We simply replace sprout's messages with seed's messages and sync counters.
func (a *Agent) syncSeedStateToSprout(seedAgent *core.Agent, userMsg api.Message, preSeedMsgCount int) {
	if a.state == nil {
		return
	}

	seedState := seedAgent.State()
	if seedState == nil {
		return
	}

	seedMsgs := seedState.Messages()

	// Seed now has the full history (via InitialMessages) plus new messages
	// from this query. Replace sprout's messages entirely.
	a.state.SetMessages(seedMsgs)

	// Accumulate token and cost counters across queries. The seed agent is
	// created fresh per query (see opts.InitialMessages earlier in this file)
	// so seedState.TotalTokens() and seedState.TotalCost() reflect only the
	// current query's API consumption. Without accumulation, sprout's
	// lifetime counters would be overwritten by each query — earlier
	// counters told a confusing story (e.g. tiny second-query delta hiding
	// the first query's cost).
	a.state.SetTotalTokens(a.state.GetTotalTokens() + seedState.TotalTokens())
	a.state.SetTotalCost(a.state.GetTotalCost() + seedState.TotalCost())

	// Calculate iteration count from seed's messages
	assistantCount := 0
	for _, msg := range seedMsgs {
		if msg.Role == "assistant" {
			assistantCount++
		}
	}

	// Determine termination reason
	terminationReason := ""
	if a.maxIterations > 0 && assistantCount >= a.maxIterations {
		terminationReason = RunTerminationMaxIterations
	} else if assistantCount > 0 {
		terminationReason = RunTerminationCompleted
	}
	a.state.SetLastRunTerminationReason(terminationReason)

	if assistantCount > 0 {
		a.state.SetCurrentIteration(assistantCount - 1)
	} else {
		a.state.SetCurrentIteration(0)
	}

	if a.debug {
		a.Logger().Debug("[sync] Seed sync complete: msgCount=%d, assistantCount=%d, terminationReason=%s, iteration=%d\n",
			len(seedMsgs), assistantCount, terminationReason, a.state.GetCurrentIteration())
	}
}

// UseSeedLoop returns true if the agent should use seed's conversation loop
// instead of the native sprout ConversationHandler.
// DEPRECATED: Always returns true now that seed is the only path.
// Kept for backward compatibility with code that checks this value.
func UseSeedLoop() bool {
	return true
}

// ---------------------------------------------------------------------------
// Self-review gate (moved from conversation_handler_review.go)
// ---------------------------------------------------------------------------

// runSelfReviewGate runs self-review validation after conversation completion.
func (a *Agent) runSelfReviewGate() error {
	if configuration.GetEnvSimple("SKIP_SELF_REVIEW_GATE") == "1" {
		a.PrintLineAsync("[WARN] Self-review gate skipped (SPROUT_SKIP_SELF_REVIEW_GATE=1)")
		return nil
	}
	activePersona := a.GetActivePersona()
	if !isSelfReviewGatePersonaEnabled(activePersona) {
		if strings.TrimSpace(activePersona) == "" {
			a.PrintLineAsync("[info] Self-review gate skipped (persona=<none>)")
		} else {
			a.PrintLineAsync(fmt.Sprintf("[info] Self-review gate skipped (persona=%s)", activePersona))
		}
		return nil
	}

	revisionID := strings.TrimSpace(a.GetRevisionID())
	if revisionID == "" {
		return agenterrors.NewPermanentError("self-review gate blocked completion: no revision ID available for changed task", nil)
	}

	var cfgErr error
	cfg := a.GetConfigManager().GetConfig()
	if cfg == nil {
		cfg, cfgErr = configuration.Load()
		if cfgErr != nil {
			return fmt.Errorf("self-review gate blocked completion: failed to load config: %w", cfgErr)
		}
	}
	mode := cfg.GetSelfReviewGateMode()
	if mode == configuration.SelfReviewGateModeOff {
		a.PrintLineAsync("[info] Self-review gate skipped (mode=off)")
		return nil
	}
	if mode == configuration.SelfReviewGateModeCode && !hasCodeLikeTrackedFiles(a.GetTrackedFiles()) {
		a.PrintLineAsync("[info] Self-review gate skipped (mode=code, no code files changed)")
		return nil
	}

	logger := utils.GetLogger(true)
	result, err := spec.ReviewTrackedChanges(revisionID, cfg, logger)
	if err != nil {
		return fmt.Errorf("self-review gate blocked completion: %w", err)
	}
	if result.ScopeResult != nil && !result.ScopeResult.InScope {
		summary := strings.TrimSpace(result.ScopeResult.Summary)
		if summary == "" {
			summary = "scope violations detected"
		}
		return fmt.Errorf("self-review gate blocked completion: %s", summary)
	}

	a.PrintLineAsync(fmt.Sprintf("[OK] Self-review gate passed: revision %s is within scope", revisionID))
	return nil
}

// ---------------------------------------------------------------------------
// Utility functions moved from deleted files
// ---------------------------------------------------------------------------

func hasCodeLikeTrackedFiles(files []string) bool {
	if len(files) == 0 {
		return false
	}

	codeExtensions := map[string]struct{}{
		".go": {}, ".py": {}, ".js": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".java": {},
		".rs": {}, ".c": {}, ".cc": {}, ".cpp": {}, ".h": {}, ".hh": {}, ".hpp": {}, ".cs": {},
		".rb": {}, ".php": {}, ".swift": {}, ".kt": {}, ".kts": {}, ".scala": {}, ".sh": {},
		".bash": {}, ".zsh": {}, ".fish": {}, ".sql": {}, ".html": {}, ".css": {}, ".scss": {},
		".vue": {}, ".svelte": {}, ".yaml": {}, ".yml": {}, ".toml": {}, ".ini": {}, ".json": {},
		".xml": {}, ".proto": {}, ".tf": {},
	}
	codeBasenames := map[string]struct{}{
		"dockerfile":       {},
		"makefile":         {},
		"justfile":         {},
		"cmakelists.txt":   {},
		"build.gradle":     {},
		"build.gradle.kts": {},
	}

	for _, f := range files {
		path := strings.TrimSpace(f)
		if path == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := codeExtensions[ext]; ok {
			return true
		}
		base := strings.ToLower(filepath.Base(path))
		if _, ok := codeBasenames[base]; ok {
			return true
		}
	}

	return false
}

func isSelfReviewGatePersonaEnabled(persona string) bool {
	switch strings.ToLower(strings.TrimSpace(persona)) {
	case "orchestrator", "coder":
		return true
	default:
		return false
	}
}
