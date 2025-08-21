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
	Files           map[string]WorkspaceFileInfo `json:"files"`
	BuildCommand    string                       `json:"build_command"`
	TestCommand     string                       `json:"test_command"`
	Languages       []string                     `json:"languages"`
	BuildRunners    []string                     `json:"build_runners"`
	TestRunnerPaths []string                     `json:"test_runner_paths"`
	ProjectGoals    ProjectGoals                 `json:"project_goals"`
	ProjectInsights ProjectInsights              `json:"project_insights"`
	// Caching baselines for goal/insight regeneration heuristics
	GoalsBaseline    map[string]string `json:"goals_baseline,omitempty"`
	InsightsBaseline map[string]string `json:"insights_baseline,omitempty"`
}

// ProjectGoals represents the goals and vision for the project.
type ProjectGoals struct {
	OverallGoal     string `json:"overall_goal"`
	KeyFeatures     string `json:"key_features"`
	TargetAudience  string `json:"target_audience"`
	TechnicalVision string `json:"technical_vision"`
}

// ProjectInsights captures additional high-level attributes inferred by an LLM.
type ProjectInsights struct {
	PrimaryFrameworks string `json:"primary_frameworks"`
	KeyDependencies   string `json:"key_dependencies"`
	BuildSystem       string `json:"build_system"`
	TestStrategy      string `json:"test_strategy"`
	Architecture      string `json:"architecture"`

	// New helpful fields
	Monorepo          string `json:"monorepo"`           // "yes"/"no"/"unknown"
	CIProviders       string `json:"ci_providers"`       // e.g., GitHub Actions, GitLab CI
	RuntimeTargets    string `json:"runtime_targets"`    // e.g., Node.js, JVM, Browser, Python
	DeploymentTargets string `json:"deployment_targets"` // e.g., Docker/K8s/Serverless/VMs
	PackageManagers   string `json:"package_managers"`   // e.g., npm/yarn/pnpm/go modules/pip/poetry
	RepoLayout        string `json:"repo_layout"`        // e.g., apps/ and packages/, cmd/ and internal/
}
