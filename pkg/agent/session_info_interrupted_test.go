package agent

import (
	"os"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestSessionInfoMarksInterruptedFromJournal(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "work"})
	if err := a.SaveStateScoped("flagged", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}
	j, err := OpenTurnJournal("flagged", workingDir)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := j.AppendTurnEvent(TurnJournalEvent{Type: "turn_start", Query: "q", Base: 1}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := j.CloseTurnJournal(); err != nil {
		t.Fatalf("close: %v", err)
	}

	sessions, err := ListSessionsWithTimestampsScoped(workingDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.SessionID != "flagged" {
			continue
		}
		found = true
		if !s.Interrupted {
			t.Fatal("session with surviving journal must be flagged interrupted")
		}
	}
	if !found {
		t.Fatal("saved session not listed")
	}

	if err := RemoveTurnJournal("flagged", workingDir); err != nil {
		t.Fatalf("remove journal: %v", err)
	}
	sessions, err = ListSessionsWithTimestampsScoped(workingDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, s := range sessions {
		if s.SessionID == "flagged" && s.Interrupted {
			t.Fatal("session must not be flagged after journal removal")
		}
	}
}

func TestSessionInfoNotInterruptedWithoutJournal(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "clean"})
	if err := a.SaveStateScoped("cleanflag", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}

	sessions, err := ListSessionsWithTimestampsScoped(workingDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, s := range sessions {
		if s.SessionID == "cleanflag" && s.Interrupted {
			t.Fatal("clean session must not be flagged interrupted")
		}
	}
}

func TestListAllSessionsInterruptedFlagSurvivesWalk(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "walk"})
	if err := a.SaveStateScoped("walkflag", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}
	j, err := OpenTurnJournal("walkflag", workingDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = j.AppendTurnEvent(TurnJournalEvent{Type: "turn_start"})
	_ = j.CloseTurnJournal()

	sessions, err := ListAllSessionsWithTimestamps()
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	for _, s := range sessions {
		if s.SessionID == "walkflag" && !s.Interrupted {
			t.Fatal("ListAllSessionsWithTimestamps must preserve the interrupted flag")
		}
	}

	os.Remove(mustJournalPath(t, "walkflag", workingDir))
}
