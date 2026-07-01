package providers

import (
	"fmt"
	"net/http"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// tryMaxCompletionTokensRetry retries the request with max_completion_tokens
// when the provider rejects max_tokens. This is a compatibility fallback for
// OpenAI-compatible backends that require max_completion_tokens instead of
// max_tokens for certain models.
func (p *GenericProvider) tryMaxCompletionTokensRetry(originalRequestBody []byte, streaming bool, firstErrorBody []byte) ([]byte, *http.Response, bool, error) {
	if !shouldRetryWithMaxCompletionTokens(firstErrorBody) {
		return originalRequestBody, nil, false, nil
	}

	retryBody, changed, err := rewriteMaxTokensToMaxCompletionTokens(originalRequestBody)
	if err != nil {
		return originalRequestBody, nil, true, agenterrors.NewValidation(fmt.Sprintf("rewrite max tokens: %v", err), nil)
	}
	if !changed {
		return originalRequestBody, nil, false, nil
	}

	req, err := p.buildHTTPRequest(retryBody, streaming)
	if err != nil {
		return retryBody, nil, true, agenterrors.NewNetwork("build HTTP request", err)
	}

	client := p.httpClient
	if streaming {
		client = p.streamingClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return retryBody, nil, true, agenterrors.NewNetwork("execute HTTP request", err)
	}

	return retryBody, resp, true, nil
}
