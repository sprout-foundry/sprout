// seed_provider_chat_stream.go — the sproutProvider chat engine's message
// preparation and streaming entry points: the turn-timestamp and pasted-image
// attach helpers (stampTurnTimestamp, attachPastedImages), the ChatStream
// entry point, and the streaming retry layer (doChatWithRetryStreaming).
// Split out of seed_provider_chat.go.

package agent

import (
	"context"
	"strings"

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

// attachPastedImages attaches previously registered image data to the
// current turn's user message. The target is the last user message carrying
// the image label processImagesInQuery rewrote into the query
// ("[image: name]" / "[image N of M: name]"); the label keeps the attachment
// off a steer message seed may append at a later loop boundary. Falls back to
// the last user message when no label is present.
//
// Attaching to the first user message instead would park the pixels on an old
// turn in a continuing conversation, disconnect them from the question, and
// flip the provider prompt-cache prefix on every paste.
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

	target := -1
	fallback := -1
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != "user" {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		if strings.Contains(out[i].Content, "[image") {
			target = i
			break
		}
	}
	if target < 0 {
		target = fallback
	}
	if target < 0 {
		return out
	}

	// Collect all registered image data
	var allImages []api.ImageData
	for _, imgs := range sp.pastedImages {
		allImages = append(allImages, imgs...)
	}
	if len(allImages) > 0 {
		// Append to any existing images
		out[target].Images = append(out[target].Images, allImages...)
	}

	return out
}

// Chat implements core.Provider
func (sp *sproutProvider) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	req = sp.withReasoningLoopNudge(req)
	resp, err := sp.doChatWithRetry(ctx, req)
	sp.fireSteerFlushHook()
	return resp, err
}

// withReasoningLoopNudge appends the turn's armed reasoning-loop nudge to this
// request's system prompt. Every request of a steered turn carries it, so the
// iteration after a tool call is steered too; prepareQueryRun clears it at the
// next turn's start. A request with no system message gets one — the
// instruction must not be lost.
func (sp *sproutProvider) withReasoningLoopNudge(req *core.ChatRequest) *core.ChatRequest {
	if sp.agent == nil || req == nil {
		return req
	}
	nudge := sp.agent.reasoningLoopNudgeForTurn()
	if nudge == "" {
		return req
	}
	messages := make([]core.Message, 0, len(req.Messages)+1)
	appended := false
	for _, msg := range req.Messages {
		messages = append(messages, msg)
		if !appended && msg.Role == "system" {
			messages[len(messages)-1].Content = msg.Content + "\n\n" + nudge
			appended = true
		}
	}
	if !appended {
		messages = append([]core.Message{{Role: "system", Content: nudge}}, messages...)
	}
	steered := *req
	steered.Messages = messages
	return &steered
}

