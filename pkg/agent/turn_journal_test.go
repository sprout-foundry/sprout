package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func readJournalLines(t *testing.T, path string) []TurnJournalEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer f.Close()
	var events []TurnJournalEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev TurnJournalEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("journal line not valid JSON: %q: %v", line, err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan journal: %v", err)
	}
	return events
}

func TestTurnJournal_AppendWritesOrderedNewlineTerminatedLines(t *testing.T) {
	stateDir, workingDir := setupScopedStateTest(t)
	sessionID := "jorder"

	j, err := OpenTurnJournal(sessionID, workingDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := j.AppendTurnEvent(TurnJournalEvent{Type: "turn_start", Query: "hello", Base: 2}); err != nil {
		t.Fatalf("append turn_start: %v", err)
	}
	if err := j.AppendTurnEvent(TurnJournalEvent{
		Type: "messages",
		Base: 2,
		Msgs: []api.Message{{Role: "assistant", Content: "hi"}},
	}); err != nil {
		t.Fatalf("append messages: %v", err)
	}
	if err := j.AppendTurnEvent(TurnJournalEvent{
		Type:        "token_totals",
		TokenTotals: &TurnJournalTokens{TotalTokens: 10, PromptTokens: 4, CompletionTokens: 6, TotalCost: 0.01},
	}); err != nil {
		t.Fatalf("append tokens: %v", err)
	}
	if err := j.CloseTurnJournal(); err != nil {
		t.Fatalf("close: %v", err)
	}

	stateFile, err := buildScopedSessionFilePath(stateDir, sessionID, workingDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(turnJournalPath(stateFile))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("journal must end with newline so replay can detect a truncated tail")
	}

	events := readJournalLines(t, turnJournalPath(stateFile))
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != "turn_start" || events[0].Query != "hello" || events[0].Base != 2 || events[0].V != turnJournalVersion {
		t.Fatalf("turn_start event mismatch: %+v", events[0])
	}
	if events[1].Type != "messages" || len(events[1].Msgs) != 1 || events[1].Msgs[0].Content != "hi" {
		t.Fatalf("messages event mismatch: %+v", events[1])
	}
	if events[2].Type != "token_totals" || events[2].TokenTotals == nil || events[2].TokenTotals.TotalTokens != 10 {
		t.Fatalf("token_totals event mismatch: %+v", events[2])
	}
	for _, ev := range events {
		if ev.Ts.IsZero() {
			t.Fatalf("event %s missing timestamp", ev.Type)
		}
	}
}

func TestTurnJournal_AppendAfterCloseReturnsError(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)
	j, err := OpenTurnJournal("jclosed", workingDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := j.CloseTurnJournal(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := j.AppendTurnEvent(TurnJournalEvent{Type: "turn_start"}); err == nil {
		t.Fatal("expected error appending to closed journal")
	}
}

func TestTurnJournal_RemoveDeletesFile(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)
	j, err := OpenTurnJournal("jremove", workingDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = j.AppendTurnEvent(TurnJournalEvent{Type: "turn_start"})
	_ = j.CloseTurnJournal()

	if !turnJournalExists("jremove", workingDir) {
		t.Fatal("journal should exist before removal")
	}
	if err := RemoveTurnJournal("jremove", workingDir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if turnJournalExists("jremove", workingDir) {
		t.Fatal("journal should be gone after removal")
	}
	if err := RemoveTurnJournal("jremove", workingDir); err != nil {
		t.Fatalf("removing a missing journal must be a no-op: %v", err)
	}
}

func TestTurnJournal_ConcurrentAppendsAreLineSafe(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)
	j, err := OpenTurnJournal("jrace", workingDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer j.CloseTurnJournal()

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				_ = j.AppendTurnEvent(TurnJournalEvent{
					Type: "messages",
					Base: 0,
					Msgs: []api.Message{{Role: "user", Content: strings.Repeat("x", 64)}},
				})
			}
		}(g)
	}
	wg.Wait()

	stateDirResolved, err := GetStateDir()
	if err != nil {
		t.Fatal(err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDirResolved, "jrace", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	events := readJournalLines(t, turnJournalPath(stateFile))
	if len(events) != 100 {
		t.Fatalf("expected 100 intact events, got %d", len(events))
	}
}

func TestDeleteSessionScoped_RemovesJournalToo(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "journaled"})
	if err := a.SaveStateScoped("jdelete", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}
	j, err := OpenTurnJournal("jdelete", workingDir)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	_ = j.AppendTurnEvent(TurnJournalEvent{Type: "turn_start"})
	_ = j.CloseTurnJournal()

	if err := DeleteSessionScoped("jdelete", workingDir); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stateDirResolved, err := GetStateDir()
	if err != nil {
		t.Fatal(err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDirResolved, "jdelete", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{stateFile, stateFile + ".bak", turnJournalPath(stateFile)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("file still present after delete: %s", p)
		}
	}
}

func TestBeginTurnJournal_SnapshotsBaseAndSkipsSubagents(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(
		api.Message{Role: "user", Content: "prior turn"},
		api.Message{Role: "assistant", Content: "prior answer"},
	)
	a.SetSessionID("jbase")
	a.SetWorkspaceRoot(workingDir)

	a.beginTurnJournal("new query")
	if a.turnJournal == nil {
		t.Fatal("journal should be open")
	}
	if a.journalBase != 2 {
		t.Fatalf("journalBase = %d, want 2", a.journalBase)
	}
	a.journalMessagesSnapshot()
	a.endTurnJournal()

	events := readJournalLines(t, mustJournalPath(t, "jbase", workingDir))
	if len(events) != 3 || events[0].Type != "turn_start" || events[0].Base != 2 {
		t.Fatalf("expected turn_start+messages+token_totals with Base=2, got %+v", events)
	}

	a.state.SetMessages(append(a.state.GetMessages(),
		api.Message{Role: "user", Content: "new query"},
		api.Message{Role: "assistant", Content: "mid-turn"},
	))
	a.journalMessagesSnapshot()
	events = readJournalLines(t, mustJournalPath(t, "jbase", workingDir))
	if len(events) != 3 {
		t.Fatalf("snapshot with closed journal must not append, got %d events", len(events))
	}
}

func TestJournalMessagesSnapshot_RecordsNewMessagesOnly(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "base"})
	a.SetSessionID("jsnap")
	a.SetWorkspaceRoot(workingDir)

	a.beginTurnJournal("query")
	a.state.SetMessages(append(a.state.GetMessages(),
		api.Message{Role: "assistant", Content: "iteration one"},
		api.Message{Role: "user", Content: "steer"},
	))
	a.journalMessagesSnapshot()
	a.journalMessagesSnapshot()
	a.endTurnJournal()

	events := readJournalLines(t, mustJournalPath(t, "jsnap", workingDir))
	var msgEvents int
	for _, ev := range events {
		if ev.Type != "messages" {
			continue
		}
		msgEvents++
		if ev.Base != 1 {
			t.Fatalf("messages Base = %d, want 1", ev.Base)
		}
		for _, m := range ev.Msgs {
			if m.Content == "base" {
				t.Fatal("snapshot must not re-record pre-turn messages")
			}
		}
	}
	if msgEvents != 2 {
		t.Fatalf("expected 2 messages events (one per snapshot call), got %d", msgEvents)
	}
}

