package workspace

import "ledit/pkg/config"

// WorkspaceContext represents the context of the current workspace
// that can be used for orchestration planning.
type WorkspaceContext struct {
	Files map[string]WorkspaceFileInfo
}

// WorkspaceFile represents the structure of the workspace.json file.
type WorkspaceFile struct {
	Files map[string]WorkspaceFileInfo `json:"files"`
}

// WorkspaceFileInfo stores information about a single file in the workspace.
type WorkspaceFileInfo struct {
	Hash                    string   `json:"hash"`
	Summary                 string   `json:"summary"`
	Exports                 string   `json:"exports"`
	References              string   `json:"references"`
	TokenCount              int      `json:"token_count"`
	SecurityConcerns        []string `json:"security_concerns"`         // New field: list of detected security concerns
	IgnoredSecurityConcerns []string `json:"ignored_security_concerns"` // New field: list of security concerns explicitly ignored by the user
}

// NewWorkspaceContext creates a WorkspaceContext from the current workspace state.
func NewWorkspaceContext(prompt string, cfg *config.Config) *WorkspaceContext {
	// Implementation would go here
	return &WorkspaceContext{}
}
