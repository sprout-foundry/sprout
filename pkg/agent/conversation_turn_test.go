//go:build !windows

package agent

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/embedding"
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

func TestConversationTurn_ToVectorRecord(t *testing.T) {
	// Test ToVectorRecord with a fully populated turn.
	now := time.Now().UTC()
	turn := &ConversationTurn{
		ID:                "abcdef0123456789abcdef0123456789",
		SessionID:         "session-123",
		TurnNumber:        5,
		Timestamp:         now,
		UserPrompt:        "Add OAuth2 login with Google and Facebook support",
		ActionableSummary: "Implemented OAuth2 flow with Google and Facebook providers",
		PromptEmbedding:   []float32{0.1, -0.2, 0.3, 0.4, -0.5},
		FilesTouched:      []string{"pkg/auth/oauth.go", "pkg/auth/providers.go"},
		WorkingDir:        "/workspace/myproject",
		Duration:          5.5,
		TokenUsage:        200,
	}

	record := turn.ToVectorRecord()

	// Explicit type reference to ensure import is used
	var _ embedding.VectorRecord = record

	// Verify basic field mappings
	if record.ID != turn.ID {
		t.Errorf("ID = %q; want %q", record.ID, turn.ID)
	}
	if record.File != "session_session-123.json" {
		t.Errorf("File = %q; want %q", record.File, "session_session-123.json")
	}
	if record.Name != "turn_5" {
		t.Errorf("Name = %q; want %q", record.Name, "turn_5")
	}
	if record.Signature != turn.UserPrompt {
		t.Errorf("Signature = %q; want %q", record.Signature, turn.UserPrompt)
	}
	if record.Type != "conversation_turn" {
		t.Errorf("Type = %q; want %q", record.Type, "conversation_turn")
	}
	if !record.IndexedAt.Equal(turn.Timestamp) {
		t.Errorf("IndexedAt = %v; want %v", record.IndexedAt, turn.Timestamp)
	}

	// Verify embedding is a copy (not aliased)
	if len(record.Embedding) != len(turn.PromptEmbedding) {
		t.Errorf("len(Embedding) = %d; want %d", len(record.Embedding), len(turn.PromptEmbedding))
	}
	// Modify original embedding to verify it's a copy
	turn.PromptEmbedding[0] = 999.0
	if record.Embedding[0] != 0.1 {
		t.Errorf("Embedding was aliased; record.Embedding[0] = %f; want 0.1", record.Embedding[0])
	}

	// Verify filesTouched was defensively copied (not aliased)
	turn.FilesTouched[0] = "mutated.go"
	filesSlice := record.Metadata["filesTouched"].([]string)
	if filesSlice[0] != "pkg/auth/oauth.go" {
		t.Errorf("filesTouched was aliased; metadata[filesTouched][0] = %q; want %q", filesSlice[0], "pkg/auth/oauth.go")
	}

	// Verify metadata map is populated
	if record.Metadata == nil {
		t.Fatal("Metadata is nil; want non-nil map")
	}

	// Verify required metadata fields
	if record.Metadata["sessionId"] != turn.SessionID {
		t.Errorf("metadata[sessionId] = %v; want %v", record.Metadata["sessionId"], turn.SessionID)
	}
	if record.Metadata["turnNumber"] != turn.TurnNumber {
		t.Errorf("metadata[turnNumber] = %v; want %v", record.Metadata["turnNumber"], turn.TurnNumber)
	}
	if record.Metadata["workingDir"] != turn.WorkingDir {
		t.Errorf("metadata[workingDir] = %v; want %v", record.Metadata["workingDir"], turn.WorkingDir)
	}
	if record.Metadata["duration"] != turn.Duration {
		t.Errorf("metadata[duration] = %v; want %v", record.Metadata["duration"], turn.Duration)
	}
	if record.Metadata["tokenUsage"] != turn.TokenUsage {
		t.Errorf("metadata[tokenUsage] = %v; want %v", record.Metadata["tokenUsage"], turn.TokenUsage)
	}

	// Verify optional metadata fields are present when populated
	if record.Metadata["actionableSummary"] != turn.ActionableSummary {
		t.Errorf("metadata[actionableSummary] = %v; want %v", record.Metadata["actionableSummary"], turn.ActionableSummary)
	}
	filesTouched := record.Metadata["filesTouched"]
	if filesTouched == nil {
		t.Error("metadata[filesTouched] is nil; want non-nil slice")
	} else {
		// Type assertion for slice comparison
		filesSlice, ok := filesTouched.([]string)
		if !ok {
			t.Errorf("metadata[filesTouched] is not []string; got %T", filesTouched)
		} else if len(filesSlice) != len(turn.FilesTouched) {
			t.Errorf("len(metadata[filesTouched]) = %d; want %d", len(filesSlice), len(turn.FilesTouched))
		}
	}
}

