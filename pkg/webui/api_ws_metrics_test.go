//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// wsMetricsForTest builds a fresh ReactWebServer with the daemon test
// scaffolding (so dispatch routes to Mode 2 + a real UserConnections
// registry) and returns the same testServer wrapper used by
// daemon_session_isolation_test.go's integration tests.
func wsMetricsForTest(t *testing.T) *testServer {
	return newDaemonTestServer(t, nil)
}

func TestHandleAPIWSMetrics_EmptyServer(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Default dispatch → Mode 2 (no agent, no flag). The metrics
	// response is computed without an agent.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ws-metrics", nil)
	ws.handleAPIWSMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp wsMetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Mode != wsModeDaemon {
		t.Errorf("mode = %q, want \"daemon\"", resp.Mode)
	}
	if resp.TotalConnections != 0 {
		t.Errorf("total = %d, want 0", resp.TotalConnections)
	}
	if resp.UsersWithConnections != 0 {
		t.Errorf("users = %d, want 0", resp.UsersWithConnections)
	}
	if resp.MaxConnectionsPerUser != 0 {
		t.Errorf("max = %d, want 0", resp.MaxConnectionsPerUser)
	}
	if len(resp.PerUser) != 0 {
		t.Errorf("perUser len = %d, want 0", len(resp.PerUser))
	}
}

func TestHandleAPIWSMetrics_MethodNotAllowed(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/ws-metrics", nil)
		ws.handleAPIWSMetrics(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("method %s: status = %d, want 405", method, rec.Code)
		}
	}
}

func TestHandleAPIWSMetrics_NoStoreCache(t *testing.T) {
	// Cache-Control: no-store must be set so monitoring scrapers don't
	// poll a stale snapshot.
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ws-metrics", nil)
	ws.handleAPIWSMetrics(rec, req)
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestHandleAPIWSMetrics_ReflectsRegistry(t *testing.T) {
	// Drive the registry directly (no live WS dials — those are
	// integration-tested in daemon_session_isolation_test.go).
	// This isolates the metrics computation from any handler bugs.
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.userConnections.Add("alice", UserConnection{Raw: "a1", SessionID: "a1"})
	ws.userConnections.Add("alice", UserConnection{Raw: "a2", SessionID: "a2"})
	ws.userConnections.Add("alice", UserConnection{Raw: "a3", SessionID: "a3"})
	ws.userConnections.Add("bob", UserConnection{Raw: "b1", SessionID: "b1"})
	ws.userConnections.Add("carol", UserConnection{Raw: "c1", SessionID: "c1"})

	resp := ws.computeWSMetrics()

	if resp.TotalConnections != 5 {
		t.Errorf("total = %d, want 5", resp.TotalConnections)
	}
	if resp.UsersWithConnections != 3 {
		t.Errorf("users = %d, want 3", resp.UsersWithConnections)
	}
	if resp.MaxConnectionsPerUser != 3 {
		t.Errorf("max = %d, want 3 (alice)", resp.MaxConnectionsPerUser)
	}
	// PerUser must be sorted descending by SessionCount, alice first.
	if len(resp.PerUser) < 1 || resp.PerUser[0].UserID != "alice" || resp.PerUser[0].SessionCount != 3 {
		t.Errorf("perUser[0] = %+v, want alice/3", resp.PerUser[0])
	}
}

func TestHandleAPIWSMetrics_CapAtMaxPerUser(t *testing.T) {
	// When many users have a single connection, the response must
	// trim to maxPerUserInMetrics entries. The total counts above
	// stay exact.
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// 25 unique IDs (well past the cap).
	for i := 0; i < maxPerUserInMetrics+5; i++ {
		uid := "u" + string(rune('a'+(i%26))) + string(rune('A'+(i/26)))
		ws.userConnections.Add(uid, UserConnection{Raw: uid, SessionID: uid})
	}

	resp := ws.computeWSMetrics()

	if resp.UsersWithConnections != maxPerUserInMetrics+5 {
		t.Errorf("users = %d, want %d (total count, not trimmed)", resp.UsersWithConnections, maxPerUserInMetrics+5)
	}
	if resp.TotalConnections != maxPerUserInMetrics+5 {
		t.Errorf("total = %d, want %d", resp.TotalConnections, maxPerUserInMetrics+5)
	}
	if got := len(resp.PerUser); got != maxPerUserInMetrics {
		t.Errorf("perUser len = %d, want %d (trimmed)", got, maxPerUserInMetrics)
	}
}

func TestHandleAPIWSMetrics_ModeFlag(t *testing.T) {
	cases := []struct {
		name string
		flag bool
		want wsMode
	}{
		{"mode-2-default", false, wsModeDaemon},
		{"mode-1-flag-true", true, wsModeAgent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
			if err != nil {
				t.Fatal(err)
			}
			ws.agentEnforceSingleSession = tc.flag
			resp := ws.computeWSMetrics()
			if resp.Mode != tc.want {
				t.Errorf("mode = %q, want %q", resp.Mode, tc.want)
			}
		})
	}
}

func TestHandleAPIWSMetrics_NilRegistry(t *testing.T) {
	// Defensive: even if userConnections is somehow nil, the
	// response is a zeroed but well-formed payload. Production never
	// reaches this state (NewReactWebServer initializes the field)
	// but the unit test guards against future refactors that
	// accidentally make the field optional.
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.userConnections = nil

	resp := ws.computeWSMetrics()
	if resp.TotalConnections != 0 || resp.MaxConnectionsPerUser != 0 || len(resp.PerUser) != 0 {
		t.Errorf("nil-registry response should be all-zero, got %+v", resp)
	}
	if resp.Mode != wsModeDaemon {
		t.Errorf("nil-registry mode = %q, want \"daemon\" (default)", resp.Mode)
	}
}

// Sanity check: the sort is stable when counts tie. Two users with
// the same count must come out in UserID-sorted order, not random.
func TestHandleAPIWSMetrics_SortStability(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"charlie", "alice", "bob"} {
		ws.userConnections.Add(uid, UserConnection{Raw: uid, SessionID: uid})
	}
	resp := ws.computeWSMetrics()
	want := []string{"alice", "bob", "charlie"}
	got := make([]string, len(resp.PerUser))
	for i, u := range resp.PerUser {
		got[i] = u.UserID
	}
	if !equalStrings(got, want) {
		t.Errorf("perUser order = %v, want %v (UserID-sorted on tie)", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// _ = wsMetricsForTest silences the unused-helper warning when all
// integration-style tests are skipped via build tag.
var _ = wsMetricsForTest
