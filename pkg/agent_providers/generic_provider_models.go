package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// generic_provider_models.go — model listing: backend detection and the
// per-backend (OpenAI / vLLM / llama.cpp) model lists, plus the cache and
// fallback. Split out of generic_provider.go.

// modelregistryFetchTimeout bounds the registry lookup in GetModelContextLimit to prevent stalling the agent loop.
const modelregistryFetchTimeout = 2 * time.Second

// ListModels returns available models, dispatching to a backend-specific fetcher (openai, vllm, llamacpp, or auto).
// Results are cached; subsequent calls return the cached list without re-fetching.
func (p *GenericProvider) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	p.mu.RLock()
	if p.modelsCached && len(p.models) > 0 {
		models := p.models
		p.mu.RUnlock()
		return models, nil
	}
	p.mu.RUnlock()

	backend := p.effectiveBackend()
	if backend == BackendAuto {
		backend = p.detectAndCacheBackend(ctx)
	}

	switch backend {
	case BackendVLLM:
		return p.listModelsVLLM(ctx)
	case BackendLlamaCPP:
		return p.listModelsLlamaCPP(ctx)
	default:
		return p.listModelsOpenAI(ctx)
	}
}

// effectiveBackend returns the backend type for model discovery (explicit config or auto-detected and cached).
func (p *GenericProvider) effectiveBackend() BackendType {
	if b := p.config.BackendResolved(); b != BackendAuto {
		return b
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.backendDetected {
		return p.detectedBackend
	}
	return BackendAuto
}

// detectAndCacheBackend probes the endpoint once, caches the result, and
// returns the detected BackendType.
func (p *GenericProvider) detectAndCacheBackend(ctx context.Context) BackendType {
	p.mu.RLock()
	if p.backendDetected {
		detected := p.detectedBackend
		p.mu.RUnlock()
		return detected
	}
	client := p.httpClient
	endpoint := p.config.Endpoint
	p.mu.RUnlock()

	detected := detectBackend(ctx, client, endpoint)

	p.mu.Lock()
	p.detectedBackend = detected
	p.backendDetected = true
	p.mu.Unlock()

	return detected
}

// listModelsOpenAI fetches models from the standard OpenAI-compatible /models endpoint.
func (p *GenericProvider) listModelsOpenAI(ctx context.Context) ([]api.ModelInfo, error) {
	var models []api.ModelInfo

	modelsEndpoint := strings.TrimSuffix(p.config.Endpoint, "/chat/completions") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", modelsEndpoint, nil)
	if err != nil {
		return p.fallbackToConfigOrCurrent()
	}

	token, err := p.config.GetAuthToken()
	if err != nil {
		if strings.Contains(p.config.Endpoint, "127.0.0.1") || strings.Contains(p.config.Endpoint, "localhost") {
			// No auth needed for local instances
		} else {
			return p.fallbackToConfigOrCurrent()
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	p.mu.RLock()
	client := p.httpClient
	p.mu.RUnlock()

	resp, err := client.Do(req)
	if err != nil {
		return p.fallbackToConfigOrCurrent()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return p.fallbackToConfigOrCurrent()
	}

	var modelsResponse struct {
		Data []struct {
			ID            string `json:"id"`
			Object        string `json:"object"`
			Created       int64  `json:"created"`
			OwnedBy       string `json:"owned_by"`
			ContextLength int    `json:"context_length,omitempty"`
			MaxModelLen   int    `json:"max_model_len,omitempty"`
			Pricing       *struct {
				Prompt     string `json:"prompt,omitempty"`
				Completion string `json:"completion,omitempty"`
			} `json:"pricing,omitempty"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, agenterrors.NewNetwork("failed to decode models response", err)
	}

	models = make([]api.ModelInfo, 0, len(modelsResponse.Data))
	for _, model := range modelsResponse.Data {
		modelInfo := api.ModelInfo{
			ID:       model.ID,
			Name:     model.ID,
			Provider: p.config.Name,
		}

		if model.ContextLength > 0 {
			modelInfo.ContextLength = model.ContextLength
		} else if model.MaxModelLen > 0 {
			modelInfo.ContextLength = model.MaxModelLen
		}

		if model.Pricing != nil {
			if promptCost, err := strconv.ParseFloat(model.Pricing.Prompt, 64); err == nil {
				modelInfo.InputCost = promptCost
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				modelInfo.OutputCost = completionCost
			}
		}

		if configModelInfo := p.config.GetModelInfo(model.ID); configModelInfo != nil {
			if configModelInfo.Name != "" {
				modelInfo.Name = configModelInfo.Name
			}
			if configModelInfo.Description != "" {
				modelInfo.Description = configModelInfo.Description
			}
			if len(configModelInfo.Tags) > 0 {
				modelInfo.Tags = configModelInfo.Tags
			}
		}

		// Use GetContextLimit for the full priority chain (model_overrides → pattern_overrides → model_info → default)
		if modelInfo.ContextLength <= 0 {
			modelInfo.ContextLength = p.config.GetContextLimit(model.ID)
		}

		models = append(models, modelInfo)
	}

	if len(models) == 0 {
		return p.fallbackToConfigOrCurrent()
	}

	p.setCachedModels(models)
	return models, nil
}

// listModelsVLLM fetches models from /models and enriches context length from vLLM's /get_model_info.
func (p *GenericProvider) listModelsVLLM(ctx context.Context) ([]api.ModelInfo, error) {
	p.mu.RLock()
	endpoint := p.config.Endpoint
	client := p.httpClient
	currentModel := p.model
	p.mu.RUnlock()

	token, _ := p.config.GetAuthToken()

	// Fetch context limit from vLLM-specific endpoint
	vllmCtx, hasVLLMCtx := fetchVLLMContextLimit(ctx, client, endpoint, token)

	// Fetch model list from OpenAI-compat /models
	rawModels, hasModels := fetchRawModelList(ctx, client, endpoint, token)

	if !hasVLLMCtx && !hasModels {
		return p.fallbackToConfigOrCurrent()
	}

	var models []api.ModelInfo

	if hasModels {
		models = make([]api.ModelInfo, 0, len(rawModels))
		for _, raw := range rawModels {
			mi := api.ModelInfo{
				ID:       raw.ID,
				Name:     raw.ID,
				Provider: p.config.Name,
			}
			// Prefer vLLM's get_model_info, then endpoint fields, then config
			if hasVLLMCtx {
				mi.ContextLength = vllmCtx
			} else if ctxLen := parseContextLengthFromRaw(raw); ctxLen > 0 {
				mi.ContextLength = ctxLen
			} else {
				mi.ContextLength = p.config.GetContextLimit(raw.ID)
			}
			models = append(models, mi)
		}
	} else if currentModel != "" {
		ctxLen := vllmCtx
		if ctxLen == 0 {
			ctxLen = p.config.GetContextLimit(currentModel)
		}
		models = []api.ModelInfo{{
			ID:            currentModel,
			Name:          currentModel,
			Provider:      p.config.Name,
			ContextLength: ctxLen,
		}}
	}

	if len(models) == 0 {
		return p.fallbackToConfigOrCurrent()
	}

	p.setCachedModels(models)
	return models, nil
}

// listModelsLlamaCPP fetches models from /models and enriches context length from llama.cpp's /props.
func (p *GenericProvider) listModelsLlamaCPP(ctx context.Context) ([]api.ModelInfo, error) {
	p.mu.RLock()
	endpoint := p.config.Endpoint
	client := p.httpClient
	currentModel := p.model
	p.mu.RUnlock()

	token, _ := p.config.GetAuthToken()

	// Fetch context limit from llama.cpp-specific endpoint
	llamaCtx, hasLlamaCtx := fetchLlamaCPPContextLimit(ctx, client, endpoint, token)

	// Fetch model list from OpenAI-compat /models
	rawModels, hasModels := fetchRawModelList(ctx, client, endpoint, token)

	if !hasLlamaCtx && !hasModels {
		return p.fallbackToConfigOrCurrent()
	}

	var models []api.ModelInfo

	if hasModels {
		models = make([]api.ModelInfo, 0, len(rawModels))
		for _, raw := range rawModels {
			mi := api.ModelInfo{
				ID:       raw.ID,
				Name:     raw.ID,
				Provider: p.config.Name,
			}
			if hasLlamaCtx {
				mi.ContextLength = llamaCtx
			} else if ctxLen := parseContextLengthFromRaw(raw); ctxLen > 0 {
				mi.ContextLength = ctxLen
			} else {
				mi.ContextLength = p.config.GetContextLimit(raw.ID)
			}
			models = append(models, mi)
		}
	} else if currentModel != "" {
		ctxLen := llamaCtx
		if ctxLen == 0 {
			ctxLen = p.config.GetContextLimit(currentModel)
		}
		models = []api.ModelInfo{{
			ID:            currentModel,
			Name:          currentModel,
			Provider:      p.config.Name,
			ContextLength: ctxLen,
		}}
	}

	if len(models) == 0 {
		return p.fallbackToConfigOrCurrent()
	}

	p.setCachedModels(models)
	return models, nil
}

// setCachedModels stores the fetched model list and marks the cache warm.
func (p *GenericProvider) setCachedModels(models []api.ModelInfo) {
	p.mu.Lock()
	p.models = models
	p.modelsCached = true
	p.mu.Unlock()
}

// fallbackToConfigOrCurrent returns config model_info or current model as fallback
func (p *GenericProvider) fallbackToConfigOrCurrent() ([]api.ModelInfo, error) {
	// First try to use config model_info
	if len(p.config.Models.ModelInfo) > 0 {
		models := make([]api.ModelInfo, len(p.config.Models.ModelInfo))
		for i, mi := range p.config.Models.ModelInfo {
			models[i] = api.ModelInfo{
				ID:            mi.ID,
				Name:          mi.Name,
				Description:   mi.Description,
				Provider:      p.config.Name,
				ContextLength: mi.ContextLength,
				Tags:          mi.Tags,
			}
		}
		p.setCachedModels(models)
		return models, nil
	}

	// Next try to use available_models list (legacy)
	if len(p.config.Models.AvailableModels) > 0 {
		models := make([]api.ModelInfo, len(p.config.Models.AvailableModels))
		for i, modelName := range p.config.Models.AvailableModels {
			// Try to enrich with config model_info
			modelInfo := api.ModelInfo{
				ID:       modelName,
				Name:     modelName,
				Provider: p.config.Name,
			}
			if configMi := p.config.GetModelInfo(modelName); configMi != nil {
				if configMi.Name != "" {
					modelInfo.Name = configMi.Name
				}
				if configMi.ContextLength > 0 {
					modelInfo.ContextLength = configMi.ContextLength
				}
				modelInfo.Description = configMi.Description
				modelInfo.Tags = configMi.Tags
			}
			if modelInfo.ContextLength <= 0 {
				modelInfo.ContextLength = p.config.GetContextLimit(modelName)
			}
			models[i] = modelInfo
		}
		p.setCachedModels(models)
		return models, nil
	}

	// Final fallback: return just the current model.
	p.mu.RLock()
	currentModel := p.model
	p.mu.RUnlock()
	models := []api.ModelInfo{{
		ID:            currentModel,
		Name:          currentModel,
		Provider:      p.config.Name,
		ContextLength: p.config.GetContextLimit(currentModel),
	}}
	p.setCachedModels(models)
	return models, nil
}

// SupportsVision is defined in generic_provider_vision.go
