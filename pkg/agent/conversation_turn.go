package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
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
	Duration          float64   `json:"duration"`   // seconds from prompt to turn completion
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
		ID:          id,
		SessionID:   sessionID,
		TurnNumber:  turnNumber,
		Timestamp:   time.Now().UTC(),
		UserPrompt:  userPrompt,
		WorkingDir:  workingDir,
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