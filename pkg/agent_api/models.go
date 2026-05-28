package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/modelregistry"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

const maxHTTPErrorBodyPreview = 240

// FormatHTTPResponseError converts an HTTP error response into a concise,
// user-facing error that avoids dumping full HTML or JSON payloads.
func FormatHTTPResponseError(statusCode int, headers http.Header, body []byte) error {
	message := summarizeHTTPResponseError(statusCode, headers, body)
	if message == "" {
		return fmt.Errorf("HTTP %d", statusCode)
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, message)
}

func summarizeHTTPResponseError(statusCode int, headers http.Header, body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	if jsonMsg := extractHTTPJSONErrorMessage(body); jsonMsg != "" {
		return limitHTTPErrorText(jsonMsg)
	}

	if looksLikeHTMLErrorPage(headers, trimmed) {
		return summarizeHTMLErrorPage(statusCode, trimmed)
	}

	return limitHTTPErrorText(trimmed)
}

func extractHTTPJSONErrorMessage(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(extractHTTPJSONErrorField(payload))
}

func extractHTTPJSONErrorField(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]interface{}:
		for _, key := range []string{"error", "message", "detail", "details", "title", "reason"} {
			if msg := extractHTTPJSONErrorField(typed[key]); msg != "" {
				return msg
			}
		}
	case []interface{}:
		for _, item := range typed {
			if msg := extractHTTPJSONErrorField(item); msg != "" {
				return msg
			}
		}
	}
	return ""
}

func looksLikeHTMLErrorPage(headers http.Header, body string) bool {
	contentType := strings.ToLower(headers.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		return true
	}

	lowerBody := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(lowerBody, "<!doctype html") ||
		strings.HasPrefix(lowerBody, "<html") ||
		strings.Contains(lowerBody, "<title>")
}

func summarizeHTMLErrorPage(statusCode int, body string) string {
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

	if title := extractHTMLTitle(body); title != "" {
		return fmt.Sprintf("%s (HTML error page)", limitHTTPErrorText(title))
	}

	if statusCode == http.StatusGatewayTimeout {
		return "upstream timeout (HTML error page)"
	}

	return "received HTML error page"
}

func extractHTMLTitle(body string) string {
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

func limitHTTPErrorText(text string) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return ""
	}
	if len(text) <= maxHTTPErrorBodyPreview {
		return text
	}
	return text[:maxHTTPErrorBodyPreview-3] + "..."
}

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
	// EligibleRoles lists the agentic roles a model meets the minimum
	// deterministic bar for ("primary", "subagent"). This is an
	// eligibility pre-filter (currently context-window based), NOT a
	// quality recommendation — the capability probe provides the
	// authoritative agentic-capable signal. Empty means below the bar or
	// unknown. Additive/omitempty so older clients ignore it.
	EligibleRoles []string `json:"eligible_roles,omitempty"`
}

// Agentic-coding eligibility thresholds. These set the *minimum* context
// window for a role and are a deterministic placeholder for the capability
// probe (which will provide the authoritative agentic-capable signal).
// Eligibility ≠ recommendation.
const (
	subagentMinContext = 32_000
	primaryMinContext  = 128_000
)

// ClassifyEligibleRoles returns the agentic roles a model meets the minimum
// deterministic bar for, based on its context window. Returns nil when the
// model is below the subagent threshold or its context length is unknown.
func ClassifyEligibleRoles(m ModelInfo) []string {
	switch {
	case m.ContextLength >= primaryMinContext:
		return []string{"primary", "subagent"}
	case m.ContextLength >= subagentMinContext:
		return []string{"subagent"}
	default:
		return nil
	}
}

// fillEligibleRoles populates EligibleRoles for any model that doesn't already
// carry it, so live-API models get the heuristic while registry- or
// probe-provided roles are preserved.
func fillEligibleRoles(models []ModelInfo) []ModelInfo {
	for i := range models {
		if len(models[i].EligibleRoles) == 0 {
			models[i].EligibleRoles = ClassifyEligibleRoles(models[i])
		}
	}
	return models
}

// ModelsListInterface defines methods for listing available models
type ModelsListInterface interface {
	ListAvailableModels() ([]ModelInfo, error)
	GetDefaultModel() string
	IsModelAvailable(modelID string) bool
}

