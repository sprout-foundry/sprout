// seed_provider_chat.go — the sproutProvider chat engine: the Chat /
// ChatStream entry points and the streaming / non-streaming chat paths
// (doChatWithRetry, doChatWithRetryStreaming, doChatOnce, doChatNonStream,
// doChatStream), plus the turn-timestamp + pasted-image attach helpers.
// Split out of seed_provider.go.

package agent

import (
	"context"
	"strings"
	"time"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// maxLiveRequestImages caps the inline image parts a single provider
// request may carry. Each image is at most ~2.7MB base64 (the per-image
// inline cap in pkg/agent_tools), so three keep the worst-case image mass
// at ~8MB of body — under a conservative 10MB server body limit — while
// typical screenshots (200-500KB) let the bound rarely bind. Older images
// are withheld with a note; a 413 from a tighter limit triggers the
// shrink-and-retry cascade in the retry loops below.
const maxLiveRequestImages = 3

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
			retryable := attempt < maxRetries && isRetryableProviderError(err)
			if isBodyTooLargeError(err) {
				retryable = attempt < maxRetries && api.CountImages(req.Messages) > 0
			}
			sp.agent.publishRetryEvent(err, attempt, maxRetries, sp.agent.GetProvider(), retryable)
		}

		// HTTP 413 (body too large): the payload, not the endpoint, is the
		// problem — shed half the inline images (most recent kept) and
		// retry. n/2 == 0 strips every image, leaving the text-only
		// request that actually fits.
		if isBodyTooLargeError(err) {
			if n := api.CountImages(req.Messages); n > 0 && attempt < maxRetries {
				// Shed against the WIRE view: doChatOnce live-trims each
				// attempt to maxLiveRequestImages, so counting req.Messages
				// raw would let a large n keep shrinking to a value the
				// live trim re-inflates the wire back up (wasted retry).
				wire := n
				if wire > maxLiveRequestImages {
					wire = maxLiveRequestImages
				}
				req.Messages = api.TrimImagesBeyondLatest(req.Messages, wire/2)
				sp.clearPastedImages()
			} else {
				// Nothing left to shed, or out of attempts.
				return nil, err
			}
		} else if !isRetryableProviderError(err) {
			// Check if this error is retryable. If not, fail immediately.
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
		sp.tokenAnchor.update(sp.currentClient().GetModel(), req.Messages, len(req.Tools), resp.Usage.PromptTokens)
	}
	return resp, err
}

// doChatNonStream performs a non-streaming chat request.
func (sp *sproutProvider) doChatNonStream(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	// Attach pasted images before the turn-timestamp safety net stamps any
	// unstamped user message (legacy restored sessions).
	messages := sp.attachPastedImages(req.Messages)
	messages = sp.stampTurnTimestamp(messages)
	// Special-token truncation guard: observe seed nudges, add corrective
	// hint if the last assistant message ends in a control-token literal.
	// Runs AFTER stampTurnTimestamp so the turn timestamp lands on the
	// user's real message, not on the appended hint.
	messages = sp.observeAndHint(messages)
	sp.recordContinuationNudges(messages)
	// Body budget: cap the inline image parts (older images withheld with
	// a note) so the request stays under a conservative server body limit.
	messages = api.TrimImagesBeyondLatest(messages, maxLiveRequestImages)

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
	// Attach pasted images before the turn-timestamp safety net (see
	// doChatNonStream).
	messages := sp.attachPastedImages(req.Messages)
	messages = sp.stampTurnTimestamp(messages)
	// Special-token truncation guard (see doChatNonStream).
	messages = sp.observeAndHint(messages)
	sp.recordContinuationNudges(messages)
	// Body budget: cap the inline image parts (older images withheld with
	// a note) so the request stays under a conservative server body limit.
	messages = api.TrimImagesBeyondLatest(messages, maxLiveRequestImages)

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

// stampTurnTimestamp is the provider-boundary safety net for user messages
// that reached the wire without an injection-time stamp: legacy restored
// sessions (persisted before injection-time stamping), wakeup-batch edge
// cases, and any future injection path that misses the stamp. Normal turns
// arrive already stamped (see prepareQueryRun), so this returns the input
// unchanged for them — the HasPrefix check below is the fast path.
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

	if !api.ResolveVisionCapability(sp.currentClient()).AcceptsImages {
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

// Chat implements core.Provider
func (sp *sproutProvider) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	resp, err := sp.doChatWithRetry(ctx, req)
	sp.fireSteerFlushHook()
	return resp, err
}

func (sp *sproutProvider) ChatStream(ctx context.Context, req *core.ChatRequest, handler core.StreamHandler) error {
	sproutReq := seedRequestToSprout(req)

	messages := sp.attachPastedImages(req.Messages)
	messages = sp.stampTurnTimestamp(messages)
	messages = sp.observeAndHint(messages)
	sp.recordContinuationNudges(messages)
	// Body budget: cap the inline image parts so the request stays under
	// a conservative server body limit (older images withheld with a note).
	messages = api.TrimImagesBeyondLatest(messages, maxLiveRequestImages)

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
	sp.fireSteerFlushHook()
	if err != nil {
		handler.OnError(err)
		return err
	}
	// Anchor future EstimateTokens calls to this response's real prompt-token count.
	sp.tokenAnchor.update(sp.currentClient().GetModel(), req.Messages, len(req.Tools), resp.Usage.PromptTokens)
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
			retryable := attempt < maxRetries && isRetryableProviderError(err)
			if isBodyTooLargeError(err) {
				retryable = attempt < maxRetries && api.CountImages(messages) > 0
			}
			sp.agent.publishRetryEvent(err, attempt, maxRetries, sp.agent.GetProvider(), retryable)
		}

		// HTTP 413 (body too large): the payload, not the endpoint, is the
		// problem — shed half the inline images (most recent kept) and
		// retry. n/2 == 0 strips every image, leaving the text-only
		// request that actually fits.
		if isBodyTooLargeError(err) {
			if n := api.CountImages(messages); n > 0 && attempt < maxRetries {
				messages = api.TrimImagesBeyondLatest(messages, n/2)
			} else {
				// Nothing left to shed, or out of attempts.
				return nil, err
			}
		} else if !isRetryableProviderError(err) {
			// Check if this error is retryable. If not, fail immediately.
			return nil, err
		}

		// If we've exhausted retries, return the last error.
		if attempt >= maxRetries {
			return nil, err
		}
	}

	return nil, lastErr
}
