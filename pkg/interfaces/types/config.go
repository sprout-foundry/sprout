package types

// ProviderConfig represents configuration for an LLM provider
type ProviderConfig struct {
	Name        string            `json:"name"`
	BaseURL     string            `json:"base_url,omitempty"`
	APIKey      string            `json:"api_key,omitempty"`
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Timeout     int               `json:"timeout,omitempty"` // seconds
	Headers     map[string]string `json:"headers,omitempty"`
	Enabled     bool              `json:"enabled"`
}

// AgentConfig represents configuration for agent behavior
type AgentConfig struct {
	MaxRetries         int     `json:"max_retries"`
	RetryDelay         int     `json:"retry_delay"` // seconds
	MaxContextRequests int     `json:"max_context_requests"`
	EnableValidation   bool    `json:"enable_validation"`
	EnableCodeReview   bool    `json:"enable_code_review"`
	ValidationTimeout  int     `json:"validation_timeout"` // seconds
	DefaultStrategy    string  `json:"default_strategy"`   // "quick" or "full"
	CostThreshold      float64 `json:"cost_threshold"`     // max cost per request
}

// EditorConfig represents configuration for code editing
type EditorConfig struct {
	BackupEnabled     bool     `json:"backup_enabled"`
	DiffStyle         string   `json:"diff_style"` // "unified", "context", etc.
	AutoFormat        bool     `json:"auto_format"`
	PreferredLanguage string   `json:"preferred_language"`
	IgnorePatterns    []string `json:"ignore_patterns"`
	MaxFileSize       int64    `json:"max_file_size"` // bytes
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
	OutputFormat   string `json:"output_format"` // "text", "json", etc.
}
