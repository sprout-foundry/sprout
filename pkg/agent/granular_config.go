//go:build !agent2refactor

package agent

// GranularEditingConfig controls the behavior of the granular editing workflow
type GranularEditingConfig struct {
	Enabled              bool     `json:"enabled"`                 // Enable granular editing workflow
	MaxStepsPerPlan      int      `json:"max_steps_per_plan"`      // Maximum steps to extract from plan
	VerifyBuildAfterStep bool     `json:"verify_build_after_step"` // Run build verification after each step
	EnableFallback       bool     `json:"enable_fallback"`         // Fall back to single edit if plan parsing fails
	StepTimeoutSeconds   int      `json:"step_timeout_seconds"`    // Timeout for individual steps
	SupportedFileTypes   []string `json:"supported_file_types"`    // File types to consider for editing
}

// DefaultGranularEditingConfig returns sensible defaults
func DefaultGranularEditingConfig() *GranularEditingConfig {
	return &GranularEditingConfig{
		Enabled:              true,
		MaxStepsPerPlan:      5,
		VerifyBuildAfterStep: false,
		EnableFallback:       true,
		StepTimeoutSeconds:   60,
		SupportedFileTypes:   []string{".go", ".py", ".js", ".ts", ".md", ".txt"},
	}
}

// getGranularEditingConfig retrieves the configuration for granular editing
func getGranularEditingConfig(ctx *SimplifiedAgentContext) *GranularEditingConfig {
	// For now, return defaults. In the future, this could read from ctx.Config
	// or a dedicated configuration file
	return DefaultGranularEditingConfig()
}