func (sp *sproutProvider) ChatStream(ctx context.Context, req *core.ChatRequest, handler core.StreamHandler) error {
	req = sp.withReasoningLoopNudge(req)
	sproutReq := seedRequestToSprout(req)

	messages := sp.attachPastedImages(req.Messages)
	messages = sp.stampTurnTimestamp(messages)
	messages = sp.observeAndHint(messages)
	sp.recordContinuationNudges(messages)
	// Body budget: keep the inline image parts whose base64 mass fits the
	// budget (older images withheld with a note), below the server body limit.
	messages = api.TrimImageBudget(messages, bodyBudgetImageBytes)

	sp.computeMaxTokensHint(req)

	if h, ok := sp.currentClient().(providers.MaxTokensHinter); ok {
		h.SetMaxTokensHint(sp.getMaxTokensHint())
	}

	// Route through OutputRouter.RouteStreamChunk for both WebUI and seed handler.
	// When the streaming hold-back is active for this turn, gate
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
	// The repetition guard is composed OUTSIDE the hold-back: assistant text
	// flows guard → hold-back → client sink. It watches the model's raw
	// streamed text for a degenerate repetition run and, when one is
	// confirmed, cuts the attempt so the degenerate copies never reach the
	// client. When the guard is disabled it is a passthrough and the path is
	// byte-for-byte unchanged.
	guardSink := func(content string) {
		if holdback != nil {
			holdback.Write(content)
			return
		}
		handler.OnContent(content)
		sp.agent.output.GetStreamingBuffer().WriteString(content) //nolint:gosec // G104: bytes.Buffer writes cannot fail
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
			handler.OnReasoning(content)
			sp.agent.output.GetReasoningBuffer().WriteString(content) //nolint:gosec // G104: bytes.Buffer writes cannot fail
			if router := sp.agent.OutputRouter(); router != nil {
				router.RouteStreamChunk(content, contentType)
			}
			return
		}
		// Assistant text flows through the guard and its sink (the hold-back
		// or the direct client delivery), which owns the routing — so the
		// chunk never also hits the router here.
		guard.Write(content)
	}

	// Use doChatWithRetry for streaming too, but wrap it to deliver through the handler.
	// The transport layer (with its backoff retries and 413 shedding) is the
	// attempt body for the repetition guard, so a cut attempt and the
	// retry-once nudge go through the same request plumbing.
	reasoning, disableThinking := sp.effectiveReasoning()
	if sproutReq.Reasoning != "" {
		reasoning = sproutReq.Reasoning
	}
	attemptFn := func(attemptCtx context.Context, msgs []api.Message, cb api.StreamCallback) (*api.ChatResponse, error) {
		// The reasoning guard is reset by repetitionAttempt (per attempt) and
		// by doChatWithRetryStreaming (per transport retry), so a latched loop
		// from a discarded attempt never steers the replay.
		return sp.doChatWithRetryStreaming(attemptCtx, msgs, sproutReq.Tools, reasoning, disableThinking, cb, holdback, guard, reasoningGuard)
	}
	// The recovery FINAL attempt runs at a capped reasoning effort: the
	// degeneration is effort-dose dependent (higher effort forms more loops
	// and burns more of the budget), so re-seeding at the recommended agentic
	// depth — never above the configured effort, a no-op where the model has
	// no effort knob — is the pre-model lever that makes the recovery re-seed
	// more likely to land. Same transport plumbing, capped reasoning only.
	recoveryReasoning := cappedRecoveryReasoning(reasoning)
	recoveryAttemptFn := func(attemptCtx context.Context, msgs []api.Message, cb api.StreamCallback) (*api.ChatResponse, error) {
		return sp.doChatWithRetryStreaming(attemptCtx, msgs, sproutReq.Tools, recoveryReasoning, disableThinking, cb, holdback, guard, reasoningGuard)
	}
	resp, err := sp.streamWithRepetitionGuard(ctx, messages, callback, holdback, guard, reasoningGuard, attemptFn, recoveryAttemptFn)
	sp.fireSteerFlushHook()
	if err != nil {
		handler.OnError(err)
		return err
	}
	// Finalize the hold-back for this streamed response —
	// release a below-threshold stream, and (when held) regenerate the reply
	// and deliver the corrected text (or the language notice) instead of the
	// wrong-language stream.
	if holdback != nil {
		sp.finalizeStreamHoldback(ctx, holdback, resp)
	}
	// A steered turn's response is scrubbed before seed records it: the
	// mid-stream scrub only sees the history as it was before this reply, so
	// without this the loop that was just detected would enter state and be
	// replayed on the next iteration. Reasoning only — content and tool calls
	// are untouched, so the provider's replay contract still holds.
	if sp.agent != nil && sp.agent.reasoningLoopNudgeForTurn() != "" {
		sp.scrubResponseReasoning(resp)
	}
	// Anchor future EstimateTokens calls to this response's real prompt-token count.
	sp.tokenAnchor.update(sp.currentClient().GetModel(), req.Messages, len(req.Tools), resp.Usage.PromptTokens)
	handler.OnDone(sproutResponseToSeed(resp))
	return nil
}

