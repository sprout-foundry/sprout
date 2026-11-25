package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewState_Empty(t *testing.T) {
	s := NewState()

	if s == nil {
		t.Fatal("NewState returned nil")
	}
	if s.Len() != 0 {
		t.Errorf("expected 0 messages, got %d", s.Len())
	}
	if s.SessionID() != "" {
		t.Errorf("expected empty session ID, got %q", s.SessionID())
	}
	if s.TotalTokens() != 0 {
		t.Errorf("expected 0 total tokens, got %d", s.TotalTokens())
	}
	if s.TotalCost() != 0 {
		t.Errorf("expected 0 cost, got %f", s.TotalCost())
	}
}

func TestState_AddMessage(t *testing.T) {
	s := NewState()

	msg := Message{Role: "user", Content: "hello"}
	s.AddMessage(msg)

	if s.Len() != 1 {
		t.Errorf("expected 1 message, got %d", s.Len())
	}
}

func TestState_MessagesReturnsCopy(t *testing.T) {
	s := NewState()
	s.AddMessage(Message{Role: "user", Content: "msg1"})
	s.AddMessage(Message{Role: "assistant", Content: "msg2"})

	// Get a copy of messages
	copied := s.Messages()

	// Mutate the copy
	copied[0].Content = "modified"

	// Original should be unchanged
	messages := s.Messages()
	if messages[0].Content != "msg1" {
		t.Errorf("Messages() should return a copy; expected 'msg1', got %q", messages[0].Content)
	}
}

func TestState_SetMessages(t *testing.T) {
	s := NewState()
	s.AddMessage(Message{Role: "user", Content: "old"})

	s.SetMessages([]Message{
		{Role: "user", Content: "new1"},
		{Role: "assistant", Content: "new2"},
		{Role: "assistant", Content: "new3"},
	})

	if s.Len() != 3 {
		t.Errorf("expected 3 messages after SetMessages, got %d", s.Len())
	}
	if s.Messages()[0].Content != "new1" {
		t.Errorf("expected first message 'new1', got %q", s.Messages()[0].Content)
	}
}

func TestState_MessagesWithNilSlice(t *testing.T) {
	s := NewState()
	s.SetMessages(nil)

	// Should handle nil gracefully — Len should be 0
	if s.Len() != 0 {
		t.Errorf("expected 0 messages for nil slice, got %d", s.Len())
	}
}

func TestState_SessionID(t *testing.T) {
	s := NewState()

	// Initially empty
	if s.SessionID() != "" {
		t.Errorf("expected empty session ID, got %q", s.SessionID())
	}

	// SetSessionID works
	s.SetSessionID("test-session-123")
	if s.SessionID() != "test-session-123" {
		t.Errorf("expected 'test-session-123', got %q", s.SessionID())
	}
}

func TestState_EnsureSessionID_GeneratesIfEmpty(t *testing.T) {
	s := NewState()

	s.EnsureSessionID()
	id := s.SessionID()

	if id == "" {
		t.Error("EnsureSessionID should generate a non-empty session ID")
	}
	if len(id) < len("session_") {
		t.Errorf("session ID too short: %q", id)
	}
	if id[:8] != "session_" {
		t.Errorf("expected session ID to start with 'session_', got %q", id)
	}
}

func TestState_EnsureSessionID_DoesNotOverwriteExisting(t *testing.T) {
	s := NewState()
	s.SetSessionID("my-custom-id")

	s.EnsureSessionID()

	if s.SessionID() != "my-custom-id" {
		t.Errorf("EnsureSessionID should not overwrite existing ID; expected 'my-custom-id', got %q", s.SessionID())
	}
}

func TestState_AddTokens(t *testing.T) {
	s := NewState()

	s.AddTokens(10, 5, 15)
	if s.TotalTokens() != 15 {
		t.Errorf("expected 15 total tokens, got %d", s.TotalTokens())
	}

	// Add more tokens
	s.AddTokens(20, 10, 30)
	if s.TotalTokens() != 45 {
		t.Errorf("expected 45 total tokens after cumulative adds, got %d", s.TotalTokens())
	}
}