func TestConversationTurn_ToVectorRecord_EmptyEmbedding(t *testing.T) {
	// Test ToVectorRecord when PromptEmbedding is nil.
	turn := &ConversationTurn{
		ID:         "test-id-123",
		SessionID:  "session-abc",
		TurnNumber: 1,
		Timestamp:  time.Now().UTC(),
		UserPrompt: "simple prompt",
		WorkingDir: "/workspace",
		Duration:   2.0,
		TokenUsage: 100,
		// PromptEmbedding intentionally left nil
	}

	record := turn.ToVectorRecord()

	if record.Embedding != nil {
		t.Errorf("Embedding = %v; want nil when turn.PromptEmbedding is nil", record.Embedding)
	}

	// Verify other fields are still populated correctly
	if record.ID != turn.ID {
		t.Errorf("ID = %q; want %q", record.ID, turn.ID)
	}
	if record.Type != "conversation_turn" {
		t.Errorf("Type = %q; want %q", record.Type, "conversation_turn")
	}
}

func TestConversationTurn_ToVectorRecord_TruncatedPrompt(t *testing.T) {
	// Test ToVectorRecord truncates long prompts to maxSignatureLen runes.
	longPrompt := ""
	for i := 0; i < maxSignatureLen+500; i++ {
		longPrompt += "x"
	}

	turn := &ConversationTurn{
		ID:              "test-id-456",
		SessionID:       "session-xyz",
		TurnNumber:      2,
		Timestamp:       time.Now().UTC(),
		UserPrompt:      longPrompt,
		PromptEmbedding: []float32{0.1, 0.2},
		WorkingDir:      "/workspace",
		Duration:        3.0,
		TokenUsage:      150,
	}

	record := turn.ToVectorRecord()

	// Check rune length after truncation
	sigRunes := []rune(record.Signature)
	if len(sigRunes) != maxSignatureLen {
		t.Errorf("len(runes(Signature)) = %d; want %d", len(sigRunes), maxSignatureLen)
	}
	if len(sigRunes) != 0 && sigRunes[0] != 'x' {
		t.Errorf("Signature not correctly truncated; first rune = %q; want 'x'", sigRunes[0])
	}

	// Verify the original prompt is still in the turn
	if turn.UserPrompt != longPrompt {
		t.Errorf("UserPrompt was modified; should remain unchanged")
	}
}

func TestConversationTurn_ToVectorRecord_NonASCIITruncation(t *testing.T) {
	// Test ToVectorRecord correctly truncates non-ASCII (multi-byte UTF-8) prompts.
	// Build a prompt where truncating at exactly maxSignatureLen bytes would
	// split a multi-byte character.
	multiByte := "résumé" // each 'é' is 2 bytes in UTF-8
	// Repeat to exceed maxSignatureLen runes
	repeatCount := (maxSignatureLen / len([]rune(multiByte))) + 2
	longPrompt := ""
	for i := 0; i < repeatCount; i++ {
		longPrompt += multiByte
	}

	turn := &ConversationTurn{
		ID:         "test-id-unicode",
		SessionID:  "session-unicode",
		TurnNumber: 1,
		Timestamp:  time.Now().UTC(),
		UserPrompt: longPrompt,
		WorkingDir: "/workspace",
		Duration:   1.0,
		TokenUsage: 50,
	}

	record := turn.ToVectorRecord()

	// Signature should be at most maxSignatureLen runes
	sigRunes := []rune(record.Signature)
	if len(sigRunes) > maxSignatureLen {
		t.Errorf("len(runes(Signature)) = %d; want <= %d", len(sigRunes), maxSignatureLen)
	}

	// Signature must be valid UTF-8 (string() from []rune guarantees this,
	// but verify the round-trip)
	if !utf8.ValidString(record.Signature) {
		t.Error("Signature contains invalid UTF-8 after truncation")
	}

	// Original prompt must be unchanged
	if turn.UserPrompt != longPrompt {
		t.Error("UserPrompt was modified during truncation")
	}
}

