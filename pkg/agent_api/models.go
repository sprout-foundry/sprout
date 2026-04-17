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

	"github.com/alantheprice/ledit/pkg/credentials"
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

// GetModelsForProviderCtx returns available models for a specific provider with context support
func GetModelsForProviderCtx(ctx context.Context, clientType ClientType) ([]ModelInfo, error) {
	// Use the provider's ListModels method
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

	return models, nil
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

	client := &http.Client{}
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
	apiKey, err := credentials.ResolveProviderAPIKey("openrouter", "OpenRouter")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
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
	apiKey, err := credentials.ResolveProviderAPIKey("deepinfra", "DeepInfra")
	if err != nil {
		return nil, err
	}

	// Use the OpenAI-compatible endpoint like the DeepInfra provider does
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.deepinfra.com/v1/openai/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DeepInfra models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch DeepInfra models: %w", FormatHTTPResponseError(resp.StatusCode, resp.Header, body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Use the same response structure as the DeepInfra provider
	var response struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`
			Pricing       *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode DeepInfra models: %w", err)
	}

	// Convert to ModelInfo format with full details
	models := make([]ModelInfo, len(response.Data))
	for i, model := range response.Data {
		modelInfo := ModelInfo{
			ID:       model.ID,
			Name:     model.Name,
			Provider: "deepinfra",
		}

		if model.Description != "" {
			modelInfo.Description = model.Description
		}
		if model.ContextLength > 0 {
			modelInfo.ContextLength = model.ContextLength
		}

		// Parse pricing if available
		if model.Pricing != nil {
			if promptCost, err := strconv.ParseFloat(model.Pricing.Prompt, 64); err == nil {
				// DeepInfra pricing is per token, convert to per million tokens
				modelInfo.InputCost = promptCost * 1000000
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				// DeepInfra pricing is per token, convert to per million tokens
				modelInfo.OutputCost = completionCost * 1000000
			}
			if modelInfo.InputCost > 0 || modelInfo.OutputCost > 0 {
				modelInfo.Cost = (modelInfo.InputCost + modelInfo.OutputCost) / 2.0
			}
		}

		models[i] = modelInfo
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

	client := &http.Client{}
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

	client := &http.Client{}
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
	configRoot := strings.TrimSpace(os.Getenv("LEDIT_CONFIG"))
	if configRoot == "" {
		xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if xdgConfigHome != "" {
			configRoot = filepath.Join(xdgConfigHome, "ledit")
		} else if homeDir, err := os.UserHomeDir(); err == nil {
			configRoot = filepath.Join(homeDir, ".ledit")
		}
	}
	return filepath.Join(configRoot, "providers", providerName+".json")
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

	client := &http.Client{}
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
