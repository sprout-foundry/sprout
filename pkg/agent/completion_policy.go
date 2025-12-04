package agent

import (
	"strings"
)

var explicitCompletionProviders = map[string]struct{}{
	"lmstudio": {},
}

func (a *Agent) shouldAllowImplicitCompletion() bool {
	if a == nil || a.client == nil {
		return true
	}
	provider := strings.ToLower(a.client.GetProvider())
	_, disallowed := explicitCompletionProviders[provider]
	return !disallowed
}
