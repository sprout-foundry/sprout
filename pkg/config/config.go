package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/shirou/gopsutil/v3/mem"
)

const (
	LargeCoder  = "qwen2.5-coder:32b"
	MediumCoder = "qwen2.5-coder:14b"
	SmallCoder  = "qwen2.5-coder:7b"
	MicroCoder  = "qwen2.5-coder:3b"
)

// CodeStylePreferences defines the preferred code style guidelines for the project.
type CodeStylePreferences struct {
	FunctionSize      string `json:"function_size"`
	FileSize          string `json:"file_size"`
	NamingConventions string `json:"naming_conventions"`
	ErrorHandling     string `json:"error_handling"`
	TestingApproach   string `json:"testing_approach"`
	Modularity        string `json:"modularity"`
	Readability       string `json:"readability"`
}

type Config struct {
	EditingModel             string               `json:"editing_model"`
	SummaryModel             string               `json:"summary_model"`
	OrchestrationModel       string               `json:"orchestration_model"` // new field for orchestration tasks
	WorkspaceModel           string               `json:"workspace_model"`     // New field for workspace analysis
	EmbeddingModel           string               `json:"embedding_model"`     // New field for embedding model
	CodeReviewModel          string               `json:"code_review_model"`   // New field for code review tasks
	LocalModel               string               `json:"local_model"`
	TrackWithGit             bool                 `json:"track_with_git"`
	EnableSecurityChecks     bool                 `json:"enable_security_checks"` // New field for security checks
	SkipPrompt               bool                 `json:"-"`                      // Internal use, not saved to config
	Interactive              bool                 `json:"-"`                      // Internal use, not saved to config
	OllamaServerURL          string               `json:"ollama_server_url"`
	OrchestrationMaxAttempts int                  `json:"orchestration_max_attempts"` // New field for max attempts
	CodeStyle                CodeStylePreferences `json:"code_style"`                 // New field for code style preferences
	SearchModel              string               `json:"search_model"`               // NEW: Field for search model
	Temperature              float64              `json:"temperature"`                // NEW: Field for LLM temperature
	MaxTokens                int                  `json:"max_tokens"`                 // NEW: Field for max output tokens
	TopP                     float64              `json:"top_p"`                      // NEW: Field for nucleus sampling
	PresencePenalty          float64              `json:"presence_penalty"`           // NEW: Field for presence penalty
	FrequencyPenalty         float64              `json:"frequency_penalty"`          // NEW: Field for frequency penalty
	RetryAttemptCount        int                  `json:"-"`                          // Internal field to track retry attempts
	UseSearchGrounding       bool                 `json:"-"`                          // Command-scoped flag to enable search grounding
	CodeToolsEnabled         bool                 `json:"-"`                          // Allow tool-calls in code flow when true
	FromAgent                bool                 `json:"-"`                          // Internal: true when invoked from agent mode
	LastTokenUsage           *types.TokenUsage    `json:"-"`                          // Last token usage from LLM call
	// New toggles
	PreapplyReview    bool     `json:"preapply_review"`
	DryRun            bool     `json:"dry_run"`
	JsonLogs          bool     `json:"json_logs"`
	HealthChecks      bool     `json:"health_checks"`
	StagedEdits       bool     `json:"staged_edits"`
	AutoGenerateTests bool     `json:"auto_generate_tests"`
	ShellAllowlist    []string `json:"shell_allowlist"`
	TelemetryEnabled  bool     `json:"telemetry_enabled"`
	TelemetryFile     string   `json:"telemetry_file"`
	PolicyVariant     string   `json:"policy_variant"`
	// Budgets and limits
	MaxRunSeconds    int     `json:"max_run_seconds"`
	MaxRunTokens     int     `json:"max_run_tokens"`
	MaxRunCostUSD    float64 `json:"max_run_cost_usd"`
	ShellTimeoutSecs int     `json:"shell_timeout_secs"`
	// Rate limiting and batch size controls
	FileBatchSize         int `json:"file_batch_size"`         // Batch size for file processing
	EmbeddingBatchSize    int `json:"embedding_batch_size"`    // Batch size for embedding generation
	MaxConcurrentRequests int `json:"max_concurrent_requests"` // Max concurrent API requests
	RequestDelayMs        int `json:"request_delay_ms"`        // Delay between requests in milliseconds
}

func getHomeConfigPath() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	configDir := filepath.Join(home, ".ledit")
	return configDir, filepath.Join(configDir, "config.json")
}

