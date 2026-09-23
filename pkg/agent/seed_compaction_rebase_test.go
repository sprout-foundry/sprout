package agent

import (
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestSeedCompactionPersistRebasesSproutCheckpoints pins the cross-repo
// half of compaction persistence: when seed persists a mid-turn compaction
// (shrinking the message list), sprout's richer TurnCheckpoints — recorded
// against the pre-compaction layout and carrying ID/FileChanges/RevisionID
// metadata — must shift with it. Stale indices made checkpoint substitution
// replace the wrong message ranges (the corruption class fixed for
// session-name shifts in 95feea807).
func TestSeedCompactionPersistRebasesSproutCheckpoints(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	// Scripted client: 128K window (factory default), two responses — a
	// final text answer keeps the turn single-request so the timing of the
	// compaction persist is deterministic (before request 1).
	client := NewScriptedClient(
		NewScriptedTextResponse("done"),
	)

	a, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	// Force a tiny compaction trigger for this test: the profile override
	// (contextProfile.CompactionTriggerFraction) wins over the reserved-
	// fraction default, so a modest history crosses it without a 90K-token
	// fixture.
	a.contextProfile = configuration.ContextProfile{
		CompactionTriggerFraction: 0.05,
		SkipProactiveContext:      true,
	}
	a.initSubManagers()

	// History over the forced trigger: 100 messages × 2000 chars ≈ 50K
	// estimated tokens vs a 6.4K trigger.
	filler := strings.Repeat("a", 2000)
	history := make([]api.Message, 0, 128)
	for i := 0; i < 50; i++ {
		history = append(history,
			api.Message{Role: "user", Content: filler},
			api.Message{Role: "assistant", Content: filler},
		)
	}
	a.SetMessages(history)

	// Two checkpoints against the raw layout: one squarely in the prunable
	// middle, one near the tail.
	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	a.state.SetTurnCheckpoints([]TurnCheckpoint{
		{
			ID:         "cp-early",
			StartIndex: 3,
			EndIndex:   8,
			Summary:    "early turn summary",
		},
		{
			ID:         "cp-recent",
			StartIndex: 91,
			EndIndex:   96,
			Summary:    "recent turn summary",
		},
	})
	mu.Unlock()

	before := len(a.state.GetMessages())

	if _, err := a.processQueryWithSeed(QuerySourceUnknown, "go"); err != nil {
		t.Fatalf("processQueryWithSeed: %v", err)
	}

	after := a.state.GetMessages()
	if len(after) >= before+2 {
		t.Fatalf("expected seed's compaction to shrink state: before=%d after=%d", before, len(after))
	}

	// The contract: after a compaction persist, every surviving checkpoint
	// has valid indices against the NEW layout. A checkpoint whose range
	// was pruned is dropped (never left stale); a surviving one is shifted.
	// Exercise both: an early checkpoint squarely inside the prunable
	// region and a checkpoint near the tail of the ORIGINAL layout (the
	// turn's own user+assistant messages are appended after compaction, so
	// old-tail checkpoints may or may not survive — the invariant holds
	// either way).
	cps := a.state.GetTurnCheckpoints()
	if len(cps) > 2 {
		t.Fatalf("checkpoint set grew: %d", len(cps))
	}
	for _, cp := range cps {
		if cp.StartIndex < 0 || cp.EndIndex >= len(after) || cp.EndIndex < cp.StartIndex {
			t.Fatalf("checkpoint %s indices invalid against the compacted layout: [%d..%d] of %d messages",
				cp.ID, cp.StartIndex, cp.EndIndex, len(after))
		}
	}
	// At least one checkpoint must survive with its metadata — the rebase
	// preserves the rich struct, it never rebuilds it.
	survived := map[string]bool{}
	for _, cp := range cps {
		survived[cp.ID] = true
	}
	if len(cps) == 0 {
		t.Fatal("all checkpoints dropped — the recent-window one must survive")
	}
	if !survived["cp-recent"] && !survived["cp-early"] {
		t.Fatal("no recognizable checkpoint survived the rebase")
	}
}

// TestRebaseTurnCheckpoints unit-pins the map semantics directly.
func TestRebaseTurnCheckpoints(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	a, err := NewAgentWithClient(NewScriptedClient(), api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	a.initSubManagers()

	msgs := make([]api.Message, 12)
	for i := range msgs {
		msgs[i] = api.Message{Role: "user", Content: strings.Repeat("m", i+1)}
	}
	a.SetMessages(msgs)

	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	a.state.SetTurnCheckpoints([]TurnCheckpoint{
		{ID: "a", StartIndex: 1, EndIndex: 2},
		{ID: "b", StartIndex: 5, EndIndex: 7, FileChanges: []CheckpointFileChange{{Path: "x.go", Op: "edit"}}, RevisionID: "r1"},
		{ID: "c", StartIndex: 9, EndIndex: 11},
	})
	mu.Unlock()

	// Compaction drops old indices 0-4 and 8; survivors shift down.
	survivorOf := map[int]int{5: 0, 6: 1, 7: 2, 9: 3, 10: 4, 11: 5}
	a.rebaseTurnCheckpoints(survivorOf)

	cps := a.state.GetTurnCheckpoints()
	if len(cps) != 2 {
		t.Fatalf("want 2 surviving checkpoints (a and c lose endpoints), got %d", len(cps))
	}
	byID := map[string]TurnCheckpoint{}
	for _, cp := range cps {
		byID[cp.ID] = cp
	}
	b, ok := byID["b"]
	if !ok {
		t.Fatal("checkpoint b must survive")
	}
	if b.StartIndex != 0 || b.EndIndex != 2 {
		t.Fatalf("checkpoint b mis-rebased: [%d..%d], want [0..2]", b.StartIndex, b.EndIndex)
	}
	if len(b.FileChanges) != 1 || b.FileChanges[0].Path != "x.go" || b.RevisionID != "r1" {
		t.Fatalf("checkpoint b metadata lost: %+v", b)
	}
	c, ok := byID["c"]
	if !ok {
		t.Fatal("checkpoint c must survive")
	}
	if c.StartIndex != 3 || c.EndIndex != 5 {
		t.Fatalf("checkpoint c mis-rebased: [%d..%d], want [3..5]", c.StartIndex, c.EndIndex)
	}
	if _, ok := byID["a"]; ok {
		t.Fatal("checkpoint a (endpoint dropped) must be removed")
	}
}

// Compile-time guard that the seed API this file exercises exists in the
// pinned version (via the local replace during development).
var _ = core.RebaseCheckpoints
