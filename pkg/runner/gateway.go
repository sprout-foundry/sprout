package runner

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// GatewayProviderName is the workspace provider name for the platform
// gateway's OpenAI-compatible endpoint. A start task naming this provider
// wires the workspace's agent to the gateway with the task's gateway key, so
// the workspace uses the gateway's models (and the user's gateway quotas)
// instead of a public provider's endpoint.
const GatewayProviderName = "gateway"

// GatewayKeyEnvVar carries the gateway key in the workspace daemon's
// environment. The key's scope, lifetime and revocation are the platform's:
// the runner only passes it to the daemon, never writes it to disk and never
// sends it anywhere but the gateway endpoint. Code running in the workspace
// can read the daemon's environment, so the platform should mint a key scoped
// to the one workspace and revoke it when the workspace ends.
const GatewayKeyEnvVar = "SPROUT_GATEWAY_KEY"

// platformGatewayPath is the platform's OpenAI-compatible gateway base,
// relative to platform_api_url, used when a task carries no gateway_url.
const platformGatewayPath = "/internal/llm/v1"

// IsGatewayProvider reports whether a start task's llm_provider names the
// platform gateway.
func IsGatewayProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), GatewayProviderName)
}

// gatewayEndpoint returns the workspace's chat endpoint: the gateway base
// (gatewayURL, or platformAPIURL plus the platform's gateway path) plus
// /chat/completions, the custom-provider endpoint convention the daemon
// also derives /models from. A base that already ends in /chat/completions
// is used as is. The key travels as a bearer token, so the URL must be https
// unless it points at this machine, and must carry no credentials, query or
// fragment.
func gatewayEndpoint(gatewayURL, platformAPIURL string) (string, error) {
	raw := strings.TrimSpace(gatewayURL)
	if raw == "" {
		origin := strings.TrimRight(strings.TrimSpace(platformAPIURL), "/")
		if origin == "" {
			return "", fmt.Errorf("gateway start task carries neither a gateway URL nor the platform API URL")
		}
		raw = origin + platformGatewayPath
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("gateway URL is not a valid http(s) URL")
	}
	if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
		return "", fmt.Errorf("gateway URL must use https unless it points at this machine")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("gateway URL must not carry credentials, a query or a fragment")
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	return u.Scheme + "://" + u.Host + path + "/chat/completions", nil
}

// loopbackHost reports whether host names this machine.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// writeGatewayProviderFile registers the gateway as the workspace's custom
// provider in the workspace's scoped config dir (SPROUT_CONFIG_DIR, never
// the runner user's global config): endpoint + key env var. No default model
// is pinned — the daemon's model picker lists the models the account may use
// from the endpoint's /v1/models. The file lives and dies with the workspace.
func writeGatewayProviderFile(configDir, gatewayURL, platformAPIURL string) error {
	endpoint, err := gatewayEndpoint(gatewayURL, platformAPIURL)
	if err != nil {
		return err
	}
	providersDir := filepath.Join(configDir, "providers")
	if err := os.MkdirAll(providersDir, 0o700); err != nil {
		return fmt.Errorf("creating workspace providers dir: %w", err)
	}
	file := map[string]any{
		"name":             GatewayProviderName,
		"endpoint":         endpoint,
		"env_var":          GatewayKeyEnvVar,
		"requires_api_key": true,
		"billing_type":     "subscription",
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding gateway provider config: %w", err)
	}
	path := filepath.Join(providersDir, GatewayProviderName+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // G306: credential-adjacent config, owner-only
		return fmt.Errorf("writing gateway provider config: %w", err)
	}
	return nil
}