func getCurrentConfigPath() (string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", ""
	}
	configDir := filepath.Join(cwd, ".ledit")
	return configDir, filepath.Join(configDir, "config.json")
}

func getLocalModel(skipPrompt bool) string {
	logger := utils.GetLogger(skipPrompt)
	v, err := mem.VirtualMemory()
	if err != nil {
		logger.Log(prompts.MemoryDetectionError(MicroCoder, err))
		return MicroCoder
	}
	gb := v.Total / (1024 * 1024 * 1024)
	if gb >= 48 {
		logger.Log(prompts.SystemMemoryFallback(int(gb), LargeCoder))
		return LargeCoder
	}
	if gb >= 38 {
		logger.Log(prompts.SystemMemoryFallback(int(gb), MediumCoder))
		return MediumCoder
	}
	if gb >= 20 {
		logger.Log(prompts.SystemMemoryFallback(int(gb), SmallCoder))
		return SmallCoder
	}
	logger.Log(prompts.SystemMemoryFallback(int(gb), MicroCoder))
	return MicroCoder
}

func (cfg *Config) setDefaultValues() {
	if cfg.SummaryModel == "" {
		cfg.SummaryModel = "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo"
	}
	if cfg.WorkspaceModel == "" {
		cfg.WorkspaceModel = "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo"
	}
	if cfg.EditingModel == "" {
		cfg.EditingModel = "deepinfra:deepseek-ai/DeepSeek-V3-0324" // Cheap, capable model; alternatives: deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo
	}
	if cfg.OrchestrationModel == "" {
		cfg.OrchestrationModel = "deepinfra:moonshotai/Kimi-K2-Instruct"
	}
	if cfg.CodeReviewModel == "" {
		cfg.CodeReviewModel = cfg.EditingModel // Default to editing model, but can be overridden for reliability
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "deepinfra:Qwen/Qwen3-Embedding-4B" // Default embedding model
	}
	if cfg.OllamaServerURL == "" {
		cfg.OllamaServerURL = "http://localhost:11434"
	}
	if cfg.OrchestrationMaxAttempts == 0 {
		cfg.OrchestrationMaxAttempts = 12 // Default max attempts for orchestration
	}
	if cfg.LocalModel == "" {
		cfg.LocalModel = getLocalModel(cfg.SkipPrompt) // Set local model based on system memory
	}
	// Ensure EnableSecurityChecks is explicitly set to true by default, but can be overridden by config file
	cfg.EnableSecurityChecks = true

	// NEW: Set default for SearchModel
	if cfg.SearchModel == "" {
		cfg.SearchModel = cfg.SummaryModel // Default to summary model for search
	}

	// NEW: Set default for Temperature
	if cfg.Temperature == 0 { // 0 is the zero value for float64, so this works for uninitialized or explicitly 0
		cfg.Temperature = 0.1 // Very low temperature for consistency
	}

	// NEW: Set default for MaxTokens
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 30000 // Reasonable limit for output length
	}

	// NEW: Set default for TopP
	if cfg.TopP == 0 {
		cfg.TopP = 0.9 // Focus on high-probability tokens
	}

	// NEW: Set default for PresencePenalty
	if cfg.PresencePenalty == 0 {
		cfg.PresencePenalty = 0.1 // Light penalty to discourage repetition
	}

	// NEW: Set default for FrequencyPenalty
	if cfg.FrequencyPenalty == 0 {
		cfg.FrequencyPenalty = 0.1 // Light penalty to discourage repeated phrases
	}

	// Set default code style preferences
	if cfg.CodeStyle.FunctionSize == "" {
		cfg.CodeStyle.FunctionSize = "Aim for smaller, single-purpose functions (under 50 lines)."
	}
	if cfg.CodeStyle.FileSize == "" {
		cfg.CodeStyle.FileSize = "Prefer smaller files, breaking down large components into multiple files (under 500 lines)."
	}
	if cfg.CodeStyle.NamingConventions == "" {
		cfg.CodeStyle.NamingConventions = "Use clear, descriptive names for variables, functions, and types. Follow Go conventions (camelCase for local, PascalCase for exported)."
	}
	if cfg.CodeStyle.ErrorHandling == "" {
		cfg.CodeStyle.ErrorHandling = "Handle errors explicitly, returning errors as the last return value. Avoid panics for recoverable errors."
	}
	if cfg.CodeStyle.TestingApproach == "" {
		cfg.CodeStyle.TestingApproach = "Write unit tests when practical."
	}
	if cfg.CodeStyle.Modularity == "" {
		cfg.CodeStyle.Modularity = "Design components to be loosely coupled and highly cohesive."
	}
	if cfg.CodeStyle.Readability == "" {
		cfg.CodeStyle.Readability = "Prioritize code readability and maintainability. Use comments where necessary to explain complex logic."
	}

	// Pre-apply review default: enabled
	if !cfg.PreapplyReview {
		cfg.PreapplyReview = true
	}
	// Dry-run default: disabled unless explicitly enabled
	// cfg.DryRun remains false by default
	// Json logs off by default
	// Health checks off by default
	// Staged edits off by default

	// Defaults for budgets/limits
	if cfg.ShellTimeoutSecs == 0 {
		cfg.ShellTimeoutSecs = 20
	}
	if cfg.TelemetryFile == "" {
		cfg.TelemetryFile = ".ledit/telemetry.jsonl"
	}
	// Default off for auto test generation
	// cfg.AutoGenerateTests remains false unless explicitly enabled

	// Default shell allowlist (safe, common cleanups)
	if len(cfg.ShellAllowlist) == 0 {
		cfg.ShellAllowlist = []string{
			"rm -rf node_modules",
			"rm -fr node_modules",
			"rm -rf ./node_modules",
			"rm -fr ./node_modules",
			"rm -rf node_modules/",
			"rm -fr node_modules/",
			"rm -rf ./node_modules/",
			"rm -fr ./node_modules/",
			"rm -f package-lock.json",
			"rm -f ./package-lock.json",
		}
	}

	// Set defaults for rate limiting and batch size controls
	if cfg.FileBatchSize == 0 {
		cfg.FileBatchSize = 30 // Reduced from 50 to avoid rate limits
	}
	if cfg.EmbeddingBatchSize == 0 {
		cfg.EmbeddingBatchSize = 30 // Small batch size for embeddings to avoid rate limits
	}
	if cfg.MaxConcurrentRequests == 0 {
		cfg.MaxConcurrentRequests = 3 // Reduced from 6 to avoid rate limits
	}
	if cfg.RequestDelayMs == 0 {
		cfg.RequestDelayMs = 100 // 100ms delay between requests
	}
}

func loadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var cfg Config
	// Provide default values for new fields if they are missing in older configs
	// These defaults will be overridden if the fields exist in the JSON.
	cfg.WorkspaceModel = ""                        // Default to empty, will fall back to SummaryModel
	cfg.OllamaServerURL = "http://localhost:11434" // Default Ollama URL
	cfg.EnableSecurityChecks = false               // Default to false for existing configs
	cfg.Temperature = 0.1                          // NEW: Initialize Temperature to very low value for consistency
	cfg.MaxTokens = 4096                           // NEW: Initialize MaxTokens
	cfg.TopP = 0.9                                 // NEW: Initialize TopP
	cfg.PresencePenalty = 0.1                      // NEW: Initialize PresencePenalty
	cfg.FrequencyPenalty = 0.1                     // NEW: Initialize FrequencyPenalty
	cfg.EmbeddingModel = ""                        // NEW: Initialize EmbeddingModel to its zero value
	cfg.PreapplyReview = true                      // NEW: default enable pre-apply review
	cfg.DryRun = false                             // NEW: default dry-run off
	// Initialize CodeStyle to ensure setDefaultValues can populate it
	cfg.CodeStyle = CodeStylePreferences{}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// Use setDefaultValues to ensure all fields have a value, especially new ones not in older configs.
	cfg.setDefaultValues()
	return &cfg, nil
}

