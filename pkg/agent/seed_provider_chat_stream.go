// seed_provider_chat_stream.go — the sproutProvider chat engine's message
// preparation and streaming entry points: the turn-timestamp and pasted-image
// attach helpers (stampTurnTimestamp, attachPastedImages), the ChatStream
// entry point, and the streaming retry layer (doChatWithRetryStreaming).
// Split out of seed_provider_chat.go.

package agent

import (
	"context"
	"strings"
	"time"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

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
	// SP-152 §152c: when the streaming hold-back is active for this turn, gate
	// assistant-text delivery through it. Reasoning chunks are never gated.
	// When inactive the assistant-text path below is byte-for-byte unchanged.
	userLang, holdbackActive := sp.agent.turnUserLanguageGuard()
	var holdback *StreamHoldback
	if holdbackActive {
		holdback = NewStreamHoldback(userLang, func(content string) {
			handler.OnContent(content)
			sp.agent.output.GetStreamingBuffer().WriteString(content) //nolint:gosec // G104: bytes.Buffer writes cannot fail
			if router := sp.agent.OutputRouter(); router != nil {
				router.RouteStreamChunk(content, "assistant_text")
			}
		})
	}
	callback := func(content string, contentType string) {
		if contentType == "reasoning" {
			handler.OnReasoning(content)
			sp.agent.output.GetReasoningBuffer().WriteString(content) //nolint:gosec // G104: bytes.Buffer writes cannot fail
		} else {
			if holdback != nil {
				holdback.Write(content)
				return
			}
			handler.OnContent(content)
			sp.agent.output.GetStreamingBuffer().WriteString(content) //nolint:gosec // G104: bytes.Buffer writes cannot fail
		}
		if router := sp.agent.OutputRouter(); router != nil {
			router.RouteStreamChunk(content, contentType)
		}
	}

	// Use doChatWithRetry for streaming too, but wrap it to deliver through the handler
	resp, err := sp.doChatWithRetryStreaming(ctx, messages, sproutReq.Tools, sproutReq.Reasoning, callback, holdback)
	sp.fireSteerFlushHook()
	if err != nil {
		handler.OnError(err)
		return err
	}
	// SP-152 §152c: finalize the hold-back for this streamed response —
	// release a below-threshold stream, and (when held) regenerate the reply
	// and deliver the corrected text (or the §152b notice) instead of the
	// wrong-language stream.
	if holdback != nil {
		sp.finalizeStreamHoldback(ctx, holdback, resp)
	}
	// Anchor future EstimateTokens calls to this response's real prompt-token count.
	sp.tokenAnchor.update(sp.currentClient().GetModel(), req.Messages, len(req.Tools), resp.Usage.PromptTokens)
	handler.OnDone(sproutResponseToSeed(resp))
	return nil
}

// doChatWithRetryStreaming performs a streaming chat request with exponential backoff retry (max 3).
// holdback (nil when the hold-back is inactive) is reset at the start of each
// attempt so a failed or wrong-language attempt never leaks into the next; the
// caller finalizes it (finalizeStreamHoldback) after a successful attempt.
func (sp *sproutProvider) doChatWithRetryStreaming(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback, holdback *StreamHoldback) (*api.ChatResponse, error) {
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
		// SP-152 §152c: each attempt is a separate streamed response — start
		// its hold-back fresh (discard the previous attempt's buffered/held
		// content) so a failed or wrong-language attempt never leaks into the
		// next one.
		if holdback != nil {
			holdback.Reset()
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
