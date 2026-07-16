package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/logging"
	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// GenericProvider implements ClientInterface using JSON configuration
type GenericProvider struct {
	config          *ProviderConfig
	httpClient      *http.Client
	streamingClient *http.Client
	debug           bool
	model           string
	models          []api.ModelInfo
	modelsCached    bool
}

// HTTP error formatting helpers are in generic_provider_http_errors.go:
//   maxProviderErrorBodyPreview, formatProviderHTTPError, formatResponseHeaders,
//   summarizeProviderHTTPError, extractProviderJSONErrorMessage,
//   extractProviderJSONErrorField, looksLikeProviderHTMLErrorPage,
//   summarizeProviderHTMLErrorPage, extractProviderHTMLTitle,
//   limitProviderErrorText, modelInfoHasVisionTag

// NewGenericProvider creates a new generic provider from configuration
func NewGenericProvider(config *ProviderConfig) (*GenericProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, agenterrors.NewValidation(fmt.Sprintf("invalid provider config: %v", err), nil)
	}

	timeout := config.GetTimeout()
	streamingTimeout := config.GetStreamingTimeout()

	return &GenericProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		streamingClient: &http.Client{
			Timeout: streamingTimeout,
		},
		debug: false,
		model: config.Defaults.Model,
	}, nil
}

// SendChatRequest sends a non-streaming chat request
func (p *GenericProvider) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	requestBody, err := p.buildChatRequest(messages, tools, reasoning, disableThinking, false)
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to build chat request")
	}

	req, sentBody, err := p.buildHTTPRequestCtx(ctx, requestBody, false)
	if err != nil {
		// Log request on build error — use the actual sent body (post-redaction)
		logging.LogRequestPayloadOnError(sentBody, p.config.Name, p.model, false, "build_http_request", err)
		return nil, agenterrors.Wrap(err, "failed to build HTTP request")
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Log request on HTTP error
		logging.LogRequestPayloadOnError(sentBody, p.config.Name, p.model, false, "http_request_failed", err)
		return nil, agenterrors.NewNetwork("HTTP request failed", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Compatibility fallback for OpenAI-compatible backends that require
		// max_completion_tokens instead of max_tokens for certain models.
		retryBody, retryResp, retried, retryErr := p.tryMaxCompletionTokensRetry(sentBody, false, body)
		if retried {
			requestBody = retryBody
			if retryErr != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false,
					"retry_max_completion_tokens_build", retryErr)
				return nil, agenterrors.NewNetwork("failed retry with max_completion_tokens", retryErr)
			}
			defer retryResp.Body.Close()
			if retryResp.StatusCode != http.StatusOK {
				retryErrBody, _ := io.ReadAll(retryResp.Body)
				formattedErr := formatProviderHTTPError(retryResp.StatusCode, retryResp.Header, retryErrBody)
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false,
					fmt.Sprintf("api_error_%d", retryResp.StatusCode), formattedErr)
				return nil, formattedErr
			}

			retryResponse, err := decodeChatResponseWithCost(retryResp.Body)
			if err != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "decode_response", err)
				return nil, agenterrors.NewNetwork("failed to decode response", err)
			}
			return retryResponse, nil
		}

		// Log request on API error — use the actual sent body (post-redaction)
		formattedErr := formatProviderHTTPError(resp.StatusCode, resp.Header, body)
		logging.LogRequestPayloadOnError(sentBody, p.config.Name, p.model, false,
			fmt.Sprintf("api_error_%d", resp.StatusCode), formattedErr)
		return nil, formattedErr
	}
	defer resp.Body.Close()

	response, err := decodeChatResponseWithCost(resp.Body)
	if err != nil {
		// Log request on decode error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "decode_response", err)
		return nil, agenterrors.NewNetwork("failed to decode response", err)
	}

	// Success - don't log the request
	return response, nil
}