// GetAvailableModels returns available models for the current provider
func GetAvailableModels() ([]ModelInfo, error) {
	// Use unified provider detection
	clientType, err := DetermineProvider("", "")
	if err != nil {
		// Fallback to a reasonable default
		clientType = OllamaLocalClientType
	}
	return GetModelsForProvider(clientType)
}

// GetModelsForProvider returns available models for a specific provider
func GetModelsForProvider(clientType ClientType) ([]ModelInfo, error) {
	return GetModelsForProviderCtx(context.Background(), clientType)
}

// GetModelsForProviderCtx returns available models for a specific provider with context support.
// It checks the model registry first (if enabled), falling back to direct per-provider API calls.
func GetModelsForProviderCtx(ctx context.Context, clientType ClientType) ([]ModelInfo, error) {
	// Try the model registry first — fast, cached, no API key required.
	// Only attempt registry fetch for known providers in the catalog to avoid unnecessary network requests.
	providerID := string(clientType)
	if _, exists := providercatalog.FindProvider(providerID); exists {
		if registryModels, err := modelregistry.FetchModels(ctx, providerID); err == nil && registryModels != nil {
			return fillEligibleRoles(convertRegistryModels(registryModels)), nil
		}
	}

	// Canonical adapters are the source of truth for live listing where one
	// exists — they normalize the provider's native API into the canonical
	// contract (capabilities, pricing, context, lifecycle) before projecting
	// down to ModelInfo for existing consumers.
	if canon, handled, listErr := canonicalAdapterModels(ctx, providerID); handled {
		if listErr != nil {
			return nil, fmt.Errorf("failed to list models for %s: %w", clientType, listErr)
		}
		modelcontract.FillEligibleRoles(canon)
		out := make([]ModelInfo, len(canon))
		for i := range canon {
			out[i] = CanonicalToModelInfo(canon[i])
		}
		return out, nil
	}

	// Fall back to the provider's direct ListModels method.
	provider, err := createProviderForType(clientType)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider for %s: %w", clientType, err)
	}

	if provider == nil {
		return nil, fmt.Errorf("provider %s does not support model listing", clientType)
	}

	models, listErr := provider.ListModels(ctx)
	if listErr != nil {
		return nil, fmt.Errorf("failed to list models for %s: %w", clientType, listErr)
	}

	return fillEligibleRoles(models), nil
}

// openRouterReference lazily builds (once per process) the OpenRouter-derived
// reference catalog used to enrich providers with sparse native metadata (e.g.
// OpenAI). Best-effort: a fetch failure yields nil and enrichment is skipped.
var (
	openRouterRefOnce sync.Once
	openRouterRefCat  *modelcontract.ReferenceCatalog
)

func openRouterReference(ctx context.Context) *modelcontract.ReferenceCatalog {
	openRouterRefOnce.Do(func() {
		canon, err := (modelcontract.OpenRouterAdapter{}).ListModels(ctx)
		if err == nil && len(canon) > 0 {
			openRouterRefCat = modelcontract.NewReferenceCatalog(canon)
		}
	})
	return openRouterRefCat
}

// canonicalAdapterModels returns canonical models from a provider's adapter,
// injecting any dependencies it needs (OpenAI requires an API key and the
// OpenRouter reference catalog). The bool reports whether an adapter handled
// the provider; providers without one fall back to the legacy ListModels path.
func canonicalAdapterModels(ctx context.Context, providerID string) ([]modelcontract.CanonicalModel, bool, error) {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "deepinfra":
		m, err := modelcontract.DeepInfraAdapter{}.ListModels(ctx)
		return m, true, err
	case "openrouter":
		m, err := modelcontract.OpenRouterAdapter{}.ListModels(ctx)
		return m, true, err
	case "openai":
		apiKey, _ := credentials.ResolveProviderAPIKey("openai", "OpenAI")
		m, err := modelcontract.OpenAIAdapter{APIKey: apiKey, Reference: openRouterReference(ctx)}.ListModels(ctx)
		return m, true, err
	default:
		return nil, false, nil
	}
}

