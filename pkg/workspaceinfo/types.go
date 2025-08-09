package workspaceinfo

import (
	"time"
)

// WorkspaceFileInfo holds information about a file in the workspace.
type WorkspaceFileInfo struct {
	Hash                    string    `json:"hash"`
	Summary                 string    `json:"summary"`
	Exports                 string    `json:"exports"`
	References              string    `json:"references"`
	TokenCount              int       `json:"token_count"`
	SecurityConcerns        []string  `json:"security_concerns"`
	IgnoredSecurityConcerns []string  `json:"ignored_security_concerns"`
	LastAnalyzed            time.Time `json:"last_analyzed"`
}

// WorkspaceFile represents the entire workspace with all file information.
type WorkspaceFile struct {
	Files        map[string]WorkspaceFileInfo `json:"files"`
	BuildCommand string                       `json:"build_command"`
	ProjectGoals ProjectGoals                 `json:"project_goals"`
}

// ProjectGoals represents the goals and vision for the project.
type ProjectGoals struct {
	OverallGoal     string `json:"overall_goal"`
	KeyFeatures     string `json:"key_features"`
	TargetAudience  string `json:"target_audience"`
	TechnicalVision string `json:"technical_vision"`
}
