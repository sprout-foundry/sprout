package api

import (
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
)

// ModelSelection provides unified model selection logic that bridges the config system
// and the agent API's provider-based model selection.
type ModelSelection struct {
	config *config.Config
}

// NewModelSelection creates a new model selection instance
func NewModelSelection(cfg *config.Config) *ModelSelection {
	return &ModelSelection{config: cfg}
}

// GetModelForTask returns the appropriate model for a specific task type
func (ms *ModelSelection) GetModelForTask(taskType string) string {
	if ms.config == nil || ms.config.LLM == nil {
		// Also check legacy fields for backward compatibility
		return ms.getLegacyModelForTask(taskType)
	}

	llmCfg := ms.config.LLM

	switch taskType {
	case "editing", "code":
		if llmCfg.EditingModel != "" {
			return llmCfg.EditingModel
		}
	case "summary", "summarization":
		if llmCfg.SummaryModel != "" {
			return llmCfg.SummaryModel
		}
	case "orchestration", "process":
		if llmCfg.OrchestrationModel != "" {
			return llmCfg.OrchestrationModel
		}
	case "workspace", "analysis":
		if llmCfg.WorkspaceModel != "" {
			return llmCfg.WorkspaceModel
		}
	case "review", "code_review":
		if llmCfg.CodeReviewModel != "" {
			return llmCfg.CodeReviewModel
		}
	case "search":
		if llmCfg.SearchModel != "" {
			return llmCfg.SearchModel
		}
	case "embedding":
		if llmCfg.EmbeddingModel != "" {
			return llmCfg.EmbeddingModel
		}
	case "local":
		if llmCfg.LocalModel != "" {
			return llmCfg.LocalModel
		}
	}

	// Fallback to primary model
	if primary := llmCfg.GetPrimaryModel(); primary != "" {
		return primary
	}

	return ms.getFallbackModel(taskType)
}

// getLegacyModelForTask checks legacy config fields for backward compatibility
func (ms *ModelSelection) getLegacyModelForTask(taskType string) string {
	if ms.config == nil {
		return ms.getFallbackModel(taskType)
	}

	// Check legacy config fields
	switch taskType {
	case "editing", "code":
		if ms.config.EditingModel != "" {
			return ms.config.EditingModel
		}
	case "summary", "summarization":
		if ms.config.SummaryModel != "" {
			return ms.config.SummaryModel
		}
	case "orchestration", "process":
		if ms.config.OrchestrationModel != "" {
			return ms.config.OrchestrationModel
		}
	case "workspace", "analysis":
		if ms.config.WorkspaceModel != "" {
			return ms.config.WorkspaceModel
		}
	case "review", "code_review":
		if ms.config.CodeReviewModel != "" {
			return ms.config.CodeReviewModel
		}
	case "embedding":
		if ms.config.EmbeddingModel != "" {
			return ms.config.EmbeddingModel
		}
	case "local":
		if ms.config.LocalModel != "" {
			return ms.config.LocalModel
		}
	}

	return ms.getFallbackModel(taskType)
}

// getFallbackModel provides hard-coded fallbacks when config is unavailable
func (ms *ModelSelection) getFallbackModel(taskType string) string {
	clientType := GetClientTypeFromEnv()

	switch taskType {
	case "editing", "code":
		// Prefer fast, capable models for editing
		switch clientType {
		case OpenRouterClientType:
			return "deepseek/deepseek-chat-v3.1:free"
		case DeepInfraClientType:
			return "google/gemini-2.5-flash"
		case OllamaClientType:
			return "gpt-oss:20b"
		default:
			return GetDefaultModelForProvider(clientType)
		}

	case "orchestration", "process":
		// Prefer reasoning-capable models for orchestration
		switch clientType {
		case OpenRouterClientType:
			return "deepseek/deepseek-chat-v3.1:free"
		case DeepInfraClientType:
			return "moonshotai/Kimi-K2-Instruct"
		default:
			return GetDefaultModelForProvider(clientType)
		}

	case "summary", "workspace", "analysis":
		// Prefer high-capacity models for analysis
		switch clientType {
		case OpenRouterClientType:
			return "deepseek/deepseek-chat-v3.1:free"
		case DeepInfraClientType:
			return "meta-llama/Llama-3.3-70B-Instruct-Turbo"
		default:
			return GetDefaultModelForProvider(clientType)
		}

	default:
		return GetDefaultModelForProvider(clientType)
	}
}

// ResolveModelReference resolves a model reference to a fully qualified model name
// This handles cases where the model might be specified as just the model name without provider
func (ms *ModelSelection) ResolveModelReference(modelRef string) (ClientType, string, error) {
	// If model contains ":", it's already provider-qualified
	if strings.Contains(modelRef, ":") {
		parts := strings.SplitN(modelRef, ":", 2)
		clientType, err := GetProviderFromString(parts[0])
		if err != nil {
			return "", "", err
		}
		return clientType, parts[1], nil
	}

	// No provider specified - use environment detection and assume model is the name
	clientType := GetClientTypeFromEnv()
	return clientType, modelRef, nil
}

// GetClientForTask creates an appropriate client for a specific task
func (ms *ModelSelection) GetClientForTask(taskType string) (ClientInterface, error) {
	modelName := ms.GetModelForTask(taskType)
	clientType, model, err := ms.ResolveModelReference(modelName)
	if err != nil {
		return nil, err
	}

	return NewUnifiedClientWithModel(clientType, model)
}

// UpdateConfigDefaults ensures config defaults are aligned with agent API best practices
func (ms *ModelSelection) UpdateConfigDefaults() {
	if ms.config == nil || ms.config.LLM == nil {
		return
	}

	llmCfg := ms.config.LLM

	// Update defaults if they're still using legacy values
	if llmCfg.EditingModel == "" {
		llmCfg.EditingModel = ms.getFallbackModel("editing")
	}

	if llmCfg.OrchestrationModel == "" {
		llmCfg.OrchestrationModel = ms.getFallbackModel("orchestration")
	}

	if llmCfg.SummaryModel == "" {
		llmCfg.SummaryModel = ms.getFallbackModel("summary")
	}

	if llmCfg.WorkspaceModel == "" {
		llmCfg.WorkspaceModel = ms.getFallbackModel("workspace")
	}

	if llmCfg.CodeReviewModel == "" {
		llmCfg.CodeReviewModel = ms.getFallbackModel("review")
	}
}