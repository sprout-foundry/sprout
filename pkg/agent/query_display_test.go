package agent

import (
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestQueryDisplaysSurviveExportImport(t *testing.T) {
	a := newTestAgent(t)
	batch := "[wakeup] Background command completed"
	a.rememberQueryDisplay(batch, "Looking into 'make build'…")
	a.rememberQueryDisplay("plain question", "plain question")
	a.rememberQueryDisplay("[wakeup] compacted away", "Looking into 'old'…")
	a.state.SetMessages([]api.Message{
		{Role: "user", Content: "plain question"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: batch},
		{Role: "assistant", Content: "Done."},
	})

	snapshot, err := a.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	restored := newTestAgent(t)
	if err := restored.ImportState(snapshot); err != nil {
		t.Fatal(err)
	}

	got := restored.queryDisplaysFor(restored.state.GetMessages())
	want := map[string]string{batch: "Looking into 'make build'…"}
	if len(got) != len(want) || got[batch] != want[batch] {
		t.Fatalf("restored displays = %v, want %v", got, want)
	}
}

func TestQueryDisplaysSurviveSessionFile(t *testing.T) {
	_, dir := setupScopedStateTest(t)
	a := newTestAgent(t)
	batch := "[wakeup] Background command completed"
	a.rememberQueryDisplay(batch, "Looking into 'make build'…")
	a.state.SetMessages([]api.Message{
		{Role: "user", Content: InjectUserMessageTimestampAt(batch, time.Now())},
		{Role: "assistant", Content: "Done."},
	})
	if err := a.SaveStateScoped("display-test", dir); err != nil {
		t.Fatal(err)
	}

	state, err := LoadStateWithoutAgentScoped("display-test", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.DisplayMessages()[0].Content; got != "Looking into 'make build'…" {
		t.Fatalf("restored bubble = %q", got)
	}
	displays := state.QueryDisplays
	state.QueryDisplays = nil
	if got := state.DisplayMessages()[0].Content; got != batch {
		t.Fatalf("without a display the bubble = %q, want the message minus its envelope", got)
	}
	state.QueryDisplays = displays
	if state.Messages[0].Content == "Looking into 'make build'…" || state.Messages[0].Content == batch {
		t.Fatal("DisplayMessages rewrote the stored conversation")
	}

	restored := newTestAgent(t)
	restored.ApplyState(state)
	if got := restored.queryDisplaysFor(restored.state.GetMessages()); got[batch] == "" {
		t.Fatalf("applied state lost the display: %v", got)
	}
}

func TestApplyStateDoesNotRebookRestoredUsage(t *testing.T) {
	a := newTestAgent(t)
	a.ApplyState(&ConversationState{PromptTokens: 900, CompletionTokens: 90})
	if got := a.TakeUnbookedUsage(); got != (Usage{}) {
		t.Fatalf("restored session booked again: %+v", got)
	}
}
