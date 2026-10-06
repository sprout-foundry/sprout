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
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/langguard"
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
	// SP-152 §152c: when the streaming hold-back is active for this turn,
	// gate assistant-text delivery through it. Reasoning chunks are never
	// gated. When inactive the assistant-text path below is byte-for-byte
	// unchanged (no hold-back, no extra behavior).
	userLang, holdbackActive := sp.agent.turnUserLanguageGuard()
	var holdback *StreamHoldback
	if holdbackActive {
		holdback = NewStreamHoldback(userLang, func(content string) {
			sp.agent.output.GetStreamingBuffer().WriteString(content)
			if router := sp.agent.OutputRouter(); router != nil {
				router.RouteStreamChunk(content, "assistant_text")
			}
		})
	}
	callback := func(content string, contentType string) {
		if contentType == "reasoning" {
			sp.agent.output.GetReasoningBuffer().WriteString(content)
		} else {
			if holdback != nil {
				holdback.Write(content)
				return
			}
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

	// Reasoning-model fallback: some models stream visible prose as
	// reasoning_content and put the visible text only in the final response.
	// When nothing was delivered as assistant-text but the response has
	// content, deliver it — through the hold-back when it is active (so a
	// wrong-language fallback is held like any streamed chunk), directly
	// otherwise. When the hold-back already holds this response's content (the
	// stream delivered it), the finalize below releases or holds it, so the
	// fallback must not re-deliver it.
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
	}

	// SP-152 §152c: finalize the hold-back for this streamed response —
	// release a below-threshold stream, and (when held) regenerate the reply
	// and deliver the corrected text (or the §152b notice) instead of the
	// wrong-language stream.
	if holdback != nil {
		sp.finalizeStreamHoldback(ctx, holdback, resp)
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
	// SP-152 §152c: when the streaming hold-back is active for this turn, gate
	// assistant-text delivery through it. Reasoning chunks are never gated.
	// When inactive the assistant-text path below is byte-for-byte unchanged.
	userLang, holdbackActive := sp.agent.turnUserLanguageGuard()
	var holdback *StreamHoldback
	if holdbackActive {
		holdback = NewStreamHoldback(userLang, func(content string) {
			handler.OnContent(content)
			sp.agent.output.GetStreamingBuffer().WriteString(content)
			if router := sp.agent.OutputRouter(); router != nil {
				router.RouteStreamChunk(content, "assistant_text")
			}
		})
	}
	callback := func(content string, contentType string) {
		if contentType == "reasoning" {
			handler.OnReasoning(content)
			sp.agent.output.GetReasoningBuffer().WriteString(content)
		} else {
			if holdback != nil {
				holdback.Write(content)
				return
			}
			handler.OnContent(content)
			sp.agent.output.GetStreamingBuffer().WriteString(content)
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

// finalizeStreamHoldback finalizes the streaming hold-back for one streamed
// response (SP-152 §152c). It releases a stream that never reached the prose
// threshold (short or code-only — not judged, so it must not be held). If the
// stream was held (a reliable language mismatch at the START), it keeps the
// held content on the response message's Meta (langGuardOriginalMetaKey) for
// "view original" and — for the turn's FINAL (no-tool-call) response only —
// regenerates the reply once in the user's language and delivers the
// regenerated text through the hold-back's sink (the same client-facing
// delivery as released content), so the user sees the corrected language as
// normal streamed content; when the regeneration errors or still mismatches
// it delivers the localized §152b notice instead. The delivered text —
// regenerated or notice — is also written back onto the response message
// (the message seed's chat loop records in state and returns as the turn
// result), so the client's buffer and the agent's state carry the SAME text
// and the final-message guard (applyLanguageGuard) sees the repaired reply,
// not the held original — no second regeneration, no display-vs-state
// split. A held mid-turn preamble (a reply carrying tool calls) stays
// suppressed and shows nothing: the user-facing repair belongs to the turn's
// final answer, handled by the final-message guard. Either held path is the
// terminal handling for the reply: it is NOT re-checked (a second event for
// the same reply would be a duplicate).
//
// A RELEASED stream (the start passed, or it was below the threshold) was
// already streamed to the client and cannot be un-streamed. If it switched
// language mid-stream, the completion re-check (recheckStreamedReply)
// re-judges the FULL content and, on a reliable mismatch, regenerates it and
// tells the client to replace the already-streamed message with a server
// event.
func (sp *sproutProvider) finalizeStreamHoldback(ctx context.Context, holdback *StreamHoldback, resp *api.ChatResponse) {
	holdback.Finish()
	if held := holdback.Held(); held != "" {
		// The stream is held (a reliable mismatch at the start): keep the held
		// content for "view original" regardless of what is shown.
		if resp != nil && len(resp.Choices) > 0 {
			resp.Choices[0].Message.SetMeta(langGuardOriginalMetaKey, held)
		}
		// A held mid-turn preamble (a reply carrying tool calls that continues
		// the turn) stays suppressed and shows nothing: the user-facing repair
		// belongs to the turn's final answer, handled by the final-message
		// guard. Only the final (no-tool-call) reply is repaired here.
		if streamResponseHasToolCalls(resp) {
			return
		}
		// A held FINAL (no-tool-call) reply: regenerate it once in the
		// user's language. When the regeneration passes the language check,
		// deliver the regenerated text through the hold-back's sink — the
		// same client-facing delivery as released content — so the CLI and
		// the web UI receive the corrected language as normal streamed content
		// instead of a notice. When the regeneration errors or still
		// mismatches, fall back to the localized §152b notice.
		display := LanguageMismatchNotice(holdback.User())
		if regenerated, ok := sp.regenerateStreamedReply(ctx, held, holdback.User()); ok {
			display = regenerated
		}
		holdback.Deliver(display)
		// State consistency: the response message is what seed's chat loop
		// records in state and returns as the turn's result, so it must carry
		// the same text the client's buffer received, plus the repaired
		// marker — applyLanguageGuard then sees the repaired reply and skips
		// it: one regeneration, one displayed text, state and client in
		// agreement, one metric count.
		//
		// Marker and metric are recorded TOGETHER, only when there is a
		// message to mark: on a pathological successful stream that returns a
		// nil/empty-choices response there is nothing to mark, so the (still
		// wrong-language) reply would be re-judged by applyLanguageGuard
		// anyway — counting the mismatch here would double-count it.
		if resp != nil && len(resp.Choices) > 0 {
			resp.Choices[0].Message.Content = display
			resp.Choices[0].Message.SetMeta(langGuardRepairedMetaKey, "held_stream")
			// SP-152 §152e: the model produced a mismatched user-facing reply;
			// record one check + one mismatch here (the reply was repaired during
			// the stream, so applyLanguageGuard will not judge it again — this is
			// the only count it gets).
			sp.recordLanguageGuardMetric()
		}
		return
	}
	// Released (or below threshold): the reply reached the client. Re-check
	// the full content for a mid-stream switch.
	sp.recheckStreamedReply(ctx, holdback, resp)
}

// recordLanguageGuardMetric records one check + one mismatch for the agent's
// (model, role) cell (SP-152 §152e). The streaming repair sites call it for a
// reply they detected as a mismatch and repaired themselves — the final-message
// guard skips already-repaired replies (the meta marker), so this is the one
// count those replies get.
func (sp *sproutProvider) recordLanguageGuardMetric() {
	GlobalLanguageGuardMetrics().Record(sp.agent.GetModel(), sp.agent.GetRole(), true)
}

// streamResponseHasToolCalls reports whether a streamed response carries tool
// calls — i.e. it is a mid-turn reply (the turn continues with tool
// execution) rather than the turn's final (no-tool-call) answer. The
// language-guard repair applies to the final answer only.
func streamResponseHasToolCalls(resp *api.ChatResponse) bool {
	return resp != nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0
}

// regenerateStreamedReply issues the single regeneration for a held or
// mid-stream-switched streamed reply (SP-152 §152b): it regenerates content
// once in the user's language — one model call, grounded in the turn's user
// message (the streaming path has no query-run context, so it uses the turn's
// stored user message and the agent's client) — and reports whether the
// regenerated text passes the language check. It returns the regenerated text
// (for delivery / replacement) and ok=true when the check passes (not a
// mismatch); ok=false when the regeneration errors, is empty, or still
// mismatches (the caller then falls back to the notice). It records no
// metric: the caller that consumes its outcome records the mismatch exactly
// once.
func (sp *sproutProvider) regenerateStreamedReply(ctx context.Context, content string, user langguard.Language) (string, bool) {
	regenerated, err := sp.agent.regenerateInUserLanguageCore(ctx, sp.agent.storedTurnUserQuery(), content, user)
	if err != nil {
		if sp.agent.debug {
			sp.agent.Logger().Debug("[langguard] streaming regeneration failed: %v\n", err)
		}
		return "", false
	}
	if strings.TrimSpace(regenerated) == "" {
		return "", false
	}
	if langguard.CheckLanguage(regenerated, user) == langguard.VerdictMismatch {
		return "", false
	}
	return regenerated, true
}

// recheckStreamedReply re-checks a RELEASED (non-held) streamed reply at
// completion (SP-152 §152c, item 152.7). The hold-back only judged the START
// of the stream; a reply whose start was fine but which switched language
// later is already streamed to the client and cannot be un-streamed. So it is
// re-judged here on its FULL content: when the full reply is a reliable
// mismatch (a mid-stream switch), it is regenerated once in the user's
// language and the client is told — via a language_guard_replacement event —
// to replace the already-streamed message. The replacement carries the
// REGENERATED text when the regeneration passes the language check (so the
// client shows the corrected reply, not just a notice), falling back to the
// localized §152b-style notice when the regeneration errors or still
// mismatches; the original (full switched content) is always carried for
// "view original" and kept on the message Meta.
//
// No event is published when:
//   - the full content is not judgable (short or code-only — §152a: not
//     judged);
//   - the full content is not a reliable mismatch (the reply is in the user's
//     language, or detection is undetermined); or
//   - the reply carries tool calls (a mid-turn preamble — the user-facing
//     repair belongs to the turn's final answer, mirroring the held path).
//
// The hold-back is only non-nil (and thus only finalized) when it is active
// for the turn (guard on, non-subagent, determined user language), so the
// "guard off / undetermined / subagent" cases never reach this function.
func (sp *sproutProvider) recheckStreamedReply(ctx context.Context, holdback *StreamHoldback, resp *api.ChatResponse) {
	full := holdback.Full()
	if !langguard.Judgable(langguard.ExtractProse(full)) {
		// Below the prose threshold: §152a says not judged — no re-check.
		return
	}
	if langguard.CheckLanguage(full, holdback.User()) != langguard.VerdictMismatch {
		// No mid-stream switch: the full reply is the user's language (or
		// detection is undetermined) — nothing to replace.
		return
	}
	if streamResponseHasToolCalls(resp) {
		// A mid-turn preamble whose start passed the hold-back: the turn
		// continues with tool execution, so no user-facing repair here —
		// mirroring the held path (the final answer is what gets repaired).
		return
	}
	// A reliable mid-stream switch: regenerate the full reply once in the
	// user's language. The replacement carries the regenerated text when it
	// passes the language check (the corrected reply, not just a notice),
	// falling back to the localized §152b-style notice when the regeneration
	// errors or still mismatches.
	replacement := LanguageMismatchNotice(holdback.User())
	if regenerated, ok := sp.regenerateStreamedReply(ctx, full, holdback.User()); ok {
		replacement = regenerated
	}
	sp.agent.publishEvent(
		events.EventTypeLanguageGuardReplacement,
		events.LanguageGuardReplacementEvent(
			sp.agent.GetChatID(),
			replacement,
			full,
			"mid_stream_switch",
		),
	)
	if sp.agent.debug {
		sp.agent.Logger().Debug("[langguard] streamed reply switched language mid-stream (%s): replacement event published\n", holdback.User())
	}
	// Keep the full switched content on the message Meta for "view original"
	// (mirrors the held path's language_guard_original) and write the
	// replacement into the message content — it is what seed's chat loop
	// records in state and returns as the turn's result, so state carries
	// what the replacement event told the client to show. The meta marker
	// marks the reply repaired, so applyLanguageGuard neither re-generates
	// nor re-publishes (no duplicate replacement event) and the metric counts
	// the mismatch here — the one count it gets.
	//
	// Marker and metric are recorded TOGETHER, only when there is a message
	// to mark: on a pathological successful stream that returns a
	// nil/empty-choices response there is nothing to mark, so the (still
	// wrong-language) reply would be re-judged by applyLanguageGuard anyway —
	// counting the mismatch here would double-count it.
	if resp != nil && len(resp.Choices) > 0 {
		resp.Choices[0].Message.SetMeta(langGuardOriginalMetaKey, full)
		resp.Choices[0].Message.SetMeta(langGuardRepairedMetaKey, "mid_stream_switch")
		resp.Choices[0].Message.Content = replacement
		sp.recordLanguageGuardMetric()
	}
}