func TestConversationTurn_ToVectorRecord_MinimalFields(t *testing.T) {
	// Test ToVectorRecord with minimal required fields only.
	turn := &ConversationTurn{
		ID:         "test-id-789",
		SessionID:  "session-minimal",
		TurnNumber: 1,
		Timestamp:  time.Now().UTC(),
		UserPrompt: "minimal prompt",
		WorkingDir: "/workspace",
		Duration:   1.5,
		TokenUsage: 50,
		// ActionableSummary, FilesTouched, PromptEmbedding left at zero/nil
	}

	record := turn.ToVectorRecord()

	// Verify basic required fields
	if record.ID != turn.ID {
		t.Errorf("ID = %q; want %q", record.ID, turn.ID)
	}
	if record.File != "session_session-minimal.json" {
		t.Errorf("File = %q; want %q", record.File, "session_session-minimal.json")
	}
	if record.Name != "turn_1" {
		t.Errorf("Name = %q; want %q", record.Name, "turn_1")
	}
	if record.Type != "conversation_turn" {
		t.Errorf("Type = %q; want %q", record.Type, "conversation_turn")
	}

	// Verify embedding is nil when PromptEmbedding is nil
	if record.Embedding != nil {
		t.Errorf("Embedding = %v; want nil", record.Embedding)
	}

	// Verify metadata has required fields
	if record.Metadata == nil {
		t.Fatal("Metadata is nil; want non-nil map")
	}
	if record.Metadata["sessionId"] != turn.SessionID {
		t.Errorf("metadata[sessionId] = %v; want %v", record.Metadata["sessionId"], turn.SessionID)
	}
	if record.Metadata["turnNumber"] != turn.TurnNumber {
		t.Errorf("metadata[turnNumber] = %v; want %v", record.Metadata["turnNumber"], turn.TurnNumber)
	}
	if record.Metadata["workingDir"] != turn.WorkingDir {
		t.Errorf("metadata[workingDir] = %v; want %v", record.Metadata["workingDir"], turn.WorkingDir)
	}
	if record.Metadata["duration"] != turn.Duration {
		t.Errorf("metadata[duration] = %v; want %v", record.Metadata["duration"], turn.Duration)
	}
	if record.Metadata["tokenUsage"] != turn.TokenUsage {
		t.Errorf("metadata[tokenUsage] = %v; want %v", record.Metadata["tokenUsage"], turn.TokenUsage)
	}

	// Verify optional metadata fields are absent when empty/nil
	if _, exists := record.Metadata["actionableSummary"]; exists {
		t.Error("metadata should not contain actionableSummary when empty")
	}
	if _, exists := record.Metadata["filesTouched"]; exists {
		t.Error("metadata should not contain filesTouched when nil")
	}

	// Verify JSON omitempty works for Metadata
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	jsonStr := string(data)

	// The metadata map itself should be present since it has required fields
	if !containsField(jsonStr, "metadata") {
		t.Error("JSON should contain metadata when non-empty")
	}

	// But optional fields should not be in the JSON
	if containsField(jsonStr, "actionableSummary") {
		t.Error("JSON should not contain actionableSummary in metadata when empty")
	}
	if containsField(jsonStr, "filesTouched") {
		t.Error("JSON should not contain filesTouched in metadata when nil")
	}
}
