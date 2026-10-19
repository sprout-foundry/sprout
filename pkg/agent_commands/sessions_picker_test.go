package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// writePickerSession stores a session in a temp state dir scoped to a temp
// working dir and returns both paths.
func writePickerSession(t *testing.T, sessionID string, state *agent.ConversationState) (stateDir, workingDir string) {
	t.Helper()
	stateDir = t.TempDir()
	workingDir = t.TempDir()
	restore := agent.SetStateDirFuncForTesting(func() (string, error) { return stateDir, nil })
	t.Cleanup(restore)
	state.WorkingDirectory = workingDir
	if err := agent.WriteTestSessionFile(stateDir, sessionID, workingDir, state); err != nil {
		t.Fatal(err)
	}
	return stateDir, workingDir
}

func TestBuildSessionPickerItemsSummaryAsDetail(t *testing.T) {
	_, workingDir := writePickerSession(t, "pick-1", &agent.ConversationState{
		Name:         "resize fix",
		TopicSummary: "Fixed the chat popover resize handle",
	})

	sessions := []agent.SessionInfo{{
		SessionID:        "pick-1",
		Name:             "resize fix",
		TopicSummary:     "Fixed the chat popover resize handle",
		WorkingDirectory: workingDir,
		LastUpdated:      time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC),
	}}

	items := buildSessionPickerItems(sessions)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Label != "resize fix" {
		t.Errorf("Label = %q, want the session name", items[0].Label)
	}
	if !strings.Contains(items[0].Detail, "Fixed the chat popover resize handle") {
		t.Errorf("Detail = %q, want the topic summary as the detail line", items[0].Detail)
	}
	if !strings.Contains(items[0].Detail, "2026-10-19 09:30") {
		t.Errorf("Detail = %q, want the timestamp alongside the summary", items[0].Detail)
	}
	if items[0].Value != "pick-1" {
		t.Errorf("Value = %q, want the session ID", items[0].Value)
	}
}

func TestBuildSessionPickerItemsNewestFirst(t *testing.T) {
	_, workingDir := writePickerSession(t, "older", &agent.ConversationState{Name: "older", TopicSummary: "old work"})
	_, _ = writePickerSession(t, "newer", &agent.ConversationState{Name: "newer", TopicSummary: "new work"})

	sessions := []agent.SessionInfo{
		{SessionID: "older", Name: "older", WorkingDirectory: workingDir, LastUpdated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{SessionID: "newer", Name: "newer", WorkingDirectory: workingDir, LastUpdated: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	}

	items := buildSessionPickerItems(sessions)
	if items[0].Value != "newer" || items[1].Value != "older" {
		t.Errorf("picker order = [%s, %s], want newest first", items[0].Value, items[1].Value)
	}
}

func TestBuildSessionPickerItemsFallsBackToPreviewWhenNoSummary(t *testing.T) {
	long := strings.Repeat("d", 120)
	stateDir, workingDir := writePickerSession(t, "legacy-pick", &agent.ConversationState{
		Messages: []api.Message{{Role: "user", Content: long}},
	})
	_ = stateDir

	items := buildSessionPickerItems([]agent.SessionInfo{{
		SessionID:        "legacy-pick",
		WorkingDirectory: workingDir,
		LastUpdated:      time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC),
	}})

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if len(items[0].Label) != 120 {
		t.Errorf("Label = %d chars, want the uncapped 120-rune preview", len(items[0].Label))
	}
	if items[0].Detail == "" {
		t.Error("Detail should still carry the timestamp when no summary resolves")
	}
}