func TestState_AddCost(t *testing.T) {
	s := NewState()

	s.AddCost(0.01)
	if s.TotalCost() != 0.01 {
		t.Errorf("expected cost 0.01, got %f", s.TotalCost())
	}

	s.AddCost(0.03)
	if s.TotalCost() != 0.04 {
		t.Errorf("expected cost 0.04 after cumulative adds, got %f", s.TotalCost())
	}
}

func TestState_ExportState(t *testing.T) {
	s := NewState()
	s.SetSessionID("test-session")
	s.AddMessage(Message{Role: "user", Content: "hello"})
	s.AddTokens(100, 50, 150)
	s.AddCost(0.005)

	data, err := s.ExportState()
	if err != nil {
		t.Fatalf("ExportState returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ExportState returned empty data")
	}

	// Verify it's valid JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("ExportState JSON is invalid: %v", err)
	}
}

func TestState_ImportState(t *testing.T) {
	s := NewState()

	importData := `{
		"messages": [{"role":"user","content":"imported"}],
		"session_id": "imported-session",
		"total_tokens": 200,
		"total_cost": 0.01,
		"prompt_tokens": 100,
		"completion_tokens": 100
	}`

	err := s.ImportState([]byte(importData))
	if err != nil {
		t.Fatalf("ImportState returned error: %v", err)
	}

	if s.SessionID() != "imported-session" {
		t.Errorf("expected session 'imported-session', got %q", s.SessionID())
	}
	if s.Len() != 1 {
		t.Errorf("expected 1 message, got %d", s.Len())
	}
	if s.Messages()[0].Content != "imported" {
		t.Errorf("expected message content 'imported', got %q", s.Messages()[0].Content)
	}
	if s.TotalTokens() != 200 {
		t.Errorf("expected 200 total tokens, got %d", s.TotalTokens())
	}
	if s.TotalCost() != 0.01 {
		t.Errorf("expected cost 0.01, got %f", s.TotalCost())
	}
}

func TestState_ExportImport_Roundtrip(t *testing.T) {
	s := NewState()
	s.SetSessionID("roundtrip-session")
	s.AddMessage(Message{Role: "user", Content: "first"})
	s.AddMessage(Message{Role: "assistant", Content: "second"})
	s.AddTokens(100, 50, 150)
	s.AddCost(0.005)

	data, err := s.ExportState()
	if err != nil {
		t.Fatalf("ExportState error: %v", err)
	}

	s2 := NewState()
	if err := s2.ImportState(data); err != nil {
		t.Fatalf("ImportState error: %v", err)
	}

	if s2.SessionID() != s.SessionID() {
		t.Errorf("session mismatch: expected %q, got %q", s.SessionID(), s2.SessionID())
	}
	if s2.Len() != s.Len() {
		t.Errorf("message count mismatch: expected %d, got %d", s.Len(), s2.Len())
	}
	if s2.TotalTokens() != s.TotalTokens() {
		t.Errorf("total tokens mismatch: expected %d, got %d", s.TotalTokens(), s2.TotalTokens())
	}
	if s2.TotalCost() != s.TotalCost() {
		t.Errorf("cost mismatch: expected %f, got %f", s.TotalCost(), s2.TotalCost())
	}
}

