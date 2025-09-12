package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	providers "github.com/alantheprice/ledit/pkg/agent_providers"
	types "github.com/alantheprice/ledit/pkg/agent_types"
	"github.com/alantheprice/ledit/pkg/config"
)

// ModelInfo represents information about an available model
type ModelInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Size          string   `json:"size,omitempty"`
	Cost          float64  `json:"cost,omitempty"`
	InputCost     float64  `json:"input_cost,omitempty"`
	OutputCost    float64  `json:"output_cost,omitempty"`
	ContextLength int      `json:"context_length,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// ModelsListInterface defines methods for listing available models
type ModelsListInterface interface {
	ListAvailableModels() ([]ModelInfo, error)
	GetDefaultModel() string
	IsModelAvailable(modelID string) bool
}

// GetAvailableModels returns available models for the current provider
func GetAvailableModels() ([]ModelInfo, error) {
	clientType := GetClientTypeFromEnv()
	return GetModelsForProvider(clientType)
}

// GetModelsForProvider returns available models for a specific provider
func GetModelsForProvider(clientType ClientType) ([]ModelInfo, error) {
	// Try to use the provider's ListModels method first
	provider, err := createProviderForType(clientType)
	if err == nil && provider != nil {
		typesModels, listErr := provider.ListModels()
		if listErr == nil {
			// Convert from types.ModelInfo to api.ModelInfo
			apiModels := make([]ModelInfo, len(typesModels))
			for i, typesModel := range typesModels {
				apiModels[i] = convertTypesToAPI(typesModel)
			}
			return apiModels, nil
		}
	}

	// Fallback to hardcoded model fetchers if provider method fails
	switch clientType {
	case OpenAIClientType:
		return getOpenAIModels()
	case DeepInfraClientType:
		return getDeepInfraModels()
	case OllamaClientType:
		return getOllamaModels()
	case CerebrasClientType:
		return getCerebrasModels()
	case OpenRouterClientType:
		return getOpenRouterModels()
	case GroqClientType:
		return getGroqModels()
	case DeepSeekClientType:
		return getDeepSeekModels()
	default:
		return nil, fmt.Errorf("unknown client type: %s", clientType)
	}
}

// getOpenAIModels gets available models from OpenAI API
var (
	openaiModelsCache       []ModelInfo
	openaiModelsInitialized bool = false // Force cache reset for corrected pricing
)

func getOpenAIModels() ([]ModelInfo, error) {
	if openaiModelsInitialized {
		return openaiModelsCache, nil
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenAI models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Read the full response body for both JSON file writing and parsing
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var modelsResp struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode OpenAI models: %w", err)
	}

	// Convert to ModelInfo format
	models := make([]ModelInfo, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		// Only include chat completion models
		if strings.Contains(model.ID, "gpt") || strings.Contains(model.ID, "o1") ||
			strings.Contains(model.ID, "chatgpt") {

			// Get context length and pricing based on model
			contextLength := getOpenAIContextLength(model.ID)
			inputCost, outputCost := getOpenAIModelPricing(model.ID)

			models = append(models, ModelInfo{
				ID:            model.ID,
				Name:          model.ID,
				Provider:      "openai",
				Description:   fmt.Sprintf("OpenAI %s model", model.ID),
				ContextLength: contextLength,
				InputCost:     inputCost,
				OutputCost:    outputCost,
				Tags:          []string{"chat", "openai"},
			})
		}
	}

	openaiModelsCache = models
	openaiModelsInitialized = true
	return models, nil
}

// getOpenAIModelPricing returns the input and output costs per 1M tokens for OpenAI models
func getOpenAIModelPricing(modelID string) (inputCost, outputCost float64) {
	// Use the same pricing map from the OpenAI client
	pricingMap := map[string]struct {
		InputPer1M       float64
		CachedInputPer1M float64
		OutputPer1M      float64
		BatchMultiplier  float64
		FlexMultiplier   float64
	}{
		// GPT-5 series (current as of September 2025)
		"gpt-5":                 {0.625, 0.3125, 5.0, 0.5, 0.6},
		"gpt-5-2025-08-07":      {0.625, 0.3125, 5.0, 0.5, 0.6},
		"gpt-5-chat-latest":     {0.625, 0.3125, 5.0, 0.5, 0.6},
		"gpt-5-mini":            {0.125, 0.0625, 1.0, 0.5, 0.6},
		"gpt-5-mini-2025-08-07": {0.125, 0.0625, 1.0, 0.5, 0.6},
		"gpt-5-nano":            {0.025, 0.0125, 0.2, 0.5, 0.6},
		"gpt-5-nano-2025-08-07": {0.025, 0.0125, 0.2, 0.5, 0.6},

		// O3 series (current pricing)
		"o3":      {1.0, 0.25, 4.0, 0.5, 0.6},
		"o3-mini": {0.55, 0.138, 2.2, 0.5, 0.6},

		// O4-mini (from screenshot)
		"o4-mini": {0.55, 0.138, 2.2, 0.5, 0.6},

		// O1 series (from screenshot)
		"o1":                 {1.0, 0.25, 4.0, 0.5, 0.6},
		"o1-2024-12-17":      {1.0, 0.25, 4.0, 0.5, 0.6},
		"o1-mini":            {0.55, 0.138, 2.2, 0.5, 0.6},
		"o1-mini-2024-09-12": {0.55, 0.138, 2.2, 0.5, 0.6},
		"o1-pro":             {3.0, 0.75, 12.0, 0.5, 0.6},
		"o1-pro-2025-03-19":  {3.0, 0.75, 12.0, 0.5, 0.6},

		// GPT-4o series (convert per-1K to per-1M for display)
		"gpt-4o":                 {0.005, 0.0025, 0.015, 0.5, 0.6},
		"gpt-4o-2024-05-13":      {0.005, 0.0025, 0.015, 0.5, 0.6},
		"gpt-4o-2024-08-06":      {0.0025, 0.00125, 0.01, 0.5, 0.6},
		"gpt-4o-2024-11-20":      {0.0025, 0.00125, 0.01, 0.5, 0.6},
		"gpt-4o-mini":            {0.00015, 0.000075, 0.0006, 0.5, 0.6},
		"gpt-4o-mini-2024-07-18": {0.00015, 0.000075, 0.0006, 0.5, 0.6},

		// Audio and specialized models
		"gpt-4o-audio-preview":                 {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4o-audio-preview-2024-10-01":      {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4o-audio-preview-2024-12-17":      {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4o-audio-preview-2025-06-03":      {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4o-mini-audio-preview":            {0.002, 0.001, 0.008, 0.5, 0.6},
		"gpt-4o-mini-audio-preview-2024-12-17": {0.002, 0.001, 0.008, 0.5, 0.6},

		// Realtime models
		"gpt-4o-realtime-preview":                 {0.015, 0.0075, 0.045, 0.5, 0.6},
		"gpt-4o-realtime-preview-2024-10-01":      {0.015, 0.0075, 0.045, 0.5, 0.6},
		"gpt-4o-realtime-preview-2024-12-17":      {0.015, 0.0075, 0.045, 0.5, 0.6},
		"gpt-4o-realtime-preview-2025-06-03":      {0.015, 0.0075, 0.045, 0.5, 0.6},
		"gpt-4o-mini-realtime-preview":            {0.002, 0.001, 0.008, 0.5, 0.6},
		"gpt-4o-mini-realtime-preview-2024-12-17": {0.002, 0.001, 0.008, 0.5, 0.6},

		// Search models
		"gpt-4o-search-preview":                 {0.005, 0.0025, 0.015, 0.5, 0.6},
		"gpt-4o-search-preview-2025-03-11":      {0.005, 0.0025, 0.015, 0.5, 0.6},
		"gpt-4o-mini-search-preview":            {0.00015, 0.000075, 0.0006, 0.5, 0.6},
		"gpt-4o-mini-search-preview-2025-03-11": {0.00015, 0.000075, 0.0006, 0.5, 0.6},

		// Transcription models
		"gpt-4o-transcribe":      {0.005, 0.0025, 0.015, 0.5, 0.6},
		"gpt-4o-mini-transcribe": {0.00015, 0.000075, 0.0006, 0.5, 0.6},
		"gpt-4o-mini-tts":        {0.00015, 0.000075, 0.0006, 0.5, 0.6},

		// GPT-4 series (legacy pricing)
		"gpt-4-turbo":            {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4-turbo-2024-04-09": {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4-turbo-preview":    {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4":                  {0.03, 0.015, 0.06, 0.5, 0.6},
		"gpt-4-0613":             {0.03, 0.015, 0.06, 0.5, 0.6},
		"gpt-4-0125-preview":     {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-4-1106-preview":     {0.01, 0.005, 0.03, 0.5, 0.6},

		// GPT-4.1 series (newer models)
		"gpt-4.1":                 {0.008, 0.004, 0.024, 0.5, 0.6},
		"gpt-4.1-2025-04-14":      {0.008, 0.004, 0.024, 0.5, 0.6},
		"gpt-4.1-mini":            {0.002, 0.001, 0.006, 0.5, 0.6},
		"gpt-4.1-mini-2025-04-14": {0.002, 0.001, 0.006, 0.5, 0.6},
		"gpt-4.1-nano":            {0.0005, 0.00025, 0.0015, 0.5, 0.6},
		"gpt-4.1-nano-2025-04-14": {0.0005, 0.00025, 0.0015, 0.5, 0.6},

		// GPT-3.5 series (legacy pricing)
		"gpt-3.5-turbo":               {0.002, 0.001, 0.002, 0.5, 0.6},
		"gpt-3.5-turbo-0125":          {0.002, 0.001, 0.002, 0.5, 0.6},
		"gpt-3.5-turbo-1106":          {0.002, 0.001, 0.002, 0.5, 0.6},
		"gpt-3.5-turbo-16k":           {0.003, 0.0015, 0.004, 0.5, 0.6},
		"gpt-3.5-turbo-instruct":      {0.0015, 0.00075, 0.002, 0.5, 0.6},
		"gpt-3.5-turbo-instruct-0914": {0.0015, 0.00075, 0.002, 0.5, 0.6},

		// ChatGPT models
		"chatgpt-4o-latest": {0.005, 0.0025, 0.015, 0.5, 0.6},

		// Audio and specialized models
		"gpt-audio":               {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-audio-2025-08-28":    {0.01, 0.005, 0.03, 0.5, 0.6},
		"gpt-realtime":            {5.0, 0.4, 0.0, 0.5, 0.6}, // Per 1M, no output cost
		"gpt-realtime-2025-08-28": {5.0, 0.4, 0.0, 0.5, 0.6},
		"gpt-image-1":             {10.0, 2.5, 40.0, 0.5, 0.6},
	}

	if pricing, exists := pricingMap[modelID]; exists {
		// All pricing values are now stored as cost per 1M tokens, return as-is
		return pricing.InputPer1M, pricing.OutputPer1M
	}

	// Return 0, 0 for unknown models (will show as N/A)
	return 0, 0
}

// getOpenAIContextLength returns the context length for OpenAI models (2025 updated)
func getOpenAIContextLength(modelID string) int {
	switch {
	// GPT-5 series (2025) - up to 272K context
	case strings.Contains(modelID, "gpt-5"):
		return 272000 // GPT-5 supports up to 272K context
	// O3 series (2025) - large context models
	case strings.Contains(modelID, "o3-mini"):
		return 200000 // O3-mini supports ~200K context
	case strings.Contains(modelID, "o3"):
		return 200000 // O3 models support large context
	// O1 series - reasoning models
	case strings.Contains(modelID, "o1"):
		return 128000 // O1 models support 128K context
	// GPT-4o series - multimodal models
	case strings.Contains(modelID, "gpt-4o"):
		return 128000 // GPT-4o supports 128K context
	// GPT-4 series
	case strings.Contains(modelID, "gpt-4-turbo"):
		return 128000 // GPT-4 Turbo supports 128K context
	case strings.Contains(modelID, "gpt-4"):
		return 8192 // Base GPT-4 supports 8K context
	// GPT-3.5 series
	case strings.Contains(modelID, "gpt-3.5-turbo"):
		return 16385 // GPT-3.5-turbo supports ~16K context
	// ChatGPT models
	case strings.Contains(modelID, "chatgpt"):
		return 128000 // ChatGPT models typically support large context
	default:
		return 16000 // Conservative default for unknown models
	}
}

// getDeepInfraModels gets available models from DeepInfra API
var (
	deepInfraModelsCache       []ModelInfo
	deepInfraModelsInitialized bool
)

func getDeepInfraModels() ([]ModelInfo, error) {
	if deepInfraModelsInitialized {
		return deepInfraModelsCache, nil
	}

	apiKey := os.Getenv("DEEPINFRA_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DEEPINFRA_API_KEY not set")
	}

	client := &http.Client{Timeout: 60 * time.Second} // Increased from 30s to 60s

	req, err := http.NewRequest("GET", "https://api.deepinfra.com/v1/openai/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DeepInfra API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response struct {
		Object string `json:"object"`
		Data   []struct {
			ID       string `json:"id"`
			Object   string `json:"object"`
			Created  int64  `json:"created"`
			OwnedBy  string `json:"owned_by"`
			Metadata *struct {
				Description   string `json:"description,omitempty"`
				ContextLength int    `json:"context_length,omitempty"`
				MaxTokens     int    `json:"max_tokens,omitempty"`
				Pricing       *struct {
					InputTokens     float64 `json:"input_tokens"`
					OutputTokens    float64 `json:"output_tokens"`
					CacheReadTokens float64 `json:"cache_read_tokens,omitempty"`
				} `json:"pricing,omitempty"`
				Tags []string `json:"tags,omitempty"`
			} `json:"metadata,omitempty"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]ModelInfo, len(response.Data))
	for i, model := range response.Data {
		modelInfo := ModelInfo{
			ID:       model.ID,
			Provider: "DeepInfra",
		}

		// Extract metadata if available
		if model.Metadata != nil {
			modelInfo.Description = model.Metadata.Description
			modelInfo.ContextLength = model.Metadata.ContextLength
			modelInfo.Tags = model.Metadata.Tags

			// Extract pricing information
			if model.Metadata.Pricing != nil {
				modelInfo.InputCost = model.Metadata.Pricing.InputTokens
				modelInfo.OutputCost = model.Metadata.Pricing.OutputTokens
				// Use average of input/output for backward compatibility
				modelInfo.Cost = (model.Metadata.Pricing.InputTokens + model.Metadata.Pricing.OutputTokens) / 2.0
			}
		}

		models[i] = modelInfo
	}

	// Sort models alphabetically by ID
	for i := 0; i < len(models); i++ {
		for j := i + 1; j < len(models); j++ {
			if models[i].ID > models[j].ID {
				models[i], models[j] = models[j], models[i]
			}
		}
	}

	return models, nil
}

