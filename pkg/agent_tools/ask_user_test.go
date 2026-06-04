package tools

import (
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// NewAskUserManager
// ---------------------------------------------------------------------------

func TestNewAskUserManager_DefaultTimeout(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()
	if mgr == nil {
		t.Fatal("NewAskUserManager returned nil")
	}
	if mgr.timeout != DefaultAskUserTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultAskUserTimeout, mgr.timeout)
	}
	if mgr.pending == nil {
		t.Fatal("expected pending map to be initialized")
	}
}

// ---------------------------------------------------------------------------
// SetTimeout
// ---------------------------------------------------------------------------

func TestSetTimeout_SetPositive(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()
	mgr.SetTimeout(30 * time.Second)
	if mgr.timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", mgr.timeout)
	}
}

func TestSetTimeout_ResetToDefault(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()
	mgr.SetTimeout(0)
	if mgr.timeout != DefaultAskUserTimeout {
		t.Errorf("expected default timeout after zero, got %v", mgr.timeout)
	}
}

func TestSetTimeout_NegativeResetsToDefault(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()
	mgr.SetTimeout(-1 * time.Second)
	if mgr.timeout != DefaultAskUserTimeout {
		t.Errorf("expected default timeout after negative, got %v", mgr.timeout)
	}
}

// ---------------------------------------------------------------------------
// RespondToAskUser
// ---------------------------------------------------------------------------

func TestRespondToAskUser_NonExistentRequestID(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()
	got := mgr.RespondToAskUser("ask_nonexistent", "hello")
	if got {
		t.Error("expected false for non-existent request ID")
	}
}

func TestRespondToAskUser_ExistingRequest(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()

	ch := make(chan string, 1)
	mgr.mu.Lock()
	mgr.pending["ask_999"] = ch
	mgr.mu.Unlock()

	got := mgr.RespondToAskUser("ask_999", "my answer")
	if !got {
		t.Fatal("expected true for existing request ID")
	}

	resp := <-ch
	if resp != "my answer" {
		t.Errorf("expected 'my answer', got %q", resp)
	}
}

// ---------------------------------------------------------------------------
// SetGlobalAskUserManager / GetGlobalAskUserManager
// ---------------------------------------------------------------------------

// NOTE: These tests modify global state and must NOT run in parallel with
// other tests that also modify the global AskUserManager.

func TestGlobalAskUserManager_SetAndGet(t *testing.T) {
	// Save current state
	prev := GetGlobalAskUserManager()
	t.Cleanup(func() { SetGlobalAskUserManager(prev) })

	mgr := NewAskUserManager()
	SetGlobalAskUserManager(mgr)

	got := GetGlobalAskUserManager()
	if got != mgr {
		t.Error("expected same manager instance from GetGlobalAskUserManager")
	}
}

