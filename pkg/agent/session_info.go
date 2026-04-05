package agent

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadSessionInfo loads session information including timestamp
func LoadSessionInfo(sessionID string) (*ConversationState, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get state directory: %w", err)
	}
	workingDir, _ := os.Getwd()
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve session file: %w", err)
	}

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