// CanonicalToModelInfo projects a canonical model down to the ModelInfo shape
// existing consumers expect. Known-true capabilities are surfaced as Tags so
// callers that inspect tags (e.g. the CLI "Supports tools" line) work unchanged.
// Exported for the registry publisher (cmd/refresh_provider_catalog).
func CanonicalToModelInfo(m modelcontract.CanonicalModel) ModelInfo {
	mi := ModelInfo{
		ID:            m.ID,
		Name:          m.DisplayName,
		Description:   m.Description,
		Provider:      m.Provider,
		ContextLength: m.ContextWindow,
		EligibleRoles: m.EligibleRoles,
		Tags:          modelcontract.CapabilityTags(m.Capabilities),
	}
	if mi.Name == "" {
		mi.Name = m.ID
	}
	if m.Pricing != nil {
		mi.InputCost = m.Pricing.InputPerMTok
		mi.OutputCost = m.Pricing.OutputPerMTok
		if mi.InputCost > 0 || mi.OutputCost > 0 {
			mi.Cost = (mi.InputCost + mi.OutputCost) / 2.0
		}
	}
	return mi
}

// modelInfoToCanonical projects a legacy ModelInfo up to the canonical shape.
// Used for providers that don't yet have a canonical adapter, so the publisher
// can emit a uniform canonical file. Capabilities are recovered from Tags
// (known-true on presence; unknown otherwise).
func modelInfoToCanonical(m ModelInfo) modelcontract.CanonicalModel {
	cm := modelcontract.CanonicalModel{
		ID:            m.ID,
		Provider:      m.Provider,
		DisplayName:   m.Name,
		Description:   m.Description,
		ContextWindow: m.ContextLength,
		Status:        modelcontract.StatusActive,
		Capabilities:  modelcontract.CapabilitiesFromTags(m.Tags),
		EligibleRoles: m.EligibleRoles,
	}
	if m.InputCost > 0 || m.OutputCost > 0 {
		cm.Pricing = &modelcontract.Pricing{
			InputPerMTok:  m.InputCost,
			OutputPerMTok: m.OutputCost,
			Currency:      "USD",
		}
	}
	return cm
}

// GetCanonicalModelsForProvider returns canonical models for a provider — from
// its adapter where one exists, otherwise by projecting the legacy ModelInfo
// path up to canonical. Used by the registry publisher to emit the canonical
// per-provider file.
func GetCanonicalModelsForProvider(ctx context.Context, clientType ClientType) ([]modelcontract.CanonicalModel, error) {
	if canon, handled, err := canonicalAdapterModels(ctx, string(clientType)); handled {
		if err != nil {
			return nil, fmt.Errorf("failed to list models for %s: %w", clientType, err)
		}
		modelcontract.FillEligibleRoles(canon)
		return canon, nil
	}
	models, err := GetModelsForProviderCtx(ctx, clientType)
	if err != nil {
		return nil, err
	}
	canon := make([]modelcontract.CanonicalModel, len(models))
	for i := range models {
		canon[i] = modelInfoToCanonical(models[i])
	}
	modelcontract.FillEligibleRoles(canon)
	return canon, nil
}

// convertRegistryModels converts modelregistry.RawModel slices to ModelInfo.
func convertRegistryModels(raw []modelregistry.RawModel) []ModelInfo {
	out := make([]ModelInfo, len(raw))
	for i, m := range raw {
		out[i] = ModelInfo{
			ID:            m.ID,
			Name:          m.Name,
			Description:   m.Description,
			Provider:      m.Provider,
			Size:          m.Size,
			Cost:          m.Cost,
			InputCost:     m.InputCost,
			OutputCost:    m.OutputCost,
			ContextLength: m.ContextLength,
			Tags:          append([]string(nil), m.Tags...),
			EligibleRoles: append([]string(nil), m.EligibleRoles...),
		}
	}
	return out
}