// getOllamaModels gets available models from local Ollama installation
func getOllamaModels() ([]ModelInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(config.DefaultOllamaURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("Ollama is not running. Please start Ollama first")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API error (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response struct {
		Models []struct {
			Name       string    `json:"name"`
			Size       int64     `json:"size"`
			Digest     string    `json:"digest"`
			ModifiedAt time.Time `json:"modified_at"`
			Details    struct {
				Format            string   `json:"format"`
				Family            string   `json:"family"`
				Families          []string `json:"families"`
				ParameterSize     string   `json:"parameter_size"`
				QuantizationLevel string   `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]ModelInfo, len(response.Models))
	for i, model := range response.Models {
		sizeGB := float64(model.Size) / (1024 * 1024 * 1024)

		models[i] = ModelInfo{
			ID:       model.Name,
			Provider: "Ollama (Local)",
			Size:     fmt.Sprintf("%.1fGB", sizeGB),
			Cost:     0.0, // Local models are free
		}

		// Add descriptions for known models
		if model.Name == "gpt-oss:20b" || model.Name == "gpt-oss:latest" || model.Name == "gpt-oss" {
			models[i].Description = "GPT-OSS 20B - Local inference, free to use"
		} else {
			models[i].Description = fmt.Sprintf("Local %s model", model.Details.Family)
		}
	}

	return models, nil
}

// getCerebrasModels gets available models from Cerebras API
func getCerebrasModels() ([]ModelInfo, error) {
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("CEREBRAS_API_KEY not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", "https://api.cerebras.ai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Cerebras API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]ModelInfo, len(response.Data))
	for i, model := range response.Data {
		models[i] = ModelInfo{
			ID:       model.ID,
			Name:     model.ID,
			Provider: "Cerebras",
		}
	}

	return models, nil
}

// getOpenRouterModels gets available models from OpenRouter API
func getOpenRouterModels() ([]ModelInfo, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenRouter API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response struct {
		Data []struct {
			ID            string `json:"id"`
			CanonicalSlug string `json:"canonical_slug"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			Pricing       *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
				Request    string `json:"request"`
				Image      string `json:"image"`
			} `json:"pricing"`
			ContextLength   int      `json:"context_length"`
			SupportedParams []string `json:"supported_parameters"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]ModelInfo, len(response.Data))
	for i, model := range response.Data {
		modelInfo := ModelInfo{
			ID:            model.ID,
			Name:          model.Name,
			Description:   model.Description,
			Provider:      "OpenRouter",
			ContextLength: model.ContextLength,
			Tags:          model.SupportedParams, // Show supported parameters as tags
		}

		if model.Pricing != nil {
			if promptCost, err := strconv.ParseFloat(model.Pricing.Prompt, 64); err == nil {
				modelInfo.InputCost = promptCost
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				modelInfo.OutputCost = completionCost
			}
			// Only calculate average if both costs are available and non-zero
			if modelInfo.InputCost > 0 || modelInfo.OutputCost > 0 {
				modelInfo.Cost = (modelInfo.InputCost + modelInfo.OutputCost) / 2.0
			}
		}

		models[i] = modelInfo
	}

	// Add availability hints to models based on known working ones
	return addAvailabilityHints(models), nil
}

// addAvailabilityHints adds availability information based on known working models
func addAvailabilityHints(models []ModelInfo) []ModelInfo {
	// Known working models based on our testing
	knownWorking := map[string]bool{
		"deepseek/deepseek-chat-v3.1:free": true,
		"deepseek/deepseek-chat-v3.1":      true,
		"qwen/qwen3-30b-a3b-thinking-2507": true,
		"bytedance/seed-oss-36b-instruct":  true,
		"moonshotai/kimi-k2-0905":          true,
		// Add more as we discover them
	}

	for i, model := range models {
		if knownWorking[model.ID] {
			// Mark as verified working
			if len(model.Tags) == 0 {
				model.Tags = []string{"✅ verified-working"}
			} else {
				model.Tags = append([]string{"✅ verified-working"}, model.Tags...)
			}
			models[i] = model
		}
	}

	return models
}

// isModelAvailable tests if a model is actually usable via OpenRouter API
func isModelAvailable(client *http.Client, apiKey, modelID string) bool {
	requestBody := map[string]interface{}{
		"model": modelID,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "test"},
		},
		"max_tokens": 1,
	}

	reqBody, _ := json.Marshal(requestBody)

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/alantheprice/coder")
	req.Header.Set("X-Title", "Coder AI Assistant")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Model is available if we don't get a 404
	if resp.StatusCode == 404 {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "No allowed providers") {
			return false
		}
	}

	return resp.StatusCode != 404
}

// ValidateOpenRouterModel tests if a specific OpenRouter model is available
func ValidateOpenRouterModel(modelID string) error {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENROUTER_API_KEY not set")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	requestBody := map[string]interface{}{
		"model": modelID,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "test"},
		},
		"max_tokens": 1,
	}

	reqBody, _ := json.Marshal(requestBody)

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/alantheprice/coder")
	req.Header.Set("X-Title", "Coder AI Assistant")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "No allowed providers") {
			return fmt.Errorf("model %s is not available - no providers found", modelID)
		}
		return fmt.Errorf("model %s not found", modelID)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// getGroqModels gets available models from Groq API
func getGroqModels() ([]ModelInfo, error) {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", "https://api.groq.com/openai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Groq API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]ModelInfo, len(response.Data))
	for i, model := range response.Data {
		models[i] = ModelInfo{
			ID:       model.ID,
			Provider: "Groq",
			Cost:     0.0, // Groq pricing varies by model
		}

		// Add descriptions for known Groq models
		switch model.ID {
		case "llama3-70b-8192":
			models[i].Description = "Llama 3 70B - Fast inference via Groq"
			models[i].Cost = 0.00059 // $0.59 per million tokens
		case "llama3-8b-8192":
			models[i].Description = "Llama 3 8B - Fast inference via Groq"
			models[i].Cost = 0.00010 // $0.10 per million tokens
		case "mixtral-8x7b-32768":
			models[i].Description = "Mixtral 8x7B - Fast inference via Groq"
			models[i].Cost = 0.00027 // $0.27 per million tokens
		default:
			models[i].Description = fmt.Sprintf("%s model via Groq", model.ID)
		}
	}

	return models, nil
}

// getDeepSeekModels gets available models from DeepSeek API
func getDeepSeekModels() ([]ModelInfo, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY not set")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", "https://api.deepseek.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DeepSeek API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]ModelInfo, len(response.Data))
	for i, model := range response.Data {
		models[i] = ModelInfo{
			ID:       model.ID,
			Provider: "DeepSeek",
			Cost:     0.0, // DeepSeek pricing varies by model
		}

		// Add descriptions for known DeepSeek models
		switch model.ID {
		case "deepseek-chat":
			models[i].Description = "DeepSeek Chat - General purpose model"
			models[i].Cost = 0.00014 // $0.14 per million tokens
		case "deepseek-coder":
			models[i].Description = "DeepSeek Coder - Coding specialized model"
			models[i].Cost = 0.00028 // $0.28 per million tokens
		default:
			models[i].Description = fmt.Sprintf("%s model via DeepSeek", model.ID)
		}
	}

	return models, nil
}

// createProviderForType creates a provider instance for the given client type
func createProviderForType(clientType ClientType) (types.ProviderInterface, error) {
	switch clientType {
	case CerebrasClientType:
		return providers.NewCerebrasProvider()
	case OpenRouterClientType:
		return providers.NewOpenRouterProvider()
	// DeepInfra provider is incomplete, will use fallback
	case DeepInfraClientType:
		return nil, fmt.Errorf("DeepInfra provider is incomplete, using fallback")
	// Add other providers as they implement ListModels
	default:
		return nil, fmt.Errorf("provider %s does not support ListModels yet", clientType)
	}
}

// convertTypesToAPI converts types.ModelInfo to api.ModelInfo
func convertTypesToAPI(typesModel types.ModelInfo) ModelInfo {
	return ModelInfo{
		ID:            typesModel.ID,
		Name:          typesModel.Name,
		Provider:      typesModel.Provider,
		Description:   typesModel.Description,
		ContextLength: typesModel.ContextLength,
		InputCost:     typesModel.InputCost,
		OutputCost:    typesModel.OutputCost,
		Cost:          typesModel.Cost,
	}
}

// ClearModelCaches clears all model caches to force refresh on next call
func ClearModelCaches() {
	openaiModelsInitialized = false
	deepInfraModelsInitialized = false
	openaiModelsCache = nil
	deepInfraModelsCache = nil
}

// ClearModelCacheForProvider clears the model cache for a specific provider
func ClearModelCacheForProvider(clientType ClientType) {
	switch clientType {
	case OpenAIClientType:
		openaiModelsInitialized = false
		openaiModelsCache = nil
	case DeepInfraClientType:
		deepInfraModelsInitialized = false
		deepInfraModelsCache = nil
		// Other providers don't have static caches currently
	}
}
