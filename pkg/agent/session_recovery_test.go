package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func toolCallMsg(id string) api.Message {
	return api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   id,
			Type: "function",
			Function: api.ToolCallFunction{
				Name:      "read_file",
				Arguments: "{\"path\":\"x\"}",
			},
		}},
	}
}

func toolResultMsg(id string) api.Message {
	return api.Message{Role: "tool", ToolCallID: id, Content: "result"}
}

func TestRepairMessageTail_CleanHistoryUntouched(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	repaired, report := RepairMessageTail(msgs)
	if len(repaired) != 2 {
		t.Fatalf("clean history modified: %+v", repaired)
	}
	if report.DroppedToolResults != 0 || report.StrippedAssistantToolCalls != 0 {
		t.Fatalf("clean history produced repair report: %+v", report)
	}
}

func TestRepairMessageTail_DropsOrphanedToolResult(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "q"},
		toolResultMsg("call_missing"),
		{Role: "assistant", Content: "done"},
	}
	repaired, report := RepairMessageTail(msgs)
	if report.DroppedToolResults != 1 {
		t.Fatalf("expected 1 dropped, got %+v", report)
	}
	if len(repaired) != 2 || repaired[1].Role == "tool" {
		t.Fatalf("orphan not dropped: %+v", repaired)
	}
}

func TestRepairMessageTail_StripsTrailingUnansweredToolCall(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "q"},
		toolCallMsg("call_unanswered"),
	}
	repaired, report := RepairMessageTail(msgs)
	if report.StrippedAssistantToolCalls != 1 {
		t.Fatalf("expected 1 stripped, got %+v", report)
	}
	if len(repaired) != 1 {
		t.Fatalf("trailing tool-call-only assistant should be removed: %+v", repaired)
	}
}

func TestRepairMessageTail_StripsToolCallsKeepsAssistantText(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "q"},
		{
			Role:    "assistant",
			Content: "Let me check that file.",
			ToolCalls: []api.ToolCall{
				{ID: "answered", Type: "function", Function: api.ToolCallFunction{Name: "read_file"}},
				{ID: "unanswered", Type: "function", Function: api.ToolCallFunction{Name: "edit_file"}},
			},
		},
		toolResultMsg("answered"),
	}
	repaired, report := RepairMessageTail(msgs)
	if report.StrippedAssistantToolCalls != 1 {
		t.Fatalf("expected 1 stripped, got %+v", report)
	}
	if len(repaired) != 3 {
		t.Fatalf("answered exchange must be kept: %+v", repaired)
	}
	assistant := repaired[1]
	if assistant.Role != "assistant" || assistant.Content != "Let me check that file." {
		t.Fatalf("assistant text must be kept: %+v", assistant)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "answered" {
		t.Fatalf("answered tool call must be kept: %+v", assistant.ToolCalls)
	}
}

func TestRepairMessageTail_EmptyInput(t *testing.T) {
	repaired, report := RepairMessageTail(nil)
	if len(repaired) != 0 || report.DroppedToolResults != 0 {
		t.Fatalf("empty input mishandled: %+v %+v", repaired, report)
	}
}

func TestRepairMessageTail_InterruptedToolExchange(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "q"},
		toolCallMsg("done"),
		toolResultMsg("done"),
		toolCallMsg("inflight"),
	}
	repaired, report := RepairMessageTail(msgs)
	if report.StrippedAssistantToolCalls != 1 {
		t.Fatalf("expected 1 stripped, got %+v", report)
	}
	if len(repaired) != 3 {
		t.Fatalf("interrupted tail should be trimmed to completed exchange: %+v", repaired)
	}
	if repaired[len(repaired)-1].Role != "tool" {
		t.Fatalf("last message should be the completed tool result: %+v", repaired)
	}
}