func saveConfig(filePath string, cfg *Config) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func createConfig(filePath string, skipPrompt bool) (*Config, error) {
	reader := bufio.NewReader(os.Stdin)
	// No logger needed here, as these are direct prompts for user input

	fmt.Print(prompts.EnterEditingModel("deepinfra:deepseek-ai/DeepSeek-V3-0324")) // Use prompt
	editingModel, _ := reader.ReadString('\n')
	editingModel = strings.TrimSpace(editingModel)
	if editingModel == "" {
		editingModel = "deepinfra:deepseek-ai/DeepSeek-V3-0324"
	}

	fmt.Print(prompts.EnterSummaryModel("deepinfra:mistralai/Mistral-Small-3.2-24B-Instruct-2506")) // Use prompt
	summaryModel, _ := reader.ReadString('\n')
	summaryModel = strings.TrimSpace(summaryModel)
	if summaryModel == "" {
		summaryModel = "deepinfra:mistralai/Mistral-Small-3.2-24B-Instruct-2506"
	}

	fmt.Print(prompts.EnterWorkspaceModel("deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo")) // Use prompt
	workspaceModel, _ := reader.ReadString('\n')
	workspaceModel = strings.TrimSpace(workspaceModel)
	if workspaceModel == "" {
		workspaceModel = "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo"
	}

	fmt.Print(prompts.EnterOrchestrationModel("same as editing model")) // Use prompt
	orchestrationModel, _ := reader.ReadString('\n')
	orchestrationModel = strings.TrimSpace(orchestrationModel)
	if orchestrationModel == "" {
		orchestrationModel = editingModel
	}

	fmt.Print("Enter Code Review Model (e.g., same as editing model): ")
	codeReviewModel, _ := reader.ReadString('\n')
	codeReviewModel = strings.TrimSpace(codeReviewModel)
	if codeReviewModel == "" {
		codeReviewModel = editingModel
	}

	fmt.Print("Enter Embedding Model (e.g., deepinfra:Qwen/Qwen3-Embedding-4B): ")
	embeddingModel, _ := reader.ReadString('\n')
	embeddingModel = strings.TrimSpace(embeddingModel)
	if embeddingModel == "" {
		embeddingModel = "deepinfra:Qwen/Qwen3-Embedding-4B"
	}

	fmt.Print(prompts.TrackGitPrompt()) // Use prompt
	autoTrackGitStr, _ := reader.ReadString('\n')
	autoTrackGit := strings.TrimSpace(strings.ToLower(autoTrackGitStr)) == "yes"

	fmt.Print(prompts.EnableSecurityChecksPrompt()) // New prompt for security checks
	enableSecurityChecksStr, _ := reader.ReadString('\n')
	enableSecurityChecks := strings.TrimSpace(strings.ToLower(enableSecurityChecksStr)) == "yes"

	fmt.Print(prompts.EnterLLMProvider("anthropic")) // NEW PROMPT for LLM Provider

	cfg := &Config{
		EditingModel:             editingModel,
		SummaryModel:             summaryModel,
		WorkspaceModel:           workspaceModel,
		OrchestrationModel:       orchestrationModel,
		CodeReviewModel:          codeReviewModel,
		EmbeddingModel:           embeddingModel, // Set from user input
		LocalModel:               getLocalModel(skipPrompt),
		TrackWithGit:             autoTrackGit,
		EnableSecurityChecks:     enableSecurityChecks, // Set from user input
		OllamaServerURL:          "http://localhost:11434",
		OrchestrationMaxAttempts: 6,                      // Default max attempts for orchestration
		CodeStyle:                CodeStylePreferences{}, // Initialize to be populated by setDefaultValues
		RetryAttemptCount:        0,                      // Initialize retry attempt count to zero
		// SearchModel and Temperature will be set by setDefaultValues
	}

	cfg.setDefaultValues()

	if err := saveConfig(filePath, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadOrInitConfig(skipPrompt bool) (*Config, error) {
	logger := utils.GetLogger(skipPrompt)

	_, currentConfigPath := getCurrentConfigPath()
	_, homeConfigPath := getHomeConfigPath()

	if _, err := os.Stat(currentConfigPath); err == nil {
		cfg, lerr := loadConfig(currentConfigPath)
		if lerr != nil {
			return nil, lerr
		}
		if os.Getenv("LEDIT_FROM_AGENT") == "1" {
			cfg.FromAgent = true
		}
		return cfg, nil
	}
	if _, err := os.Stat(homeConfigPath); err == nil {
		cfg, lerr := loadConfig(homeConfigPath)
		if lerr != nil {
			return nil, lerr
		}
		if os.Getenv("LEDIT_FROM_AGENT") == "1" {
			cfg.FromAgent = true
		}
		return cfg, nil
	}

	logger.LogUserInteraction(prompts.NoConfigFound())
	_, homeConfigPath = getHomeConfigPath()
	cfg, err := createConfig(homeConfigPath, skipPrompt)
	if err != nil {
		return nil, fmt.Errorf("could not create initial config: %w", err)
	}
	if os.Getenv("LEDIT_FROM_AGENT") == "1" {
		cfg.FromAgent = true
	}
	logger.LogUserInteraction(prompts.ConfigSaved(homeConfigPath))
	return cfg, nil
}

func InitConfig(skipPrompt bool) error {
	logger := utils.GetLogger(skipPrompt)

	_, currentConfigPath := getCurrentConfigPath()
	_, err := createConfig(currentConfigPath, skipPrompt)
	if err != nil {
		return err
	}
	logger.LogUserInteraction(prompts.ConfigSaved(currentConfigPath))
	return nil
}
