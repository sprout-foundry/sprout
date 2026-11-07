package providers

// Token usage for every response. Streaming requests ask for the usage block
// by default (StreamingConfig.WantsUsage); a server that rejects the field
// is retried once without it and not asked again in the session. A response
// that still arrives with no usage gets an estimate from the text sent and
// received, so a provider that reports nothing never records zero tokens and
// zero cost; the estimate is reported once per provider.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// rejectsStreamOptions reports whether an error answer is the server refusing
// the stream_options field.
func rejectsStreamOptions(status int, body []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "stream_options") || strings.Contains(lower, "include_usage")
}

// withoutStreamOptions is a request body without its stream_options field.
func withoutStreamOptions(body []byte) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false
	}
	if _, ok := raw["stream_options"]; !ok {
		return body, false
	}
	delete(raw, "stream_options")
	out, err := json.Marshal(raw)
	if err != nil {
		return body, false
	}
	return out, true
}

// tryWithoutStreamOptionsRetry resends a streaming request whose server
// refused stream_options, without it, and remembers the refusal so later
// requests leave it out. It has tryMaxCompletionTokensRetry's contract: the
// body sent, the response, whether it retried, and an error building or
// sending the retry.
func (p *GenericProvider) tryWithoutStreamOptionsRetry(sentBody []byte, streaming bool, status int, errBody []byte) ([]byte, *http.Response, bool, error) {
	if !streaming || !rejectsStreamOptions(status, errBody) || !bytes.Contains(sentBody, []byte("stream_options")) {
		return sentBody, nil, false, nil
	}
	retryBody, changed := withoutStreamOptions(sentBody)
	if !changed {
		return sentBody, nil, false, nil
	}
	p.streamUsageRefused.Store(true)
	utils.GetLogger(false).Logf("[usage] %s rejected stream_options; retrying without it (token counts will be estimated when the stream has none)", p.config.Name)
	req, err := p.buildHTTPRequest(retryBody, true)
	if err != nil {
		return retryBody, nil, true, agenterrors.NewNetwork("build HTTP request", err)
	}
	p.mu.RLock()
	client := p.streamingClient
	p.mu.RUnlock()
	resp, err := client.Do(req)
	if err != nil {
		return retryBody, nil, true, agenterrors.NewNetwork("execute HTTP request", err)
	}
	return retryBody, resp, true, nil
}

// fillMissingUsage estimates a response's token usage when the server sent
// none: the prompt from the messages and tools sent, the completion from what
// came back. Usage the server did report is never touched.
func (p *GenericProvider) fillMissingUsage(messages []api.Message, tools []api.Tool, resp *api.ChatResponse) {
	if resp == nil || resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		return
	}
	prompt := 0
	for _, m := range messages {
		prompt += api.EstimateTokens(m.Content) + api.EstimateTokens(m.ReasoningContent) + 4
		for _, tc := range m.ToolCalls {
			prompt += api.EstimateTokens(tc.Function.Name) + api.EstimateTokens(tc.Function.Arguments)
		}
	}
	if len(tools) > 0 {
		if b, err := json.Marshal(tools); err == nil {
			prompt += api.EstimateTokens(string(b))
		}
	}
	completion := 0
	for _, c := range resp.Choices {
		completion += api.EstimateTokens(c.Message.Content) + api.EstimateTokens(c.Message.ReasoningContent)
		for _, tc := range c.Message.ToolCalls {
			completion += api.EstimateTokens(tc.Function.Name) + api.EstimateTokens(tc.Function.Arguments)
		}
	}
	if prompt == 0 && completion == 0 {
		return
	}
	resp.Usage.PromptTokens = prompt
	resp.Usage.CompletionTokens = completion
	resp.Usage.TotalTokens = prompt + completion
	if p.usageEstimateWarned.CompareAndSwap(false, true) {
		utils.GetLogger(false).Logf("[usage] %s sent no token usage; token counts for it are estimated", p.config.Name)
	}
}
