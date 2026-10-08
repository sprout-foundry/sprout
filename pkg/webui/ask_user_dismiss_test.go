package webui

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestBroadcastAskUserDismissed pins the multi-window dismiss contract: an
// answered ask_user publishes a status=responded event with NO client_id
// scoping, so every window that received the fan-out question (see
// shouldForwardEventToConnection's ask_user early-return) also receives the
// dismissal and closes its dialog.
func TestBroadcastAskUserDismissed(t *testing.T) {
	bus := events.NewEventBus()
	ws := &ReactWebServer{eventBus: bus}

	eventCh := bus.Subscribe("ask-user-dismiss-" + t.Name())

	ws.broadcastAskUserDismissed("client-A", "ask-1")

	select {
	case ev := <-eventCh:
		if ev.Type != eventTypeAskUser {
			t.Fatalf("event type = %q, want %q", ev.Type, eventTypeAskUser)
		}
		data, _ := ev.Data.(map[string]interface{})
		if data["request_id"] != "ask-1" {
			t.Errorf("request_id = %v, want ask-1", data["request_id"])
		}
		if data["status"] != "responded" {
			t.Errorf("status = %v, want responded", data["status"])
		}
		// The dismissal must not be client-scoped: every window that saw the
		// question must see the dismissal.
		if _, scoped := data["client_id"]; scoped {
			t.Errorf("dismissal carries client_id %v; it must stay unscoped so all windows dismiss", data["client_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dismissal event never arrived on the bus")
	}
}