func TestState_ImportState_InvalidJSON(t *testing.T) {
	s := NewState()
	err := s.ImportState([]byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestState_ImportState_EmptyData(t *testing.T) {
	s := NewState()
	err := s.ImportState([]byte(""))
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
}

func TestState_ConcurrentAccess(t *testing.T) {
	s := NewState()

	done := make(chan struct{})

	// Concurrent readers
	go func() {
		for i := 0; i < 100; i++ {
			_ = s.Len()
			_ = s.Messages()
			_ = s.TotalTokens()
			_ = s.SessionID()
			_ = s.TotalCost()
		}
		done <- struct{}{}
	}()

	// Concurrent writers
	go func() {
		for i := 0; i < 100; i++ {
			s.AddMessage(Message{Role: "user", Content: string(rune('a' + i%26))})
			s.AddTokens(1, 1, 2)
			s.AddCost(0.001)
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	// Verify state is consistent
	if s.Len() != 100 {
		t.Errorf("expected 100 messages, got %d", s.Len())
	}
	if s.TotalTokens() != 200 {
		t.Errorf("expected 200 total tokens, got %d", s.TotalTokens())
	}
}

func TestState_EnsureSessionID_WithinTimeout(t *testing.T) {
	// Ensure session generation completes promptly
	s := NewState()

	done := make(chan struct{})
	go func() {
		s.EnsureSessionID()
		done <- struct{}{}
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureSessionID blocked for too long")
	}
}

func TestState_LastAssistantMessage_Empty(t *testing.T) {
	s := NewState()
	if got := s.LastAssistantMessage(); got != nil {
		t.Errorf("expected nil for empty state, got %+v", got)
	}
}

func TestState_LastAssistantMessage_NoAssistant(t *testing.T) {
	s := NewState()
	s.AddMessage(Message{Role: "user", Content: "hello"})
	s.AddMessage(Message{Role: "system", Content: "be nice"})
	if got := s.LastAssistantMessage(); got != nil {
		t.Errorf("expected nil when no assistant messages, got %+v", got)
	}
}

func TestState_LastAssistantMessage_ReturnsLast(t *testing.T) {
	s := NewState()
	s.AddMessage(Message{Role: "assistant", Content: "first"})
	s.AddMessage(Message{Role: "user", Content: "reply"})
	s.AddMessage(Message{Role: "assistant", Content: "second"})

	got := s.LastAssistantMessage()
	if got == nil {
		t.Fatal("expected assistant message, got nil")
	}
	if got.Content != "second" {
		t.Errorf("expected 'second', got %q", got.Content)
	}
	if got.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", got.Role)
	}
}

func TestState_LastAssistantMessage_ReturnsCopy(t *testing.T) {
	s := NewState()
	s.AddMessage(Message{Role: "assistant", Content: "original"})

	got := s.LastAssistantMessage()
	got.Content = "mutated"

	// Verify the internal state is unchanged
	got2 := s.LastAssistantMessage()
	if got2.Content != "original" {
		t.Errorf("LastAssistantMessage should return a copy; expected 'original', got %q", got2.Content)
	}
}

func TestState_LastAssistantMessage_Concurrent(t *testing.T) {
	s := NewState()
	s.AddMessage(Message{Role: "assistant", Content: "initial"})

	done := make(chan struct{})

	// Concurrent readers
	go func() {
		for i := 0; i < 100; i++ {
			_ = s.LastAssistantMessage()
		}
		done <- struct{}{}
	}()

	// Concurrent writers
	go func() {
		for i := 0; i < 100; i++ {
			s.AddMessage(Message{Role: "assistant", Content: string(rune('a' + i%26))})
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	// Should not panic; final result should be an assistant message
	got := s.LastAssistantMessage()
	if got == nil {
		t.Fatal("expected assistant message, got nil")
	}
}

func TestState_AddAndGetCheckpoint(t *testing.T) {
	s := NewState()

	cp := TurnCheckpoint{
		StartIndex:        0,
		EndIndex:          3,
		Summary:           "User asked about Go",
		ActionableSummary: "- Answered Go question",
	}
	s.AddCheckpoint(cp)

	cps := s.GetCheckpoints()
	if len(cps) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(cps))
	}
	if cps[0].Summary != "User asked about Go" {
		t.Errorf("expected summary 'User asked about Go', got %q", cps[0].Summary)
	}
	if cps[0].StartIndex != 0 || cps[0].EndIndex != 3 {
		t.Errorf("expected indices [0,3], got [%d,%d]", cps[0].StartIndex, cps[0].EndIndex)
	}
}

func TestState_CheckpointsReturnsCopy(t *testing.T) {
	s := NewState()
	s.AddCheckpoint(TurnCheckpoint{Summary: "first"})
	s.AddCheckpoint(TurnCheckpoint{Summary: "second"})

	copied := s.GetCheckpoints()
	copied[0].Summary = "mutated"

	// Original should be unchanged
	cps := s.GetCheckpoints()
	if cps[0].Summary != "first" {
		t.Errorf("GetCheckpoints should return a copy; expected 'first', got %q", cps[0].Summary)
	}
}

func TestState_SetCheckpoints(t *testing.T) {
	s := NewState()
	s.AddCheckpoint(TurnCheckpoint{Summary: "old"})

	s.SetCheckpoints([]TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "new1"},
		{StartIndex: 2, EndIndex: 5, Summary: "new2"},
	})

	cps := s.GetCheckpoints()
	if len(cps) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(cps))
	}
	if cps[0].Summary != "new1" {
		t.Errorf("expected 'new1', got %q", cps[0].Summary)
	}
	if cps[1].Summary != "new2" {
		t.Errorf("expected 'new2', got %q", cps[1].Summary)
	}
}