// createProviderForType creates a provider instance for the given client type
func createProviderForType(clientType ClientType) (interface {
	ListModels(context.Context) ([]ModelInfo, error)
}, error) {
	switch clientType {
	case OllamaClientType, OllamaLocalClientType:
		client, err := NewOllamaLocalClient("llama3.1:8b") // Use an available model
		if err != nil {
			return nil, fmt.Errorf("failed to create Ollama local client: %w", err)
		}
		return &ollamaLocalListModelsWrapper{client: client}, nil
	case OllamaTurboClientType:
		return &genericConfigListModelsWrapper{providerName: "ollama-turbo"}, nil
	case OpenAIClientType:
		return &genericConfigListModelsWrapper{providerName: "openai"}, nil
	case OpenRouterClientType:
		// Create OpenRouter wrapper that uses the provider's ListModels directly
		return &openRouterListModelsWrapper{}, nil
	case ZAIClientType:
		// Use generic provider wrapper to get models from config
		return &genericConfigListModelsWrapper{providerName: "zai"}, nil
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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenAI models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch OpenAI models: %w", FormatHTTPResponseError(resp.StatusCode, resp.Header, body))
	}

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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenRouter models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch OpenRouter models: %w", FormatHTTPResponseError(resp.StatusCode, resp.Header, body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var modelsResp struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Pricing     struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			ContextLength int `json:"context_length"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode OpenRouter models: %w", err)
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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DeepInfra models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch DeepInfra models: %w", FormatHTTPResponseError(resp.StatusCode, resp.Header, body))
	}

	// Decode as raw entries so pricing can be probed via ModelPricingPerMillion
	// (DeepInfra reports cents-per-token under pricing.cents_per_*_token).
	var entries []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode DeepInfra models: %w", err)
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
		}
		models = append(models, m)
	}

	return models, nil
}

type lmStudioListModelsWrapper struct{}

func (w *lmStudioListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	baseURL := os.Getenv("LMSTUDIO_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:1234/v1"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch LM Studio models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch LM Studio models: %w", FormatHTTPResponseError(resp.StatusCode, resp.Header, body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var modelsResp struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode LM Studio models: %w", err)
	}

	// Convert to ModelInfo format
	models := make([]ModelInfo, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		modelInfo := ModelInfo{
			ID:            model.ID,
			Name:          model.ID, // Use ID as name since name field isn't provided
			Description:   fmt.Sprintf("LM Studio model: %s", model.ID),
			Provider:      "lmstudio",
			ContextLength: 32768, // Assume 32k context length for LM Studio models
		}
		models = append(models, modelInfo)
	}

	return models, nil
}

type mistralListModelsWrapper struct{}

func (w *mistralListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	apiKey, err := credentials.ResolveProviderAPIKey("mistral", "Mistral")
	if err != nil {
		return nil, err
	}

	// Use OpenAI-compatible models endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.mistral.ai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Mistral models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch Mistral models: %w", FormatHTTPResponseError(resp.StatusCode, resp.Header, body))
	}

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
		return nil, fmt.Errorf("failed to decode Mistral models: %w", err)
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

// genericConfigListModelsWrapper uses provider config for model listing
// This allows providers without dedicated model endpoints to fallback to config-based model info
type genericConfigListModelsWrapper struct {
	providerName string
}

// configModelInfo mirrors providers.ModelInfo for our local use
type configModelInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length"`
	Tags          []string `json:"tags,omitempty"`
}

// configModels mirrors providers.ModelConfig for our local use
type configModels struct {
	ModelInfo []configModelInfo `json:"model_info,omitempty"`
}

// config mirrors providers.ProviderConfig for our local use
type config struct {
	Endpoint string       `json:"endpoint,omitempty"`
	Auth     configAuth   `json:"auth,omitempty"`
	Name     string       `json:"name,omitempty"`
	Models   configModels `json:"models"`
}

type configAuth struct {
	EnvVar string `json:"env_var,omitempty"`
	Key    string `json:"key,omitempty"`
}

type customProviderFile struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model_name,omitempty"`
	EnvVar   string `json:"env_var,omitempty"`
}

func (w *genericConfigListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if builtInModels, err := w.loadBuiltInProviderModels(); err == nil {
		return builtInModels, nil
	}

	return w.loadCustomProviderModels(ctx)
}

