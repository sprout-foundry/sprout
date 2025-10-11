package agent

import (
	"strings"
)

var implicitCompletionProviders = map[string]string{
	"openai": "allow",
	"zai":    "allow",
}

func (a *Agent) shouldAllowImplicitCompletion() bool {
	// Simplified policy: disallow implicit completion for OpenAI, allow for all others.
	if a == nil || a.client == nil {
		return false
	}
	provider := strings.ToLower(a.client.GetProvider())

	return implicitCompletionProviders[provider] == "allow"
}