// doChatWithRetryStreaming performs a streaming chat request under the same
// time-budget retry policy as doChatWithRetry (see runWithProviderRetry). The
// loop lives in doChatStreamingRetrying; this wrapper supplies the real clock.
//
// holdback (nil when the hold-back is inactive) is reset at the start of each
// attempt so a failed or wrong-language attempt never leaks into the next; the
// caller finalizes it (finalizeStreamHoldback) after a successful attempt.
// guard (nil when the repetition guard is inactive) is reset alongside it, so a
// transport retry's replayed chunks never accumulate into a false repetition
// run. reasoningGuard is reset for the same reason, and its grace-budget cut is
// recognized here so it is not mistaken for a provider failure and retried as
// one.
func (sp *sproutProvider) doChatWithRetryStreaming(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback, holdback *StreamHoldback, guard *RepetitionGuard, reasoningGuard *RepetitionGuard) (*api.ChatResponse, error) {
	return sp.doChatStreamingRetrying(ctx, defaultProviderRetryClock(), messages, tools, reasoning, disableThinking, callback, holdback, guard, reasoningGuard)
}

// doChatStreamingRetrying is the streaming attempt body of the retry policy.
func (sp *sproutProvider) doChatStreamingRetrying(ctx context.Context, clock providerRetryClock, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback, holdback *StreamHoldback, guard *RepetitionGuard, reasoningGuard *RepetitionGuard) (*api.ChatResponse, error) {
	var resp *api.ChatResponse
	attempt := func(n int) (error, bool) {
		// Each attempt is a separate streamed response — start
		// its hold-back fresh (discard the previous attempt's buffered/held
		// content) so a failed or wrong-language attempt never leaks into the
		// next one. The guards are reset for the same reasons.
		if holdback != nil {
			holdback.Reset()
		}
		if guard != nil {
			guard.Reset()
		}
		if reasoningGuard != nil {
			// A transport retry replays the stream from the start: a guard
			// left latched from the discarded attempt would flag (or cut) on
			// the replayed chunks.
			reasoningGuard.Reset()
		}

		r, err := sp.currentClient().SendChatRequestStream(ctx, messages, tools, reasoning, disableThinking, callback)
		if err == nil {
			sp.clearProviderError()
			// Fleet budget tracking: debit tokens after each LLM call
			if budgetErr := sp.trackFleetBudgetForResponse(r); budgetErr != nil {
				return budgetErr, false
			}
			resp = r
			return nil, false
		}

		// A cancelled context caused by the repetition guard cutting a
		// degenerate stream is not a provider failure: skip the error record
		// and retry notices and surface the cancellation unchanged. A genuine
		// user cancellation still records/publishes as before.
		if ctx.Err() != nil && ((guard != nil && guard.Looping()) || (reasoningGuard != nil && reasoningGuard.cutReasoning())) {
			return ctx.Err(), false
		}

		// HTTP 413 (body too large): the payload, not the endpoint, is the
		// problem. Shed text first — the oldest non-system history, a tail
		// that halves each retry — leaving the current turn's attached
		// images alone, since in a long session the text is the overflow and
		// the images are the user's explicit intent for this turn. Only when
		// the text is at its floor is the image mass the overflow: shed
		// image bytes (most recent kept).
		if isBodyTooLargeError(err) {
			before := len(messages)
			keep := max(bodyBudgetKeepTail>>uint(n), 1)
			messages = api.TrimMessagesForBodyBudget(messages, keep)
			if len(messages) == before {
				if imgBytes := api.MessagesBodyImageBytes(messages); imgBytes > 0 {
					messages = api.TrimImageBudget(messages, imgBytes/2)
				}
			}
			if n >= maxInPlaceRetries {
				return err, false
			}
			return err, true
		}

		sp.recordProviderError(err, n)
		return err, false
	}

	if err := runWithProviderRetry(ctx, clock, attempt, sp.providerRetryHooks()); err != nil {
		return nil, err
	}
	return resp, nil
}
