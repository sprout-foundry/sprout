package workspaceinfo

import (
	"encoding/json"
	"os"
)

// LoadWorkspaceFile loads the workspace.json file.
func LoadWorkspaceFile() (WorkspaceFile, error) {
	var ws WorkspaceFile
	data, err := os.ReadFile(".ledit/workspace.json")
	if err != nil {
		return ws, err
	}
	if err := json.Unmarshal(data, &ws); err != nil {
		return ws, err
	}
	return ws, nil
}

// SaveWorkspaceFile saves the workspace.json file.
func SaveWorkspaceFile(ws WorkspaceFile) error {
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(".ledit/workspace.json", data, 0644)
}

