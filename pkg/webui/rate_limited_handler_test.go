//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestRateLimitedHandler_RegistersOutboundType(t *testing.T) {
	// The init() in rate_limited_handler.go registers the event type.
	// Verify it's in the allow-list by checking the registry directly.
	if !validateOutboundMessageType(events.EventTypeRateLimited) {
		t.Errorf("expected %q to be registered in outbound allow-list", events.EventTypeRateLimited)
	}
	if !validateOutboundMessageType("rate_limited_status") {
		t.Error("expected 'rate_limited_status' to be registered in outbound allow-list")
	}
}
