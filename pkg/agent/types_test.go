package agent

import (
	"encoding/json"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestAgentStateJSONRoundTrip(t *testing.T) {
	original := AgentState{
		Messages: []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
		TurnCheckpoints: []TurnCheckpoint{
			{StartIndex: 0, EndIndex: 1, Summary: "greeting"},
		},
		PreviousSummary:         "initial prompt",
		CompactSummary:          "short summary",
		TaskActions:             []TaskAction{{Type: "file_created", Description: "created test.go"}},
		SessionID:               "session-123",
		TotalTokens:             1500,
		TotalCost:               0.05,
		PromptTokens:            1000,
		CompletionTokens:        500,
		EstimatedTokenResponses: 200,
		CachedTokens:            300,
		CachedCostSavings:       0.01,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded AgentState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID = %q; want %q", decoded.SessionID, original.SessionID)
	}
	if decoded.TotalTokens != original.TotalTokens {
		t.Errorf("TotalTokens = %d; want %d", decoded.TotalTokens, original.TotalTokens)
	}
	if decoded.TotalCost != original.TotalCost {
		t.Errorf("TotalCost = %f; want %f", decoded.TotalCost, original.TotalCost)
	}
	if decoded.PromptTokens != original.PromptTokens {
		t.Errorf("PromptTokens = %d; want %d", decoded.PromptTokens, original.PromptTokens)
	}
	if decoded.CompletionTokens != original.CompletionTokens {
		t.Errorf("CompletionTokens = %d; want %d", decoded.CompletionTokens, original.CompletionTokens)
	}
	if decoded.EstimatedTokenResponses != original.EstimatedTokenResponses {
		t.Errorf("EstimatedTokenResponses = %d; want %d", decoded.EstimatedTokenResponses, original.EstimatedTokenResponses)
	}
	if decoded.CachedTokens != original.CachedTokens {
		t.Errorf("CachedTokens = %d; want %d", decoded.CachedTokens, original.CachedTokens)
	}
	if decoded.CachedCostSavings != original.CachedCostSavings {
		t.Errorf("CachedCostSavings = %f; want %f", decoded.CachedCostSavings, original.CachedCostSavings)
	}
	if len(decoded.Messages) != len(original.Messages) {
		t.Errorf("len(Messages) = %d; want %d", len(decoded.Messages), len(original.Messages))
	}
	if decoded.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q; want %q", decoded.Messages[0].Role, "user")
	}
	if len(decoded.TurnCheckpoints) != len(original.TurnCheckpoints) {
		t.Errorf("len(TurnCheckpoints) = %d; want %d", len(decoded.TurnCheckpoints), len(original.TurnCheckpoints))
	}
	if decoded.PreviousSummary != original.PreviousSummary {
		t.Errorf("PreviousSummary = %q; want %q", decoded.PreviousSummary, original.PreviousSummary)
	}
	if decoded.CompactSummary != original.CompactSummary {
		t.Errorf("CompactSummary = %q; want %q", decoded.CompactSummary, original.CompactSummary)
	}
	if len(decoded.TaskActions) != len(original.TaskActions) {
		t.Errorf("len(TaskActions) = %d; want %d", len(decoded.TaskActions), len(original.TaskActions))
	}
}

