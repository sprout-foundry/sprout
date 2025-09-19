package types

// ProviderConfig represents configuration for an LLM provider
type ProviderConfig struct {
	Name        string  `json:"name"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	Timeout     int     `json:"timeout"`
	Enabled     bool    `json:"enabled"`
	BaseURL     string  `json:"base_url,omitempty"`
	APIKey      string  `json:"api_key,omitempty"`
}

// AgentConfig represents configuration for agent behavior
type AgentConfig struct {
	MaxRetries         int    `json:"max_retries"`
	RetryDelay         int    `json:"retry_delay"`
	MaxContextRequests int    `json:"max_context_requests"`
	EnableValidation   bool   `json:"enable_validation"`
	EnableCodeReview   bool   `json:"enable_code_review"`
	ValidationTimeout  int    `json:"validation_timeout"`
	DefaultStrategy    string `json:"default_strategy"`
	CostThreshold      float64 `json:"cost_threshold"`
}

// EditorConfig represents configuration for code editing
type EditorConfig struct {
	BackupEnabled     bool     `json:"backup_enabled"`
	DiffStyle         string   `json:"diff_style"`
	AutoFormat        bool     `json:"auto_format"`
	PreferredLanguage string   `json:"preferred_language"`
	IgnorePatterns    []string `json:"ignore_patterns"`
	MaxFileSize       int      `json:"max_file_size"`
}

// SecurityConfig represents security-related configuration
type SecurityConfig struct {
	EnableCredentialScanning bool     `json:"enable_credential_scanning"`
	BlockedPatterns          []string `json:"blocked_patterns"`
	AllowedCommands          []string `json:"allowed_commands"`
	RequireConfirmation      bool     `json:"require_confirmation"`
}

// UIConfig represents user interface configuration
type UIConfig struct {
	SkipPrompts    bool   `json:"skip_prompts"`
	ColorOutput    bool   `json:"color_output"`
	VerboseLogging bool   `json:"verbose_logging"`
	ProgressBars   bool   `json:"progress_bars"`
	OutputFormat   string `json:"output_format"`
}