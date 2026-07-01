package agent

import (
	"encoding/json"
	"os"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// LoadSessionInfo loads session information including timestamp
func LoadSessionInfo(sessionID string) (*ConversationState, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, agenterrors.NewConfig("failed to get state directory", err)
	}
	workingDir, _ := os.Getwd()
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return nil, agenterrors.NewConfig("failed to resolve session file", err)
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, agenterrors.NewConfig("failed to read session file", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, agenterrors.NewConfig("failed to unmarshal session state", err)
	}

	return &state, nil
}
