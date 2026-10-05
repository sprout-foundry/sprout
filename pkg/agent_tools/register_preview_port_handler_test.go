package tools

// SP-155 §155a (TODO 155.5): register_preview_port publishes a
// preview_port_registered event (on the shared event bus) after a
// successful platform registration, so the in-process webui server can
// learn the hosted URL. The tool's model-visible output is unchanged, and
// the publish is best-effort — failure cases and a nil bus never fail the
// tool. These tests stub the platform API (an httptest server standing in
// for $PLATFORM_API_URL) and a real events.EventBus.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newPlatformStub stands in for the platform's /internal/workspace/{id}/ports
// endpoint, returning status with body. The path the handler hits is
// /internal/workspace/ws1/ports (WORKSPACE_ID is "ws1" in these tests).
func newPlatformStub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

// runRegister executes the tool and, when an event bus is present, captures
// the first event published (within timeout). It returns the tool result,
// whether an event arrived, and the event (nil when none).
func runRegister(t *testing.T, env ToolEnv, args map[string]any, wait time.Duration) (ToolResult, bool, *events.UIEvent) {
	t.Helper()
	var sub <-chan events.UIEvent
	if env.EventBus != nil {
		sub = env.EventBus.Subscribe("register-capture")
	}
	result, err := (&registerPreviewPortHandler{}).Execute(context.Background(), env, args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if sub == nil {
		return result, false, nil
	}
	select {
	case ev := <-sub:
		return result, true, &ev
	case <-time.After(wait):
		return result, false, nil
	}
}

// setPlatformEnv points the handler at a platform stub for workspace ws1.
func setPlatformEnv(t *testing.T, stubURL string) {
	t.Helper()
	t.Setenv("WORKSPACE_ID", "ws1")
	t.Setenv("WORKSPACE_TOKEN", "test-token")
	t.Setenv("PLATFORM_API_URL", stubURL)
}

func TestRegisterPreviewPort_PublishesOnSuccess(t *testing.T) {
	stub := newPlatformStub(t, http.StatusCreated, `{"preview_url":"https://preview.example/ws1/8080"}`)
	setPlatformEnv(t, stub.URL)
	bus := events.NewEventBus()
	env := ToolEnv{EventBus: bus}

	result, got, ev := runRegister(t, env, map[string]any{"port": 8080, "label": "My App"}, 2*time.Second)

	// (a) the model-visible output still carries the URL (printing unchanged).
	if !strings.Contains(result.Output, "https://preview.example/ws1/8080") {
		t.Fatalf("expected the URL in the tool output, got %q", result.Output)
	}
	if result.IsError {
		t.Fatalf("a successful registration must not be an error, got %q", result.Output)
	}

	// (b) the bus received preview_port_registered with url/port/label.
	if !got {
		t.Fatalf("expected a preview_port_registered event on the bus")
	}
	if ev.Type != events.EventTypePreviewPortRegistered {
		t.Fatalf("expected event type %q, got %q", events.EventTypePreviewPortRegistered, ev.Type)
	}
	data, ok := ev.Data.(map[string]any)
	if !ok {
		t.Fatalf("event data is %T, want map[string]any", ev.Data)
	}
	if data["preview_url"] != "https://preview.example/ws1/8080" {
		t.Errorf("preview_url = %v, want the stub URL", data["preview_url"])
	}
	if data["port"] != 8080 {
		t.Errorf("port = %v, want 8080", data["port"])
	}
	if data["label"] != "My App" {
		t.Errorf("label = %v, want %q", data["label"], "My App")
	}
}

func TestRegisterPreviewPort_NoEventWhenWorkspaceIDUnset(t *testing.T) {
	// A platform stub is present but must never be reached.
	stub := newPlatformStub(t, http.StatusCreated, `{"preview_url":"https://preview.example/ws1/8080"}`)
	setPlatformEnv(t, stub.URL)
	t.Setenv("WORKSPACE_ID", "")

	env := ToolEnv{EventBus: events.NewEventBus()}
	result, got, _ := runRegister(t, env, map[string]any{"port": 8080}, 500*time.Millisecond)

	if !strings.Contains(result.Output, "WORKSPACE_ID not set") {
		t.Fatalf("expected the missing-workspace error, got %q", result.Output)
	}
	if !result.IsError {
		t.Error("a missing WORKSPACE_ID must be an error result")
	}
	if got {
		t.Fatal("no event must be published when registration cannot happen")
	}
}

func TestRegisterPreviewPort_NoEventOnPlatformError(t *testing.T) {
	stub := newPlatformStub(t, http.StatusInternalServerError, `{"error":"boom"}`)
	setPlatformEnv(t, stub.URL)

	env := ToolEnv{EventBus: events.NewEventBus()}
	result, got, _ := runRegister(t, env, map[string]any{"port": 8080}, 500*time.Millisecond)

	if !result.IsError {
		t.Fatalf("a platform 500 must be an error result, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "registration failed") {
		t.Fatalf("expected the platform-failure message, got %q", result.Output)
	}
	if got {
		t.Fatal("no event must be published on a platform failure")
	}
}

func TestRegisterPreviewPort_NoEventWhenPlatformUnreachable(t *testing.T) {
	// A closed server: the POST fails (connection refused) before any
	// registration, so the tool reports "unavailable" and publishes nothing.
	stub := newPlatformStub(t, http.StatusCreated, `{"preview_url":"https://preview.example/ws1/8080"}`)
	stub.Close()
	setPlatformEnv(t, stub.URL)

	env := ToolEnv{EventBus: events.NewEventBus()}
	result, got, _ := runRegister(t, env, map[string]any{"port": 8080}, 500*time.Millisecond)

	if !result.IsError {
		t.Fatalf("an unreachable platform must be an error result, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "unavailable") {
		t.Fatalf("expected the unavailable message, got %q", result.Output)
	}
	if got {
		t.Fatal("no event must be published when the platform is unreachable")
	}
}

func TestRegisterPreviewPort_NilEventBusDoesNotPanic(t *testing.T) {
	stub := newPlatformStub(t, http.StatusCreated, `{"preview_url":"https://preview.example/ws1/8080"}`)
	setPlatformEnv(t, stub.URL)

	// No event bus: the tool must still succeed and report the URL.
	env := ToolEnv{}
	result, got, ev := runRegister(t, env, map[string]any{"port": 8080, "label": "dev server"}, 500*time.Millisecond)

	if result.IsError {
		t.Fatalf("a nil bus must not fail the tool, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "https://preview.example/ws1/8080") {
		t.Fatalf("expected the URL in the output, got %q", result.Output)
	}
	if got || ev != nil {
		t.Fatalf("no event expected without a bus (got=%v)", got)
	}
}

// TestRegisterPreviewPort_PublishesDefaultsLabel pins that a missing label
// still publishes (with the handler's default label) rather than dropping
// the event.
func TestRegisterPreviewPort_PublishesDefaultsLabel(t *testing.T) {
	stub := newPlatformStub(t, http.StatusOK, `{"preview_url":"https://preview.example/ws1/3000"}`)
	setPlatformEnv(t, stub.URL)

	env := ToolEnv{EventBus: events.NewEventBus()}
	result, got, ev := runRegister(t, env, map[string]any{"port": 3000}, 2*time.Second)

	if !strings.Contains(result.Output, "https://preview.example/ws1/3000") {
		t.Fatalf("expected the URL in the output, got %q", result.Output)
	}
	if !got || ev == nil {
		t.Fatal("expected a preview_port_registered event")
	}
	data := ev.Data.(map[string]any)
	if data["label"] != "dev server" {
		t.Errorf("missing label should default to %q, got %v", "dev server", data["label"])
	}
}
