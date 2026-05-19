package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func shouldRetryWithMaxCompletionTokens(errBody []byte) bool {
	bodyLower := strings.ToLower(string(errBody))
	return strings.Contains(bodyLower, "max_tokens") &&
		strings.Contains(bodyLower, "max_completion_tokens") &&
		strings.Contains(bodyLower, "unsupported")
}

func rewriteMaxTokensToMaxCompletionTokens(requestBody []byte) ([]byte, bool, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		return nil, false, fmt.Errorf("parse request body: %w", err)
	}

	maxTokens, hasMaxTokens := payload["max_tokens"]
	if !hasMaxTokens {
		return requestBody, false, nil
	}
	if _, exists := payload["max_completion_tokens"]; exists {
		return requestBody, false, nil
	}

	payload["max_completion_tokens"] = maxTokens
	delete(payload, "max_tokens")

	updated, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("marshal updated request body: %w", err)
	}
	return updated, true, nil
}

func (p *GenericProvider) tryMaxCompletionTokensRetry(originalRequestBody []byte, streaming bool, firstErrorBody []byte) ([]byte, *http.Response, bool, error) {
	if !shouldRetryWithMaxCompletionTokens(firstErrorBody) {
		return originalRequestBody, nil, false, nil
	}

	retryBody, changed, err := rewriteMaxTokensToMaxCompletionTokens(originalRequestBody)
	if err != nil {
		return originalRequestBody, nil, true, fmt.Errorf("rewrite max tokens: %w", err)
	}
	if !changed {
		return originalRequestBody, nil, false, nil
	}

	req, err := p.buildHTTPRequest(retryBody, streaming)
	if err != nil {
		return retryBody, nil, true, fmt.Errorf("build HTTP request: %w", err)
	}

	client := p.httpClient
	if streaming {
		client = p.streamingClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return retryBody, nil, true, fmt.Errorf("execute HTTP request: %w", err)
	}

	return retryBody, resp, true, nil
}