func TestTurnCheckpointJSONRoundTrip(t *testing.T) {
	original := TurnCheckpoint{
		StartIndex:        0,
		EndIndex:          5,
		Summary:           "user asked about files",
		ActionableSummary: "list files in directory",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded TurnCheckpoint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.StartIndex != original.StartIndex {
		t.Errorf("StartIndex = %d; want %d", decoded.StartIndex, original.StartIndex)
	}
	if decoded.EndIndex != original.EndIndex {
		t.Errorf("EndIndex = %d; want %d", decoded.EndIndex, original.EndIndex)
	}
	if decoded.Summary != original.Summary {
		t.Errorf("Summary = %q; want %q", decoded.Summary, original.Summary)
	}
	if decoded.ActionableSummary != original.ActionableSummary {
		t.Errorf("ActionableSummary = %q; want %q", decoded.ActionableSummary, original.ActionableSummary)
	}
}

func TestCircuitBreakerStateActions(t *testing.T) {
	cb := &CircuitBreakerState{
		mu:      sync.RWMutex{},
		Actions: make(map[string]*CircuitBreakerAction),
	}

	// Write an action
	cb.mu.Lock()
	cb.Actions["edit_file:test.go"] = &CircuitBreakerAction{
		ActionType: "edit_file",
		Target:     "test.go",
		Count:      3,
		LastUsed:   1000,
	}
	cb.mu.Unlock()

	// Read the action back
	cb.mu.RLock()
	action, ok := cb.Actions["edit_file:test.go"]
	cb.mu.RUnlock()

	if !ok {
		t.Fatal("action not found in map")
	}
	if action.ActionType != "edit_file" {
		t.Errorf("ActionType = %q; want %q", action.ActionType, "edit_file")
	}
	if action.Target != "test.go" {
		t.Errorf("Target = %q; want %q", action.Target, "test.go")
	}
	if action.Count != 3 {
		t.Errorf("Count = %d; want 3", action.Count)
	}
	if action.LastUsed != 1000 {
		t.Errorf("LastUsed = %d; want 1000", action.LastUsed)
	}
}

func TestCircuitBreakerActionFields(t *testing.T) {
	act := CircuitBreakerAction{
		ActionType: "shell_command",
		Target:     "ls -la",
		Count:      5,
		LastUsed:   2000,
	}

	if act.ActionType != "shell_command" {
		t.Errorf("ActionType = %q; want %q", act.ActionType, "shell_command")
	}
	if act.Target != "ls -la" {
		t.Errorf("Target = %q; want %q", act.Target, "ls -la")
	}
	if act.Count != 5 {
		t.Errorf("Count = %d; want 5", act.Count)
	}
	if act.LastUsed != 2000 {
		t.Errorf("LastUsed = %d; want 2000", act.LastUsed)
	}
}

func TestDiffChangeFields(t *testing.T) {
	dc := DiffChange{
		OldStart:  10,
		OldLength: 5,
		NewStart:  20,
		NewLength: 8,
	}

	if dc.OldStart != 10 {
		t.Errorf("OldStart = %d; want 10", dc.OldStart)
	}
	if dc.OldLength != 5 {
		t.Errorf("OldLength = %d; want 5", dc.OldLength)
	}
	if dc.NewStart != 20 {
		t.Errorf("NewStart = %d; want 20", dc.NewStart)
	}
	if dc.NewLength != 8 {
		t.Errorf("NewLength = %d; want 8", dc.NewLength)
	}
}

func TestShellCommandResultFields(t *testing.T) {
	scr := ShellCommandResult{
		Command:         "ls",
		FullOutput:      "file1\nfile2\n",
		TruncatedOutput: "file1...",
		Error:           nil,
		ExecutedAt:      1000,
		MessageIndex:    5,
		WasTruncated:    true,
		FullOutputPath:  "/tmp/out.txt",
		TruncatedTokens: 50,
		TruncatedLines:  10,
	}

	if scr.Command != "ls" {
		t.Errorf("Command = %q; want %q", scr.Command, "ls")
	}
	if scr.FullOutput != "file1\nfile2\n" {
		t.Errorf("FullOutput = %q; want %q", scr.FullOutput, "file1\nfile2\n")
	}
	if scr.TruncatedOutput != "file1..." {
		t.Errorf("TruncatedOutput = %q; want %q", scr.TruncatedOutput, "file1...")
	}
	if scr.ExecutedAt != 1000 {
		t.Errorf("ExecutedAt = %d; want 1000", scr.ExecutedAt)
	}
	if scr.MessageIndex != 5 {
		t.Errorf("MessageIndex = %d; want 5", scr.MessageIndex)
	}
	if !scr.WasTruncated {
		t.Error("WasTruncated should be true")
	}
	if scr.FullOutputPath != "/tmp/out.txt" {
		t.Errorf("FullOutputPath = %q; want %q", scr.FullOutputPath, "/tmp/out.txt")
	}
	if scr.TruncatedTokens != 50 {
		t.Errorf("TruncatedTokens = %d; want 50", scr.TruncatedTokens)
	}
	if scr.TruncatedLines != 10 {
		t.Errorf("TruncatedLines = %d; want 10", scr.TruncatedLines)
	}
}

func TestTaskActionFields(t *testing.T) {
	task := TaskAction{
		Type:        "file_created",
		Description: "created new test file",
		Details:     "/path/to/test.go",
	}

	if task.Type != "file_created" {
		t.Errorf("Type = %q; want %q", task.Type, "file_created")
	}
	if task.Description != "created new test file" {
		t.Errorf("Description = %q; want %q", task.Description, "created new test file")
	}
	if task.Details != "/path/to/test.go" {
		t.Errorf("Details = %q; want %q", task.Details, "/path/to/test.go")
	}
}
