package workspace

import (
	"encoding/json"
	"os"
)

// saveWorkspaceFile writes the WorkspaceFile struct to a JSON file.
func saveWorkspaceFile(workspace WorkspaceFile, filePath string) error {
	data, err := json.MarshalIndent(workspace, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// loadWorkspaceFile reads a WorkspaceFile struct from a JSON file.
func loadWorkspaceFile(filePath string) (WorkspaceFile, error) {
	var workspace WorkspaceFile
	data, err := os.ReadFile(filePath)
	if err != nil {
		return workspace, err
	}
	err = json.Unmarshal(data, &workspace)
	if err != nil {
		return workspace, err
	}
	return workspace, nil
}