func writeJournal(t *testing.T, sessionID, workingDir string, events []TurnJournalEvent) {
	t.Helper()
	j, err := OpenTurnJournal(sessionID, workingDir)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	for _, ev := range events {
		if err := j.AppendTurnEvent(ev); err != nil {
			t.Fatalf("append %s: %v", ev.Type, err)
		}
	}
	if err := j.CloseTurnJournal(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestLoadStateRecoverable_NoJournalReturnsBaseState(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)
	a := newScopedStateAgent(api.Message{Role: "user", Content: "base"})
	if err := a.SaveStateScoped("norecover", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}

	state, report, err := LoadStateRecoverable("norecover", workingDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if report.JournalReplayed {
		t.Fatal("no journal must mean no replay")
	}
	if state.RecoveredFromJournal {
		t.Fatal("base state must not be marked recovered")
	}
	if len(state.Messages) != 1 || state.Messages[0].Content != "base" {
		t.Fatalf("base messages wrong: %+v", state.Messages)
	}
}

func TestLoadStateRecoverable_ReplaysJournalTail(t *testing.T) {
	stateDir, workingDir := setupScopedStateTest(t)

	base := newScopedStateAgent(
		api.Message{Role: "user", Content: "turn one"},
		api.Message{Role: "assistant", Content: "answer one"},
	)
	if err := base.SaveStateScoped("replayme", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}

	writeJournal(t, "replayme", workingDir, []TurnJournalEvent{
		{Type: "turn_start", Query: "turn two", Base: 2},
		{
			Type: "messages",
			Base: 2,
			Msgs: []api.Message{
				{Role: "user", Content: "turn two"},
				{Role: "assistant", Content: "partial work"},
			},
		},
		{
			Type:        "token_totals",
			TokenTotals: &TurnJournalTokens{TotalTokens: 42, PromptTokens: 20, CompletionTokens: 22, TotalCost: 0.5},
		},
	})

	state, report, err := LoadStateRecoverable("replayme", workingDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !report.JournalReplayed {
		t.Fatal("journal should have replayed")
	}
	if report.JournalEvents != 3 {
		t.Fatalf("applied events = %d, want 3", report.JournalEvents)
	}
	if len(state.Messages) != 4 {
		t.Fatalf("messages = %d, want 4 (2 base + 2 journal): %+v", len(state.Messages), state.Messages)
	}
	if state.Messages[3].Content != "partial work" {
		t.Fatalf("journal tail missing: %+v", state.Messages)
	}
	if state.TotalTokens != 42 || state.TotalCost != 0.5 {
		t.Fatalf("token totals not applied: %d/%f", state.TotalTokens, state.TotalCost)
	}
	if !state.RecoveredFromJournal || state.InterruptedAt == nil {
		t.Fatal("recovered markers not set")
	}
	if report.InterruptedAt == nil || report.InterruptedAt.IsZero() {
		t.Fatal("interruption timestamp missing")
	}

	stateFile, err := buildScopedSessionFilePath(stateDir, "replayme", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(turnJournalPath(stateFile)); err != nil {
		t.Fatalf("load must not delete the journal: %v", err)
	}
}

func TestLoadStateRecoverable_ToleratesTruncatedTail(t *testing.T) {
	stateDir, workingDir := setupScopedStateTest(t)

	base := newScopedStateAgent(api.Message{Role: "user", Content: "base"})
	if err := base.SaveStateScoped("tornwrite", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeJournal(t, "tornwrite", workingDir, []TurnJournalEvent{
		{Type: "turn_start", Query: "q", Base: 1},
		{
			Type: "messages",
			Base: 1,
			Msgs: []api.Message{{Role: "user", Content: "q"}, {Role: "assistant", Content: "ok"}},
		},
	})

	stateFile, err := buildScopedSessionFilePath(stateDir, "tornwrite", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	jpath := turnJournalPath(stateFile)
	f, err := os.OpenFile(jpath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"v":1,"type":"messages","base":1,"msgs":[{"role":"ass`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	state, report, err := LoadStateRecoverable("tornwrite", workingDir)
	if err != nil {
		t.Fatalf("load with torn tail: %v", err)
	}
	if !report.JournalReplayed {
		t.Fatal("valid prefix should still replay")
	}
	if len(state.Messages) != 3 {
		t.Fatalf("messages = %d, want 3: %+v", len(state.Messages), state.Messages)
	}
}

func TestLoadStateRecoverable_RepairInterruptedToolExchange(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	base := newScopedStateAgent(api.Message{Role: "user", Content: "base"})
	if err := base.SaveStateScoped("midtool", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeJournal(t, "midtool", workingDir, []TurnJournalEvent{
		{Type: "turn_start", Query: "go", Base: 1},
		{
			Type: "messages",
			Base: 1,
			Msgs: []api.Message{
				{Role: "user", Content: "go"},
				toolCallMsg("ran"),
				toolResultMsg("ran"),
				toolCallMsg("inflight"),
			},
		},
	})

	state, report, err := LoadStateRecoverable("midtool", workingDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if report.Repair.StrippedAssistantToolCalls != 1 {
		t.Fatalf("repair report = %+v", report.Repair)
	}
	if len(state.Messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(state.Messages), state.Messages)
	}
	if state.Messages[len(state.Messages)-1].Role != "tool" {
		t.Fatalf("tail should end at completed tool result: %+v", state.Messages)
	}
}

func TestApplyRecoveredState_PrimesSupplementOnce(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent()
	a.initSubManagers()
	a.SetWorkspaceRoot(workingDir)

	recovered := &ConversationState{
		Messages:             []api.Message{{Role: "user", Content: "recovered"}},
		RecoveredFromJournal: true,
	}
	ts := time.Now()
	recovered.InterruptedAt = &ts

	report := a.ApplyRecoveredState(recovered)
	if !report.JournalReplayed {
		t.Fatal("report should carry recovered flag")
	}
	if a.state.GetPendingSystemSupplement() == "" ||
		!strings.Contains(a.state.GetPendingSystemSupplement(), "Recovered Session") {
		t.Fatalf("supplement not primed: %q", a.state.GetPendingSystemSupplement())
	}
	if len(a.state.GetMessages()) != 1 || a.state.GetMessages()[0].Content != "recovered" {
		t.Fatalf("messages not applied: %+v", a.state.GetMessages())
	}

	existing := "## Context From Previous Session\n\nkeep me"
	a.state.SetPendingSystemSupplement(existing)
	a.ApplyRecoveredState(recovered)
	if a.state.GetPendingSystemSupplement() != existing {
		t.Fatal("ApplyRecoveredState must not clobber an existing supplement")
	}
}

func TestConversationStateRecoveredFieldsOmitEmpty(t *testing.T) {
	data, err := json.Marshal(ConversationState{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "interrupted_at") || strings.Contains(string(data), "recovered_from_journal") {
		t.Fatalf("zero-value state must not serialize recovery fields: %s", data)
	}
}
