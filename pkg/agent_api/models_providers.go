package api

// models_providers.go — the model-listing provider wrappers: the
// LocalModelsProvider registration hook, the createProviderForType factory,
// the provider-specific wrappers (openAI, ollama-local, openRouter, deepInfra,
// mistral), and the parseFloat helper. The generic config-based wrapper +
// config types + custom-provider loaders live in models_providers_config.go.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// LocalModelsProvider, when non-nil, supplies the live model listing for
// SproutLocalClientType. pkg/localmodel owns the real implementation (the
// in-process MLX LocalProvider) but already imports this package for shared
// types, so it can't be imported back here without a cycle — its init()
// sets this var instead, a standard one-directional registration hook.
// Left nil (falling through to the generic config-file wrapper, which has
// no real model list for "sprout-local") only on platforms where
// pkg/localmodel never gets linked in at all.
var LocalModelsProvider func(ctx context.Context) ([]ModelInfo, error)

type localModelsListWrapper struct {
	fn func(ctx context.Context) ([]ModelInfo, error)
}

func (w localModelsListWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return w.fn(ctx)
}

// createProviderForType creates a provider instance for the given client type
func createProviderForType(clientType ClientType) (interface {
	ListModels(context.Context) ([]ModelInfo, error)
}, error) {
	switch clientType {
	case SproutLocalClientType:
		if LocalModelsProvider != nil {
			return localModelsListWrapper{fn: LocalModelsProvider}, nil
		}
		return &genericConfigListModelsWrapper{providerName: string(clientType)}, nil
	case OllamaClientType, OllamaLocalClientType:
		client, err := NewOllamaLocalClient("llama3.1:8b") // Use an available model
		if err != nil {
			return nil, agenterrors.Wrap(err, "failed to create Ollama local client")
		}
		return &ollamaLocalListModelsWrapper{client: client}, nil
	case OllamaCloudClientType:
		return &genericConfigListModelsWrapper{providerName: "ollama-cloud"}, nil
	case OpenAIClientType:
		return &genericConfigListModelsWrapper{providerName: "openai"}, nil
	case OpenRouterClientType:
		// Create OpenRouter wrapper that uses the provider's ListModels directly
		return &openRouterListModelsWrapper{}, nil
	case ZAIClientType:
		// Use generic provider wrapper to get models from config
		return &genericConfigListModelsWrapper{providerName: "zai"}, nil
	case ZAICodingClientType:
		// Coding plan uses the same model list but a different endpoint
		return &genericConfigListModelsWrapper{providerName: "zai-coding"}, nil
	case DeepInfraClientType:
		// Create DeepInfra wrapper that uses the provider's ListModels directly
		return &deepInfraListModelsWrapper{}, nil
	case LMStudioClientType:
		// LM Studio doesn't require an API key or base URL (has default fallback)
		// Create LM Studio wrapper that uses the provider's ListModels directly
		return &lmStudioListModelsWrapper{}, nil
	case MistralClientType:
		// Create Mistral wrapper using OpenAI-compatible models endpoint
		return &mistralListModelsWrapper{}, nil
	default:
		return &genericConfigListModelsWrapper{providerName: string(clientType)}, nil
	}
}

// Wrapper adapters to normalize ListModels return types

type openAIListModelsWrapper struct{}

