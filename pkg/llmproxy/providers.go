package llmproxy

import (
	"net/url"
	"strings"
)

// knownProviders maps direct LLM provider host[+path-prefix] pairs to the
// short provider identifier the sprout-foundry platform uses in its
// /api/proxy/llm/{provider}/* paths.
//
// Order matters: more specific prefixes go first because we match by
// HasPrefix on the URL path.
type providerMatch struct {
	host        string // hostname (case-insensitive)
	pathPrefix  string // path prefix that must match (empty = any path)
	stripPrefix string // path prefix to strip before forwarding (empty = none)
	provider    string // identifier used in /api/proxy/llm/{provider}/...
}

var knownProviders = []providerMatch{
	// OpenAI (api.openai.com/v1/...)
	{host: "api.openai.com", provider: "openai"},
	// Anthropic (api.anthropic.com/v1/...)
	{host: "api.anthropic.com", provider: "anthropic"},
	// OpenRouter (openrouter.ai/api/v1/... — note the /api prefix gets stripped)
	{host: "openrouter.ai", pathPrefix: "/api", stripPrefix: "/api", provider: "openrouter"},
	// DeepInfra (api.deepinfra.com/v1/openai/... and api.deepinfra.com/v1/inference/...)
	{host: "api.deepinfra.com", provider: "deepinfra"},
	// Mistral (api.mistral.ai/v1/...)
	{host: "api.mistral.ai", provider: "mistral"},
	// Cerebras Cloud (api.cerebras.ai/v1/...)
	{host: "api.cerebras.ai", provider: "cerebras"},
	// Groq (api.groq.com/openai/v1/...)
	{host: "api.groq.com", provider: "groq"},
	// Together AI (api.together.xyz/v1/...)
	{host: "api.together.xyz", provider: "together"},
}

// matchProvider returns the provider identifier and the path suffix that
// should be appended after `{base}/api/proxy/llm/{provider}` for the
// given request URL. Returns ok=false if the URL doesn't target a known
// provider.
//
// The match is purely host+path based — no header inspection, no body
// inspection. This keeps the transport stateless and predictable.
func matchProvider(u *url.URL) (provider, suffix string, ok bool) {
	if u == nil {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range knownProviders {
		if host != p.host {
			continue
		}
		if p.pathPrefix != "" && !strings.HasPrefix(u.Path, p.pathPrefix) {
			continue
		}
		suffix := u.Path
		if p.stripPrefix != "" {
			suffix = strings.TrimPrefix(suffix, p.stripPrefix)
		}
		if u.RawQuery != "" {
			suffix += "?" + u.RawQuery
		}
		return p.provider, suffix, true
	}
	return "", "", false
}
