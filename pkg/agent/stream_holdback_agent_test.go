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
