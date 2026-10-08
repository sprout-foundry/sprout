// seed_provider_chat.go — the sproutProvider chat engine: the Chat entry
// point, the non-streaming chat path (doChatWithRetry, doChatOnce,
// doChatNonStream), and the non-streaming stream-callback path (doChatStream).
// The streaming entry points and message preparation live in
// seed_provider_chat_stream.go; the streaming language-guard repair lives in
// seed_provider_chat_langguard.go. Split out of seed_provider.go.

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
	// When the streaming hold-back is active for this turn,
	// gate assistant-text delivery through it. Reasoning chunks are never
	// gated. When inactive the assistant-text path below is byte-for-byte
	// unchanged (no hold-back, no extra behavior).
	userLang, holdbackActive := sp.agent.turnUserLanguageGuard()
	var holdback *StreamHoldback
	if holdbackActive {
		holdback = NewStreamHoldback(userLang, func(content string) {
			_, _ = sp.agent.output.GetStreamingBuffer().WriteString(content)
			if router := sp.agent.OutputRouter(); router != nil {
				router.RouteStreamChunk(content, "assistant_text")
			}
		})
	}
	// The repetition guard is composed OUTSIDE the hold-back: assistant text
	// flows guard → hold-back → client sink. When the guard is disabled it is
	// a passthrough and the path is byte-for-byte unchanged.
	guardSink := func(content string) {
		if holdback != nil {
			holdback.Write(content)
			return
		}
		_, _ = sp.agent.output.GetStreamingBuffer().WriteString(content)
		if router := sp.agent.OutputRouter(); router != nil {
			router.RouteStreamChunk(content, "assistant_text")
		}
	}
	guard := NewRepetitionGuard(sp.repetitionGuardConfigFromAgent(), guardSink)
	callback := func(content string, contentType string) {
		if contentType == "reasoning" {
			sp.agent.output.GetReasoningBuffer().WriteString(content)
			if router := sp.agent.OutputRouter(); router != nil {
				router.RouteStreamChunk(content, contentType)
			}
			return
		}
		// Assistant text flows through the guard and its sink (the hold-back
		// or the direct client delivery), which owns the routing.
		guard.Write(content)
	}

	// The buffer-only path keeps its single-attempt transport (it had no
	// backoff retries before the guard existed); the repetition guard adds
	// only the cut-and-retry-once handling on top.
	attemptFn := func(attemptCtx context.Context, msgs []api.Message, cb api.StreamCallback) (*api.ChatResponse, error) {
		return sp.currentClient().SendChatRequestStream(attemptCtx, msgs, sproutReq.Tools, sproutReq.Reasoning, false, cb)
	}
	resp, err := sp.streamWithRepetitionGuard(ctx, messages, callback, holdback, guard, attemptFn)
	if err != nil {
		return nil, err
	}

	// Reasoning-model fallback: some models stream visible prose as
	// reasoning_content and put the visible text only in the final response.
	// When nothing was delivered as assistant-text but the response has
	// content, deliver it — through the hold-back when it is active (so a
	// wrong-language fallback is held like any streamed chunk), directly
	// otherwise. When the hold-back already holds this response's content (the
	// stream delivered it), the finalize below releases or holds it, so the
	// fallback must not re-deliver it. The fallback is a single post-hoc blob,
	// not a per-token stream, so it is not re-checked for repetition.
	if resp != nil && len(resp.Choices) > 0 {
		msgContent := resp.Choices[0].Message.Content
		if sp.agent.output.GetStreamingBuffer().Len() == 0 && strings.TrimSpace(msgContent) != "" {
			switch {
			case holdback != nil && holdback.RawLen() > 0:
				// The hold-back already holds this response's content (the
				// stream delivered it); the finalize below releases or holds
				// it.
			case holdback != nil:
				// The hold-back is active but holds nothing (a reasoning-model
				// stream); route the content through it so it is
				// language-gated, then the finalize releases or holds it.
				holdback.Write(msgContent)
			default:
				_, _ = sp.agent.output.GetStreamingBuffer().WriteString(msgContent)
				if router := sp.agent.OutputRouter(); router != nil {
					for _, line := range strings.SplitAfter(msgContent, "\n") {
						if line != "" {
							router.RouteStreamChunk(line, "assistant_text")
						}
					}
				}
			}
		}
	}

	// Finalize the hold-back for this streamed response —
	// release a below-threshold stream, and (when held) regenerate the reply
	// and deliver the corrected text (or the language notice) instead of the
	// wrong-language stream.
	if holdback != nil {
		sp.finalizeStreamHoldback(ctx, holdback, resp)
	}

	return sproutResponseToSeed(resp), nil
}
