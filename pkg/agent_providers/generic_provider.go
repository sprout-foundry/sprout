package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
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

const maxProviderErrorBodyPreview = 240

func formatProviderHTTPError(statusCode int, headers http.Header, body []byte) error {
	message := summarizeProviderHTTPError(statusCode, headers, body)
	if message == "" {
		// Include response headers when body is empty — providers like ZAI
		// sometimes return error info only in headers (e.g. X-Error-Code).
		hdr := formatResponseHeaders(headers)
		if hdr != "" {
			return fmt.Errorf("HTTP %d (empty body, headers: %s)", statusCode, hdr)
		}
		return fmt.Errorf("HTTP %d", statusCode)
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, message)
}

func formatResponseHeaders(headers http.Header) string {
	// Collect known error headers first
	var parts []string
	for _, key := range []string{
		"Content-Type", "X-Error-Code", "X-Error-Message",
		"X-Request-Id", "X-Trace-Id", "Error-Code",
		"X-Status", "Retry-After",
	} {
		if v := headers.Get(key); v != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", key, v))
		}
	}
	// If no known error headers found, include all headers for diagnosis
	if len(parts) == 0 {
		for key, vals := range headers {
			for _, v := range vals {
				parts = append(parts, fmt.Sprintf("%s=%s", key, v))
			}
		}
	}
	return strings.Join(parts, ", ")
}

func summarizeProviderHTTPError(statusCode int, headers http.Header, body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	if jsonMsg := extractProviderJSONErrorMessage(body); jsonMsg != "" {
		return limitProviderErrorText(jsonMsg)
	}

	if looksLikeProviderHTMLErrorPage(headers, trimmed) {
		return summarizeProviderHTMLErrorPage(statusCode, trimmed)
	}

	return limitProviderErrorText(trimmed)
}

func extractProviderJSONErrorMessage(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(extractProviderJSONErrorField(payload))
}

func extractProviderJSONErrorField(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]interface{}:
		for _, key := range []string{"error", "message", "detail", "details", "title", "reason"} {
			if msg := extractProviderJSONErrorField(typed[key]); msg != "" {
				return msg
			}
		}
	case []interface{}:
		for _, item := range typed {
			if msg := extractProviderJSONErrorField(item); msg != "" {
				return msg
			}
		}
	}
	return ""
}

func looksLikeProviderHTMLErrorPage(headers http.Header, body string) bool {
	contentType := strings.ToLower(headers.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		return true
	}

	lowerBody := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(lowerBody, "<!doctype html") ||
		strings.HasPrefix(lowerBody, "<html") ||
		strings.Contains(lowerBody, "<title>")
}

func summarizeProviderHTMLErrorPage(statusCode int, body string) string {
	lowerBody := strings.ToLower(body)
	if strings.Contains(lowerBody, "cloudflare") {
		switch {
		case statusCode == 524 || strings.Contains(lowerBody, "error code 524"):
			return "upstream timeout (Cloudflare 524 HTML error page)"
		case statusCode >= 520 && statusCode <= 527:
			return fmt.Sprintf("gateway error (Cloudflare %d HTML error page)", statusCode)
		default:
			return "gateway error (Cloudflare HTML error page)"
		}
	}

	if title := extractProviderHTMLTitle(body); title != "" {
		return fmt.Sprintf("%s (HTML error page)", limitProviderErrorText(title))
	}

	if statusCode == http.StatusGatewayTimeout {
		return "upstream timeout (HTML error page)"
	}

	return "received HTML error page from provider"
}

func extractProviderHTMLTitle(body string) string {
	lowerBody := strings.ToLower(body)
	start := strings.Index(lowerBody, "<title>")
	if start == -1 {
		return ""
	}
	start += len("<title>")
	end := strings.Index(lowerBody[start:], "</title>")
	if end == -1 {
		return ""
	}
	title := html.UnescapeString(body[start : start+end])
	return strings.TrimSpace(strings.Join(strings.Fields(title), " "))
}

func limitProviderErrorText(text string) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return ""
	}
	if len(text) <= maxProviderErrorBodyPreview {
		return text
	}
	return text[:maxProviderErrorBodyPreview-3] + "..."
}

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

// NewGenericProvider creates a new generic provider from configuration
func NewGenericProvider(config *ProviderConfig) (*GenericProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid provider config: %w", err)
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
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}

	req, err := p.buildHTTPRequestCtx(ctx, requestBody, false)
	if err != nil {
		// Log request on build error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "build_http_request", err)
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Log request on HTTP error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "http_request_failed", err)
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Compatibility fallback for OpenAI-compatible backends that require
		// max_completion_tokens instead of max_tokens for certain models.
		retryBody, retryResp, retried, retryErr := p.tryMaxCompletionTokensRetry(requestBody, false, body)
		if retried {
			requestBody = retryBody
			if retryErr != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false,
					"retry_max_completion_tokens_build", retryErr)
				return nil, fmt.Errorf("failed retry with max_completion_tokens: %w", retryErr)
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
				return nil, fmt.Errorf("failed to decode response: %w", err)
			}
			return retryResponse, nil
		}

		// Log request on API error
		formattedErr := formatProviderHTTPError(resp.StatusCode, resp.Header, body)
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false,
			fmt.Sprintf("api_error_%d", resp.StatusCode), formattedErr)
		return nil, formattedErr
	}
	defer resp.Body.Close()

	response, err := decodeChatResponseWithCost(resp.Body)
	if err != nil {
		// Log request on decode error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "decode_response", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
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
		return fmt.Errorf("check connection: failed to ensure model: %w", err)
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
		return fmt.Errorf("check connection: test request failed: %w", err)
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
		return fmt.Errorf("failed to re-resolve API key for %q: %w", p.config.Name, err)
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
	//    → default_context_limit → conservative 32k).
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

// SupportsVision is defined in generic_provider_vision.go

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
