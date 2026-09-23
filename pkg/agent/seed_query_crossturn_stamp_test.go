package agent

import (
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestCrossTurnTimestampStability pins the prompt-cache-eligibility invariant
// for the <current-time> envelope: a user message's wire bytes must be
// identical in its own turn and in every later turn's requests.
//
// History: stamping only at the provider boundary (per-request copies) sent
// user message N out stamped during turn N and unstamped from turn N+1 on —
// flipping prefix bytes at every turn boundary and forcing a full re-read of
// the ~85K-token prefix each turn (provider cache hit rate 0% on the affected
// population). The fix stamps once at injection; the stamped bytes live in
// conversation state and every later request replays them unchanged.
func TestCrossTurnTimestampStability(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	disabled := false
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ChangeTracking = &configuration.ChangeTrackingConfig{Enabled: &disabled}
		return nil
	}); err != nil {
		t.Fatalf("disable change tracking: %v", err)
	}

	// Two turns, one model response each (no tool calls — keep the flow
	// minimal so the wire requests are exactly [.. turn1 user, assistant,
	// turn2 user]).
	client := NewScriptedClient(
		NewScriptedTextResponse("first turn done"),
		NewScriptedTextResponse("second turn done"),
	)

	a, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	a.initSubManagers()

	// Pin distinct turn timestamps so cross-turn byte drift is observable;
	// processQueryWithSeed assigns time.Now() itself, so capture the actual
	// stamped bytes from turn 1's request and require turn 2 to replay them.
	stampTurn := func(at time.Time) {
		a.turnTimestampMu.Lock()
		a.turnTimestamp = at
		a.turnTimestampMu.Unlock()
	}
	_ = stampTurn // overwritten per turn by processQueryWithSeed; see above

	if _, err := a.processQueryWithSeed(QuerySourceUnknown, "first question"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if _, err := a.processQueryWithSeed(QuerySourceUnknown, "second question"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}

	if len(client.sentRequests) != 2 {
		t.Fatalf("expected exactly 2 recorded requests (one per turn), got %d", len(client.sentRequests))
	}

	// Turn 1's request: the (only) user message must carry a stamp envelope.
	first := client.sentRequests[0]
	var user1 string
	for _, m := range first {
		if m.Role == "user" {
			user1 = m.Content
			break
		}
	}
	if !strings.HasPrefix(user1, "<current-time>") || !strings.Contains(user1, "</current-time>\n\nfirst question") {
		t.Fatalf("turn 1 request user message = %q, want a <current-time> envelope around \"first question\"", user1)
	}

	// The turn-2 request must replay user1 byte-identically (still its own
	// turn's stamp) and carry user2 with a stamp envelope of its own.
	for i, req := range client.sentRequests[1:] {
		var gotUser1, gotUser2 string
		for _, m := range req {
			switch {
			case m.Role == "user" && gotUser1 == "":
				gotUser1 = m.Content
			case m.Role == "user":
				gotUser2 = m.Content
			}
		}
		if gotUser1 != user1 {
			t.Fatalf("turn 2 request %d: user message 1 bytes changed across turns:\n got  %q\n want %q", i, gotUser1, user1)
		}
		if !strings.HasPrefix(gotUser2, "<current-time>") || !strings.Contains(gotUser2, "</current-time>\n\nsecond question") {
			t.Fatalf("turn 2 request %d: user message 2 = %q, want a <current-time> envelope around \"second question\"", i, gotUser2)
		}
	}

	// The stored conversation carries the stamps (that is what makes later
	// requests stable), but ExportState must strip them — persisted state
	// stays envelope-free for restored-session consumers.
	stored := a.state.GetMessages()
	stampedStored := 0
	for _, m := range stored {
		if m.Role == "user" && strings.HasPrefix(m.Content, "<current-time>") {
			stampedStored++
		}
	}
	if stampedStored := stampedStored; stampedStored != 2 {
		t.Fatalf("stored conversation carries %d stamped user messages, want 2", stampedStored)
	}

	data, err := a.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	if strings.Contains(string(data), "<current-time>") {
		t.Fatal("ExportState output contains a <current-time> envelope; persisted state must be envelope-free")
	}
}
