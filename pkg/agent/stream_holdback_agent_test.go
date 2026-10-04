// stream_holdback_agent_test.go — agent-level scripted tests for the
// streaming language-guard hold-back (SP-152 §152c): a wrong-language streamed
// reply never reaches the client's streaming buffer (the user sees the §152b
// notice instead), while a correct-language stream is delivered. These drive
// the real provider streaming path (doChatStream → SendChatRequestStream) with
// the scripted streaming client (StreamConfig.Chunks playback).
package agent

import (
	"context"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/langguard"
)

// newStreamingResponse builds a scripted streaming response: the full content
// is delivered as StreamConfig.Chunks so the scripted client's
// SendChatRequestStream replays it chunk by chunk through the hold-back.
func newStreamingResponse(content string, chunks []string) *ScriptedResponse {
	return NewScriptedResponseBuilder().
		Content(content).
		FinishReason("stop").
		StreamConfig(&StreamConfig{Chunks: chunks, FinishReason: "stop"}).
		Build()
}

// englishChunks / spanishChunks split the reliably-detectable fixtures into
// the chunks the scripted client replays.
var (
	englishChunks = []string{
		"The build succeeded after applying the patch, so the tests ",
		"can run and the release is ready to ship.",
	}
	spanishChunks = []string{
		"El paquete está ",
		"listo para compilar ahora ",
		"mismo y las pruebas pasan sin errores.",
	}
)

// TestStreamHoldbackWrongLanguageNeverReachesClient drives a full scripted
// turn where the user writes in Spanish (configured fallback) and the reply
// streams back in English. The hold-back holds the wrong-language stream, so
// the English content never reaches the client's streaming buffer — the user
// sees the localized §152b notice instead. The held content is kept on the
// message Meta for "view original".
func TestStreamHoldbackWrongLanguageNeverReachesClient(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "es", false,
		newStreamingResponse(hbEnglishProse, englishChunks), // the turn's answer: wrong language
		NewStopResponse(hbSpanishProse),                     // the §152b regeneration
	)
	ag.SetStreamingEnabled(true)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	buffer := ag.output.GetStreamingBuffer().String()
	// The wrong-language content must never reach the client.
	if strings.Contains(buffer, "The build succeeded") {
		t.Errorf("the wrong-language stream reached the client's buffer: %q", buffer)
	}
	if strings.Contains(buffer, hbEnglishProse) {
		t.Errorf("the held English reply leaked into the client's buffer: %q", buffer)
	}
	// The user sees the localized §152b notice (in their Spanish) instead.
	wantNotice := LanguageMismatchNotice(langguard.Language{Code: "es", Name: "Spanish"})
	if !strings.Contains(buffer, wantNotice) {
		t.Errorf("buffer does not carry the §152b notice %q; got %q", wantNotice, buffer)
	}
	// The held content is kept available for "view original" (the final
	// message's Meta, mirroring 152.5's language_guard_original).
	last := lastAssistantMessage(t, ag)
	if original := last.Meta[langGuardOriginalMetaKey]; original != hbEnglishProse {
		t.Errorf("view-original payload = %q, want the held reply %q", original, hbEnglishProse)
	}
}

// TestStreamHoldbackCorrectLanguageStreamsLive drives a full scripted turn
// where the user writes in Spanish and the reply also streams back in Spanish.
// The hold-back buffers until the prose threshold, releases the buffer, and
// streams the rest live — so the full Spanish reply reaches the client.
func TestStreamHoldbackCorrectLanguageStreamsLive(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "es", false,
		newStreamingResponse(hbSpanishProse, spanishChunks),
	)
	ag.SetStreamingEnabled(true)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	buffer := ag.output.GetStreamingBuffer().String()
	// The correct-language reply is delivered (buffered prefix + live chunks).
	if !strings.Contains(buffer, "El paquete está") {
		t.Errorf("the correct-language stream did not reach the client's buffer: %q", buffer)
	}
	if !strings.Contains(buffer, "sin errores") {
		t.Errorf("the live (post-release) portion did not reach the client's buffer: %q", buffer)
	}
	// The whole reply is present, in order.
	if !strings.Contains(buffer, hbSpanishProse) {
		t.Errorf("buffer does not contain the full reply %q; got %q", hbSpanishProse, buffer)
	}
}