func TestState_ClearCheckpoints(t *testing.T) {
	s := NewState()
	s.AddCheckpoint(TurnCheckpoint{Summary: "1"})
	s.AddCheckpoint(TurnCheckpoint{Summary: "2"})
	s.AddCheckpoint(TurnCheckpoint{Summary: "3"})

	s.ClearCheckpoints()

	cps := s.GetCheckpoints()
	if len(cps) != 0 {
		t.Errorf("expected 0 checkpoints after clear, got %d", len(cps))
	}

	// Verify we can still add after clearing
	s.AddCheckpoint(TurnCheckpoint{Summary: "after-clear"})
	cps = s.GetCheckpoints()
	if len(cps) != 1 || cps[0].Summary != "after-clear" {
		t.Errorf("expected 1 checkpoint after clear+add, got %d", len(cps))
	}
}

func TestState_ConcurrentCheckpointAccess(t *testing.T) {
	s := NewState()

	done := make(chan struct{})

	// Concurrent readers
	go func() {
		for i := 0; i < 100; i++ {
			_ = s.GetCheckpoints()
		}
		done <- struct{}{}
	}()

	// Concurrent writers
	go func() {
		for i := 0; i < 100; i++ {
			s.AddCheckpoint(TurnCheckpoint{
				StartIndex: i,
				EndIndex:   i + 1,
				Summary:    "test",
			})
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	cps := s.GetCheckpoints()
	if len(cps) != 100 {
		t.Errorf("expected 100 checkpoints, got %d", len(cps))
	}
}

func TestState_ExportState_WithCheckpoints(t *testing.T) {
	s := NewState()
	s.SetSessionID("checkpoint-session")
	s.AddCheckpoint(TurnCheckpoint{
		StartIndex:        0,
		EndIndex:          3,
		Summary:           "User asked about Go",
		ActionableSummary: "- Answered Go question",
	})
	s.AddCheckpoint(TurnCheckpoint{
		StartIndex:        4,
		EndIndex:          6,
		Summary:           "User asked about Rust",
		ActionableSummary: "- Answered Rust question",
	})

	data, err := s.ExportState()
	if err != nil {
		t.Fatalf("ExportState returned error: %v", err)
	}

	// Verify the JSON contains a "checkpoints" key with the correct number of entries
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("ExportState JSON is invalid: %v", err)
	}

	checkpointsRaw, ok := raw["checkpoints"]
	if !ok {
		t.Fatal("ExportState JSON should contain a 'checkpoints' key")
	}

	checkpoints, ok := checkpointsRaw.([]interface{})
	if !ok {
		t.Fatalf("expected 'checkpoints' to be an array, got %T", checkpointsRaw)
	}
	if len(checkpoints) != 2 {
		t.Fatalf("expected 2 checkpoints in JSON, got %d", len(checkpoints))
	}

	// Verify each checkpoint has the expected fields
	for i, cpRaw := range checkpoints {
		cp, ok := cpRaw.(map[string]interface{})
		if !ok {
			t.Fatalf("checkpoint %d is not an object", i)
		}

		if cp["start_index"] == nil {
			t.Errorf("checkpoint %d: expected 'start_index' to be present", i)
		}
		if cp["end_index"] == nil {
			t.Errorf("checkpoint %d: expected 'end_index' to be present", i)
		}
		if cp["summary"] == nil {
			t.Errorf("checkpoint %d: expected 'summary' to be present", i)
		}
		if cp["actionable_summary"] == nil {
			t.Errorf("checkpoint %d: expected 'actionable_summary' to be present", i)
		}
	}
}

func TestState_ImportState_WithCheckpoints(t *testing.T) {
	s := NewState()

	importData := `{
		"messages": [{"role":"user","content":"imported msg"}],
		"session_id": "imported-checkpoint-session",
		"total_tokens": 100,
		"total_cost": 0.005,
		"prompt_tokens": 50,
		"completion_tokens": 50,
		"checkpoints": [
			{
				"start_index": 0,
				"end_index": 2,
				"summary": "Turn 1 summary",
				"actionable_summary": "- Did something"
			},
			{
				"start_index": 3,
				"end_index": 5,
				"summary": "Turn 2 summary",
				"actionable_summary": "- Did another thing"
			}
		]
	}`

	err := s.ImportState([]byte(importData))
	if err != nil {
		t.Fatalf("ImportState returned error: %v", err)
	}

	cps := s.GetCheckpoints()
	if len(cps) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(cps))
	}

	if cps[0].StartIndex != 0 || cps[0].EndIndex != 2 {
		t.Errorf("checkpoint 0 indices: expected [0,2], got [%d,%d]", cps[0].StartIndex, cps[0].EndIndex)
	}
	if cps[0].Summary != "Turn 1 summary" {
		t.Errorf("checkpoint 0 summary: expected 'Turn 1 summary', got %q", cps[0].Summary)
	}
	if cps[0].ActionableSummary != "- Did something" {
		t.Errorf("checkpoint 0 actionable_summary: expected '- Did something', got %q", cps[0].ActionableSummary)
	}

	if cps[1].StartIndex != 3 || cps[1].EndIndex != 5 {
		t.Errorf("checkpoint 1 indices: expected [3,5], got [%d,%d]", cps[1].StartIndex, cps[1].EndIndex)
	}
	if cps[1].Summary != "Turn 2 summary" {
		t.Errorf("checkpoint 1 summary: expected 'Turn 2 summary', got %q", cps[1].Summary)
	}
	if cps[1].ActionableSummary != "- Did another thing" {
		t.Errorf("checkpoint 1 actionable_summary: expected '- Did another thing', got %q", cps[1].ActionableSummary)
	}
}

