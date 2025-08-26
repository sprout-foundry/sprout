package interfaces

import (
	"context"

	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// CodeGenerator defines the interface for generating and applying code changes
type CodeGenerator interface {
	// GenerateCode generates code based on instructions and context
	GenerateCode(ctx context.Context, req CodeGenerationRequest) (*CodeGenerationResult, error)

	// ApplyChanges applies a set of changes to the workspace
	ApplyChanges(ctx context.Context, changes types.ChangeSet) error

	// ValidateChanges validates that proposed changes are safe and correct
	ValidateChanges(ctx context.Context, changes types.ChangeSet) error

	// PreviewChanges returns a preview of what changes would be made
	PreviewChanges(ctx context.Context, changes types.ChangeSet) (string, error)
}

// CodeGenerationRequest represents a request to generate code
type CodeGenerationRequest struct {
	Instructions    string                  `json:"instructions"`
	TargetFile      string                  `json:"target_file,omitempty"`
	Context         *types.WorkspaceContext `json:"context,omitempty"`
	Strategy        string                  `json:"strategy"` // "quick", "full", "auto"
	MaxTokens       int                     `json:"max_tokens,omitempty"`
	Model           string                  `json:"model,omitempty"`
	AdditionalFiles []string                `json:"additional_files,omitempty"`
}

// CodeGenerationResult represents the result of code generation
type CodeGenerationResult struct {
	Changes    types.ChangeSet         `json:"changes"`
	Metadata   *types.ResponseMetadata `json:"metadata"`
	Strategy   string                  `json:"strategy"`
	Validation *ValidationResult       `json:"validation,omitempty"`
}

// ValidationResult represents the result of code validation
type ValidationResult struct {
	IsValid     bool     `json:"is_valid"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// WorkspaceAnalyzer defines the interface for analyzing workspace structure and content
type WorkspaceAnalyzer interface {
	// AnalyzeStructure analyzes the overall structure of the workspace
	AnalyzeStructure(ctx context.Context, path string) (*WorkspaceStructure, error)

	// AnalyzeFile analyzes a specific file and returns insights
	AnalyzeFile(ctx context.Context, path string) (*FileAnalysis, error)

	// FindRelevantFiles finds files relevant to a specific query or task
	FindRelevantFiles(ctx context.Context, query string, maxFiles int) ([]types.FileInfo, error)

	// GenerateSummary generates a summary of the workspace
	GenerateSummary(ctx context.Context, includeDetails bool) (string, error)

	// GetDependencies analyzes and returns dependency information
	GetDependencies(ctx context.Context) (*DependencyInfo, error)
}

// WorkspaceStructure represents the analyzed structure of a workspace
type WorkspaceStructure struct {
	RootPath      string           `json:"root_path"`
	ProjectType   string           `json:"project_type"` // "go", "javascript", "python", etc.
	Framework     string           `json:"framework,omitempty"`
	BuildTool     string           `json:"build_tool,omitempty"`
	TestFramework string           `json:"test_framework,omitempty"`
	Directories   []DirectoryInfo  `json:"directories"`
	KeyFiles      []types.FileInfo `json:"key_files"`
	ConfigFiles   []types.FileInfo `json:"config_files"`
	Metadata      map[string]any   `json:"metadata"`
}

// DirectoryInfo represents information about a directory
type DirectoryInfo struct {
	Path        string `json:"path"`
	Type        string `json:"type"` // "source", "test", "config", "docs", etc.
	FileCount   int    `json:"file_count"`
	Description string `json:"description,omitempty"`
}

// FileAnalysis represents detailed analysis of a file
type FileAnalysis struct {
	Path         string         `json:"path"`
	Language     string         `json:"language"`
	FileType     string         `json:"file_type"` // "source", "test", "config", etc.
	Summary      string         `json:"summary"`
	Functions    []FunctionInfo `json:"functions,omitempty"`
	Imports      []string       `json:"imports,omitempty"`
	Exports      []string       `json:"exports,omitempty"`
	Dependencies []string       `json:"dependencies,omitempty"`
	Complexity   int            `json:"complexity,omitempty"`
	LineCount    int            `json:"line_count"`
}

// FunctionInfo represents information about a function or method
type FunctionInfo struct {
	Name        string   `json:"name"`
	StartLine   int      `json:"start_line"`
	EndLine     int      `json:"end_line"`
	Parameters  []string `json:"parameters,omitempty"`
	ReturnType  string   `json:"return_type,omitempty"`
	Description string   `json:"description,omitempty"`
	Complexity  int      `json:"complexity,omitempty"`
}

// DependencyInfo represents dependency information for the workspace
type DependencyInfo struct {
	Language     string            `json:"language"`
	PackageFile  string            `json:"package_file,omitempty"` // package.json, go.mod, requirements.txt, etc.
	Dependencies []DependencyEntry `json:"dependencies"`
	DevDeps      []DependencyEntry `json:"dev_dependencies,omitempty"`
	Scripts      map[string]string `json:"scripts,omitempty"`
}

// DependencyEntry represents a single dependency
type DependencyEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    string `json:"type"` // "runtime", "dev", "peer", etc.
	Source  string `json:"source,omitempty"`
}

// AgentOrchestrator defines the interface for coordinating agent operations
type AgentOrchestrator interface {
	// ExecuteTask executes a high-level task using appropriate sub-agents
	ExecuteTask(ctx context.Context, task AgentTask) (*AgentResult, error)

	// CreatePlan creates an execution plan for a complex task
	CreatePlan(ctx context.Context, goal string) (*ExecutionPlan, error)

	// ExecutePlan executes a pre-defined execution plan
	ExecutePlan(ctx context.Context, plan *ExecutionPlan) (*AgentResult, error)

	// MonitorProgress monitors the progress of long-running tasks
	MonitorProgress(ctx context.Context, taskID string) (*ProgressInfo, error)

	// CancelTask cancels a running task
	CancelTask(ctx context.Context, taskID string) error
}

// AgentTask represents a task to be executed by an agent
type AgentTask struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "code_generation", "analysis", "refactoring", etc.
	Description  string         `json:"description"`
	Instructions string         `json:"instructions"`
	Context      map[string]any `json:"context,omitempty"`
	Priority     int            `json:"priority,omitempty"`
	Timeout      int            `json:"timeout,omitempty"` // seconds
}

// AgentResult represents the result of an agent operation
type AgentResult struct {
	TaskID   string                  `json:"task_id"`
	Status   string                  `json:"status"` // "success", "failure", "partial"
	Result   interface{}             `json:"result,omitempty"`
	Changes  *types.ChangeSet        `json:"changes,omitempty"`
	Metadata *types.ResponseMetadata `json:"metadata,omitempty"`
	Errors   []string                `json:"errors,omitempty"`
	Warnings []string                `json:"warnings,omitempty"`
	Duration int64                   `json:"duration"` // milliseconds
}

// ExecutionPlan represents a plan for executing a complex task
type ExecutionPlan struct {
	ID                string              `json:"id"`
	Goal              string              `json:"goal"`
	Steps             []PlanStep          `json:"steps"`
	Dependencies      map[string][]string `json:"dependencies"`       // step_id -> [dependency_step_ids]
	EstimatedDuration int                 `json:"estimated_duration"` // seconds
	CreatedAt         int64               `json:"created_at"`
}

// PlanStep represents a single step in an execution plan
type PlanStep struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Agent       string         `json:"agent,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Status      string         `json:"status"` // "pending", "running", "completed", "failed"
	Result      interface{}    `json:"result,omitempty"`
}

// ProgressInfo represents progress information for a task
type ProgressInfo struct {
	TaskID      string  `json:"task_id"`
	Status      string  `json:"status"`
	Progress    float64 `json:"progress"` // 0.0 to 1.0
	CurrentStep string  `json:"current_step,omitempty"`
	Message     string  `json:"message,omitempty"`
	StartTime   int64   `json:"start_time"`
	UpdateTime  int64   `json:"update_time"`
}
