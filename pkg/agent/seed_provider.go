// Package agent provides the seed integration layer.
// sproutProvider implements seed/core.Provider by wrapping api.ClientInterface.

package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// sproutProvider adapts sprout's ClientInterface to seed's Provider interface.
type sproutProvider struct {
	agent          *Agent
	client         api.ClientInterface
	pastedImages   map[string][]api.ImageData
	pastedImagesMu sync.RWMutex
	tokenAnchor    tokenAnchor
	maxTokensHint   int
	maxTokensHintMu sync.RWMutex
}

// currentClient returns the agent's live client if available, otherwise the snapshot.
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

// RegisterPastedImages associates extracted image data with file paths for multimodal attachment.
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

// exponentialBackoffDelay calculates the delay for a given retry attempt.
// Formula: 2^attempt * baseDelay, capped at maxDelay.
// Base delay is 100ms, max delay is 10s.
func exponentialBackoffDelay(attempt int) time.Duration {
	const baseDelay = 100 * time.Millisecond
	const maxDelay = 10 * time.Second

	delay := baseDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// isRetryableProviderError returns true if the error should trigger a provider retry.
// Retries are performed on:
//   - RateLimitError (always retryable)
//   - ProviderError with a retryable cause (server errors, overload)
func isRetryableProviderError(err error) bool {
	if err == nil {
		return false
	}
	if agenterrors.IsRateLimited(err) {
		return true
	}
	if agenterrors.IsProviderError(err) && agenterrors.IsRetryable(err) {
		return true
	}
	return false
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

// doChatWithRetry performs a chat request with exponential backoff retry (max 3).
func (sp *sproutProvider) doChatWithRetry(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// On retry attempts, wait for the backoff delay before retrying.
		if attempt > 0 {
			delay := exponentialBackoffDelay(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := sp.doChatOnce(ctx, req)
		if err == nil {
			sp.clearProviderError()
			// Fleet budget tracking: debit tokens after each LLM call.
			if budgetErr := sp.trackFleetBudgetForResponse(resp); budgetErr != nil {
				return nil, budgetErr
			}
			return resp, nil
		}

		lastErr = err

		// Record the error for observability and emit retry event.
		sp.recordProviderError(err, attempt)
		if sp.agent != nil && sp.agent.eventBus != nil {
			sp.agent.publishRetryEvent(err, attempt, maxRetries, sp.agent.GetProvider())
		}

		// Check if this error is retryable. If not, fail immediately.
		if !isRetryableProviderError(err) {
			return nil, err
		}

		// If we've exhausted retries, return the last error.
		if attempt >= maxRetries {
			return nil, err
		}
	}

	return nil, lastErr
}

// doChatOnce performs a single chat request, attaching pasted images if supported.
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
		sp.tokenAnchor.update(req.Messages, len(req.Tools), resp.Usage.PromptTokens)
	}
	return resp, err
}

// accumulateResponseCost adds the provider-reported cost to the agent's lifetime cost counter.
// Also populates prompt/completion token breakdowns and debits the fleet USD budget.
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
		ImageTokens:      resp.Usage.ImageTokens,
	}
	sp.agent.state.AddCostEntry(entry)

	sp.agent.state.SetPromptTokens(sp.agent.state.GetPromptTokens() + resp.Usage.PromptTokens)
	sp.agent.state.SetCompletionTokens(sp.agent.state.GetCompletionTokens() + resp.Usage.CompletionTokens)
	sp.agent.state.SetLLMCallCount(sp.agent.state.GetLLMCallCount() + 1)

	// Debit the fleet USD budget (only charged cost, not subscription/free).
	if sp.agent.fleetUsdBudget != nil && chargedCost > 0 {
		spent, crossed, justExceeded := sp.agent.fleetUsdBudget.Add(chargedCost)
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

	if n := resp.Usage.CachedTokens; n > 0 {
		sp.agent.state.SetCachedTokens(sp.agent.state.GetCachedTokens() + n)
	}
	if resp.Usage.CacheWriteTokens != nil {
		if n := *resp.Usage.CacheWriteTokens; n > 0 {
			sp.agent.state.SetCacheWriteTokens(sp.agent.state.GetCacheWriteTokens() + n)
		}
	}
	if n := resp.Usage.ImageTokens; n > 0 {
		sp.agent.state.SetImageTokens(sp.agent.state.GetImageTokens() + n)
	}
}

