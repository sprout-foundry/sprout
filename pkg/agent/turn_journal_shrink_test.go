package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"
)

// fakeMsg / newFakeSeedState build minimal seed states for journal tests.
type fakeMsg struct {
	role, content string
}

func makeSeedMsgs(n int, prefix string) []fakeMsg {
	out := make([]fakeMsg, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fakeMsg{role: "user", content: prefix})
	}
	return out
}

func newFakeSeedState(msgs []fakeMsg) *core.State {
	st := core.NewState()
	for _, m := range msgs {
		st.AddMessage(core.Message{Role: m.role, Content: m.content})
	}
	return st
}

// TestTurnJournalCompactionShrinkEvent pins the mid-turn compaction journal
// shape: when state shrinks below the journal's high-water mark (a seed
// compaction persist), the journal must record a replace-style event
// carrying the full compacted list — not an append event whose Base exceeds
// the live list, and not silence (which leaves the journal permanently
// ahead of state). Replay must replace, not append.
func TestTurnJournalCompactionShrinkEvent(t *testing.T) {
	dir := t.TempDir()

	a := &Agent{workspaceRoot: dir}
	a.initSubManagers()
	a.state.SetSessionID("journal-shrink-test")

	a.beginTurnJournal("go")
	defer a.endTurnJournal()

	// Fake seed state through growth then shrink.
	grown := makeSeedMsgs(10, "grown")
	st := newFakeSeedState(grown)
	a.journalSeedState(st)

	shrunk := makeSeedMsgs(4, "compacted")
	a.journalSeedState(newFakeSeedState(shrunk))

	// Grow again post-compaction — the append must resume from the new mark.
	resumed := append(shrunk, fakeMsg{role: "assistant", content: "final"})
	a.journalSeedState(newFakeSeedState(resumed))

	// Read the journal and validate the event sequence.
	sid := a.state.GetSessionID()
	p, err := turnJournalPathFor(sid, dir)
	if err != nil {
		t.Fatalf("journal path: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}

	var types []string
	var shrinkSeen bool
	var shrinkMsgs int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev TurnJournalEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("journal line not valid JSON: %v", err)
		}
		types = append(types, ev.Type)
		if ev.Type == "compaction" {
			shrinkSeen = true
			shrinkMsgs = len(ev.CompactionShrink)
		}
	}

	if !shrinkSeen {
		t.Fatalf("no compaction event recorded; events were %v", types)
	}
	if shrinkMsgs != 4 {
		t.Fatalf("compaction event carried %d messages, want the 4 compacted ones", shrinkMsgs)
	}
}
