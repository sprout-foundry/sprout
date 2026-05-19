package configuration

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

var personaDefaultsWarningOnce sync.Once

const (
	ConfigVersion   = "2.0"
	ConfigDirName   = ".sprout"
	ConfigFileName  = "config.json"
	APIKeysFileName = "api_keys.json"

	SelfReviewGateModeOff    = "off"
	SelfReviewGateModeCode   = "code"
	SelfReviewGateModeAlways = "always"
)

// Config represents the unified application configuration
type Config struct {
	Version string `json:"version"`

	// Provider and Model Configuration
	LastUsedProvider string            `json:"last_used_provider"`
	ProviderModels   map[string]string `json:"provider_models"`
	ProviderPriority []string          `json:"provider_priority"`

	// MCP Configuration
	MCP mcp.MCPConfig `json:"mcp"`

	// Preferences
	Preferences map[string]interface{} `json:"preferences,omitempty"`

	// AllowOrchestratorGitWrite controls whether the orchestrator persona is allowed to execute
	// writable git operations (commit, push, add, etc.) via shell_command.
	// When true (default), the orchestrator can use git write commands through shell_command
	// as an alternative to the dedicated git tool. Other personas are never allowed.
	// When false, ALL personas must use the git tool for write operations.
	AllowOrchestratorGitWrite bool `json:"allow_orchestrator_git_write,omitempty"`

	// ResourceDirectory stores captured web/vision resources relative to the current working directory.
	// This can be overridden at runtime with --resource-directory.
	ResourceDirectory string `json:"resource_directory,omitempty"`

	// ReasoningEffort sets a global default reasoning effort for chat requests.
	// Valid values: "low", "medium", "high". Empty means automatic selection.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// DisableThinking disables thinking/reasoning mode for thinking-capable models.
	// When true, models like Qwen3, Qwen3.5, GLM models, and Minimax models will
	// not use their thinking/reasoning mode. Note: GPT-OSS models do not support
	// disabling thinking (they use reasoning_effort instead).
	DisableThinking bool `json:"disable_thinking,omitempty"`

	// SystemPromptText overrides the main agent system prompt inline.
	// Empty means use the embedded default prompt.
	SystemPromptText string `json:"system_prompt_text,omitempty"`

	// SkipPrompt - for non-interactive mode
	SkipPrompt bool `json:"skip_prompt,omitempty"`

	// DismissedPrompts tracks which one-time prompts the user has dismissed.
	DismissedPrompts map[string]bool `json:"dismissed_prompts,omitempty"`

	// API Timeout Configuration (in seconds)
	APITimeouts *APITimeoutConfig `json:"api_timeouts,omitempty"`

	// Custom Providers Configuration
	CustomProviders map[string]CustomProviderConfig `json:"custom_providers,omitempty"`

	// Command History Configuration
	CommandHistoryByPath map[string][]string `json:"command_history_by_path,omitempty"`
	HistoryIndexByPath   map[string]int      `json:"history_index_by_path,omitempty"`

	// Change History Configuration
	HistoryScope string `json:"history_scope,omitempty"` // "project" or "global"

	// Self-Review Gate Configuration
	SelfReviewGateMode string `json:"self_review_gate_mode,omitempty"` // "off", "code", or "always"

	// Subagent Configuration
	SubagentProvider        string                  `json:"subagent_provider,omitempty"`
	SubagentModel           string                  `json:"subagent_model,omitempty"`
	SubagentTypes           map[string]SubagentType `json:"subagent_types,omitempty"`
	SubagentMaxParallel     int                     `json:"subagent_max_parallel,omitempty"`
	SubagentParallelEnabled *bool                   `json:"subagent_parallel_enabled,omitempty"`
	SubagentMaxDepth        int                     `json:"subagent_max_depth,omitempty"`

	// EAMode controls how the Executive Assistant persona operates.
	// "interactive" = standard chat interface (default)
	// "queue" = autonomous task processing, exits when queue is empty
	EAMode string `json:"ea_mode,omitempty"`

	// Commit Configuration
	CommitProvider string `json:"commit_provider,omitempty"`
	CommitModel    string `json:"commit_model,omitempty"`

	// Review Configuration
	ReviewProvider string `json:"review_provider,omitempty"`
	ReviewModel    string `json:"review_model,omitempty"`

	// PDF OCR Configuration
	PDFOCREnabled    bool   `json:"pdf_ocr_enabled,omitempty"`
	PDFOCRProvider   string `json:"pdf_ocr_provider,omitempty"`
	PDFOCRModel      string `json:"pdf_ocr_model,omitempty"`
	PDFOCRDownloaded bool   `json:"pdf_ocr_downloaded,omitempty"`

	// Embedding Index Configuration
	EmbeddingIndex *EmbeddingIndexConfig `json:"embedding_index,omitempty"`

	// Persistent Context Configuration
	PersistentContext *PersistentContextConfig `json:"persistent_context,omitempty"`

	// Skills Configuration
	Skills map[string]Skill `json:"skills,omitempty"`

	// Zsh Command Execution
	EnableZshCommandDetection   bool `json:"enable_zsh_command_detection,omitempty"`
	AutoExecuteDetectedCommands bool `json:"auto_execute_detected_commands,omitempty"`

	// Other flags
	FromAgent bool `json:"-"` // Internal flag, not persisted
}