func (w *openAIListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	apiKey, err := credentials.ResolveProviderAPIKey("openai", "OpenAI")
	if err != nil {
		return nil, err
	}

	// Use context for request timeout - no need for separate client timeout
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to create request", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to fetch OpenAI models", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, agenterrors.NewProviderError("failed to fetch OpenAI models", FormatHTTPResponseError(resp.StatusCode, resp.Header, body), "openai", "")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to read response body", err)
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
		return nil, agenterrors.NewConfig("failed to decode OpenAI models", err)
	}

	// Convert to ModelInfo format
	models := make([]ModelInfo, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		// Only include chat completion models
		if strings.Contains(model.ID, "gpt") || strings.Contains(model.ID, "o1") {
			modelInfo := ModelInfo{
				ID:       model.ID,
				Name:     model.ID,
				Provider: "openai",
			}

			// Add pricing info for common models
			switch {
			case strings.HasPrefix(model.ID, "gpt-4o-mini"):
				modelInfo.InputCost = 0.15
				modelInfo.OutputCost = 0.60
				modelInfo.ContextLength = 128000
			case strings.HasPrefix(model.ID, "gpt-4o"):
				modelInfo.InputCost = 2.50
				modelInfo.OutputCost = 10.00
				modelInfo.ContextLength = 128000
			case strings.HasPrefix(model.ID, "gpt-4-turbo"):
				modelInfo.InputCost = 10.00
				modelInfo.OutputCost = 30.00
				modelInfo.ContextLength = 128000
			case strings.HasPrefix(model.ID, "gpt-4"):
				modelInfo.InputCost = 30.00
				modelInfo.OutputCost = 60.00
				modelInfo.ContextLength = 8192
			case strings.HasPrefix(model.ID, "gpt-3.5-turbo"):
				modelInfo.InputCost = 0.50
				modelInfo.OutputCost = 1.50
				modelInfo.ContextLength = 16385
			case strings.HasPrefix(model.ID, "o1-preview"):
				modelInfo.InputCost = 15.00
				modelInfo.OutputCost = 60.00
				modelInfo.ContextLength = 128000
			case strings.HasPrefix(model.ID, "o1-mini"):
				modelInfo.InputCost = 3.00
				modelInfo.OutputCost = 12.00
				modelInfo.ContextLength = 128000
			}

			if modelInfo.InputCost > 0 || modelInfo.OutputCost > 0 {
				modelInfo.Cost = (modelInfo.InputCost + modelInfo.OutputCost) / 2.0
			}

			models = append(models, modelInfo)
		}
	}

	return models, nil
}

type ollamaLocalListModelsWrapper struct {
	client *OllamaLocalClient
}

func (w *ollamaLocalListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return w.client.ListModels(ctx)
}

type openRouterListModelsWrapper struct{}

