package agent

import (
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Use shared types from the types package
type AgentTokenUsage = types.AgentTokenUsage
type SplitUsage = types.SplitUsage

// AgentContext holds the state and context for agent execution
type AgentContext struct {
	UserIntent         string
	CurrentPlan        *EditPlan
	IntentAnalysis     *IntentAnalysis
	TaskComplexity     TaskComplexityLevel // For optimization routing
	ExecutedOperations []string            // Track what has been completed
	Errors             []string            // Track errors encountered
	ValidationResults  []string            // Track validation outcomes
	ValidationFailed   bool                // Flag to indicate validation failure that needs fixing
	IterationCount     int
	MaxIterations      int
	StartTime          time.Time
	TokenUsage         *AgentTokenUsage
	Config             *config.Config
	Logger             *utils.Logger
	IsCompleted        bool // Flag to indicate task completion (e.g., via immediate execution)
}

// ProgressEvaluation represents the current state and next action for the agent
type ProgressEvaluation struct {
	Status               string   `json:"status"`                // "on_track", "needs_adjustment", "critical_error", "completed"
	CompletionPercentage int      `json:"completion_percentage"` // 0-100
	NextAction           string   `json:"next_action"`           // "continue", "revise_plan", "run_command", "validate"
	Reasoning            string   `json:"reasoning"`             // Why this decision was made
	Concerns             []string `json:"concerns"`              // Any issues identified
	Commands             []string `json:"commands"`              // Shell commands to run if next_action is "run_command"
	NewPlan              *string  `json:"new_plan"`              // New plan if next_action is "revise_plan"
}

// IntentAnalysis represents the analysis of user intent
type IntentAnalysis struct {
	Category         string   // "code", "fix", "docs", "test", "review"
	Complexity       string   // "simple", "moderate", "complex"
	EstimatedFiles   []string // Files likely to be involved
	RequiresContext  bool     // Whether workspace context is needed
	ImmediateCommand string   // Optional command to execute immediately for simple tasks
	CanExecuteNow    bool     // Whether the task can be completed immediately
}

// TaskComplexityLevel represents the complexity level of a task
type TaskComplexityLevel int

const (
	TaskSimple TaskComplexityLevel = iota
	TaskModerate
	TaskComplex
)

// EditPlan represents a plan for editing files
type EditPlan struct {
	FilesToEdit    []string        `json:"files_to_edit"`   // Files that need to be modified
	EditOperations []EditOperation `json:"edit_operations"` // Specific operations to perform
	Context        string          `json:"context"`         // Additional context for the edits
	ScopeStatement string          `json:"scope_statement"` // Clear statement of what this plan addresses
}

// EditOperation represents a specific edit operation
type EditOperation struct {
	FilePath           string `json:"file_path"`           // Path to the file to edit
	Description        string `json:"description"`         // What change to make
	Instructions       string `json:"instructions"`        // Detailed instructions for the editing model
	ScopeJustification string `json:"scope_justification"` // Explanation of how this change serves the user request
}

// ValidationFixPlan represents a plan to fix validation issues
type ValidationFixPlan struct {
	ErrorAnalysis string   `json:"error_analysis"`
	AffectedFiles []string `json:"affected_files"`
	FixStrategy   string   `json:"fix_strategy"`
	Instructions  []string `json:"instructions"`
}

// WorkspaceInfo represents information about the workspace
type WorkspaceInfo struct {
	ProjectType   string              // "go", "typescript", "python", etc.
	RootFiles     []string            // Files in root directory
	AllFiles      []string            // All source files
	FilesByDir    map[string][]string // Files organized by directory
	RelevantFiles map[string]string   // file path -> brief content summary
}

// WorkspacePatterns represents patterns found in the workspace
type WorkspacePatterns struct {
	AverageFileSize      int
	PreferredPackageSize int
	ModularityLevel      string
	GoSpecificPatterns   map[string]string
}

// ProjectContext represents context about the project
type ProjectContext struct {
	Type         string // "go", "python", "node", "other"
	HasTests     bool
	HasLinting   bool
	BuildCommand string
	TestCommand  string
	LintCommand  string
}

// ValidationStep represents a single validation step
type ValidationStep struct {
	Type        string // "build", "test", "lint", "syntax"
	Command     string
	Description string
	Required    bool // If false, failure won't block
}

// ValidationStrategy represents a strategy for validation
type ValidationStrategy struct {
	ProjectType string
	Steps       []ValidationStep
	Context     string // Additional context about why these steps were chosen
}

// ProjectInfo represents basic information about the project
type ProjectInfo struct {
	AvailableFiles  []string
	HasGoMod        bool
	HasPackageJSON  bool
	HasRequirements bool
	HasMakefile     bool
}