func TestState_ExportImport_CheckpointRoundtrip(t *testing.T) {
	s := NewState()
	s.SetSessionID("checkpoint-roundtrip-session")
	s.AddTokens(200, 100, 300)
	s.AddCost(0.01)

	s.AddCheckpoint(TurnCheckpoint{
		StartIndex:        0,
		EndIndex:          3,
		Summary:           "First turn summary",
		ActionableSummary: "- Completed task one",
	})
	s.AddCheckpoint(TurnCheckpoint{
		StartIndex:        4,
		EndIndex:          7,
		Summary:           "Second turn summary with special chars: <>&\"'",
		ActionableSummary: "- Completed task two\n- Ran command: echo hello",
	})
	s.AddCheckpoint(TurnCheckpoint{
		StartIndex:        8,
		EndIndex:          10,
		Summary:           "Third turn summary",
		ActionableSummary: "", // empty actionable summary
	})

	data, err := s.ExportState()
	if err != nil {
		t.Fatalf("ExportState error: %v", err)
	}

	s2 := NewState()
	if err := s2.ImportState(data); err != nil {
		t.Fatalf("ImportState error: %v", err)
	}

	// Verify checkpoints match
	cps := s2.GetCheckpoints()
	expected := s.GetCheckpoints()

	if len(cps) != len(expected) {
		t.Fatalf("checkpoint count mismatch: expected %d, got %d", len(expected), len(cps))
	}

	for i := range cps {
		if cps[i].StartIndex != expected[i].StartIndex {
			t.Errorf("checkpoint %d start_index: expected %d, got %d", i, expected[i].StartIndex, cps[i].StartIndex)
		}
		if cps[i].EndIndex != expected[i].EndIndex {
			t.Errorf("checkpoint %d end_index: expected %d, got %d", i, expected[i].EndIndex, cps[i].EndIndex)
		}
		if cps[i].Summary != expected[i].Summary {
			t.Errorf("checkpoint %d summary: expected %q, got %q", i, expected[i].Summary, cps[i].Summary)
		}
		if cps[i].ActionableSummary != expected[i].ActionableSummary {
			t.Errorf("checkpoint %d actionable_summary: expected %q, got %q", i, expected[i].ActionableSummary, cps[i].ActionableSummary)
		}
	}

	// Also verify that the non-checkpoint fields survived the roundtrip
	if s2.SessionID() != s.SessionID() {
		t.Errorf("session mismatch: expected %q, got %q", s.SessionID(), s2.SessionID())
	}
	if s2.TotalTokens() != s.TotalTokens() {
		t.Errorf("total tokens mismatch: expected %d, got %d", s.TotalTokens(), s2.TotalTokens())
	}
	if s2.TotalCost() != s.TotalCost() {
		t.Errorf("cost mismatch: expected %f, got %f", s.TotalCost(), s2.TotalCost())
	}
}

