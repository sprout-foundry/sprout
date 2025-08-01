package workspace

import (
	"encoding/json"
	"os"
)

var (
	// DefaultWorkspaceFilePath is the default path for the workspace file.
	DefaultWorkspaceFilePath = ".ledit/workspace.json"
)

// saveWorkspaceFile writes the WorkspaceFile struct to a JSON file.
func saveWorkspaceFile(workspace WorkspaceFile) error {
	data, err := json.MarshalIndent(workspace, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(DefaultWorkspaceFilePath, data, 0644)
}

func LoadWorkspaceFile() (WorkspaceFile, error) {
	// Load the workspace file from the default path
	return loadWorkspaceFile(DefaultWorkspaceFilePath)
}

// LoadWorkspaceFile reads a WorkspaceFile struct from a JSON file.
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
