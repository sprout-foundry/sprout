package config

// UIConfig contains all User Interface and logging related configuration
type UIConfig struct {
	// Output and Display
	JsonLogs       bool `json:"json_logs"`       // Output logs in JSON format
	HealthChecks   bool `json:"health_checks"`   // Enable health check displays
	PreapplyReview bool `json:"preapply_review"` // Show review before applying changes

	// Telemetry
	TelemetryEnabled bool   `json:"telemetry_enabled"` // Enable telemetry collection
	TelemetryFile    string `json:"telemetry_file"`    // File to store telemetry data

	// Git Integration
	TrackWithGit bool `json:"track_with_git"` // Track changes with Git

	// File Operations
	StagedEdits bool `json:"staged_edits"` // Enable staged edit mode
}

// DefaultUIConfig returns sensible defaults for UI configuration
func DefaultUIConfig() *UIConfig {
	return &UIConfig{
		JsonLogs:         false,
		HealthChecks:     true,
		PreapplyReview:   false,
		TelemetryEnabled: false,
		TelemetryFile:    ".ledit/telemetry.json",
		TrackWithGit:     true,
		StagedEdits:      false,
	}
}

// ShouldDisplayProgress returns true if progress should be displayed
func (c *UIConfig) ShouldDisplayProgress() bool {
	// Don't show progress if JSON logs are enabled (for programmatic use)
	return !c.JsonLogs
}

// GetTelemetryPath returns the full path for telemetry file
func (c *UIConfig) GetTelemetryPath() string {
	if c.TelemetryFile == "" {
		return ".ledit/telemetry.json"
	}
	return c.TelemetryFile
}