func (w *openRouterListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	// OpenRouter's model list is public — resolve the key best-effort and
	// only attach it when present. Listing must not require a key.
	apiKey, _ := credentials.ResolveProviderAPIKey("openrouter", "OpenRouter")

	req, err := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to create request", err)
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to fetch OpenRouter models", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, agenterrors.NewProviderError("failed to fetch OpenRouter models", FormatHTTPResponseError(resp.StatusCode, resp.Header, body), "openrouter", "")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to read response body", err)
	}

	var modelsResp struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Pricing     struct {
				Prompt         string `json:"prompt"`
				Completion     string `json:"completion"`
				InputCacheRead string `json:"input_cache_read"`
			} `json:"pricing"`
			ContextLength int `json:"context_length"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, agenterrors.NewConfig("failed to decode OpenRouter models", err)
	}

	// Convert to ModelInfo format
	models := make([]ModelInfo, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		modelInfo := ModelInfo{
			ID:            model.ID,
			Name:          model.Name,
			Description:   model.Description,
			Provider:      "openrouter",
			ContextLength: model.ContextLength,
		}

		// Parse pricing if available
		if model.Pricing.Prompt != "" {
			if promptCost, err := parseFloat(model.Pricing.Prompt); err == nil {
				modelInfo.InputCost = promptCost * 1000000 // Convert to per million tokens
			}
		}
		if model.Pricing.Completion != "" {
			if completionCost, err := parseFloat(model.Pricing.Completion); err == nil {
				modelInfo.OutputCost = completionCost * 1000000 // Convert to per million tokens
			}
		}
		if model.Pricing.InputCacheRead != "" {
			if cacheCost, err := parseFloat(model.Pricing.InputCacheRead); err == nil {
				modelInfo.CachedInputCost = cacheCost * 1000000
			}
		}

		if modelInfo.InputCost > 0 || modelInfo.OutputCost > 0 {
			modelInfo.Cost = (modelInfo.InputCost + modelInfo.OutputCost) / 2.0
		}

		models = append(models, modelInfo)
	}

	return models, nil
}

type deepInfraListModelsWrapper struct{}

func (w *deepInfraListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	// DeepInfra's native /models/list is public and far richer than its
	// OpenAI-compatible /v1/openai/models endpoint (which omits context size
	// and pricing): it reports max_tokens (context), per-token pricing,
	// capability tags, a type, and a deprecated flag. Listing must not require
	// a key — resolve best-effort and only attach when present.
	apiKey, _ := credentials.ResolveProviderAPIKey("deepinfra", "DeepInfra")

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.deepinfra.com/models/list", nil)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to create request", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to fetch DeepInfra models", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, agenterrors.NewProviderError("failed to fetch DeepInfra models", FormatHTTPResponseError(resp.StatusCode, resp.Header, body), "deepinfra", "")
	}

	// Decode as raw entries so pricing can be probed via ModelPricingPerMillion
	// (DeepInfra reports cents-per-token under pricing.cents_per_*_token).
	var entries []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, agenterrors.NewConfig("failed to decode DeepInfra models", err)
	}

	models := make([]ModelInfo, 0, len(entries))
	for _, e := range entries {
		// Skip deprecated entries and anything that isn't a chat/agentic text
		// model (image, audio, embedding, …).
		if deprecated, _ := e["deprecated"].(bool); deprecated {
			continue
		}
		modelType, _ := e["reported_type"].(string)
		if modelType == "" {
			modelType, _ = e["type"].(string)
		}
		if modelType != "text-generation" {
			continue
		}
		name, _ := e["model_name"].(string)
		if name == "" {
			continue
		}

		m := ModelInfo{ID: name, Name: name, Provider: "deepinfra"}
		if d, ok := e["description"].(string); ok {
			m.Description = d
		}
		if mt, ok := e["max_tokens"].(float64); ok {
			m.ContextLength = int(mt)
		}
		if tags, ok := e["tags"].([]any); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					m.Tags = append(m.Tags, s)
				}
			}
		}
		if in, out := ModelPricingPerMillion(e); in > 0 || out > 0 {
			m.InputCost, m.OutputCost = in, out
			m.Cost = (in + out) / 2.0
			m.CachedInputCost = ModelCachedPricingPerMillion(e)
		}
		models = append(models, m)
	}

	return models, nil
}

// lmStudioListModelsWrapper and the OpenAI-compat fallback live in
// lmstudio_models.go to keep this file under the 500-line ceiling.

type mistralListModelsWrapper struct{}

func (w *mistralListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	apiKey, err := credentials.ResolveProviderAPIKey("mistral", "Mistral")
	if err != nil {
		return nil, err
	}

	// Use OpenAI-compatible models endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.mistral.ai/v1/models", nil)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to create request", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to fetch Mistral models", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, agenterrors.NewProviderError("failed to fetch Mistral models", FormatHTTPResponseError(resp.StatusCode, resp.Header, body), "mistral", "")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to read response body", err)
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
		return nil, agenterrors.NewConfig("failed to decode Mistral models", err)
	}

	// Convert to ModelInfo format
	models := make([]ModelInfo, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		modelInfo := ModelInfo{
			ID:       model.ID,
			Name:     model.ID,
			Provider: "mistral",
		}

		// Use the context length defaults from the config (will be set by the provider)
		if strings.Contains(model.ID, "codestral") {
			modelInfo.Tags = []string{"tools", "coding"}
			modelInfo.ContextLength = 32768
		} else if strings.Contains(model.ID, "large") {
			modelInfo.Tags = []string{"tools"}
			modelInfo.ContextLength = 131072
		} else {
			modelInfo.Tags = []string{"tools"}
			modelInfo.ContextLength = 32768
		}

		models = append(models, modelInfo)
	}

	return models, nil
}

// Helper function to parse float from string
func parseFloat(s string) (float64, error) {
	// Remove any currency symbols and parse
	cleaned := strings.TrimPrefix(s, "$")
	return strconv.ParseFloat(cleaned, 64)
}
