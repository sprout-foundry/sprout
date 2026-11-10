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

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// bodyBudgetImageBytes is the pre-flight image budget, in base64 payload
// bytes, for a single provider request. It is a BYTES budget, not a count:
// a fixed part count either shed a vision model's images to make room for
// long history (the failure this replaces) or let a bulk-screenshot turn
// blow the provider's body limit. The budget covers the image parts only;
// the request's text gets the rest of the conservative 10MB server limit,
// and a 413 from a tighter limit triggers the shrink-and-retry cascade in
// the retry loops below, which sheds old history text before images.
const bodyBudgetImageBytes = 8 << 20

// bodyBudgetKeepTail is the number of most-recent non-system messages kept
// when a 413 persists after every inline image has been shed — the point at
// which the text body (system prompt + history) is the overflow. Each
// further retry halves the tail (see the 413 branch in the retry loops), so
// a still-too-large request converges on a small recent-turn window instead
// of failing the whole turn.
const bodyBudgetKeepTail = 16

// maxInPlaceRetries bounds the in-place retries of one model call: the
// attempts a path makes without backoff because the request itself was
// repaired (a shed 413 body, stripped malformed images). A repair either
// shrinks the request towards fitting or runs out of payload to drop, so a
// few attempts are enough; the bound keeps an always-413 endpoint from
// looping forever. Outage retries are bounded by time instead
// (providerRetryBudget).
const maxInPlaceRetries = 4

// doChatWithRetry performs a chat request, retrying transient failures for up
// to providerRetryBudget (see runWithProviderRetry). The loop lives in
// doChatRetrying; this wrapper supplies the real clock.
func (sp *sproutProvider) doChatWithRetry(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return sp.doChatRetrying(ctx, defaultProviderRetryClock(), req)
}

