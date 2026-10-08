// seed_provider_chat_repetition.go — the sproutProvider streaming
// repetition-guard integration: it wires the pure RepetitionGuard into the
// streaming request, cuts the attempt when a degenerate run is confirmed,
// and retries the request once with a short system nudge. Split out of
// seed_provider_chat_stream.go to keep the streaming entry points readable.

package agent

import (
	"context"
	"errors"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// errStreamRepetitionLoop is the internal sentinel a streamed attempt returns
// when the repetition guard cut it. It never reaches the client: the driver
// either retries once with a nudge or, when the retry loops too, surfaces it
// as a normal turn error.
var errStreamRepetitionLoop = errors.New("streamed reply degenerated into a repetition loop")

// repetitionNudge is the short system message appended for the single retry.
// It tells the model, in one line, what went wrong — act or answer — without
// re-explaining the task.
const repetitionNudge = "Your previous reply repeated itself without acting. Stop repeating and either emit the tool call that makes progress or give the final answer directly."

// repetitionGuardConfigFromAgent reads the guard's thresholds from the agent's
// config, falling back to the enabled defaults (a nil config resolves to
// enabled with sane thresholds).
func (sp *sproutProvider) repetitionGuardConfigFromAgent() RepetitionGuardConfig {
	if sp.agent == nil {
		return RepetitionGuardConfig{}
	}
	cfg := sp.agent.GetConfig()
	return RepetitionGuardConfig{
		Enabled:        cfg.RepetitionGuardEnabled(),
		MinRepetitions: cfg.RepetitionGuardMinRepetitions(),
		MaxLineChars:   cfg.RepetitionGuardMaxLineChars(),
	}
}

// noteRepetitionLoop records one cut loop for the agent's (provider, model)
// cell and logs exactly one line. It is the single detection-recording site,
// so the metric and the log can never diverge.
func (sp *sproutProvider) noteRepetitionLoop() {
	if sp.agent == nil {
		return
	}
	provider := sp.agent.GetProvider()
	model := sp.agent.GetModel()
	GlobalRepetitionMetrics().Record(provider, model)
	sp.agent.Logger().Info(
		"[repetition] degenerate repetition loop cut for %s/%s; retrying once with a nudge\n",
		provider, model)
}

// repetitionAttempt runs one streamed attempt under the guard: it creates a
// cancelable child context so the guard can stop the stream the moment a
// degenerate run is confirmed, and it returns the sentinel when the guard (not
// the transport) ended the attempt. The hold-back and the guard are reset at
// the start of each attempt so a cut attempt leaves no state behind.
//
// attemptFn is the caller's transport layer: the ChatStream entry passes its
// retry/backoff layer (doChatWithRetryStreaming), while the buffer-only path
// passes a single SendChatRequestStream — each path keeps the exact transport
// behavior it had before the guard existed.
func (sp *sproutProvider) repetitionAttempt(
	ctx context.Context,
	messages []api.Message,
	callback api.StreamCallback,
	holdback *StreamHoldback,
	guard *RepetitionGuard,
	attemptFn func(ctx context.Context, messages []api.Message, callback api.StreamCallback) (*api.ChatResponse, error),
) (*api.ChatResponse, error) {
	if holdback != nil {
		holdback.Reset()
	}
	if guard != nil {
		guard.Reset()
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Wrap the callback so a confirmed loop cancels the in-flight stream: the
	// provider's SSE reader returns promptly on the canceled context, and the
	// degenerate copies the guard held back are dropped.
	wrapped := func(content string, contentType string) {
		callback(content, contentType)
		if guard != nil && guard.Looping() {
			cancel()
		}
	}
	resp, err := attemptFn(attemptCtx, messages, wrapped)
	// A reply that carries a tool call acts rather than degenerating: if the
	// attempt completed with tool calls, keep it (the turn continues) instead
	// of reporting the cut. This is the reachable completion-time check — the
	// streaming callback cannot see tool-call deltas mid-stream. A reply whose
	// tool call would arrive in a later chunk than the flagging one has
	// already had its stream cut by the cancel above, so it is retried once;
	// the nudge asks for exactly that tool call, which the retry then emits.
	if resp != nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0 {
		if guard != nil {
			guard.NoteToolCall()
		}
		return resp, err
	}
	if guard != nil && guard.Looping() {
		return nil, errStreamRepetitionLoop
	}
	return resp, err
}

// streamWithRepetitionGuard runs the streamed request with the repetition
// guard active, retrying ONCE with a short system nudge when a degenerate run
// is cut. A second loop is surfaced as a normal turn error, so a persistently
// degenerating model fails the turn instead of looping forever. When the guard
// is disabled (or the loop is not detected) the behavior is the existing
// streaming request, byte-for-byte.
//
// The guard is composed OUTSIDE the language hold-back: assistant text flows
// guard → hold-back → client sink. The guard evaluates the model's raw stream
// (its degenerate-run detection is about the model's output, not the
// language-gated delivery), while the hold-back keeps its own gate on what the
// language guard has judged. Neither wraps the other's state; a held
// wrong-language reply and a cut repetition loop are independent outcomes.
//
// attemptFn is the caller's transport layer (see repetitionAttempt): the
// retry-once nudge wraps it, so a cut attempt goes through the same request
// plumbing — CLI, subagents, and web UI all share this stream.
func (sp *sproutProvider) streamWithRepetitionGuard(
	ctx context.Context,
	messages []api.Message,
	callback api.StreamCallback,
	holdback *StreamHoldback,
	guard *RepetitionGuard,
	attemptFn func(ctx context.Context, messages []api.Message, callback api.StreamCallback) (*api.ChatResponse, error),
) (*api.ChatResponse, error) {
	attempt := func(msgs []api.Message) (*api.ChatResponse, error) {
		return sp.repetitionAttempt(ctx, msgs, callback, holdback, guard, attemptFn)
	}

	resp, err := attempt(messages)
	if !errors.Is(err, errStreamRepetitionLoop) {
		// The attempt finished without a loop: flush the guard's trailing
		// partial line so the whole reply reaches the hold-back / client sink.
		if guard != nil {
			guard.Finish()
		}
		return resp, err
	}

	// First detection: record it, log one line, and retry once with the
	// nudge appended as a system message.
	sp.noteRepetitionLoop()
	retryMessages := append(append([]api.Message(nil), messages...),
		api.Message{Role: "system", Content: repetitionNudge})

	resp, err = attempt(retryMessages)
	if !errors.Is(err, errStreamRepetitionLoop) {
		if guard != nil {
			guard.Finish()
		}
		return resp, err
	}

	// The retry looped too: a second loop is a normal turn error, not
	// another retry. It is wrapped as a non-retryable provider error so the
	// caller's turn fails fast instead of looping — the second loop is the
	// terminal failure the retry existed to prevent.
	if sp.agent != nil {
		sp.agent.Logger().Warn(
			"[repetition] second repetition loop for %s/%s; failing the turn\n",
			sp.agent.GetProvider(), sp.agent.GetModel())
	}
	provider := ""
	if sp.agent != nil {
		provider = sp.agent.GetProvider()
	}
	return nil, &core.ClientError{Provider: provider, Wrapped: errStreamRepetitionLoop}
}