// resolveBillingType returns the billing model for the current provider.
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

// estimateCostFromPricing computes a cost estimate from token counts and per-million pricing.
func (sp *sproutProvider) estimateCostFromPricing(promptTokens, completionTokens int) float64 {
	if sp.agent == nil || sp.agent.client == nil {
		return 0
	}
	model := sp.agent.client.GetModel()
	if model == "" {
		return 0
	}

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

	provider := sp.agent.GetProvider()
	if inPerM, outPerM, _, ok := providercatalog.FindModelPricing(provider, model); ok {
		return float64(promptTokens)/1e6*inPerM + float64(completionTokens)/1e6*outPerM
	}

	return 0
}

// doChatNonStream performs a non-streaming chat request.
func (sp *sproutProvider) doChatNonStream(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	// Attach pasted images before adding the provider-only turn timestamp.
	messages := sp.attachPastedImages(req.Messages)
	messages = sp.stampTurnTimestamp(messages)

	sproutReq := seedRequestToSprout(req)

	// Pre-compute max_tokens using the anchored token breakdown.
	sp.computeMaxTokensHint(req)

	// If the client supports max_tokens hints, set the pre-computed value.
	if h, ok := sp.currentClient().(providers.MaxTokensHinter); ok {
		h.SetMaxTokensHint(sp.getMaxTokensHint())
	}

	resp, err := sp.currentClient().SendChatRequest(ctx, messages, sproutReq.Tools, sproutReq.Reasoning, false)
	if err != nil {
		return nil, err
	}
	return sproutResponseToSeed(resp), nil
}

// doChatStream performs a streaming chat request.
func (sp *sproutProvider) doChatStream(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	// Attach pasted images before adding the provider-only turn timestamp.
	messages := sp.attachPastedImages(req.Messages)
	messages = sp.stampTurnTimestamp(messages)

	sproutReq := seedRequestToSprout(req)

	// Pre-compute max_tokens using the anchored token breakdown.
	sp.computeMaxTokensHint(req)

	// If the client supports max_tokens hints, set the pre-computed value.
	if h, ok := sp.currentClient().(providers.MaxTokensHinter); ok {
		h.SetMaxTokensHint(sp.getMaxTokensHint())
	}

	// Route every chunk through OutputRouter.RouteStreamChunk for both WebUI and CLI.
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

	// Reasoning-model fallback: some models stream visible prose as reasoning_content.
	// If the streaming buffer is empty but the response has content, stream it.
	if resp != nil && len(resp.Choices) > 0 {
		msgContent := resp.Choices[0].Message.Content
		if sp.agent.output.GetStreamingBuffer().Len() == 0 && strings.TrimSpace(msgContent) != "" {
			sp.agent.output.GetStreamingBuffer().WriteString(msgContent)
			if router := sp.agent.OutputRouter(); router != nil {
				for _, line := range strings.SplitAfter(msgContent, "\n") {
					if line != "" {
						router.RouteStreamChunk(line, "assistant_text")
					}
				}
			}
		}
	}

	return sproutResponseToSeed(resp), nil
}

// stampTurnTimestamp adds the current turn's fixed timestamp to the latest user message.
func (sp *sproutProvider) stampTurnTimestamp(messages []core.Message) []core.Message {
	if sp.agent == nil {
		return messages
	}
	sp.agent.turnTimestampMu.RLock()
	turnTimestamp := sp.agent.turnTimestamp
	sp.agent.turnTimestampMu.RUnlock()
	if turnTimestamp.IsZero() {
		return messages
	}

	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != "user" {
			continue
		}
		if strings.HasPrefix(message.Content, "<current-time>") {
			return messages
		}
		out := make([]core.Message, len(messages))
		copy(out, messages)
		out[i].Content = InjectUserMessageTimestampAt(message.Content, turnTimestamp)
		return out
	}
	return messages
}

// attachPastedImages attaches previously registered image data to the first user message.
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

