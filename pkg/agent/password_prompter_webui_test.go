//go:build !js

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestWebUIPasswordPrompter_NoEventBus verifies that Prompt returns
// ErrNoInteractiveSurface when the agent has no event bus.
func TestWebUIPasswordPrompter_NoEventBus(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	wp := NewWebUIPasswordPrompter(agent)
	_, err := wp.Prompt(context.Background(), "test reason")

	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Errorf("expected ErrNoInteractiveSurface, got: %v", err)
	}
}

// TestWebUIPasswordPrompter_NoWebUIClients verifies that Prompt returns
// ErrNoInteractiveSurface when there's no active WebUI client.
func TestWebUIPasswordPrompter_NoWebUIClients(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	// No WebUI clients set — HasActiveWebUIClients returns false.

	wp := NewWebUIPasswordPrompter(agent)
	_, err := wp.Prompt(context.Background(), "test reason")

	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Errorf("expected ErrNoInteractiveSurface, got: %v", err)
	}
}

// TestWebUIPasswordPrompter_CancelledContext verifies that a pre-cancelled
// context returns immediately without hanging.
func TestWebUIPasswordPrompter_CancelledContext(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	wp := NewWebUIPasswordPrompter(agent)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel

	_, err := wp.Prompt(ctx, "test reason")
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// TestWebUIPasswordPrompter_Timeout verifies that Prompt times out when no
// response arrives within the timeout window.
func TestWebUIPasswordPrompter_Timeout(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	// Use a short timeout for the test.
	oldTimeout := passwordPromptTimeout
	passwordPromptTimeout = 100 * time.Millisecond
	defer func() { passwordPromptTimeout = oldTimeout }()

	wp := NewWebUIPasswordPrompter(agent)

	start := time.Now()
	_, err := wp.Prompt(context.Background(), "test reason")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error message, got: %v", err)
	}
	// Should have waited at least the timeout duration.
	if elapsed < 80*time.Millisecond {
		t.Errorf("expected to wait ~100ms, but returned in %v", elapsed)
	}
}

// TestWebUIPasswordPrompter_RespondDeliversPassword verifies the full flow:
// Prompt publishes events, blocks on channel, and receives the password when
// RespondToPasswordRequest is called.
func TestWebUIPasswordPrompter_RespondDeliversPassword(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	// Subscribe to verify events are published.
	subCh := bus.Subscribe("test-respond-delivers")

	wp := NewWebUIPasswordPrompter(agent)

	// Start Prompt in a goroutine.
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		pwd, err := wp.Prompt(context.Background(), "sudo password")
		if err != nil {
			errCh <- err
		} else {
			resultCh <- pwd
		}
	}()

	// Wait for the password_request event to be published (confirms registration).
	select {
	case ev := <-subCh:
		if ev.Type != events.EventTypePasswordRequest {
			t.Fatalf("expected password_request event, got: %s", ev.Type)
		}
		data, ok := ev.Data.(map[string]interface{})
		if !ok {
			t.Fatal("event data should be map[string]interface{}")
		}
		requestID, _ := data["request_id"].(string)
		if requestID == "" {
			t.Fatal("request_id should not be empty")
		}

		// Now deliver the password.
		delivered := agent.RespondToPasswordRequest(requestID, "secret123")
		if !delivered {
			t.Fatal("expected RespondToPasswordRequest to return true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for password_request event")
	}

	// Verify the password was received.
	select {
	case pwd := <-resultCh:
		if pwd != "secret123" {
			t.Errorf("expected password 'secret123', got: %q", pwd)
		}
	case err := <-errCh:
		t.Errorf("expected password delivery, got error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for password result")
	}
}

// TestRespondToPasswordRequest_UnknownID verifies that calling
// RespondToPasswordRequest with an unknown ID returns false.
func TestRespondToPasswordRequest_UnknownID(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	delivered := agent.RespondToPasswordRequest("nonexistent_id", "password")
	if delivered {
		t.Error("expected false for unknown request ID")
	}
}

// TestPasswordRequestEventPayload verifies the PasswordRequestEvent helper
// produces the expected fields.
func TestPasswordRequestEventPayload(t *testing.T) {
	payload := events.PasswordRequestEvent("pwd_1", "sudo apt update", "[sudo] password for user:")

	if payload["request_id"] != "pwd_1" {
		t.Errorf("request_id = %v, want 'pwd_1'", payload["request_id"])
	}
	if payload["command"] != "sudo apt update" {
		t.Errorf("command = %v, want 'sudo apt update'", payload["command"])
	}
	if payload["prompt"] != "[sudo] password for user:" {
		t.Errorf("prompt = %v, want '[sudo] password for user:'", payload["prompt"])
	}
	if _, ok := payload["timestamp"]; !ok {
		t.Error("payload should have timestamp field")
	}
}
