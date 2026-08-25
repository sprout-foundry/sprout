//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newDaemonTestServer is newTestServer's Mode 2 counterpart: same
// scaffolding, but agentEnforceSingleSession is left false so the
// dispatcher routes to handleWebSocket_Daemon (multi-session).
//
// Also enables serviceMode with trustedUserHeader="X-Sprout-User" so
// ExtractUserID honors the dialConnUser-supplied header. This mirrors
// the production daemon (SPROUT_SERVICE=1) configuration where the
// daemon authenticates per-user connections via a trusted proxy.
func newDaemonTestServer(t *testing.T, preConfigure func(*ReactWebServer)) *testServer {
	t.Helper()
	bus := events.NewEventBus()
	srv, err := NewReactWebServer(nil, bus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }
	// SP-118 Phase 2: route to Mode 2 (multi-session). newTestServer
	// forces this to true; this helper deliberately does NOT.
	//
	// Service mode + trusted user header: required for ExtractUserID
	// to return the dialed-in user id. Without these, every test
	// session collapses onto the clientID and the user-scoped
	// assertions in TestDaemon_TwoSessionsSameUser_NoConflict fail.
	srv.serviceMode = true
	srv.trustedUserHeader = "X-Sprout-User"
	if preConfigure != nil {
		preConfigure(srv)
	}

	ts := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	t.Cleanup(func() {
		ts.Close()
	})

	return &testServer{ts: ts, srv: srv, bus: bus}
}

// dialConnUser dials the WebSocket endpoint with both a clientID and a
// service-mode user header. Used to exercise Mode 2 service-mode
// paths where the tracking key is the user id, not the client id.
func (ts *testServer) dialConnUser(t *testing.T, clientID, userID string) *websocket.Conn {
	t.Helper()
	dialer := &websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	headers := http.Header{}
	if clientID != "" {
		headers.Set("X-Sprout-Client-ID", clientID)
	}
	if userID != "" {
		headers.Set("X-Sprout-User", userID)
	}

	conn, _, err := dialer.Dial(ts.wsDialURL(), headers)
	if err != nil {
		t.Fatalf("dial WebSocket with user: %v", err)
	}
	return conn
}

// ---------------------------------------------------------------------------
// Tests: Mode 2 — N parallel sessions, no conflict
// ---------------------------------------------------------------------------

// TestDaemon_TwoSessionsSameUser_NoConflict verifies that two WS
// connections for the same user both register and both receive
// connection_status, with no session_conflict message. This is the
// core Mode 2 invariant: N parallel browser windows per user.
func TestDaemon_TwoSessionsSameUser_NoConflict(t *testing.T) {
	ts := newDaemonTestServer(t, nil)

	conn1 := ts.dialConnUser(t, "client-1", "user-A")
	defer conn1.Close()
	conn2 := ts.dialConnUser(t, "client-2", "user-A")
	defer conn2.Close()

	// Both must receive connection_status, neither session_conflict.
	expectType(t, conn1, 5*time.Second, "connection_status")
	expectType(t, conn2, 5*time.Second, "connection_status")

	if got := ts.srv.userConnections.Count("user-A"); got != 2 {
		t.Fatalf("userConnections.Count(user-A) = %d, want 2", got)
	}

	snap := ts.srv.userConnections.Snapshot("user-A")
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}
	// Distinct sessions, distinct clients.
	if snap[0].SessionID == snap[1].SessionID {
		t.Errorf("sessions collided: %s == %s", snap[0].SessionID, snap[1].SessionID)
	}
	if snap[0].ClientID == snap[1].ClientID {
		t.Errorf("clients collided: %s == %s", snap[0].ClientID, snap[1].ClientID)
	}
}