// NewConfig creates a new configuration with sensible defaults
func NewConfig() *Config {
	return &Config{
		Version:          ConfigVersion,
		LastUsedProvider: "",
		ProviderModels: map[string]string{
			"openai":       "gpt-5-mini",
			"zai":          "GLM-4.6",
			"deepinfra":    "deepseek-ai/DeepSeek-V3.1-Terminus",
			"openrouter":   "openai/gpt-5",
			"ollama-local": "qwen3-coder:30b",
			"ollama-turbo": "deepseek-v3.1:671b",
		},
		ProviderPriority: []string{
			"openrouter",
			"zai",
			"deepinfra",
			"ollama-turbo",
			"ollama-local",
			"openai",
		},
		CustomProviders:      make(map[string]CustomProviderConfig),
		CommandHistoryByPath: make(map[string][]string),
		HistoryIndexByPath:   make(map[string]int),
		MCP:                  mcp.DefaultMCPConfig(),
		Preferences:          make(map[string]interface{}),
		APITimeouts: &APITimeoutConfig{
			ConnectionTimeoutSec:    300,
			FirstChunkTimeoutSec:    600,
			ChunkTimeoutSec:         600,
			OverallTimeoutSec:       1800,
			CommitMessageTimeoutSec: 300,
		},
		HistoryScope:                "project",
		SelfReviewGateMode:          SelfReviewGateModeOff,
		EnableZshCommandDetection:   true,
		AutoExecuteDetectedCommands: true,
		SubagentTypes:               defaultSubagentTypes(),
		Skills:                      defaultSkills(),
		PDFOCREnabled:               true,
		PDFOCRProvider:              "ollama",
		PDFOCRModel:                 "glm-ocr",
		SubagentMaxParallel:         2,
		SubagentParallelEnabled:     func() *bool { t := true; return &t }(),
		EmbeddingIndex: &EmbeddingIndexConfig{
			Enabled:             false,
			Provider:            "auto",
			AutoIndex:           false,
			SimilarityThreshold: 0.90,
			MaxResults:          3,
			ONNX: &ONNXConfig{
				ModelURL:     "https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/onnx/model_q8.onnx",
				TokenizerURL: "https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/tokenizer.json",
				Dimensions:   256,
			},
		},
		PersistentContext: &PersistentContextConfig{
			ProactiveContextEnabled:  func() *bool { b := true; return &b }(),
			MaxContextualResults:     5,
			MinRelevanceScore:        0.50,
			MaxContextChars:          4000,
			WorkspaceScopedRetrieval: false,
			DriftDetectionEnabled:    func() *bool { b := true; return &b }(),
			DriftThreshold:           0.60,
			DriftCheckInterval:       5,
		},
	}
}