func TestGlobalAskUserManager_NilByDefault(t *testing.T) {
	// Save and restore
	prev := GetGlobalAskUserManager()
	t.Cleanup(func() { SetGlobalAskUserManager(prev) })

	SetGlobalAskUserManager(nil)
	got := GetGlobalAskUserManager()
	if got != nil {
		t.Errorf("expected nil global manager, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// RequestAskUser
// ---------------------------------------------------------------------------

func TestRequestAskUser_NilEventBus(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()
	_, err := mgr.RequestAskUser(context.Background(), nil, AskUserRequest{Question: "question"}, "client", "", "")
	if err == nil {
		t.Fatal("expected error for nil event bus")
	}
	if !strings.Contains(err.Error(), "no event bus available") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRequestAskUser_EmptyQuestion(t *testing.T) {
	t.Parallel()
	mgr := NewAskUserManager()
	bus := events.NewEventBus()
	_, err := mgr.RequestAskUser(context.Background(), bus, AskUserRequest{Question: ""}, "client", "", "")
	if err == nil {
		t.Fatal("expected error for empty question")
	}
	if !strings.Contains(err.Error(), "empty question") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRequestAskUser_SuccessfulResponse(t *testing.T) {
	mgr := NewAskUserManager()
	mgr.SetTimeout(5 * time.Second)
	bus := events.NewEventBus()

	// Subscribe to the ask_user_request event so we can capture the requestID
	sub := bus.Subscribe("test-subscriber")

	go func() {
		select {
		case ev := <-sub:
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				return
			}
			requestID, _ := data["request_id"].(string)
			// Respond to the ask_user request
			mgr.RespondToAskUser(requestID, "hello world")
		case <-time.After(3 * time.Second):
			// Prevent goroutine leak if channel never receives
			return
		}
	}()

	result, err := mgr.RequestAskUser(context.Background(), bus, AskUserRequest{Question: "What is your name?"}, "client1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

// Timeout increased from 100ms to 250ms for more robustness on CI systems
// under load where GC pauses or scheduler delays could affect timing.
func TestRequestAskUser_Timeout(t *testing.T) {
	mgr := NewAskUserManager()
	mgr.SetTimeout(250 * time.Millisecond)
	bus := events.NewEventBus()

	// Do not respond — let it timeout
	_, err := mgr.RequestAskUser(context.Background(), bus, AskUserRequest{Question: "Will you wait forever?"}, "client1", "", "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "did not respond") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AskUserWithEventBus
// ---------------------------------------------------------------------------

func TestAskUserWithEventBus_EmptyQuestion(t *testing.T) {
	// Save and restore global
	prev := GetGlobalAskUserManager()
	t.Cleanup(func() { SetGlobalAskUserManager(prev) })

	_, err := AskUserWithEventBus(context.Background(), AskUserRequest{Question: ""}, nil, "", "", "", nil)
	if err == nil {
		t.Fatal("expected error for empty question")
	}
	if !strings.Contains(err.Error(), "empty question") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAskUserWithEventBus_RoutesThroughEventBus(t *testing.T) {
	mgr := NewAskUserManager()
	mgr.SetTimeout(5 * time.Second)
	bus := events.NewEventBus()

	sub := bus.Subscribe("test-subscriber")
	go func() {
		select {
		case ev := <-sub:
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				return
			}
			requestID, _ := data["request_id"].(string)
			mgr.RespondToAskUser(requestID, "yes")
		case <-time.After(3 * time.Second):
			// Prevent goroutine leak if channel never receives
			return
		}
	}()

	result, err := AskUserWithEventBus(context.Background(), AskUserRequest{Question: "Do you agree?"}, bus, "c1", "", "", mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "yes" {
		t.Errorf("expected 'yes', got %q", result)
	}
}

func TestRequestAskUser_ChatIDInPayload(t *testing.T) {
	mgr := NewAskUserManager()
	mgr.SetTimeout(5 * time.Second)
	bus := events.NewEventBus()

	sub := bus.Subscribe("test-chatid")
	go func() {
		select {
		case ev := <-sub:
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				return
			}
			// Verify chat_id is present in the event payload
			if chatID, _ := data["chat_id"].(string); chatID != "chat_42" {
				log.Printf("TestRequestAskUser_ChatIDInPayload: expected chat_id=chat_42, got %q", chatID)
			}
			requestID, _ := data["request_id"].(string)
			mgr.RespondToAskUser(requestID, "ok")
		case <-time.After(3 * time.Second):
			return
		}
	}()

	result, err := mgr.RequestAskUser(context.Background(), bus, AskUserRequest{Question: "Question?"}, "c1", "u1", "chat_42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestRequestAskUser_ContextCancelled(t *testing.T) {
	mgr := NewAskUserManager()
	mgr.SetTimeout(5 * time.Second) // long timeout — context should cancel first
	bus := events.NewEventBus()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := mgr.RequestAskUser(ctx, bus, AskUserRequest{Question: "Will context cancel?"}, "c1", "", "")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected cancellation error, got: %v", err)
	}
}
