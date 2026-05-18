package agent

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"
)

func TestNewConversationTurn(t *testing.T) {
	// Create a turn and verify all fields are set correctly.
	sessionID := "test-session-123"
	turnNumber := 5
	userPrompt := "what files are in the project?"
	workingDir := "/workspace/test"

	before := time.Now()
	turn, err := NewConversationTurn(sessionID, turnNumber, userPrompt, workingDir)
	if err != nil {
		t.Fatalf("NewConversationTurn returned error: %v", err)
	}

	// Verify ID is a 32-character hex string.
	hexRe := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !hexRe.MatchString(turn.ID) {
		t.Errorf("ID = %q; want 32-char hex string", turn.ID)
	}

	// Verify input fields match.
	if turn.SessionID != sessionID {
		t.Errorf("SessionID = %q; want %q", turn.SessionID, sessionID)
	}
	if turn.TurnNumber != turnNumber {
		t.Errorf("TurnNumber = %d; want %d", turn.TurnNumber, turnNumber)
	}
	if turn.UserPrompt != userPrompt {
		t.Errorf("UserPrompt = %q; want %q", turn.UserPrompt, userPrompt)
	}
	if turn.WorkingDir != workingDir {
		t.Errorf("WorkingDir = %q; want %q", turn.WorkingDir, workingDir)
	}

	// Verify Timestamp is recent and in UTC.
	after := time.Now()
	if turn.Timestamp.After(after) || turn.Timestamp.Before(before) {
		t.Errorf("Timestamp = %v; want time between %v and %v", turn.Timestamp, before, after)
	}
	if turn.Timestamp.Location() != time.UTC {
		t.Errorf("Timestamp location = %v; want UTC", turn.Timestamp.Location())
	}

	// Verify defaults: Duration and TokenUsage should be 0.
	if turn.Duration != 0 {
		t.Errorf("Duration = %f; want 0", turn.Duration)
	}
	if turn.TokenUsage != 0 {
		t.Errorf("TokenUsage = %d; want 0", turn.TokenUsage)
	}

	// Verify omitempty fields default to zero/nil values.
	if turn.ActionableSummary != "" {
		t.Errorf("ActionableSummary = %q; want empty string", turn.ActionableSummary)
	}
	if turn.PromptEmbedding != nil {
		t.Errorf("PromptEmbedding = %v; want nil", turn.PromptEmbedding)
	}
	if turn.FilesTouched != nil {
		t.Errorf("FilesTouched = %v; want nil", turn.FilesTouched)
	}
}

func TestConversationTurnIDUniqueness(t *testing.T) {
	// Create two turns and verify their IDs are different.
	turn1, err := NewConversationTurn("session-1", 1, "prompt 1", "/dir1")
	if err != nil {
		t.Fatalf("NewConversationTurn(1) returned error: %v", err)
	}

	turn2, err := NewConversationTurn("session-2", 2, "prompt 2", "/dir2")
	if err != nil {
		t.Fatalf("NewConversationTurn(2) returned error: %v", err)
	}

	if turn1.ID == turn2.ID {
		t.Errorf("IDs should be unique; both = %q", turn1.ID)
	}
}

func TestConversationTurnJSONRoundTrip(t *testing.T) {
	// Marshal a fully populated ConversationTurn to JSON and back,
	// then verify all fields survive the round trip.
	now := time.Now().UTC()
	turn := &ConversationTurn{
		ID:                "abcdef0123456789abcdef0123456789",
		SessionID:         "test-session",
		TurnNumber:        3,
		Timestamp:         now,
		UserPrompt:        "list all files",
		ActionableSummary: "user wanted to list files",
		PromptEmbedding:   []float32{0.1, -0.2, 0.3},
		FilesTouched:      []string{"file1.go", "file2.go"},
		WorkingDir:        "/workspace",
		Duration:          5.5,
		TokenUsage:        200,
	}

	data, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ConversationTurn
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != turn.ID {
		t.Errorf("ID = %q; want %q", decoded.ID, turn.ID)
	}
	if decoded.SessionID != turn.SessionID {
		t.Errorf("SessionID = %q; want %q", decoded.SessionID, turn.SessionID)
	}
	if decoded.TurnNumber != turn.TurnNumber {
		t.Errorf("TurnNumber = %d; want %d", decoded.TurnNumber, turn.TurnNumber)
	}
	if !decoded.Timestamp.Equal(turn.Timestamp) {
		t.Errorf("Timestamp = %v; want %v", decoded.Timestamp, turn.Timestamp)
	}
	if decoded.UserPrompt != turn.UserPrompt {
		t.Errorf("UserPrompt = %q; want %q", decoded.UserPrompt, turn.UserPrompt)
	}
	if decoded.ActionableSummary != turn.ActionableSummary {
		t.Errorf("ActionableSummary = %q; want %q", decoded.ActionableSummary, turn.ActionableSummary)
	}
	if len(decoded.PromptEmbedding) != len(turn.PromptEmbedding) {
		t.Errorf("len(PromptEmbedding) = %d; want %d", len(decoded.PromptEmbedding), len(turn.PromptEmbedding))
	} else {
		for i, v := range turn.PromptEmbedding {
			if decoded.PromptEmbedding[i] != v {
				t.Errorf("PromptEmbedding[%d] = %f; want %f", i, decoded.PromptEmbedding[i], v)
			}
		}
	}
	if len(decoded.FilesTouched) != len(turn.FilesTouched) {
		t.Errorf("len(FilesTouched) = %d; want %d", len(decoded.FilesTouched), len(turn.FilesTouched))
	} else {
		for i, v := range turn.FilesTouched {
			if decoded.FilesTouched[i] != v {
				t.Errorf("FilesTouched[%d] = %q; want %q", i, decoded.FilesTouched[i], v)
			}
		}
	}
	if decoded.WorkingDir != turn.WorkingDir {
		t.Errorf("WorkingDir = %q; want %q", decoded.WorkingDir, turn.WorkingDir)
	}
	if decoded.Duration != turn.Duration {
		t.Errorf("Duration = %f; want %f", decoded.Duration, turn.Duration)
	}
	if decoded.TokenUsage != turn.TokenUsage {
		t.Errorf("TokenUsage = %d; want %d", decoded.TokenUsage, turn.TokenUsage)
	}
}

