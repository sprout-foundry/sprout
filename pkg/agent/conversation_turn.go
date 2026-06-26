package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// ConversationTurn represents a completed conversation turn stored for
// persistent context retrieval and semantic search across sessions.
type ConversationTurn struct {
	ID                string    `json:"id"`
	SessionID         string    `json:"session_id"`
	TurnNumber        int       `json:"turn_number"`
	Timestamp         time.Time `json:"timestamp"`
	UserPrompt        string    `json:"user_prompt"`
	ActionableSummary string    `json:"actionable_summary,omitempty"`
	PromptEmbedding   []float32 `json:"prompt_embedding,omitempty"`
	FilesTouched      []string  `json:"files_touched,omitempty"`
	WorkingDir        string    `json:"working_dir"`
	Duration          float64   `json:"duration"`    // seconds from prompt to turn completion
	TokenUsage        int       `json:"token_usage"` // total tokens in this turn
}

// NewConversationTurn creates a new ConversationTurn with a generated ID
// and current timestamp. Returns an error if ID generation fails.
func NewConversationTurn(sessionID string, turnNumber int, userPrompt, workingDir string) (*ConversationTurn, error) {
	id, err := generateConversationTurnID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate conversation turn ID: %w", err)
	}

	return &ConversationTurn{
		ID:         id,
		SessionID:  sessionID,
		TurnNumber: turnNumber,
		Timestamp:  time.Now().UTC(),
		UserPrompt: userPrompt,
		WorkingDir: workingDir,
	}, nil
}

// String returns a human-readable representation of the turn, omitting the
// embedding vector for readability.
func (t *ConversationTurn) String() string {
	return fmt.Sprintf("ConversationTurn{ID: %s, Session: %s, Turn: %d, Duration: %.1fs, Tokens: %d}",
		t.ID, t.SessionID, t.TurnNumber, t.Duration, t.TokenUsage)
}

// generateConversationTurnID generates a UUID-like identifier using crypto/rand.
// Returns a 32-character hex string (e.g., "a1b2c3d4e5f67890abcdef1234567890").
func generateConversationTurnID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

const maxSignatureLen = 2000

// ToVectorRecord converts a ConversationTurn into a VectorRecord for storage
// in the conversation embedding store. The prompt text is truncated to
// maxSignatureLen characters for the Signature field. All turn metadata
// (summary, files, working dir, duration, tokens) is preserved in the
// Metadata map so no information is lost.
func (t *ConversationTurn) ToVectorRecord() embedding.VectorRecord {
	// Truncate the user prompt for the signature field at a rune boundary
	// to avoid splitting multi-byte UTF-8 characters.
	runes := []rune(t.UserPrompt)
	if len(runes) > maxSignatureLen {
		runes = runes[:maxSignatureLen]
	}
	signature := string(runes)

	// Create a defensive copy of the embedding to avoid aliasing.
	// Note: The caller (EmbedAndStoreTurn in Phase 1d) is expected to set
	// PromptEmbedding to the mean of the prompt and summary embeddings
	// before calling ToVectorRecord(). See SP-027 §3.2 and §4.2.
	var emb []float32
	if t.PromptEmbedding != nil {
		emb = make([]float32, len(t.PromptEmbedding))
		copy(emb, t.PromptEmbedding)
	}

	// Build metadata map with turn-specific information
	metadata := make(map[string]interface{})
	metadata["sessionId"] = t.SessionID
	metadata["turnNumber"] = t.TurnNumber
	metadata["workingDir"] = t.WorkingDir
	metadata["duration"] = t.Duration
	metadata["tokenUsage"] = t.TokenUsage

	// Only include optional fields if they have meaningful values.
	// filesTouched is defensively copied to prevent aliasing.
	if t.ActionableSummary != "" {
		metadata["actionableSummary"] = t.ActionableSummary
	}
	if t.FilesTouched != nil && len(t.FilesTouched) > 0 {
		filesCopy := make([]string, len(t.FilesTouched))
		copy(filesCopy, t.FilesTouched)
		metadata["filesTouched"] = filesCopy
	}

	return embedding.VectorRecord{
		ID:        t.ID,
		File:      fmt.Sprintf("session_%s.json", t.SessionID),
		Name:      fmt.Sprintf("turn_%d", t.TurnNumber),
		Signature: signature,
		Embedding: emb,
		Type:      "conversation_turn",
		IndexedAt: t.Timestamp,
		Metadata:  metadata,
	}
}