// TestStreamHoldbackDisabledIsByteIdentical pins the opt-out: with the guard
// disabled, a wrong-language stream is NOT held — it streams through
// unchanged (byte-for-byte), and no §152b notice is delivered.
func TestStreamHoldbackDisabledIsByteIdentical(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "es", true,
		newStreamingResponse(hbEnglishProse, englishChunks),
	)
	ag.SetStreamingEnabled(true)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	buffer := ag.output.GetStreamingBuffer().String()
	// Guard disabled: the wrong-language content streams through untouched.
	if !strings.Contains(buffer, hbEnglishProse) {
		t.Errorf("with the guard disabled the stream must be byte-identical (unheld); buffer = %q", buffer)
	}
	wantNotice := LanguageMismatchNotice(langguard.Language{Code: "es", Name: "Spanish"})
	if strings.Contains(buffer, wantNotice) {
		t.Errorf("with the guard disabled no §152b notice should be delivered; buffer = %q", buffer)
	}
}

// TestStreamHoldbackUndeterminedUserIsByteIdentical pins the undetermined path:
// no configured language and a too-short user message, so the user language is
// undetermined and the hold-back is a no-op passthrough — a wrong-language
// stream streams through unchanged.
func TestStreamHoldbackUndeterminedUserIsByteIdentical(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "", false,
		newStreamingResponse(hbEnglishProse, englishChunks),
	)
	ag.SetStreamingEnabled(true)

	if _, err := ag.ProcessQuery("hi"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	buffer := ag.output.GetStreamingBuffer().String()
	// Undetermined user language: the stream is a passthrough (unheld).
	if !strings.Contains(buffer, hbEnglishProse) {
		t.Errorf("with an undetermined user the stream must be a passthrough (unheld); buffer = %q", buffer)
	}
}

// reasoningModelClient simulates a "reasoning-model" stream: the visible prose
// is NOT delivered as assistant-text (the provider's callback is never invoked
// with it), so the provider's reasoning-model fallback is the only path that
// delivers the response content. It embeds a ScriptedClient for the rest of
// the ClientInterface and overrides SendChatRequestStream to discard the
// scripted assistant-text.
type reasoningModelClient struct {
	*ScriptedClient
}

func (c *reasoningModelClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	// Discard the scripted assistant-text: the prose is not delivered as
	// assistant-text, but the final response still carries the content — so
	// the provider's reasoning-model fallback is the only delivery path.
	return c.ScriptedClient.SendChatRequestStream(ctx, messages, tools, reasoning, disableThinking, func(string, string) {})
}

// newReasoningModelAgent builds an Agent backed by a reasoningModelClient
// (scripted responses, assistant-text discarded) with the language-guard
// config knobs set: the configured fallback language and the guard opt-out.
func newReasoningModelAgent(t *testing.T, configuredLanguage string, guardDisabled bool, responses ...*ScriptedResponse) *Agent {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if configuredLanguage != "" || guardDisabled {
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.Language = configuredLanguage
			cfg.DisableLanguageGuard = guardDisabled
			return nil
		}); err != nil {
			t.Fatalf("UpdateConfigNoSave: %v", err)
		}
	}
	client := &reasoningModelClient{ScriptedClient: NewScriptedClient(responses...)}
	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(func() { ag.Shutdown() })
	return ag
}

// TestStreamHoldbackReasoningModelFallbackHeld pins the reasoning-model
// fallback interaction (SP-152 §152c): when the model streams its visible
// prose as reasoning_content (no assistant-text), the provider's fallback is
// the only path that delivers the content. The fallback must route it through
// the hold-back, so a wrong-language reply is held (not delivered) and the
// user sees the §152b notice instead.
func TestStreamHoldbackReasoningModelFallbackHeld(t *testing.T) {
	ag := newReasoningModelAgent(t, "es", false,
		newStreamingResponse(hbEnglishProse, englishChunks), // the turn's answer: wrong language
		NewStopResponse(hbSpanishProse),                     // the §152b regeneration
	)
	ag.SetStreamingEnabled(true)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	buffer := ag.output.GetStreamingBuffer().String()
	// The wrong-language content (delivered only via the fallback) must not
	// reach the client.
	if strings.Contains(buffer, "The build succeeded") {
		t.Errorf("the wrong-language fallback content reached the client's buffer: %q", buffer)
	}
	if strings.Contains(buffer, hbEnglishProse) {
		t.Errorf("the held English reply leaked into the client's buffer: %q", buffer)
	}
	// The user sees the localized §152b notice (in their Spanish) instead.
	wantNotice := LanguageMismatchNotice(langguard.Language{Code: "es", Name: "Spanish"})
	if !strings.Contains(buffer, wantNotice) {
		t.Errorf("buffer does not carry the §152b notice %q; got %q", wantNotice, buffer)
	}
}

