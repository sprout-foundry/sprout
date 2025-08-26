package interfaces

import (
	"context"
	"io"

	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// LLMProvider defines the interface for interacting with Large Language Models
type LLMProvider interface {
	// GetName returns the provider name (e.g., "openai", "gemini", "ollama")
	GetName() string

	// GetModels returns a list of available models for this provider
	GetModels(ctx context.Context) ([]types.ModelInfo, error)

	// GenerateResponse generates a response from the LLM
	GenerateResponse(ctx context.Context, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error)

	// GenerateResponseStream generates a streaming response from the LLM
	GenerateResponseStream(ctx context.Context, messages []types.Message, options types.RequestOptions, writer io.Writer) (*types.ResponseMetadata, error)

	// IsAvailable checks if the provider is available and properly configured
	IsAvailable(ctx context.Context) error

	// EstimateTokens estimates the token count for given messages
	EstimateTokens(messages []types.Message) (int, error)

	// CalculateCost calculates the cost for given token usage
	CalculateCost(usage types.TokenUsage) float64
}

// PromptProvider defines the interface for managing prompts and templates
type PromptProvider interface {
	// LoadPrompt loads a prompt by name
	LoadPrompt(name string) (string, error)

	// LoadPromptWithVariables loads a prompt with variable substitution
	LoadPromptWithVariables(name string, variables map[string]string) (string, error)

	// ListPrompts returns a list of available prompt names
	ListPrompts() []string

	// SavePrompt saves a prompt template
	SavePrompt(name, content string) error

	// DeletePrompt deletes a prompt template
	DeletePrompt(name string) error

	// ValidatePrompt validates a prompt template
	ValidatePrompt(content string) error

	// WatchPrompts watches for changes to prompt files and reloads them
	WatchPrompts(callback func(name string)) error
}

// ConfigProvider defines the interface for configuration management
type ConfigProvider interface {
	// GetProviderConfig returns configuration for a specific provider
	GetProviderConfig(providerName string) (*types.ProviderConfig, error)

	// GetAgentConfig returns agent configuration
	GetAgentConfig() *types.AgentConfig

	// GetEditorConfig returns editor configuration
	GetEditorConfig() *types.EditorConfig

	// GetSecurityConfig returns security configuration
	GetSecurityConfig() *types.SecurityConfig

	// GetUIConfig returns UI configuration
	GetUIConfig() *types.UIConfig

	// SetConfig updates configuration values
	SetConfig(key string, value interface{}) error

	// SaveConfig saves the current configuration to disk
	SaveConfig() error

	// ReloadConfig reloads configuration from disk
	ReloadConfig() error

	// WatchConfig watches for configuration changes
	WatchConfig(callback func()) error
}

// WorkspaceProvider defines the interface for workspace operations
type WorkspaceProvider interface {
	// AnalyzeWorkspace analyzes the current workspace and returns context
	AnalyzeWorkspace(ctx context.Context, path string) (*types.WorkspaceContext, error)

	// GetFileContent retrieves the content of a specific file
	GetFileContent(path string) (string, error)

	// ListFiles lists all files in the workspace matching optional patterns
	ListFiles(patterns []string) ([]types.FileInfo, error)

	// FindFiles finds files matching a search query
	FindFiles(query string) ([]types.FileInfo, error)

	// GetWorkspaceSummary returns a summary of the workspace
	GetWorkspaceSummary() (string, error)

	// IsIgnored checks if a file or directory should be ignored
	IsIgnored(path string) bool

	// WatchWorkspace watches for changes in the workspace
	WatchWorkspace(callback func(path string)) error
}

// ChangeTracker defines the interface for tracking and managing changes
type ChangeTracker interface {
	// RecordChange records a change set
	RecordChange(change types.ChangeSet) error

	// GetChanges retrieves changes by ID or criteria
	GetChanges(filter map[string]interface{}) ([]types.ChangeSet, error)

	// RollbackChange rolls back a specific change
	RollbackChange(changeID string) error

	// GetChangeHistory returns the history of changes
	GetChangeHistory() ([]types.ChangeSet, error)

	// CreateCheckpoint creates a checkpoint of the current state
	CreateCheckpoint(description string) (string, error)

	// RestoreCheckpoint restores to a specific checkpoint
	RestoreCheckpoint(checkpointID string) error
}