func TestState_ExportState_EmptyCheckpointsOmitted(t *testing.T) {
	s := NewState()
	s.SetSessionID("no-checkpoints-session")
	s.AddMessage(Message{Role: "user", Content: "hello"})

	data, err := s.ExportState()
	if err != nil {
		t.Fatalf("ExportState returned error: %v", err)
	}

	// Parse and verify that checkpoints is either omitted or null/empty
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("ExportState JSON is invalid: %v", err)
	}

	// With omitempty on AgentState.Checkpoints, the key should be omitted when empty
	// but either way the JSON should not error and checkpoints should be null or absent
	checkpointsRaw, exists := raw["checkpoints"]
	if exists {
		// If present, it should be null (Go marshals nil slice as null)
		if checkpointsRaw != nil {
			t.Errorf("expected checkpoints to be null or omitted, got %v", checkpointsRaw)
		}
	}
	// If the key is not present, that's also valid (omitempty behavior)
}

func TestState_ImportState_BackwardCompat_NoCheckpointsKey(t *testing.T) {
	s := NewState()
	// Old-format JSON without a checkpoints key
	importData := `{
		"messages": [{"role":"user","content":"imported"}],
		"session_id": "legacy-session",
		"total_tokens": 100,
		"total_cost": 0.005,
		"prompt_tokens": 50,
		"completion_tokens": 50
	}`

	err := s.ImportState([]byte(importData))
	if err != nil {
		t.Fatalf("ImportState returned error: %v", err)
	}

	cps := s.GetCheckpoints()
	if len(cps) != 0 {
		t.Errorf("expected 0 checkpoints from legacy JSON, got %d", len(cps))
	}
	if s.SessionID() != "legacy-session" {
		t.Errorf("expected session 'legacy-session', got %q", s.SessionID())
	}
}

func TestState_ImportState_CheckpointsNull(t *testing.T) {
	s := NewState()
	importData := `{
		"messages": [],
		"session_id": "null-checkpoint-session",
		"checkpoints": null
	}`

	err := s.ImportState([]byte(importData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.GetCheckpoints()) != 0 {
		t.Errorf("expected 0 checkpoints, got %d", len(s.GetCheckpoints()))
	}
}

func TestState_ImportState_OldCheckpointsOverwritten(t *testing.T) {
	s := NewState()
	s.AddCheckpoint(TurnCheckpoint{Summary: "old"})
	s.AddCheckpoint(TurnCheckpoint{Summary: "old2"})

	importData := `{
		"messages": [],
		"session_id": "overwrite-session",
		"checkpoints": [{"start_index":10,"end_index":15,"summary":"new","actionable_summary":"- new item"}]
	}`

	err := s.ImportState([]byte(importData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cps := s.GetCheckpoints()
	if len(cps) != 1 {
		t.Fatalf("expected 1 checkpoint after import, got %d", len(cps))
	}
	if cps[0].Summary != "new" {
		t.Errorf("expected summary 'new', got %q", cps[0].Summary)
	}
	if cps[0].StartIndex != 10 || cps[0].EndIndex != 15 {
		t.Errorf("expected indices [10,15], got [%d,%d]", cps[0].StartIndex, cps[0].EndIndex)
	}
}