// decodeChatResponseWithCost decodes a chat-completion body into the typed
// ChatResponse, then — when no cost arrived via the canonical typed fields —
// probes the raw JSON for a cost reported under a differently-named property
// (see api.CostFromJSON). This keeps cost capture working across providers
// that report cost under non-standard property names.
func decodeChatResponseWithCost(r io.Reader) (*api.ChatResponse, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var response api.ChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if api.UsageCost(response.Usage) == 0 {
		if cost, ok := api.CostFromJSON(body); ok {
			response.Usage.EstimatedCost = cost
		}
	}
	return &response, nil
}

// SendChatRequestStream is defined in generic_provider_streaming.go

// CheckConnection tests provider connection with current model
func (p *GenericProvider) CheckConnection() error {
	if err := p.ensureModel(); err != nil {
		return agenterrors.NewNetwork("check connection: failed to ensure model", err)
	}

	// Send a minimal test request to verify the model works
	testMessages := []api.Message{
		{
			Role:    "user",
			Content: "Hi",
		},
	}

	_, err := p.SendChatRequest(context.Background(), testMessages, nil, "", false)
	if err != nil {
		return agenterrors.NewNetwork("check connection: test request failed", err)
	}
	return nil
}

// SetDebug enables or disables debug mode
func (p *GenericProvider) SetDebug(debug bool) {
	p.debug = debug
}

// SetModel sets the current model
func (p *GenericProvider) SetModel(model string) error {
	p.model = model
	p.modelsCached = false
	return nil
}

// SetHTTPClient sets the HTTP client used for non-streaming requests.
func (p *GenericProvider) SetHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	p.httpClient = c
}

// SetStreamingClient sets the HTTP client used for streaming requests.
func (p *GenericProvider) SetStreamingClient(c *http.Client) {
	if c == nil {
		return
	}
	p.streamingClient = c
}

// RefreshAPIKey re-resolves the provider's API key from the credential store,
// updating the cached key in p.config.Auth.Key. This is called after a rate-limit
// rotation advances the key pool counter, so subsequent requests use the new key.
func (p *GenericProvider) RefreshAPIKey() error {
	if p.config == nil {
		return nil
	}
	if p.config.Auth.Type == "none" {
		return nil
	}

	resolved, err := credentials.ResolveProvider(p.config.Name)
	if err != nil {
		return agenterrors.NewNetwork(fmt.Sprintf("failed to re-resolve API key for %q", p.config.Name), err)
	}

	p.config.Auth.Key = resolved.Value
	return nil
}

// GetModel returns the current model
func (p *GenericProvider) GetModel() string {
	return p.model
}

// GetProvider returns the provider name
func (p *GenericProvider) GetProvider() string {
	return p.config.Name
}

// GetHTTPClient returns the current HTTP client used for non-streaming requests.
// Useful for WASM environments that need to verify client injection.
func (p *GenericProvider) GetHTTPClient() *http.Client {
	return p.httpClient
}

// GetStreamingClient returns the current HTTP client used for streaming requests.
// Useful for WASM environments that need to verify client injection.
func (p *GenericProvider) GetStreamingClient() *http.Client {
	return p.streamingClient
}

// GetModelContextLimit returns the context limit for the current model
func (p *GenericProvider) GetModelContextLimit() (int, error) {
	// 1. If ListModels() has been called and cached a context length for this
	//    model, use it — it came from the provider's own API.
	if p.modelsCached {
		for _, model := range p.models {
			if model.ID == p.model && model.ContextLength > 0 {
				return model.ContextLength, nil
			}
		}
	}

	// 2. Consult the published model registry (canonical per-provider files at
	//    sprout-foundry.github.io). These carry the exact context window that
	//    the refresh workflow fetched from the provider's API, which is more
	//    accurate than the static config defaults below. Best-effort: a fetch
	//    failure or cache miss silently falls through to the config.
	if p.model != "" {
		ctx, cancel := context.WithTimeout(context.Background(), modelregistryFetchTimeout)
		defer cancel()
		if rawModels, err := modelregistry.FetchModels(ctx, p.config.Name); err == nil {
			for _, rm := range rawModels {
				if rm.ID == p.model && rm.ContextLength > 0 {
					return rm.ContextLength, nil
				}
			}
		}
	}

	// 3. Fall back to the static config (model_overrides → pattern_overrides
	//    → model_info → default_context_limit → legacy context_limit → 32k).
	return p.config.GetContextLimit(p.model), nil
}

