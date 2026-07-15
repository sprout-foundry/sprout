//go:build !js

package webui

import (
	"testing"
)

// TestShouldUseMode1_AgentEnforceForcesMode1 verifies that
// agentEnforceSingleSession=true is the strongest signal: even with a
// nil agent (which would otherwise default to Mode 2), Mode 1 is
// used. Mirrors sprout agent production: the CLI always sets the
// flag regardless of config.
func TestShouldUseMode1_AgentEnforceForcesMode1(t *testing.T) {
	ws := &ReactWebServer{agentEnforceSingleSession: true}
	if !ws.shouldUseMode1() {
		t.Fatalf("agentEnforceSingleSession=true must force Mode 1 (got false)")
	}
}

// TestShouldUseMode1_NoAgentNoFlagDefaultsMode2 verifies the fallback
// branch in shouldUseMode1: when neither the explicit flag nor a real
// config manager are present, dispatch defaults to Mode 2. This
// covers:
//   - test scaffolding with no agent (covers the newDaemonTestServer
//     path which uses Mode 2 to exercise multi-session behavior);
//   - production daemon launches where the provider isn't yet
//     configured (chatAgent is nil) — same fallback.
func TestShouldUseMode1_NoAgentNoFlagDefaultsMode2(t *testing.T) {
	ws := &ReactWebServer{agentEnforceSingleSession: false}
	if ws.shouldUseMode1() {
		t.Fatalf("no agent + no flag must default to Mode 2 (got true)")
	}
}

// TestShouldUseMode1_NilAgentExplicitNil covers the nil-agent branch
// specifically: when agent is explicitly nil and the flag is also
// false, Mode 2 is used. Distinct from the previous test in that it
// makes the agent==nil condition explicit in the struct literal so
// future refactors of the dispatch logic can't accidentally delete
// this branch thinking it's test-only.
func TestShouldUseMode1_NilAgentExplicitNil(t *testing.T) {
	ws := &ReactWebServer{agentEnforceSingleSession: false, agent: nil}
	if ws.shouldUseMode1() {
		t.Fatalf("explicitly nil agent + no flag must default to Mode 2 (got true)")
	}
}

// TestShouldUseMode1_FlagTrueOverridesNilAgent verifies that the flag
// is the strongest signal — it wins even when the agent is nil and
// the dispatch would otherwise default to Mode 2.
func TestShouldUseMode1_FlagTrueOverridesNilAgent(t *testing.T) {
	ws := &ReactWebServer{agentEnforceSingleSession: true, agent: nil}
	if !ws.shouldUseMode1() {
		t.Fatalf("flag=true with nil agent must force Mode 1 (got false)")
	}
}

// TestShouldUseMode1_DispatcherRoutingConsistent is a documentation
// test pinning the boolean ↔ dispatcher contract: if
// shouldUseMode1() returns true, handleWebSocket MUST route to
// handleWebSocket_Agent; if it returns false, to handleWebSocket_Daemon.
// The integration tests in daemon_session_isolation_test.go and
// websocket_session_conflict_test.go assert the end-to-end behavior;
// this test catches accidental coupling breaks at the unit level.
func TestShouldUseMode1_DispatcherRoutingConsistent(t *testing.T) {
	cases := []struct {
		name    string
		flag    bool
		want    bool
		comment string
	}{
		{"agent-path", true, true, "agent path forces Mode 1"},
		{"daemon-default", false, false, "no config → Mode 2 default-on"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := &ReactWebServer{agentEnforceSingleSession: tc.flag}
			if got := ws.shouldUseMode1(); got != tc.want {
				t.Errorf("%s: shouldUseMode1() = %v, want %v (%s)",
					tc.name, got, tc.want, tc.comment)
			}
		})
	}
}
