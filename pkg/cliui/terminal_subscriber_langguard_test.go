//go:build !js

package cliui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestHandleLanguageGuardReplacementEvent_RendersReplacement pins that the
// terminal subscriber renders the replacement text of a
// language_guard_replacement event on stdout (via PrintExternal, so it does
// not corrupt an active input line) — the streamed prose it corrects is
// already on the screen, so without this handler the CLI would keep showing
// the switched (wrong-language) reply.
func TestHandleLanguageGuardReplacementEvent_RendersReplacement(t *testing.T) {
	stdout := captureStdout(t, func() {
		eb := events.NewEventBus()
		var indicatorBuf, footerBuf bytes.Buffer
		indicator := console.NewActivityIndicator(&indicatorBuf)
		footer := console.NewStatusFooter(&footerBuf, nil)

		reset := StartTerminalToolSubscriber(t.Context(), nil, eb, indicator, footer)
		defer reset()

		eb.Publish(events.EventTypeLanguageGuardReplacement, events.LanguageGuardReplacementEvent(
			"chat-1",
			"El paquete está listo para compilar ahora mismo y las pruebas pasan sin errores.",
			"Hecho. El paquete está listo. The build succeeded after applying the patch.",
			"mid_stream_switch",
		))
		// Give the subscriber goroutine time to consume the event (the
		// channel is unbuffered but the loop drains in a tight loop; 100ms
		// mirrors the existing integration test's wait).
		time.Sleep(100 * time.Millisecond)
	})

	if !strings.Contains(stdout, "El paquete está listo") {
		t.Errorf("stdout does not carry the replacement text; got %q", stdout)
	}
	if strings.Contains(stdout, "The build succeeded") {
		t.Errorf("stdout must not show the switched (original) reply; got %q", stdout)
	}
}

// TestHandleLanguageGuardReplacementEvent_OriginalHint pins that the
// rendered replacement is followed by a pointer to /original (so the
// "view original" promise in the notice is actionable in the terminal),
// while the switched original text itself is still NOT printed — the
// handler renders the replacement, never the payload it was given.
func TestHandleLanguageGuardReplacementEvent_OriginalHint(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	state := NewTerminalSubscriberState(nil, nil)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	footer := console.NewStatusFooter(&bytes.Buffer{}, nil)

	out := captureStdout(t, func() {
		state.HandleLanguageGuardReplacementEvent(map[string]interface{}{
			"replacement": "La respuesta llegó en un idioma diferente. Puedes ver el texto original.",
			"original":    "The build succeeded after applying the patch.",
			"reason":      "mid_stream_switch",
		}, indicator, footer)
	})

	if !strings.Contains(out, "La respuesta llegó en un idioma diferente") {
		t.Errorf("stdout missing the replacement text; got %q", out)
	}
	if !strings.Contains(out, "/original") {
		t.Errorf("stdout missing the /original hint; got %q", out)
	}
	if strings.Contains(out, "The build succeeded") {
		t.Errorf("stdout must not show the original text itself; got %q", out)
	}
	// Same terminal-state cleanup contract as the render test.
	if state.run != nil {
		t.Error("rendered replacement must break the collapse run (s.run = nil)")
	}
	if state.thinkingActive {
		t.Error("rendered replacement must clear the thinking flag")
	}
}