// doChatRetrying performs a chat request with the time-budget retry policy of
// runWithProviderRetry: each attempt goes through doChatOnce; a transient
// failure is retried after an exponential backoff until the budget, the
// context's deadline or the run's task deadline ends it. A malformed image
// payload and an HTTP 413 are handled in place (strip, shed) and retried at
// once, without counting against the backoff.
func (sp *sproutProvider) doChatRetrying(ctx context.Context, clock providerRetryClock, req *core.ChatRequest) (*core.ChatResponse, error) {
	var resp *core.ChatResponse
	attempt := func(attempt int) (error, bool) {
		r, err := sp.doChatOnce(ctx, req)
		if err == nil {
			resp = r
			sp.clearProviderError()
			// Fleet budget tracking: debit tokens after each LLM call.
			return sp.trackFleetBudgetForResponse(r), false
		}

		// A malformed inline image payload is a request-body problem, not a
		// transport one: the provider rejects the same bytes every time.
		// Strip the images that do not decode and retry once without them,
		// instead of burning the retry budget and reporting an
		// authentication failure. When nothing could be stripped, fail now
		// with a message naming the real cause.
		if isImagePayloadError(err) {
			stripped := stripInvalidImagePayloads(req.Messages)
			if len(stripped) != len(req.Messages) || messagesImageCount(stripped) != messagesImageCount(req.Messages) {
				req.Messages = stripped
				sp.clearPastedImages()
				return err, true
			}
			return classifyImagePayloadError(err), false
		}

		// HTTP 413 (body too large): the payload, not the endpoint, is the
		// problem. A vision model's 413 in a long session is almost always
		// the TEXT history overflowing, not a handful of images — so shed
		// text first: trim the oldest non-system history (a tail that halves
		// each retry) and leave the current turn's attached images alone,
		// since those are the user's explicit intent for this turn and
		// losing them is the failure this cascade must avoid. Only when the
		// text is already at its floor is the image mass the overflow —
		// then shed image bytes (most recent kept) and stop re-attaching the
		// current paste, since the images themselves now exceed the limit.
		// Without the text stage a long session whose text alone exceeds the
		// server limit 413s forever; without the image stage a bulk
		// screenshot turn does the same.
		if isBodyTooLargeError(err) {
			before := len(req.Messages)
			keep := max(bodyBudgetKeepTail>>uint(attempt), 1)
			req.Messages = api.TrimMessagesForBodyBudget(req.Messages, keep)
			if len(req.Messages) == before {
				// Text is at the floor: the image mass is the overflow.
				if n := api.MessagesBodyImageBytes(req.Messages); n > 0 {
					req.Messages = api.TrimImageBudget(req.Messages, n/2)
					sp.clearPastedImages()
				}
			}
			if attempt >= maxInPlaceRetries {
				return err, false
			}
			return err, true
		}

		sp.recordProviderError(err, attempt)
		return err, false
	}

	if err := runWithProviderRetry(ctx, clock, attempt, sp.providerRetryHooks()); err != nil {
		return nil, err
	}
	return resp, nil
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
	// Body budget: keep the inline image parts whose base64 mass fits the
	// budget (older images withheld with a note) so the request stays
	// under the provider's body limit.
	messages = api.TrimImageBudget(messages, bodyBudgetImageBytes)

	sproutReq := seedRequestToSprout(req)

	// Pre-compute max_tokens using the anchored token breakdown.
	sp.computeMaxTokensHint(req)

	// If the client supports max_tokens hints, set the pre-computed value.
	if h, ok := sp.currentClient().(providers.MaxTokensHinter); ok {
		h.SetMaxTokensHint(sp.getMaxTokensHint())
	}

	reasoning, disableThinking := sp.effectiveReasoning()
	if sproutReq.Reasoning != "" {
		reasoning = sproutReq.Reasoning
	}

	resp, err := sp.currentClient().SendChatRequest(ctx, messages, sproutReq.Tools, reasoning, disableThinking)
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
	// Body budget: keep the inline image parts whose base64 mass fits the
	// budget (older images withheld with a note) so the request stays
	// under the provider's body limit.
	messages = api.TrimImageBudget(messages, bodyBudgetImageBytes)

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
	// The reasoning-channel guard watches reasoning_content with the same
	// line-run detection. A confirmed reasoning loop does not cut the
	// stream — the reply that follows is usable and the loop is already in
	// the context the model reads next — so it steers instead: scrub the
	// degenerated reasoning from history and arm the nudge. It only
	// observes; reasoning chunks are delivered unchanged.
	reasoningGuard := NewRepetitionGuard(sp.reasoningGuardConfigFromAgent(), nil)
	callback := func(content string, contentType string) {
		if contentType == "reasoning" {
			reasoningGuard.Write(content)
			if reasoningGuard.Looping() {
				sp.steerReasoningLoop(reasoningGuard.LoopEvidence())
			}
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
	reasoning, disableThinking := sp.effectiveReasoning()
	if sproutReq.Reasoning != "" {
		reasoning = sproutReq.Reasoning
	}
	attemptFn := func(attemptCtx context.Context, msgs []api.Message, cb api.StreamCallback) (*api.ChatResponse, error) {
		return sp.currentClient().SendChatRequestStream(attemptCtx, msgs, sproutReq.Tools, reasoning, disableThinking, cb)
	}
	// The recovery FINAL attempt runs at a capped reasoning effort (see the
	// ChatStream counterpart): the degeneration is effort-dose dependent, so
	// re-seeding at the recommended agentic depth — never above the configured
	// effort, a no-op where the model has no effort knob — is the pre-model
	// lever that makes the recovery re-seed more likely to land.
	recoveryReasoning := cappedRecoveryReasoning(reasoning)
	recoveryAttemptFn := func(attemptCtx context.Context, msgs []api.Message, cb api.StreamCallback) (*api.ChatResponse, error) {
		return sp.currentClient().SendChatRequestStream(attemptCtx, msgs, sproutReq.Tools, recoveryReasoning, disableThinking, cb)
	}
	resp, err := sp.streamWithRepetitionGuard(ctx, messages, callback, holdback, guard, reasoningGuard, attemptFn, recoveryAttemptFn)
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
	// A steered turn's response is scrubbed before seed records it (see
	// scrubResponseReasoning): the mid-stream scrub only sees history that
	// already existed, so the reply that triggered the steering is cleaned
	// here, on its way into state.
	if sp.agent != nil && sp.agent.reasoningLoopNudgeForTurn() != "" {
		sp.scrubResponseReasoning(resp)
	}

	return sproutResponseToSeed(resp), nil
}
