package workspaceinfo

// WorkspaceFile represents the structure of the .ledit/workspace.json file.
type WorkspaceFile struct {
	Files map[string]WorkspaceFileInfo `json:"files"`

	// Monorepo-aware structure
	Projects         map[string]ProjectInfo `json:"projects,omitempty"`
	MonorepoType     string                 `json:"monorepo_type,omitempty"` // "single", "multi", "hybrid"
	RootBuildCommand string                 `json:"root_build_command,omitempty"`
	RootTestCommand  string                 `json:"root_test_command,omitempty"`

	// Legacy single-project fields (maintained for compatibility)
	BuildCommand      string            `json:"build_command,omitempty"`
	TestCommand       string            `json:"test_command,omitempty"`
	BuildRunners      []string          `json:"build_runners,omitempty"`
	TestRunnerPaths   []string          `json:"test_runner_paths,omitempty"`
	Languages         []string          `json:"languages,omitempty"`
	ProjectGoals      ProjectGoals      `json:"project_goals,omitempty"`
	GoalsBaseline     map[string]string `json:"goals_baseline,omitempty"`
	ProjectInsights   ProjectInsights   `json:"project_insights,omitempty"`
	InsightsBaseline  map[string]string `json:"insights_baseline,omitempty"`
	EmbeddingProvider string            `json:"embedding_provider,omitempty"`
	TotalTokens       int               `json:"total_tokens,omitempty"`
}

// ProjectInfo represents a single project within a workspace (monorepo support)
type ProjectInfo struct {
	Path           string   `json:"path"`                      // Relative path from workspace root
	Name           string   `json:"name"`                      // Project name
	Type           string   `json:"type"`                      // "frontend", "backend", "shared", "service", "library"
	Language       string   `json:"language"`                  // "javascript", "typescript", "python", "go", "rust"
	Framework      string   `json:"framework,omitempty"`       // "react", "vue", "express", "fastapi", "gin"
	BuildCommand   string   `json:"build_command,omitempty"`   // Project-specific build command
	TestCommand    string   `json:"test_command,omitempty"`    // Project-specific test command
	DevCommand     string   `json:"dev_command,omitempty"`     // Project-specific dev command
	Dependencies   []string `json:"dependencies,omitempty"`    // Dependencies on other projects in monorepo
	PackageManager string   `json:"package_manager,omitempty"` // "npm", "yarn", "pip", "go mod", "cargo"
	ConfigFiles    []string `json:"config_files,omitempty"`    // Key config files for this project
	EntryPoints    []string `json:"entry_points,omitempty"`    // Main entry point files
	Version        string   `json:"version,omitempty"`         // Project version
}

// WorkspaceFileInfo holds metadata for each file in the workspace.
type WorkspaceFileInfo struct {
	Hash                    string    `json:"hash"`
	Summary                 string    `json:"summary,omitempty"`
	Exports                 string    `json:"exports,omitempty"`
	References              string    `json:"references,omitempty"`
	TokenCount              int       `json:"token_count,omitempty"`
	Embedding               []float32 `json:"embedding,omitempty"`
	SecurityConcerns        []string  `json:"security_concerns,omitempty"`
	IgnoredSecurityConcerns []string  `json:"ignored_security_concerns,omitempty"`
}

// ProjectGoals defines high-level project goals.
type ProjectGoals struct {
	Mission          string `json:"mission"`
	PrimaryFunctions string `json:"primary_functions,omitempty"`
	SuccessMetrics   string `json:"success_metrics,omitempty"`
}

// ProjectInsights provides high-level insights about the project.
type ProjectInsights struct {
	PrimaryFrameworks string `json:"primary_frameworks,omitempty"`
	KeyDependencies   string `json:"key_dependencies,omitempty"`
	BuildSystem       string `json:"build_system,omitempty"`
	TestStrategy      string `json:"test_strategy,omitempty"`
	Architecture      string `json:"architecture,omitempty"`
	Monorepo          string `json:"monorepo,omitempty"`
	CIProviders       string `json:"ci_providers,omitempty"`
	RuntimeTargets    string `json:"runtime_targets,omitempty"`
	DeploymentTargets string `json:"deployment_targets,omitempty"`
	PackageManagers   string `json:"package_managers,omitempty"`
	RepoLayout        string `json:"repo_layout,omitempty"`
}
