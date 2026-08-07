package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
	Duration          float64   `json:"duration"`
	TokenUsage        int       `json:"token_usage"`
}

// NewConversationTurn creates a new ConversationTurn with a generated ID.
func NewConversationTurn(sessionID string, turnNumber int, userPrompt, workingDir string) (*ConversationTurn, error) {
	id, err := generateConversationTurnID()
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to generate conversation turn ID")
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

// String returns a human-readable representation of the turn.
func (t *ConversationTurn) String() string {
	return fmt.Sprintf("ConversationTurn{ID: %s, Session: %s, Turn: %d, Duration: %.1fs, Tokens: %d}",
		t.ID, t.SessionID, t.TurnNumber, t.Duration, t.TokenUsage)
}

// generateConversationTurnID generates a UUID-like identifier using crypto/rand.
func generateConversationTurnID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", agenterrors.Wrap(err, "failed to generate random bytes")
	}
	return hex.EncodeToString(bytes), nil
}

const maxSignatureLen = 2000

// ToVectorRecord converts a ConversationTurn into a VectorRecord for storage.
func (t *ConversationTurn) ToVectorRecord() embedding.VectorRecord {
	runes := []rune(t.UserPrompt)
	if len(runes) > maxSignatureLen {
		runes = runes[:maxSignatureLen]
	}
	signature := string(runes)

	var emb []float32
	if t.PromptEmbedding != nil {
		emb = make([]float32, len(t.PromptEmbedding))
		copy(emb, t.PromptEmbedding)
	}

	metadata := make(map[string]interface{})
	metadata["sessionId"] = t.SessionID
	metadata["turnNumber"] = t.TurnNumber
	metadata["workingDir"] = t.WorkingDir
	metadata["duration"] = t.Duration
	metadata["tokenUsage"] = t.TokenUsage

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
