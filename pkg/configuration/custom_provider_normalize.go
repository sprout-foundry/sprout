package configuration

import (
	"fmt"
	"net/url"
	"strings"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// NormalizeCustomProviderConfig fills in defaults, trims whitespace, and
// rejects ill-formed values. Called by Save and Load paths so a hand-edited
// JSON file cannot put the runtime into a state where discovery silently
// fails later.
func NormalizeCustomProviderConfig(cfg CustomProviderConfig) (CustomProviderConfig, error) {
	name, err := CanonicalizeCustomProviderName(cfg.Name)
	if err != nil {
		return CustomProviderConfig{}, fmt.Errorf("canonicalize provider name: %w", err)
	}

	endpoint, err := normalizeOpenAIEndpoint(cfg.Endpoint)
	if err != nil {
		return CustomProviderConfig{}, fmt.Errorf("normalize endpoint: %w", err)
	}

	cfg.Name = name
	cfg.Endpoint = endpoint
	cfg.EnvVar = strings.TrimSpace(cfg.EnvVar)
	cfg.ModelName = strings.TrimSpace(cfg.ModelName)
	cfg.ReasoningEffort = strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	cfg.VisionModel = strings.TrimSpace(cfg.VisionModel)
	cfg.VisionFallbackProvider = strings.TrimSpace(cfg.VisionFallbackProvider)
	cfg.VisionFallbackModel = strings.TrimSpace(cfg.VisionFallbackModel)
	cfg.ToolCalls = normalizeUniqueStrings(cfg.ToolCalls)

	// Initialize model context sizes map if nil
	if cfg.ModelContextSizes == nil {
		cfg.ModelContextSizes = make(map[string]int)
	}

	if cfg.ContextSize <= 0 {
		cfg.ContextSize = 32768
	}
	if cfg.EnvVar != "" {
		cfg.RequiresAPIKey = true
	}
	if cfg.ChunkTimeoutMs <= 0 {
		cfg.ChunkTimeoutMs = 300000
	}

	return cfg, nil
}

// CanonicalizeCustomProviderName lowercases and trims the input, then
// rejects anything outside the [a-z0-9_-] character set. Empty input
// is also rejected.
func CanonicalizeCustomProviderName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", fmt.Errorf("provider name cannot be empty")
	}
	for _, r := range normalized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("provider name must contain only lowercase letters, numbers, '-' or '_'")
	}
	return normalized, nil
}

// ValidateCustomProviderEndpoint checks that raw is a syntactically valid
// http or https URL with a host. It runs before NormalizeCustomProviderConfig
// (which auto-appends /v1/chat/completions), so it catches typos that would
// otherwise produce a config that silently fails model discovery.
//
// Returns nil for empty input — that's allowed at this layer so the wizard
// can detect "user pressed enter on an empty prompt" separately from
// "user typed garbage".
func ValidateCustomProviderEndpoint(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("endpoint URL cannot be empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL must include a host (e.g. https://api.example.com/v1)")
	}
	return nil
}

// defaultCustomProviderBillingType returns the effective BillingType for a
// custom provider. Custom providers added at runtime are typically
// subscription gateways (flat monthly fee, no marginal per-token cost), so
// an empty BillingType defaults to subscription to avoid the cost tracker
// estimating a fake "charged cost" from the live pricing catalog. Explicit
// values (subscription, pay_per_token, free) are preserved. Endpoints on
// localhost / 127.0.0.1 (e.g., self-hosted Ollama) default to free instead
// — those are zero-marginal-cost regardless of the user's plan intent.
func defaultCustomProviderBillingType(explicit, endpoint string) string {
	if explicit != "" {
		return explicit
	}
	e := strings.ToLower(strings.TrimSpace(endpoint))
	if strings.Contains(e, "127.0.0.1") || strings.Contains(e, "localhost") {
		return providers.BillingFree
	}
	return providers.BillingSubscription
}

func normalizeOpenAIEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("endpoint cannot be empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("endpoint must be a valid absolute URL")
	}

	path := strings.TrimRight(u.Path, "/")
	switch {
	case path == "":
		path = "/v1/chat/completions"
	case strings.HasSuffix(path, "/v1"):
		path += "/chat/completions"
	case strings.HasSuffix(path, "/v1/models"):
		path = strings.TrimSuffix(path, "/models") + "/chat/completions"
	case strings.HasSuffix(path, "/v1/chat/completions"):
	default:
		if strings.HasSuffix(path, "/models") {
			path = strings.TrimSuffix(path, "/models") + "/chat/completions"
		} else if strings.HasSuffix(path, "/chat/completions") {
		} else {
			path += "/chat/completions"
		}
	}

	u.Path = path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func normalizeUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}