// TestStreamHoldbackReasoningModelFallbackCorrectLanguageReleased is the
// counterpart: a reasoning-model stream whose content is the user's language.
// The fallback routes it through the hold-back, which releases it (correct
// language), so the full reply reaches the client.
func TestStreamHoldbackReasoningModelFallbackCorrectLanguageReleased(t *testing.T) {
	ag := newReasoningModelAgent(t, "es", false,
		newStreamingResponse(hbSpanishProse, spanishChunks),
	)
	ag.SetStreamingEnabled(true)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	buffer := ag.output.GetStreamingBuffer().String()
	// The correct-language content (delivered via the fallback, then
	// released by the hold-back) reaches the client.
	if !strings.Contains(buffer, "El paquete está") {
		t.Errorf("the correct-language fallback content did not reach the client's buffer: %q", buffer)
	}
	if !strings.Contains(buffer, hbSpanishProse) {
		t.Errorf("buffer does not contain the full reply %q; got %q", hbSpanishProse, buffer)
	}
}

// ---------------------------------------------------------------------------
// Completion re-check (SP-152 §152c, item 152.7)
//
// The hold-back only judges the START of a streamed reply. A reply whose start
// passes (and is released) but which switches language later in the stream is
// already streamed to the client and cannot be un-streamed — so it is re-checked
// at completion on its FULL content and, on a reliable mismatch, the client is
// told to replace the already-streamed message via a language_guard_replacement
// event.
// ---------------------------------------------------------------------------

// Mid-stream-switch fixtures, tuned against the detector (pkg/langguard): the
// Spanish START is reliably Spanish (conf > 0.8) so the hold-back releases it,
// but the FULL reply (Spanish start + English body) is reliably English, a
// mismatch for a Spanish user — exactly the mid-stream switch the completion
// re-check exists to catch.
const (
	midStreamSpanishStart = "Hecho. El paquete está listo para compilar ahora"
	midStreamEnglishRest  = " The build succeeded after applying the patch, so the tests can run and the release is ready to ship."
)

var midStreamFullReply = midStreamSpanishStart + midStreamEnglishRest

// wireLanguageGuardEventBus attaches an EventBus to the agent (with a chat_id
// in the event metadata, so the replacement payload carries it) and returns a
// subscriber channel for the published events. Publish is synchronous (it waits
// for every subscriber to have processed the event), so after ProcessQuery
// returns the published events are already in the channel.
func wireLanguageGuardEventBus(t *testing.T, ag *Agent) <-chan events.UIEvent {
	t.Helper()
	eb := events.NewEventBus()
	subName := "langguard-completion-sub"
	sub := eb.Subscribe(subName)
	t.Cleanup(func() { eb.Unsubscribe(subName) })
	ag.SetEventBus(eb)
	ag.SetEventMetadata(map[string]interface{}{"chat_id": "chat-lgt-1"})
	return sub
}

// drainEvents reads every event currently in the subscriber channel.
func drainEvents(sub <-chan events.UIEvent) []events.UIEvent {
	var evs []events.UIEvent
	for {
		select {
		case ev := <-sub:
			evs = append(evs, ev)
		default:
			return evs
		}
	}
}

// languageGuardReplacementEvents filters the replacement events out of a set.
func languageGuardReplacementEvents(evs []events.UIEvent) []events.UIEvent {
	var out []events.UIEvent
	for _, ev := range evs {
		if ev.Type == events.EventTypeLanguageGuardReplacement {
			out = append(out, ev)
		}
	}
	return out
}

