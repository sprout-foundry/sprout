package workspace

import (
	"github.com/alantheprice/ledit/pkg/workspaceinfo"
)

// WorkspaceFile is an alias for workspaceinfo.WorkspaceFile
type WorkspaceFile = workspaceinfo.WorkspaceFile

// WorkspaceFileInfo is an alias for workspaceinfo.WorkspaceFileInfo
type WorkspaceFileInfo = workspaceinfo.WorkspaceFileInfo

// ProjectGoals is an alias for workspaceinfo.ProjectGoals
type ProjectGoals = workspaceinfo.ProjectGoals

// OrchestrationPlan represents the high-level plan for a complex task.
type OrchestrationPlan struct {
	Requirements []Requirement `json:"requirements"`
}

// Requirement represents a single step in the orchestration plan.
type Requirement struct {
	Instruction string `json:"instruction"`
	IsCompleted bool   `json:"is_completed"`
}

// FileChange represents a specific change to be made to a file.
type FileChange struct {
	Filepath    string `json:"filepath"`
	Instruction string `json:"instruction"`
}

// FileChanges represents a collection of file changes for a requirement.
type FileChanges struct {
	Changes []FileChange `json:"changes"`
}
