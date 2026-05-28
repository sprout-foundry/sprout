//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestCloseSSHSessionEmptyKey(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	err = ws.closeSSHSession("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if err.Error() != "ssh session key is required" {
		t.Fatalf("expected 'ssh session key is required', got %q", err.Error())
	}
}

func TestCloseSSHSessionWhitespaceKey(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	err = ws.closeSSHSession("   ")
	if err == nil {
		t.Fatal("expected error for whitespace key")
	}
}

func TestCloseSSHSessionNonexistentKey(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Should not error - just silently handle a nonexistent key
	err = ws.closeSSHSession("nonexistent-key")
	if err != nil {
		t.Fatalf("expected no error for nonexistent key, got %v", err)
	}
}

func TestShutdownSSHSessionsEmpty(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic with no sessions
	ws.shutdownSSHSessions()
}