func (w *genericConfigListModelsWrapper) loadBuiltInProviderModels() ([]ModelInfo, error) {
	var configPath string
	if _, filename, _, ok := runtime.Caller(0); ok {
		configPath = filepath.Join(filepath.Dir(filename), "../agent_providers/configs", w.providerName+".json")
	} else {
		configPath = "pkg/agent_providers/configs/" + w.providerName + ".json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read provider config: %w", err)
	}

	var providerConfig config
	if err := json.Unmarshal(data, &providerConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider config: %w", err)
	}

	models := make([]ModelInfo, len(providerConfig.Models.ModelInfo))
	for i, mi := range providerConfig.Models.ModelInfo {
		models[i] = ModelInfo{
			ID:            mi.ID,
			Name:          mi.Name,
			Description:   mi.Description,
			Provider:      w.providerName,
			ContextLength: mi.ContextLength,
			Tags:          mi.Tags,
		}
	}
	return models, nil
}

func (w *genericConfigListModelsWrapper) loadCustomProviderModels(ctx context.Context) ([]ModelInfo, error) {
	data, err := os.ReadFile(customProviderFilePath(w.providerName))
	if err != nil {
		return nil, fmt.Errorf("failed to load %s provider config: %w", w.providerName, err)
	}

	var providerConfig customProviderFile
	if err := json.Unmarshal(data, &providerConfig); err != nil {
		return nil, fmt.Errorf("failed to parse %s provider config: %w", w.providerName, err)
	}

	models, err := fetchOpenAICompatibleModels(ctx, w.providerName, providerConfig.Endpoint)
	if err == nil && len(models) > 0 {
		for i := range models {
			models[i].Provider = w.providerName
		}
		return models, nil
	}

	if strings.TrimSpace(providerConfig.Model) != "" {
		return []ModelInfo{{
			ID:       strings.TrimSpace(providerConfig.Model),
			Name:     strings.TrimSpace(providerConfig.Model),
			Provider: w.providerName,
		}}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch models from %s: %w", w.providerName, err)
	}
	return nil, fmt.Errorf("no models available for provider %s", w.providerName)
}

func customProviderFilePath(providerName string) string {
	configDir, err := envutil.GetConfigDir()
	if err != nil {
		// Fallback to env-based resolution if GetConfigDir fails
		configRoot := strings.TrimSpace(envutil.GetEnvSimple("CONFIG"))
		if configRoot == "" {
			if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
				configRoot = filepath.Join(homeDir, ".config", "sprout")
			}
		}
		return filepath.Join(configRoot, "providers", providerName+".json")
	}
	return filepath.Join(configDir, "providers", providerName+".json")
}

func fetchOpenAICompatibleModels(ctx context.Context, providerName, endpoint string) ([]ModelInfo, error) {
	modelsEndpoint := strings.TrimSuffix(strings.TrimSpace(endpoint), "/chat/completions") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", modelsEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	var apiKey string
	if resolved, err := credentials.ResolveProviderAPIKey(strings.TrimSpace(providerName), strings.TrimSpace(providerName)); err == nil {
		apiKey = resolved
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models from %s: %w", providerName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, FormatHTTPResponseError(resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Data []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name,omitempty"`
			Description   string   `json:"description,omitempty"`
			ContextLength int      `json:"context_length,omitempty"`
			Tags          []string `json:"tags,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	models := make([]ModelInfo, 0, len(payload.Data))
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		models = append(models, ModelInfo{
			ID:            id,
			Name:          strings.TrimSpace(entry.Name),
			Description:   strings.TrimSpace(entry.Description),
			Provider:      "",
			ContextLength: entry.ContextLength,
			Tags:          entry.Tags,
		})
	}
	return models, nil
}

// ModelSelection represents a model selection system
// This is a stub implementation for backward compatibility
// The actual model selection logic has been moved to configuration-based system
type ModelSelection struct {
	config interface{}
}

// NewModelSelection creates a new ModelSelection instance
// This is a stub for backward compatibility - the actual model selection
// is now handled through the configuration system
func NewModelSelection(config interface{}) *ModelSelection {
	return &ModelSelection{config: config}
}

// Helper function to parse float from string
func parseFloat(s string) (float64, error) {
	// Remove any currency symbols and parse
	cleaned := strings.TrimPrefix(s, "$")
	return strconv.ParseFloat(cleaned, 64)
}
