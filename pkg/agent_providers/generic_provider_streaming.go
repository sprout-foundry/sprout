package providers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/logging"
)

// SendChatRequestStream sends a streaming chat request
func (p *GenericProvider) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	requestBody, err := p.buildChatRequest(messages, tools, reasoning, disableThinking, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}

	req, err := p.buildHTTPRequestCtx(ctx, requestBody, true)
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

		// Log response details for non-200 responses to help diagnose
		// provider-specific errors (e.g. ZAI returning empty-body 400s).
		_ = body // already logged by formatProviderHTTPError below

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

// handleStreamingResponse processes the streaming response
func (p *GenericProvider) handleStreamingResponse(resp *http.Response, callback api.StreamCallback) (*api.ChatResponse, error) {
	// Process streaming response using shared builder to support tool_calls
	reader := bufio.NewReader(resp.Body)
	builder := api.NewStreamingResponseBuilder(callback)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read streaming response: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var data string
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
		} else {
			continue
		}
		if data == "[DONE]" {
			break
		}

		if chunk, err := api.ParseSSEData(data); err == nil && chunk != nil {
			_ = builder.ProcessChunk(chunk)
		}
	}

	// Finalize response from builder
	respObj := builder.GetResponse()
	if respObj == nil {
		// Fallback empty response
		respObj = &api.ChatResponse{Choices: []api.Choice{{}}}
	}
	if respObj.Model == "" {
		respObj.Model = p.model
	}

	// If the provider didn't send a finish_reason but we received content and the stream
	// ended normally (not due to error), default to "stop" to prevent false incompleteness detection
	// This handles providers like DeepInfra that don't always send finish_reason in streaming mode
	if len(respObj.Choices) > 0 {
		choice := &respObj.Choices[0]
		if choice.FinishReason == "" && choice.Message.Content != "" {
			// Stream ended normally with content but no explicit finish_reason
			// Default to "stop" since the provider completed the response
			choice.FinishReason = "stop"
		}
	}

	return respObj, nil
}
