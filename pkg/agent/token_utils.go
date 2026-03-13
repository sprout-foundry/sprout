package agent

import (
	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// EstimateTokens provides a token estimation based on OpenAI's tiktoken approach.
// Delegates to the centralized implementation in agent_api for consistency across all providers.
func EstimateTokens(text string) int {
	return api.EstimateTokens(text)
}