// modelregistryFetchTimeout bounds the registry lookup in
// GetModelContextLimit so a slow network can't stall the agent loop.
const modelregistryFetchTimeout = 2 * time.Second

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
		return nil, agenterrors.NewNetwork("failed to decode models response", err)
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

// SupportsVision is defined in generic_provider_vision.go

// VisionCapabilities returns the per-provider vision limits for this
// GenericProvider (OpenAI-compatible). When p.config is nil (a defensive
// guard for unit tests that construct a bare struct), returns the safe
// defaults via VisionCapabilitiesDefault(). Otherwise returns the OpenAI
// tier:
//
//	20MB per image, 500 images per request, 2048px longest side,
//	detail tiers {low, high, auto}.
//
// The OpenAI tier is the conservative pick for all GenericProvider
// backends (Anthropic, OpenRouter, Chutes, etc.). Per-provider override
// (e.g. tighter Anthropic caps) can land later by switching on
// p.config.Name; keeping a single safe value here avoids silent
// variation while the SP-103-B2 / SP-103-D2 wiring stabilises.
// SP-103-D3 / AUDIT-GAP-2.
func (p *GenericProvider) VisionCapabilities() api.VisionCapabilities {
	if p.config == nil {
		return api.VisionCapabilitiesDefault()
	}
	switch p.config.Name {
	case "anthropic":
		// Anthropic's documented limits: ~5MB per image, auto-resizes to
		// 1568px on the longest side, no hard image-count limit (20 is
		// a safe practical cap).
		return api.VisionCapabilities{
			MaxImageBytes:     5_000_000,
			MaxImageCount:     20,
			MaxImageDimension: 1568,
		}
	case "openai":
		// OpenAI's gpt-4o: ~20MB per image, supports low/high/auto detail
		// tiers, up to 500 images in some endpoints (10 is a safe
		// practical cap for most use-cases).
		return api.VisionCapabilities{
			MaxImageBytes:     20_000_000,
			MaxImageCount:     10,
			MaxImageDimension: 2048,
			DetailTiers:       []string{"low", "high", "auto"},
		}
	default:
		// Conservative defaults for all other OpenAI-compatible backends
		// (OpenRouter, Chutes, LM Studio, etc.).
		return api.VisionCapabilities{
			MaxImageBytes:     20_000_000,
			MaxImageCount:     500,
			MaxImageDimension: 2048,
			DetailTiers:       []string{"low", "high", "auto"},
		}
	}
}

// TPS tracking methods - simplified for now
func (p *GenericProvider) GetLastTPS() float64 {
	return 0.0
}

func (p *GenericProvider) GetAverageTPS() float64 {
	return 0.0
}

func (p *GenericProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{}
}

func (p *GenericProvider) ResetTPSStats() {
	// No-op for now
}

// --- Functions split into separate files ---
//
// generic_provider_http_errors.go:
//   maxProviderErrorBodyPreview, formatProviderHTTPError, formatResponseHeaders,
//   summarizeProviderHTTPError, extractProviderJSONErrorMessage,
//   extractProviderJSONErrorField, looksLikeProviderHTMLErrorPage,
//   summarizeProviderHTMLErrorPage, extractProviderHTMLTitle,
//   limitProviderErrorText, modelInfoHasVisionTag
//
// generic_provider_request.go:
//   buildChatRequest, applyReasoningEffort, applyDisableThinking,
//   ensureModel, applyModelSpecificSettings, shouldRetryWithMaxCompletionTokens,
//   rewriteMaxTokensToMaxCompletionTokens, buildHTTPRequest, buildHTTPRequestCtx
//
// generic_provider_streaming.go:
//   SendChatRequestStream, handleStreamingResponse
//
// generic_provider_vision.go:
//   SupportsVision, GetVisionModel, SendVisionRequest,
//   buildMultiModalContent, buildImageURL
//
// generic_provider_messages.go:
//   convertMessages, shouldSkipReasoningContentHistory, convertToolCalls,
//   getModelCompletionLimit
//
// generic_provider_retry.go:
//   tryMaxCompletionTokensRetry