func TestConversationTurnJSONOmitEmpty(t *testing.T) {
	// Verify omitempty fields are absent from JSON when zero/empty.
	turn := &ConversationTurn{
		ID:         "abcdef0123456789abcdef0123456789",
		SessionID:  "test-session",
		TurnNumber: 1,
		Timestamp:  time.Now().UTC(),
		UserPrompt: "hello",
		WorkingDir: "/dir",
	}

	data, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	jsonStr := string(data)

	// Check that omitempty fields are not present when zero/empty.
	if containsField(jsonStr, "actionable_summary") {
		t.Error("JSON should not contain action_summary when empty")
	}
	if containsField(jsonStr, "prompt_embedding") {
		t.Error("JSON should not contain prompt_embedding when nil")
	}
	if containsField(jsonStr, "files_touched") {
		t.Error("JSON should not contain files_touched when nil")
	}
}

// containsField checks if a JSON string contains a given field key.
func containsField(jsonStr, field string) bool {
	pattern := `"` + field + `"[\s:]`
	return regexp.MustCompile(pattern).MatchString(jsonStr)
}

func TestConversationTurnJSONNilSliceRoundTrip(t *testing.T) {
	// Verify that nil slices remain nil after a JSON round-trip.
	// This is important for distinguishing "not set" from "empty" in
	// downstream consumers (e.g., embedding storage).
	turn := &ConversationTurn{
		ID:         "abcdef0123456789abcdef012345678912",
		SessionID:  "test-session",
		TurnNumber: 1,
		Timestamp:  time.Now().UTC(),
		UserPrompt: "hello",
		WorkingDir: "/dir",
		// PromptEmbedding and FilesTouched intentionally left nil
	}

	data, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ConversationTurn
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.PromptEmbedding != nil {
		t.Errorf("PromptEmbedding = %v; want nil after round-trip", decoded.PromptEmbedding)
	}
	if decoded.FilesTouched != nil {
		t.Errorf("FilesTouched = %v; want nil after round-trip", decoded.FilesTouched)
	}
}

func TestConversationTurnString(t *testing.T) {
	turn := &ConversationTurn{
		ID:         "abc123",
		SessionID:  "session-1",
		TurnNumber: 3,
		Duration:   12.5,
		TokenUsage: 500,
	}
	s := turn.String()
	if s != "ConversationTurn{ID: abc123, Session: session-1, Turn: 3, Duration: 12.5s, Tokens: 500}" {
		t.Errorf("String() = %q; want expected format", s)
	}
}

func TestGenerateConversationTurnID(t *testing.T) {
	// Test the ID generator directly: verify 32-char hex string with valid hex chars.
	id, err := generateConversationTurnID()
	if err != nil {
		t.Fatalf("generateConversationTurnID returned error: %v", err)
	}

	// Verify length is exactly 32 characters (16 bytes hex-encoded).
	if len(id) != 32 {
		t.Errorf("ID length = %d; want 32", len(id))
	}

	// Verify all characters are valid lowercase hex digits.
	hexRe := regexp.MustCompile(`^[0-9a-f]+$`)
	if !hexRe.MatchString(id) {
		t.Errorf("ID = %q; want only lowercase hex characters", id)
	}
}
