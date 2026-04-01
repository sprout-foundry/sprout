package webui

import (
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

func TestTouchClientLastSeen_ExistingContext(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	// Create a client context
	ctx := server.getOrCreateClientContext("test-client")

	originalTime := ctx.LastSeenAt
	// Wait a tiny bit to ensure time.Now() would be different
	time.Sleep(time.Millisecond)

	server.touchClientLastSeen("test-client")

	if !ctx.LastSeenAt.After(originalTime) {
		t.Error("expected LastSeenAt to be updated after touchClientLastSeen")
	}
}

func TestTouchClientLastSeen_DefaultClientID(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	ctx := server.getOrCreateClientContext(defaultWebClientID)

	originalTime := ctx.LastSeenAt
	time.Sleep(time.Millisecond)

	// Touch with empty string - should default to defaultWebClientID
	server.touchClientLastSeen("")
	server.touchClientLastSeen("  ")

	if !ctx.LastSeenAt.After(originalTime) {
		t.Error("expected LastSeenAt to be updated for default client")
	}
}

func TestTouchClientLastSeen_NonexistentContext(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	// Should not panic or create a context for a nonexistent client
	server.touchClientLastSeen("nonexistent-client")

	server.mutex.RLock()
	_, exists := server.clientContexts["nonexistent-client"]
	server.mutex.RUnlock()

	// touchClientLastSeen should NOT create a new context
	if exists {
		t.Error("touchClientLastSeen should not create a new client context")
	}
}

func TestTouchClientLastSeen_WhitespaceClientID(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	ctx := server.getOrCreateClientContext(defaultWebClientID)

	originalTime := ctx.LastSeenAt
	time.Sleep(time.Millisecond)

	// Whitespace should normalize to default client ID
	server.touchClientLastSeen("   ")

	if !ctx.LastSeenAt.After(originalTime) {
		t.Error("expected default client LastSeenAt to be updated via whitespace normalization")
	}
}
