package runner

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// GatewayProviderName is the workspace provider name for the platform
// gateway's OpenAI-compatible models endpoint. A start task naming this
// provider wires the workspace's agent to <platform_api_url>/v1 with the
// task's workspace-scoped gateway key, so the workspace uses the gateway's
// models (and the user's gateway quotas) instead of a public provider's
// endpoint (issue #115).
const GatewayProviderName = "gateway"

// GatewayKeyEnvVar carries the workspace-scoped gateway key in the workspace
// daemon's environment. The gateway key authenticates against the platform's
// OpenAI-compatible /v1 as the user, with the user's entitlements applied;
// it is revoked platform-side when the workspace terminates.
const GatewayKeyEnvVar = "SPROUT_GATEWAY_KEY"

// IsGatewayProvider reports whether a start task's llm_provider names the
// platform gateway.
func IsGatewayProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), GatewayProviderName)
}

// gatewayEndpoint derives the workspace's chat endpoint from the platform
// API URL: the gateway's OpenAI-compatible base plus /v1/chat/completions
// (the custom-provider endpoint convention: a bare origin gains /v1, an
// explicit /v1 gains /chat/completions).
func gatewayEndpoint(platformAPIURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(platformAPIURL), "/")
	if base == "" {
		return "", fmt.Errorf("gateway provider needs the platform API URL, which the start task did not carry")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("gateway endpoint %q is not a valid http(s) URL", platformAPIURL)
	}
	if !strings.HasSuffix(u.Path, "/v1") {
		u.Path = strings.TrimRight(u.Path, "/") + "/v1"
	}
	return u.Scheme + "://" + u.Host + u.Path + "/chat/completions", nil
}

// writeGatewayProviderFile registers the gateway as the workspace's custom
// provider in the workspace's scoped config dir (SPROUT_CONFIG_DIR, never
// the runner user's global config): endpoint + key env var. No default model
// is pinned — the daemon's model picker lists the models the account may use
// from the endpoint's /v1/models. The file lives and dies with the workspace.
func writeGatewayProviderFile(configDir, platformAPIURL string) error {
	endpoint, err := gatewayEndpoint(platformAPIURL)
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
