//go:build !js

package webui

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newFocusTestServer returns a server with two client contexts: "window-a"
// (the window on screen) and "window-b" (behind it).
func newFocusTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.clientContexts["window-a"] = &webClientContext{}
	ws.clientContexts["window-b"] = &webClientContext{}
	return ws
}

func askUserEvent(requestID string) events.UIEvent {
	return events.UIEvent{
		Type: events.EventTypeAskUserRequest,
		Data: map[string]interface{}{"request_id": requestID, "question": "Proceed?"},
	}
}

// TestAskUserGoesOnlyToForegroundWindow pins the fix for a question surfacing
// in a window the user is not looking at: both windows of a client used to
// receive the fan-out, so the dialog popped up everywhere.
func TestAskUserGoesOnlyToForegroundWindow(t *testing.T) {
	ws := newFocusTestServer(t)

	front := &ConnectionInfo{ClientID: "window-a"}
	back := &ConnectionInfo{ClientID: "window-b"}
	front.setForeground(true)
	back.setForeground(false)

	ws.setClientForeground("window-a", true)
	ws.setClientForeground("window-b", false)

	if !ws.shouldForwardEventToConnection(askUserEvent("ask-1"), front) {
		t.Error("foreground window must receive the ask_user question")
	}
	if ws.shouldForwardEventToConnection(askUserEvent("ask-1"), back) {
		t.Error("background window must not receive the ask_user question")
	}
}

// TestAskUserFanOutWithoutFocusReports keeps the old behaviour for clients
// that never report focus (single window, older client, tests).
func TestAskUserFanOutWithoutFocusReports(t *testing.T) {
	ws := newFocusTestServer(t)

	a := &ConnectionInfo{ClientID: "window-a"}
	b := &ConnectionInfo{ClientID: "window-b"}

	if !ws.shouldForwardEventToConnection(askUserEvent("ask-1"), a) ||
		!ws.shouldForwardEventToConnection(askUserEvent("ask-1"), b) {
		t.Error("without a focus report every window must stay in the fan-out")
	}
}

// TestClientForegroundFollowsFocusChanges checks that bringing a window
// forward hands the question over to it: the window that reports focus owns
// the foreground record, and the one that reported earlier stops being a
// candidate once it reports blur.
func TestClientForegroundFollowsFocusChanges(t *testing.T) {
	ws := newFocusTestServer(t)
	ws.setClientForeground("window-a", true)
	ws.setClientForeground("window-b", false)

	if got, ok := ws.clientForegroundWindow("window-a"); !ok || got != "window-a" {
		t.Fatalf("foreground window = %q (ok=%v), want window-a", got, ok)
	}

	// The user clicks window-b: both windows report on their next heartbeat.
	ws.setClientForeground("window-b", true)
	ws.setClientForeground("window-a", false)

	if got, ok := ws.clientForegroundWindow("window-b"); !ok || got != "window-b" {
		t.Fatalf("after focus change foreground window = %q (ok=%v), want window-b", got, ok)
	}
	if _, ok := ws.clientForegroundWindow("window-a"); ok {
		t.Error("window-a reported blur and must no longer be a foreground candidate")
	}
}

// TestClientForegroundExpires covers a window that goes away without a final
// heartbeat: its report must not starve the other windows forever.
func TestClientForegroundExpires(t *testing.T) {
	ws := newFocusTestServer(t)
	ws.setClientForeground("window-a", true)
	ws.clientContexts["window-a"].ForegroundAt = time.Now().Add(-foregroundReportTTL - time.Second)

	if _, ok := ws.clientForegroundWindow("window-a"); ok {
		t.Error("a stale focus report must not be treated as authoritative")
	}
}

// TestHeartbeatReportsFocus drives the heartbeat path end to end.
func TestHeartbeatReportsFocus(t *testing.T) {
	ws := newFocusTestServer(t)
	connInfo := &ConnectionInfo{ClientID: "window-b"}
	pair := newTestingConnPair(t)
	defer pair.server.Close()

	background := false
	ws.handleHeartbeatMessage(pair.server, "window-b", connInfo, &HeartbeatData{Foreground: &background})

	if connInfo.isForeground() {
		t.Error("connection must be marked background after reporting foreground=false")
	}
	if _, ok := ws.clientForegroundWindow("window-b"); ok {
		t.Error("a window reporting foreground=false must not become the foreground candidate")
	}

	foreground := true
	ws.handleHeartbeatMessage(pair.server, "window-b", connInfo, &HeartbeatData{Foreground: &foreground})

	if !connInfo.isForeground() {
		t.Error("connection must be marked foreground after reporting foreground=true")
	}
	if got, ok := ws.clientForegroundWindow("window-b"); !ok || got != "window-b" {
		t.Fatalf("client foreground window = %q (ok=%v), want window-b", got, ok)
	}
}

// TestHeartbeatWithoutFocusLeavesStateAlone pins the backward-compatible
// path: an older client's heartbeat (no data) must not demote the window.
func TestHeartbeatWithoutFocusLeavesStateAlone(t *testing.T) {
	ws := newFocusTestServer(t)
	connInfo := &ConnectionInfo{ClientID: "window-a"}
	connInfo.setForeground(true)
	ws.clientContexts["window-a"].LastSeenAt = time.Now().Add(-time.Minute)
	pair := newTestingConnPair(t)
	defer pair.server.Close()

	ws.handleHeartbeatMessage(pair.server, "window-a", connInfo, &HeartbeatData{})

	if !connInfo.isForeground() {
		t.Error("a heartbeat without a focus field must leave the connection foreground")
	}
	if _, ok := ws.clientForegroundWindow("window-a"); ok {
		t.Error("a heartbeat without a focus field must not record a focus report")
	}
}
