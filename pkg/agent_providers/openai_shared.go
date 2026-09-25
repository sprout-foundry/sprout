package providers

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"strconv"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

const defaultMaxRequestCompletionTokens = 64000

// CalculateMaxTokens returns an appropriate max_tokens value given the context
// window and prompt size. The caller passes the effective context limit, making
// it easy to reuse across providers with custom limit lookups.
func CalculateMaxTokens(contextLimit int, messages []api.Message, tools []api.Tool) int {
	return CalculateMaxTokensWithLimits(contextLimit, 0, messages, tools)
}

// CalculateMaxTokensWithLimits computes a token budget from context and optional completion caps.
// Uses centralized token estimation for consistency across all providers.
func CalculateMaxTokensWithLimits(contextLimit int, completionLimit int, messages []api.Message, tools []api.Tool) int {
	if contextLimit == 0 {
		contextLimit = 32000
	}

	// Use centralized token estimation
	inputTokens := api.EstimateInputTokens(messages, tools)

	// Use centralized output budget calculation
	maxOutput, _ := api.CalculateOutputBudget(contextLimit, inputTokens)
	// maxOutput <= 0 means the estimate claims input already fills the
	// window (ok=false from CalculateOutputBudget). That estimate can
	// overestimate against the provider's real prompt count, and the old
	// MinOutputTokens pin here decapitated responses (finish=output_limit
	// at exactly the floor). Fall back to the request cap — the same shape
	// healthy sibling calls send. If the estimate was right, the provider
	// rejects with a context-overflow error and seed's recovery compaction
	// fires; the provider enforces the real ceiling either way.
	if maxOutput <= 0 {
		maxOutput = getMaxRequestCompletionTokensCap()
	}

	// Apply completion limit if specified
	if completionLimit > 0 && maxOutput > completionLimit {
		maxOutput = completionLimit
	}

	// Apply request cap from environment
	requestCap := getMaxRequestCompletionTokensCap()
	if requestCap > 0 && maxOutput > requestCap {
		maxOutput = requestCap
	}
	return maxOutput
}

func getMaxRequestCompletionTokensCap() int {
	raw := strings.TrimSpace(envutil.GetEnvSimple("MAX_REQUEST_COMPLETION_TOKENS"))
	if raw == "" {
		return defaultMaxRequestCompletionTokens
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultMaxRequestCompletionTokens
	}
	return value
}