// trackFleetBudgetForResponse debits tokens from this LLM response to the fleet budget tracker.
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

	messages := sp.attachPastedImages(req.Messages)
	messages = sp.stampTurnTimestamp(messages)

	sp.computeMaxTokensHint(req)

	if h, ok := sp.currentClient().(providers.MaxTokensHinter); ok {
		h.SetMaxTokensHint(sp.getMaxTokensHint())
	}

	// Route through OutputRouter.RouteStreamChunk for both WebUI and seed handler.
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
	// Anchor future EstimateTokens calls to this response's real prompt-token count.
	sp.tokenAnchor.update(req.Messages, len(req.Tools), resp.Usage.PromptTokens)
	handler.OnDone(sproutResponseToSeed(resp))
	return nil
}

// doChatWithRetryStreaming performs a streaming chat request with exponential backoff retry (max 3).
func (sp *sproutProvider) doChatWithRetryStreaming(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// On retry attempts, wait for the backoff delay before retrying.
		if attempt > 0 {
			delay := exponentialBackoffDelay(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := sp.currentClient().SendChatRequestStream(ctx, messages, tools, reasoning, false, callback)
		if err == nil {
			sp.clearProviderError()
			// Fleet budget tracking: debit tokens after each LLM call
			if budgetErr := sp.trackFleetBudgetForResponse(resp); budgetErr != nil {
				return nil, budgetErr
			}
			return resp, nil
		}

		lastErr = err

		// Record the error for observability and emit retry event.
		sp.recordProviderError(err, attempt)
		if sp.agent != nil && sp.agent.eventBus != nil {
			sp.agent.publishRetryEvent(err, attempt, maxRetries, sp.agent.GetProvider())
		}

		// Check if this error is retryable. If not, fail immediately.
		if !isRetryableProviderError(err) {
			return nil, err
		}

		// If we've exhausted retries, return the last error.
		if attempt >= maxRetries {
			return nil, err
		}
	}

	return nil, lastErr
}

func (sp *sproutProvider) Info() core.ProviderInfo {
	ctxLimit, _ := sp.currentClient().GetModelContextLimit()
	// Apply the effective context cap so seed's internal budget math receives the capped value.
	if sp.agent != nil {
		if cap := sp.agent.effectiveContextCap; cap > 0 && ctxLimit > cap {
			ctxLimit = cap
		}
	}
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

// EstimateTokens estimates the input token count for a chat request.
// Uses the token anchor when available; falls back to the centralized estimator.
func (sp *sproutProvider) EstimateTokens(req *core.ChatRequest) int {
	if req == nil {
		return 0
	}
	// Anchor to the last real Usage.PromptTokens count when the message prefix still matches.
	// Falls back to a full from-scratch heuristic estimate on the first call or after compaction.
	if total, _, ok := sp.tokenAnchor.estimate(req.Messages, len(req.Tools)); ok {
		return total
	}

	// Delegate to sprout's centralized estimator.
	return api.EstimateInputTokens(req.Messages, req.Tools)
}

// setMaxTokensHint stores a pre-computed max_tokens hint for the next request.
func (sp *sproutProvider) setMaxTokensHint(v int) {
	sp.maxTokensHintMu.Lock()
	sp.maxTokensHint = v
	sp.maxTokensHintMu.Unlock()
}

// getMaxTokensHint returns the pre-computed max_tokens hint.
func (sp *sproutProvider) getMaxTokensHint() int {
	sp.maxTokensHintMu.RLock()
	v := sp.maxTokensHint
	sp.maxTokensHintMu.RUnlock()
	return v
}

// getContextLimit returns the effective context limit for the current provider.
func (sp *sproutProvider) getContextLimit() int {
	info := sp.Info()
	if info.ContextSize > 0 {
		return info.ContextSize
	}
	return 32000
}

// computeMaxTokensHint pre-computes max_tokens using the anchored token breakdown when available.
func (sp *sproutProvider) computeMaxTokensHint(req *core.ChatRequest) {
	if req == nil {
		sp.setMaxTokensHint(0)
		return
	}
	total, heuristic, ok := sp.tokenAnchor.estimate(req.Messages, len(req.Tools))
	if !ok {
		sp.setMaxTokensHint(0) // no hint — let provider compute from scratch
		return
	}
	contextLimit := sp.getContextLimit()
	maxOutput, budgetOK := api.CalculateOutputBudgetAnchored(contextLimit, total-heuristic, heuristic)
	if !budgetOK {
		sp.setMaxTokensHint(0)
		return
	}
	sp.setMaxTokensHint(maxOutput)
}