func mustJournalPath(t *testing.T, sessionID, workingDir string) string {
	t.Helper()
	stateDir, err := GetStateDir()
	if err != nil {
		t.Fatal(err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDir, sessionID, workingDir)
	if err != nil {
		t.Fatal(err)
	}
	return turnJournalPath(stateFile)
}

func TestEndTurnJournalKeepsFileForFinalizeToRemove(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "crash sim"})
	a.SetSessionID("jcrash")
	a.SetWorkspaceRoot(workingDir)

	a.beginTurnJournal("work")
	a.journalMessagesSnapshot()
	a.endTurnJournal()

	if !turnJournalExists("jcrash", workingDir) {
		t.Fatal("journal file must survive handle close — its presence is the interrupted-session signal")
	}

	a.finalizeTurnJournal()
	if turnJournalExists("jcrash", workingDir) {
		t.Fatal("finalizeTurnJournal must remove the file")
	}
}

func TestTurnJournalEventsInTmpDirAreHermetic(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)
	j, err := OpenTurnJournal("jhermetic", workingDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = j.AppendTurnEvent(TurnJournalEvent{Type: "turn_start"})
	_ = j.CloseTurnJournal()
	path := mustJournalPath(t, "jhermetic", workingDir)
	if !strings.Contains(path, "scoped") {
		t.Fatalf("journal path should live under the scoped sessions dir: %s", path)
	}
	if filepath.Dir(filepath.Dir(filepath.Dir(path))) == string(filepath.Separator) {
		t.Fatal("journal should not be written to filesystem root")
	}
}
