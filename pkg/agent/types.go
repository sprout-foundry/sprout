//go:build !agent2refactor

package agent

import (
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// TodoItem represents a task to be executed
type TodoItem struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Description string `json:"description"`
	Status      string `json:"status"` // pending, in_progress, completed, failed
	FilePath    string `json:"file_path,omitempty"`
	Priority    int    `json:"priority"`
}

// SimplifiedAgentContext holds the simplified agent state
type SimplifiedAgentContext struct {
	UserIntent      string
	Todos           []TodoItem
	Config          *config.Config
	Logger          *utils.Logger
	CurrentTodo     *TodoItem
	BuildCommand    string
	AnalysisResults map[string]string

	// Context management for persistent analysis across todos
	ContextManager *ContextManager
	PersistentCtx  *PersistentContext
	SessionID      string

	// Token usage and cost tracking
	TotalTokensUsed       int
	TotalPromptTokens     int
	TotalCompletionTokens int
	TotalCost             float64

	// User interaction settings
	SkipPrompt bool

	// Change tracking to avoid unnecessary validation
	FilesModified bool

	// Enhanced context awareness
	ProjectContext *ProjectContext
	TaskIntent     TaskIntent
	IntentType     IntentType

	// Quality optimization
	QualityLevel     QualityLevel
	QualityOptimizer *QualityOptimizer
}

// IntentType represents the type of user intent
type IntentType string

const (
	IntentTypeCodeUpdate    IntentType = "code_update"
	IntentTypeQuestion      IntentType = "question"
	IntentTypeCommand       IntentType = "command"
	IntentTypeDocumentation IntentType = "documentation"
	IntentTypeCreation      IntentType = "creation"
	IntentTypeAnalysis      IntentType = "analysis"
)

// TaskIntent represents more detailed task classification for strategy selection
type TaskIntent string

const (
	TaskIntentAnalysis      TaskIntent = "analysis"
	TaskIntentCreation      TaskIntent = "creation"
	TaskIntentModification  TaskIntent = "modification"
	TaskIntentDocumentation TaskIntent = "documentation"
	TaskIntentRefactoring   TaskIntent = "refactoring"
)

// ProjectContext holds information about the project being worked on
type ProjectContext struct {
	Language     string            `json:"language"`
	Framework    string            `json:"framework"`
	ProjectType  string            `json:"project_type"`
	OutputFormat string            `json:"output_format"`
	FileTypes    []string          `json:"file_types"`
	Patterns     map[string]string `json:"patterns"` // Common patterns like route decorators
}

// ExecutionType is now defined in todo_management.go

// AgentContext represents the full agent execution context
type AgentContext struct {
	UserIntent         string
	CurrentPlan        *EditPlan
	IntentAnalysis     *IntentAnalysis
	ExecutedOperations []string
	TokenUsage         *types.AgentTokenUsage
	IterationCount     int
	MaxIterations      int
	IsCompleted        bool
	Errors             []string
	ValidationResults  []string
	ValidationFailed   bool
	Logger             *utils.Logger
	Config             *config.Config
}

// IntentAnalysis represents the analysis of user intent
type IntentAnalysis struct {
	Category         string // "code", "fix", "docs", "test", "review"
	Complexity       string // "simple", "moderate", "complex"
	EstimatedFiles   []string
	CanExecuteNow    bool
	ImmediateCommand string
}

// EditPlan represents a plan for code edits
type EditPlan struct {
	Operations      []EditOperation
	EditOperations  []EditOperation // Alias for compatibility
	EstimatedTokens int
	RequiresBuild   bool
}

// EditOperation represents a single edit operation
type EditOperation struct {
	FilePath    string
	Operation   string // "create", "update", "delete", "move"
	Content     string
	LineNumber  int
	Description string
}

// ValidationFixPlan represents a plan for fixing validation issues
type ValidationFixPlan struct {
	Issues       []string
	Fixes        []string
	Priority     int
	RequiresLLM  bool
	Instructions string // Added for compatibility
}

// ValidationStrategy represents a strategy for validation
type ValidationStrategy struct {
	Name        string
	Description string
	Steps       []ValidationStep
	Priority    int
	ProjectType string // Added for compatibility
	Context     string // Added for compatibility
}

// ValidationStep represents a single validation step
type ValidationStep struct {
	Name        string
	Description string
	Action      string
	Timeout     time.Duration
	Type        string // Added for compatibility
	Required    bool   // Added for compatibility
	Command     string // Added for compatibility
}

// WorkspaceInfo represents workspace information
type WorkspaceInfo struct {
	Path          string
	Files         []string
	AllFiles      []string // Added for compatibility
	Structure     map[string]interface{}
	BuildSystem   string
	FilesByDir    map[string][]string // Added for compatibility
	RelevantFiles map[string]string   // Added for compatibility
	ProjectType   string              // Added for compatibility
	RootFiles     []string            // Added for compatibility
}

// ProgressEvaluation represents progress evaluation results
type ProgressEvaluation struct {
	// Add fields as needed for compatibility
}
