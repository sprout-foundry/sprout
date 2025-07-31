package workspace

import "github.com/alantheprice/ledit/pkg/config"

// ProjectGoals represents the long-term goals and vision for the project.
type ProjectGoals struct {
	OverallGoal      string `json:"overall_goal"`
	KeyFeatures      string `json:"key_features"`
	TargetAudience   string `json:"target_audience"`
	TechnicalVision  string `json:"technical_vision"`
	// Add more fields as needed for comprehensive goals
}

// WorkspaceContext represents the context of the current workspace
// that can be used for orchestration planning.
type WorkspaceContext struct {
	Files map[string]WorkspaceFileInfo
}

// WorkspaceFile represents the structure of the workspace.json file.
type WorkspaceFile struct {
	Files        map[string]WorkspaceFileInfo `json:"files"`
	ProjectGoals ProjectGoals                 `json:"project_goals"` // New field
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
