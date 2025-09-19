package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadSessionInfo loads session information including timestamp
func LoadSessionInfo(sessionID string) (*ConversationState, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, err
	}

	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", sessionID))

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session state: %w", err)
	}

	return &state, nil
}