// TestDaemon_ThreeSessions_DistinctUsers verifies per-user isolation:
// three sessions across three users don't see each other.
func TestDaemon_ThreeSessions_DistinctUsers(t *testing.T) {
	ts := newDaemonTestServer(t, nil)

	a1 := ts.dialConnUser(t, "c1", "user-A")
	defer a1.Close()
	b1 := ts.dialConnUser(t, "c2", "user-B")
	defer b1.Close()
	c1 := ts.dialConnUser(t, "c3", "user-C")
	defer c1.Close()

	expectType(t, a1, 5*time.Second, "connection_status")
	expectType(t, b1, 5*time.Second, "connection_status")
	expectType(t, c1, 5*time.Second, "connection_status")

	for _, uid := range []string{"user-A", "user-B", "user-C"} {
		if got := ts.srv.userConnections.Count(uid); got != 1 {
			t.Errorf("Count(%s) = %d, want 1", uid, got)
		}
	}
	if got := len(ts.srv.userConnections.AllUserIDs()); got != 3 {
		t.Errorf("AllUserIDs len = %d, want 3", got)
	}
}

// TestDaemon_DisconnectDoesNotAffectSibling verifies that closing one
// window does NOT impact a sibling window on the same user. After
// conn1 closes, conn2 must still be alive in the registry and able
// to send / receive.
func TestDaemon_DisconnectDoesNotAffectSibling(t *testing.T) {
	ts := newDaemonTestServer(t, nil)

	conn1 := ts.dialConnUser(t, "client-1", "user-A")
	defer conn1.Close()
	conn2 := ts.dialConnUser(t, "client-2", "user-A")
	defer conn2.Close()

	expectType(t, conn1, 5*time.Second, "connection_status")
	expectType(t, conn2, 5*time.Second, "connection_status")

	if got := ts.srv.userConnections.Count("user-A"); got != 2 {
		t.Fatalf("Count(user-A) = %d, want 2", got)
	}

	// Close conn1. Server-side Remove should run on its deferred
	// exit path; the registry count must drop to 1.
	conn1.Close()

	// Poll for the registry to settle. The defer runs after the WS
	// read goroutine exits, which happens when the close frame is
	// processed. A 2s ceiling is generous on a local test.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ts.srv.userConnections.Count("user-A") == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := ts.srv.userConnections.Count("user-A"); got != 1 {
		t.Fatalf("Count(user-A) after close = %d, want 1", got)
	}

	// conn2 is still healthy: it can receive a no-op message and
	// respond. Use the existing request_stats path as a heartbeat.
	sendMessage(t, conn2, map[string]interface{}{
		"type": AllowedMessageTypeRequestStats,
	})
	expectType(t, conn2, 5*time.Second, "stats_update")
}

// TestDaemon_LocalModeUsesClientIDAsKey verifies that without a user
// header, the local-mode clientID is used as the tracking key —
// matching Mode 1 conventions for a single-machine installation.
func TestDaemon_LocalModeUsesClientIDAsKey(t *testing.T) {
	ts := newDaemonTestServer(t, nil)

	conn1 := ts.dialConn(t, "client-A")
	defer conn1.Close()
	conn2 := ts.dialConn(t, "client-A")
	defer conn2.Close()

	expectType(t, conn1, 5*time.Second, "connection_status")
	expectType(t, conn2, 5*time.Second, "connection_status")

	// Both connections sit under the clientID (local-mode tracking
	// key), not under any user id.
	if got := ts.srv.userConnections.Count("client-A"); got != 2 {
		t.Errorf("Count(client-A) = %d, want 2 (local-mode key)", got)
	}
}

// TestDaemon_RegistryConcurrentSessions runs goroutines that open and
// close connections in parallel. We don't assert exact counts (the
// timing of defers means snapshots can briefly disagree); the test
// catches torn reads and use-after-free in the registry.
func TestDaemon_RegistryConcurrentSessions(t *testing.T) {
	ts := newDaemonTestServer(t, nil)

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			userID := "user-conc"
			conn := ts.dialConnUser(t, "client", userID)
			defer conn.Close()
			expectType(t, conn, 5*time.Second, "connection_status")
			// Linger briefly then disconnect.
			time.Sleep(20 * time.Millisecond)
		}(g)
	}
	wg.Wait()

	// Allow deferred Remove to drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ts.srv.userConnections.Count("user-conc") == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := ts.srv.userConnections.Count("user-conc"); got != 0 {
		t.Errorf("Count(user-conc) after concurrent disconnect = %d, want 0", got)
	}
}
