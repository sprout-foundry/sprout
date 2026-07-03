// Package agent provides the seed integration layer.
//
// seed_provider.go — sproutProvider implements seed/core.Provider by wrapping
// api.ClientInterface, handling streaming, cost accumulation, fleet budget
// tracking, and image attachment.

package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/sprout-foundry/seed/core"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
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

// currentClient returns the agent's live client if available, otherwise returns the snapshot.
// This ensures the provider uses the current model even after SetProvider/SetModel.
func (sp *sproutProvider) currentClient() api.ClientInterface {
	if sp.agent != nil {
		if c := sp.agent.getClient(); c != nil {
			return c
		}
	}
	return sp.client
}

// NewSproutProvider creates a Provider that wraps a sprout ClientInterface.
func NewSproutProvider(agent *Agent, client api.ClientInterface) (core.Provider, error) {
	if client == nil {
		return nil, agenterrors.NewValidation("sprout provider requires a non-nil client", nil)
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

// doChatWithRetry performs a single chat request with fleet budget tracking and error recording.
// Retry logic is handled by the seed core's chatFn in conversation.go;
// this layer no longer retries to avoid nested retry explosion.
func (sp *sproutProvider) doChatWithRetry(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	resp, err := sp.doChatOnce(ctx, req)
	if err != nil {
		sp.recordProviderError(err, 0)
		return nil, err
	}
	sp.clearProviderError()
	// Fleet budget tracking: debit tokens after each LLM call.
	if budgetErr := sp.trackFleetBudgetForResponse(resp); budgetErr != nil {
		return nil, budgetErr
	}
	return resp, nil
}

// doChatOnce performs a single chat request, attaching pasted images to the
// first user message if the client supports vision.
func (sp *sproutProvider) doChatOnce(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	var resp *core.ChatResponse
	var err error
	if sp.agent != nil && sp.agent.output.IsStreamingEnabled() {
		resp, err = sp.doChatStream(ctx, req)
	} else {
		resp, err = sp.doChatNonStream(ctx, req)
	}
	if err == nil {
		sp.accumulateResponseCost(resp)
	}
	return resp, err
}

// accumulateResponseCost adds the provider-reported cost of a single
// response to the agent's lifetime cost counter. seed's chat loop tracks
// tokens (State.AddTokens) but never cost, so without this every footer
// reads $0. We add cost here — once per successful LLM call — directly on
// sprout's state, independent of seed's always-zero cost counter, so the
// seedState.TotalCost() reconciliation in syncSeedStateToSprout can't double
// count. The cost itself is captured at decode time (api.UsageCost), with a
// provider-agnostic field-name fallback (api.CostFromJSON) so providers that
// report cost under differing property names are all covered.
func (sp *sproutProvider) accumulateResponseCost(resp *core.ChatResponse) {
	if sp.agent == nil || sp.agent.state == nil || resp == nil {
		return
	}
	billingType := sp.resolveBillingType()
	chargedCost := api.UsageCost(resp.Usage)
	if chargedCost == 0 && billingType == BillingPayPerToken && resp.Usage.TotalTokens > 0 {
		chargedCost = sp.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	var tokenCost float64
	if billingType != BillingPayPerToken {
		tokenCost = sp.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	entry := CostEntry{
		BillingType:      billingType,
		Provider:         sp.agent.GetProvider(),
		Model:            sp.agent.GetModel(),
		ChargedCost:      chargedCost,
		TokenCost:        tokenCost,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		CachedTokens:     resp.Usage.CachedTokens,
	}
	sp.agent.state.AddCostEntry(entry)

	// Seed's conversation loop tracks total tokens but not prompt/completion
	// breakdown or LLM call count. Populate them here so --output-json and
	// the status footer report accurate per-call metrics.
	sp.agent.state.SetPromptTokens(sp.agent.state.GetPromptTokens() + resp.Usage.PromptTokens)
	sp.agent.state.SetCompletionTokens(sp.agent.state.GetCompletionTokens() + resp.Usage.CompletionTokens)
	sp.agent.state.SetLLMCallCount(sp.agent.state.GetLLMCallCount() + 1)

	// Debit the fleet USD budget so the workflow runner's budget display
	// ($X of $Y) reflects primary-agent LLM calls, not just subagents.
	// Use chargedCost when available (pay_per_token), fall back to
	// tokenCost for subscription/free providers.
	costForBudget := chargedCost
	if costForBudget == 0 {
		costForBudget = tokenCost
	}
	if sp.agent.fleetUsdBudget != nil && costForBudget > 0 {
		spent, crossed, justExceeded := sp.agent.fleetUsdBudget.Add(costForBudget)
		_, limit := sp.agent.fleetUsdBudget.Snapshot()
		for _, t := range crossed {
			if cb, ok := sp.agent.budgetWarningCallback.Load().(func(threshold, spent, limit float64)); ok && cb != nil {
				cb(t, spent, limit)
			}
		}
		if justExceeded {
			sp.agent.fleetBudgetTrunc.Store(true)
			if cb, ok := sp.agent.budgetExceededCallback.Load().(func(spent, limit float64)); ok && cb != nil {
				cb(spent, limit)
			}
		}
	}

	// Keep existing cached token tracking
	if n := resp.Usage.CachedTokens; n > 0 {
		sp.agent.state.SetCachedTokens(sp.agent.state.GetCachedTokens() + n)
	}
	if resp.Usage.CacheWriteTokens != nil {
		if n := *resp.Usage.CacheWriteTokens; n > 0 {
			sp.agent.state.SetCacheWriteTokens(sp.agent.state.GetCacheWriteTokens() + n)
		}
	}
}

// resolveBillingType returns the billing model for the current provider.
// It checks the embedded provider config for an explicit billing_type, then
// falls back to heuristics (zai-coding → subscription, else pay_per_token).
func (sp *sproutProvider) resolveBillingType() string {
	if sp.agent == nil {
		return BillingPayPerToken
	}
	provider := sp.agent.GetProvider()
	// Check embedded provider configs for explicit billing_type
	cfg, err := providers.GlobalFactory().GetProviderConfig(provider)
	if err == nil && cfg != nil {
		return cfg.BillingTypeResolved()
	}
	// Fallback heuristics for custom/dynamic providers
	if provider == "zai-coding" {
		return BillingSubscription
	}
	return BillingPayPerToken
}

// estimateCostFromPricing computes a cost estimate from token counts and the
// current model's per-million pricing, used when the provider doesn't report
// cost in its API response. Tries the live model registry first, then falls
// back to the embedded provider catalog (which carries manually-curated
// pricing for providers whose /v1/models endpoint omits cost fields, e.g.
// DeepSeek). Returns 0 when no pricing data is available from either source.
func (sp *sproutProvider) estimateCostFromPricing(promptTokens, completionTokens int) float64 {
	if sp.agent == nil || sp.agent.client == nil {
		return 0
	}
	model := sp.agent.client.GetModel()
	if model == "" {
		return 0
	}

	// Primary path: live model registry / canonical adapter.
	if models, err := api.GetModelsForProviderCtx(context.Background(), sp.agent.getClientType()); err == nil {
		for _, m := range models {
			if m.ID != model {
				continue
			}
			if m.InputCost > 0 || m.OutputCost > 0 {
				return float64(promptTokens)/1e6*m.InputCost + float64(completionTokens)/1e6*m.OutputCost
			}
			break
		}
	}

	// Fallback: embedded provider catalog (curated pricing for providers
	// whose live API doesn't report costs).
	provider := sp.agent.GetProvider()
	if inPerM, outPerM, _, ok := providercatalog.FindModelPricing(provider, model); ok {
		return float64(promptTokens)/1e6*inPerM + float64(completionTokens)/1e6*outPerM
	}

	return 0
}

// doChatNonStream performs a non-streaming chat request.
func (sp *sproutProvider) doChatNonStream(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	// Attach pasted images to the first user message
	messages := sp.attachPastedImages(req.Messages)

	sproutReq := seedRequestToSprout(req)
	resp, err := sp.currentClient().SendChatRequest(ctx, messages, sproutReq.Tools, sproutReq.Reasoning, false)
	if err != nil {
		return nil, err
	}
	return sproutResponseToSeed(resp), nil
}

// doChatStream performs a streaming chat request.
func (sp *sproutProvider) doChatStream(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	// Attach pasted images to the first user message
	messages := sp.attachPastedImages(req.Messages)

	sproutReq := seedRequestToSprout(req)

	// Route every chunk through OutputRouter.RouteStreamChunk so it reaches
	// BOTH paths:
	//   - EventBus → stream_chunk events → WebUI (real-time streaming)
	//   - streamingCallback → CLI terminal (character-by-character display)
	//
	// Previously the callback called the raw streamingCallback directly,
	// which is a no-op for WebUI agents — so stream_chunk events were never
	// published and the browser saw only the final query_completed payload.
	// RouteStreamChunk publishes the event AND calls the callback, so both
	// CLI and WebUI get every chunk with no duplication.
	callback := func(content string, contentType string) {
		if contentType == "reasoning" {
			sp.agent.output.GetReasoningBuffer().WriteString(content)
		} else {
			sp.agent.output.GetStreamingBuffer().WriteString(content)
		}
		if router := sp.agent.OutputRouter(); router != nil {
			router.RouteStreamChunk(content, contentType)
		}
	}

	resp, err := sp.currentClient().SendChatRequestStream(ctx, messages, sproutReq.Tools, sproutReq.Reasoning, false, callback)
	if err != nil {
		return nil, err
	}
	return sproutResponseToSeed(resp), nil
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

	if !sp.currentClient().SupportsVision() {
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

// Chat implements core.Provider
func (sp *sproutProvider) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return sp.doChatWithRetry(ctx, req)
}

func (sp *sproutProvider) ChatStream(ctx context.Context, req *core.ChatRequest, handler core.StreamHandler) error {
	sproutReq := seedRequestToSprout(req)

	// Attach pasted images to the first user message
	messages := sp.attachPastedImages(req.Messages)

	// Route through OutputRouter.RouteStreamChunk (same as doChatStream)
	// and forward to the seed handler so both the EventBus/WebUI and the
	// seed core's stream handling receive every chunk.
	callback := func(content string, contentType string) {
		if contentType == "reasoning" {
			handler.OnReasoning(content)
			sp.agent.output.GetReasoningBuffer().WriteString(content)
		} else {
			handler.OnContent(content)
			sp.agent.output.GetStreamingBuffer().WriteString(content)
		}
		if router := sp.agent.OutputRouter(); router != nil {
			router.RouteStreamChunk(content, contentType)
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

// doChatWithRetryStreaming performs a single streaming chat request with fleet budget tracking and error recording.
// Retry logic is handled by the seed core's chatFn in conversation.go;
// this layer no longer retries to avoid nested retry explosion.
func (sp *sproutProvider) doChatWithRetryStreaming(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	resp, err := sp.currentClient().SendChatRequestStream(ctx, messages, tools, reasoning, false, callback)
	if err != nil {
		sp.recordProviderError(err, 0)
		return nil, err
	}
	sp.clearProviderError()
	// Fleet budget tracking: debit tokens after each LLM call
	if budgetErr := sp.trackFleetBudgetForResponse(resp); budgetErr != nil {
		return nil, budgetErr
	}
	return resp, nil
}

func (sp *sproutProvider) Info() core.ProviderInfo {
	ctxLimit, _ := sp.currentClient().GetModelContextLimit()
	return core.ProviderInfo{
		Model:       sp.currentClient().GetModel(),
		ContextSize: ctxLimit,
		HasVision:   sp.currentClient().SupportsVision(),
	}
}

func (sp *sproutProvider) GetModel() string {
	if c := sp.currentClient(); c != nil {
		return c.GetModel()
	}
	return "unknown"
}

func (sp *sproutProvider) EstimateTokens(req *core.ChatRequest) int {
	if req == nil {
		return 0
	}
	// Delegate to sprout's centralized estimator, which accounts for:
	//   - per-message content + reasoning content (tiktoken-ish word/char hybrid)
	//   - tool-call payloads (id + name + args + overhead)
	//   - tool-result tool_call_id overhead
	//   - image attachments
	//   - per-message role/wrapper overhead
	//   - tool catalog (200 tokens × len(tools))
	//   - system-instruction buffer
	//
	// The previous "len(content) / 4" stub under-counted by an order of
	// magnitude on tool-heavy iter-0 prompts (the entire tool catalog and
	// every assistant tool_call payload went uncounted), making compaction
	// triggers and max_tokens math both stale on every call.
	//
	// core.Message and core.Tool are type aliases for api.Message / api.Tool
	// (see pkg/agent_api/types.go), so we can pass through directly.
	return api.EstimateInputTokens(req.Messages, req.Tools)
}
