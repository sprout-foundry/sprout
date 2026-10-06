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