// TestStreamCompletionRecheckMidStreamSwitchPublishesReplacement drives a full
// scripted turn: the user writes in Spanish (configured fallback), the reply
// STARTS in Spanish (so the hold-back releases it and it streams live) and then
// switches to English. At completion the full reply is a reliable mismatch, so
// a language_guard_replacement event is published carrying the replacement
// notice, the original (full switched content), the reason, and the chat_id.
func TestStreamCompletionRecheckMidStreamSwitchPublishesReplacement(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "es", false,
		newStreamingResponse(midStreamFullReply, []string{midStreamSpanishStart, midStreamEnglishRest}),
		NewStopResponse(hbSpanishProse), // the 152.5 regeneration (always runs on a mismatch)
	)
	ag.SetStreamingEnabled(true)
	sub := wireLanguageGuardEventBus(t, ag)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The reply was RELEASED (its start passed), so the switched content reached
	// the client's streaming buffer — it was already streamed and cannot be
	// un-streamed.
	buffer := ag.output.GetStreamingBuffer().String()
	if !strings.Contains(buffer, midStreamSpanishStart) {
		t.Errorf("the released (Spanish-start) reply did not reach the client's buffer: %q", buffer)
	}
	if !strings.Contains(buffer, "The build succeeded") {
		t.Errorf("the switched (English) tail did not reach the client's buffer: %q", buffer)
	}

	repl := languageGuardReplacementEvents(drainEvents(sub))
	if len(repl) != 1 {
		t.Fatalf("expected exactly 1 language_guard_replacement event, got %d", len(repl))
	}
	data, ok := repl[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("replacement event data is not a map: %T", repl[0].Data)
	}
	wantReplacement := LanguageMismatchNotice(langguard.Language{Code: "es", Name: "Spanish"})
	if got := data["replacement"]; got != wantReplacement {
		t.Errorf("replacement = %v, want the localized notice %q", got, wantReplacement)
	}
	if got := data["original"]; got != midStreamFullReply {
		t.Errorf("original = %v, want the full switched reply %q", got, midStreamFullReply)
	}
	if got := data["reason"]; got != "mid_stream_switch" {
		t.Errorf("reason = %v, want %q", got, "mid_stream_switch")
	}
	if got := data["chat_id"]; got != "chat-lgt-1" {
		t.Errorf("chat_id = %v, want %q", got, "chat-lgt-1")
	}

	// The full switched content is kept on the message Meta for "view original".
	last := lastAssistantMessage(t, ag)
	if original := last.Meta[langGuardOriginalMetaKey]; original != midStreamFullReply {
		t.Errorf("view-original payload = %q, want the full switched reply %q", original, midStreamFullReply)
	}
}

// TestStreamCompletionRecheckCorrectLanguageNoEvent pins the no-switch path:
// a reply that streams entirely in the user's language is released and, at
// completion, is NOT a mismatch — so no replacement event is published.
func TestStreamCompletionRecheckCorrectLanguageNoEvent(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "es", false,
		newStreamingResponse(hbSpanishProse, spanishChunks),
	)
	ag.SetStreamingEnabled(true)
	sub := wireLanguageGuardEventBus(t, ag)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if repl := languageGuardReplacementEvents(drainEvents(sub)); len(repl) != 0 {
		t.Errorf("a correct-language stream must not publish a replacement event; got %d", len(repl))
	}
}

// TestStreamCompletionRecheckHeldStreamNoEvent pins that a HELD stream (a
// reliable mismatch at the START, handled by the 152.6 notice) is NOT
// re-checked: the held path is terminal, so no language_guard_replacement event
// is published (it would be a duplicate of the notice already delivered).
func TestStreamCompletionRecheckHeldStreamNoEvent(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "es", false,
		newStreamingResponse(hbEnglishProse, englishChunks), // wrong language from the start
		NewStopResponse(hbSpanishProse),                     // the §152b regeneration
	)
	ag.SetStreamingEnabled(true)
	sub := wireLanguageGuardEventBus(t, ag)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The held (English) content must never reach the client.
	if strings.Contains(ag.output.GetStreamingBuffer().String(), hbEnglishProse) {
		t.Errorf("the held wrong-language stream reached the client's buffer")
	}
	// And the held path is terminal: no completion re-check event.
	if repl := languageGuardReplacementEvents(drainEvents(sub)); len(repl) != 0 {
		t.Errorf("a held stream must not publish a replacement event; got %d", len(repl))
	}
}

// TestStreamCompletionRecheckShortStreamNoEvent pins that a reply below the
// prose threshold is not judged (§152a): it is released on Finish and the
// completion re-check does not run — so no replacement event is published.
func TestStreamCompletionRecheckShortStreamNoEvent(t *testing.T) {
	ag, _ := newLanguageGuardAgent(t, "es", false,
		newStreamingResponse("Hola", []string{"Hola"}),
	)
	ag.SetStreamingEnabled(true)
	sub := wireLanguageGuardEventBus(t, ag)

	if _, err := ag.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if repl := languageGuardReplacementEvents(drainEvents(sub)); len(repl) != 0 {
		t.Errorf("a below-threshold (short) stream must not publish a replacement event; got %d", len(repl))
	}
}
