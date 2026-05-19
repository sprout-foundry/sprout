package configuration

import (
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// APITimeoutConfig represents timeout settings for API calls
type APITimeoutConfig struct {
	ConnectionTimeoutSec    int `json:"connection_timeout_sec,omitempty"`
	FirstChunkTimeoutSec    int `json:"first_chunk_timeout_sec,omitempty"`
	ChunkTimeoutSec         int `json:"chunk_timeout_sec,omitempty"`
	OverallTimeoutSec       int `json:"overall_timeout_sec,omitempty"`
	CommitMessageTimeoutSec int `json:"commit_message_timeout_sec,omitempty"`
}

// MCPConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/sprout-foundry/sprout/pkg/mcp

// MCPServerConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/sprout-foundry/sprout/pkg/mcp

type APIKeys map[string]string

func (a APIKeys) Get(provider string) string {
	return a[provider]
}

func (a *APIKeys) Set(provider, key string) {
	if *a == nil {
		*a = make(map[string]string)
	}
	(*a)[provider] = key
}

// CustomProviderConfig represents a custom model provider configuration
type CustomProviderConfig struct {
	Name                   string                      `json:"name"`
	Endpoint               string                      `json:"endpoint"`
	ModelName              string                      `json:"model_name"`
	ContextSize            int                         `json:"context_size"`
	ModelContextSizes      map[string]int              `json:"model_context_sizes,omitempty"`
	ReasoningEffort        string                      `json:"reasoning_effort,omitempty"`
	Temperature            *float64                    `json:"temperature,omitempty"`
	TopP                   *float64                    `json:"top_p,omitempty"`
	Parameters             map[string]interface{}      `json:"parameters,omitempty"`
	RequiresAPIKey         bool                        `json:"requires_api_key"`
	ToolCalls              []string                    `json:"tool_calls,omitempty"`
	EnvVar                 string                      `json:"env_var,omitempty"`
	ChunkTimeoutMs         int                         `json:"chunk_timeout_ms,omitempty"`
	Conversion             providers.MessageConversion `json:"message_conversion,omitempty"`
	SupportsVision         bool                        `json:"supports_vision,omitempty"`
	VisionModel            string                      `json:"vision_model,omitempty"`
	VisionFallbackProvider string                      `json:"vision_fallback_provider,omitempty"`
	VisionFallbackModel    string                      `json:"vision_fallback_model,omitempty"`
}

// Skill defines an Agent Skill that can be loaded into context
type Skill struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Path         string            `json:"path"`
	Enabled      bool              `json:"enabled"`
	Metadata     map[string]string `json:"metadata"`
	AllowedTools string            `json:"allowed_tools"`
}

// EmbeddingIndexConfig configures the embedding-based duplicate detection and semantic search.
type EmbeddingIndexConfig struct {
	Enabled             bool     `json:"enabled,omitempty"`
	Provider            string   `json:"provider,omitempty"`
	IndexDir            string   `json:"index_dir,omitempty"`
	SimilarityThreshold float32  `json:"similarity_threshold,omitempty"`
	MaxResults          int      `json:"max_results,omitempty"`
	AutoIndex           bool     `json:"auto_index,omitempty"`
	ExcludePaths        []string `json:"exclude_paths,omitempty"`
}

// PersistentContextConfig configures persistent conversational context across sessions.
type PersistentContextConfig struct {
	ProactiveContextEnabled  *bool   `json:"proactive_context_enabled,omitempty"`
	MaxContextualResults     int     `json:"max_contextual_results,omitempty"`
	MinRelevanceScore        float64 `json:"min_relevance_score,omitempty"`
	MaxContextChars          int     `json:"max_context_chars,omitempty"`
	WorkspaceScopedRetrieval bool    `json:"workspace_scoped_retrieval,omitempty"`
	DriftDetectionEnabled    *bool   `json:"drift_detection_enabled,omitempty"`
	DriftThreshold           float64 `json:"drift_threshold,omitempty"`
	DriftCheckInterval       int     `json:"drift_check_interval,omitempty"`
}
