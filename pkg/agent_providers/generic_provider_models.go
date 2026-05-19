package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func modelInfoHasVisionTag(modelInfo *ModelInfo) bool {
	if modelInfo == nil {
		return false
	}
	for _, tag := range modelInfo.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), "vision") {
			return true
		}
	}
	return false
}

// ListModels returns available models
// Priority:
// 1. Fetch from provider API models endpoint (primary source of truth)
// 2. Enrich endpoint data with config (context_length, tags, name)
// 3. Fall back to config model_info if endpoint fails
// 4. Final fallback: return just current model
func (p *GenericProvider) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	if p.modelsCached && len(p.models) > 0 {
		return p.models, nil
	}

	var models []api.ModelInfo

	// Try to fetch models from provider API (OpenAI-compatible endpoint)
	modelsEndpoint := strings.TrimSuffix(p.config.Endpoint, "/chat/completions") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", modelsEndpoint, nil)
	if err != nil {
		// Endpoint construction failed, skip to fallback
		return p.fallbackToConfigOrCurrent()
	}

	token, err := p.config.GetAuthToken()
	if err != nil {
		// For local instances like LM Studio, skip auth if no token is configured
		if strings.Contains(p.config.Endpoint, "127.0.0.1") || strings.Contains(p.config.Endpoint, "localhost") {
			// No auth needed for local instances
		} else {
			// Auth failed and not local, skip to fallback
			return p.fallbackToConfigOrCurrent()
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add custom headers
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Request failed, skip to fallback
		return p.fallbackToConfigOrCurrent()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Models endpoint not available or failed, use fallback
		return p.fallbackToConfigOrCurrent()
	}

	var modelsResponse struct {
		Data []struct {
			ID            string `json:"id"`
			Object        string `json:"object"`
			Created       int64  `json:"created"`
			OwnedBy       string `json:"owned_by"`
			ContextLength int    `json:"context_length,omitempty"`
			Pricing       *struct {
				Prompt     string `json:"prompt,omitempty"`
				Completion string `json:"completion,omitempty"`
			} `json:"pricing,omitempty"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	// Build model list from endpoint data, enriched with config
	models = make([]api.ModelInfo, 0, len(modelsResponse.Data))
	for _, model := range modelsResponse.Data {
		modelInfo := api.ModelInfo{
			ID:       model.ID,
			Name:     model.ID,
			Provider: p.config.Name,
		}

		// Use context_length from endpoint if available
		if model.ContextLength > 0 {
			modelInfo.ContextLength = model.ContextLength
		}

		// Enrich with pricing info if available from endpoint
		if model.Pricing != nil {
			if promptCost, err := strconv.ParseFloat(model.Pricing.Prompt, 64); err == nil {
				modelInfo.InputCost = promptCost
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				modelInfo.OutputCost = completionCost
			}
		}

		// Enrich with config model_info data (context_length, tags, name, description)
		if configModelInfo := p.config.GetModelInfo(model.ID); configModelInfo != nil {
			// Only override if endpoint didn't provide context_length
			if modelInfo.ContextLength <= 0 && configModelInfo.ContextLength > 0 {
				modelInfo.ContextLength = configModelInfo.ContextLength
			}
			// Override name/description from config
			if configModelInfo.Name != "" {
				modelInfo.Name = configModelInfo.Name
			}
			if configModelInfo.Description != "" {
				modelInfo.Description = configModelInfo.Description
			}
			// Add tags from config
			if len(configModelInfo.Tags) > 0 {
				modelInfo.Tags = configModelInfo.Tags
			}
		}

		// If still no context_length, use config fallback
		if modelInfo.ContextLength <= 0 {
			modelInfo.ContextLength = p.config.GetContextLimit(model.ID)
		}

		models = append(models, modelInfo)
	}

	// If we got no models from endpoint, use fallback
	if len(models) == 0 {
		return p.fallbackToConfigOrCurrent()
	}

	p.models = models
	p.modelsCached = true
	return p.models, nil
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
		p.models = models
		p.modelsCached = true
		return p.models, nil
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
		p.models = models
		p.modelsCached = true
		return p.models, nil
	}

	// Final fallback: return just the current model
	p.models = []api.ModelInfo{{
		ID:            p.model,
		Name:          p.model,
		Provider:      p.config.Name,
		ContextLength: p.config.GetContextLimit(p.model),
	}}
	p.modelsCached = true
	return p.models, nil
}

// SupportsVision returns whether the provider supports vision
func (p *GenericProvider) SupportsVision() bool {
	if !p.config.Models.SupportsVision {
		return false
	}

	currentModel := strings.TrimSpace(p.model)
	if currentModel == "" {
		currentModel = strings.TrimSpace(p.config.Defaults.Model)
	}
	if currentModel == "" {
		return false
	}

	if modelInfoHasVisionTag(p.config.GetModelInfo(currentModel)) {
		return true
	}

	visionModel := strings.TrimSpace(p.config.Models.VisionModel)
	if visionModel != "" && strings.EqualFold(currentModel, visionModel) {
		return true
	}

	return false
}

// GetVisionModel returns the vision model
func (p *GenericProvider) GetVisionModel() string {
	if p.config.Models.VisionModel != "" {
		return p.config.Models.VisionModel
	}
	return p.model // Fallback to current model
}

// SendVisionRequest sends a vision request (for providers that support it)
func (p *GenericProvider) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	if !p.SupportsVision() {
		return nil, fmt.Errorf("provider %s does not support vision", p.config.Name)
	}

	// Use vision model if specified
	visionModel := p.GetVisionModel()
	if visionModel != p.model {
		originalModel := p.model
		p.model = visionModel
		defer func() { p.model = originalModel }()
	}

	return p.SendChatRequest(messages, tools, reasoning, disableThinking)
}
