package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/logging"
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
func (p *GenericProvider) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	requestBody, err := p.buildChatRequest(messages, tools, reasoning, disableThinking, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}

	req, err := p.buildHTTPRequest(requestBody, false)
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

			var retryResponse api.ChatResponse
			if err := json.NewDecoder(retryResp.Body).Decode(&retryResponse); err != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "decode_response", err)
				return nil, fmt.Errorf("failed to decode response: %w", err)
			}
			return &retryResponse, nil
		}

		// Log request on API error
		formattedErr := formatProviderHTTPError(resp.StatusCode, resp.Header, body)
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false,
			fmt.Sprintf("api_error_%d", resp.StatusCode), formattedErr)
		return nil, formattedErr
	}
	defer resp.Body.Close()

	var response api.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		// Log request on decode error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "decode_response", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Success - don't log the request
	return &response, nil
}

// SendChatRequestStream sends a streaming chat request
func (p *GenericProvider) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	requestBody, err := p.buildChatRequest(messages, tools, reasoning, disableThinking, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}

	req, err := p.buildHTTPRequest(requestBody, true)
	if err != nil {
		// Log request on build error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true, "build_http_request", err)
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}

	resp, err := p.streamingClient.Do(req)
	if err != nil {
		// Log request on HTTP error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true, "http_request_failed", err)
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		retryBody, retryResp, retried, retryErr := p.tryMaxCompletionTokensRetry(requestBody, true, body)
		if retried {
			requestBody = retryBody
			if retryErr != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true,
					"retry_max_completion_tokens_build", retryErr)
				return nil, fmt.Errorf("failed retry with max_completion_tokens: %w", retryErr)
			}
			defer retryResp.Body.Close()
			if retryResp.StatusCode != http.StatusOK {
				retryErrBody, _ := io.ReadAll(retryResp.Body)
				formattedErr := formatProviderHTTPError(retryResp.StatusCode, retryResp.Header, retryErrBody)
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true,
					fmt.Sprintf("api_error_%d", retryResp.StatusCode), formattedErr)
				return nil, formattedErr
			}

			response, err := p.handleStreamingResponse(retryResp, callback)
			if err != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true, "streaming_response", err)
				return nil, fmt.Errorf("chat request failed: %w", err)
			}
			return response, nil
		}

		// Log request on API error
		formattedErr := formatProviderHTTPError(resp.StatusCode, resp.Header, body)
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true,
			fmt.Sprintf("api_error_%d", resp.StatusCode), formattedErr)
		return nil, formattedErr
	}
	defer resp.Body.Close()

	response, err := p.handleStreamingResponse(resp, callback)
	if err != nil {
		// Log request on streaming error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true, "streaming_response", err)
		return nil, fmt.Errorf("chat request failed (streaming): %w", err)
	}

	// Success - don't log the request
	return response, nil
}

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

	_, err := p.SendChatRequest(testMessages, nil, "", false)
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

// GetModelContextLimit returns the context limit for the current model
func (p *GenericProvider) GetModelContextLimit() (int, error) {
	if p.modelsCached {
		for _, model := range p.models {
			if model.ID == p.model && model.ContextLength > 0 {
				return model.ContextLength, nil
			}
		}
	}

	return p.config.GetContextLimit(p.model), nil
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
