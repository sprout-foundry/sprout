package credentials

import (
	"encoding/json"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

// commonSecretPrefixes are env var prefixes that are typically NOT secrets.
// NOTE: Keep these lists aligned with pkg/mcp/secrets.go commonSecretPrefixes,
// secretKeywords, and knownSecretVars. Due to a circular-import constraint
// (pkg/mcp → pkg/credentials), the lists are duplicated. If you change one,
// update the other.
var commonSecretPrefixes = []string{
	"PATH", "HOME", "NODE", "PYTHON", "JAVA", "GO", "GOPATH", "GOROOT",
	"NPM", "NVM", "CARGO", "RUSTUP", "MCP_",
	"SPROUT_CONFIG", "LEDIT_CONFIG", "SPROUT_MODE", "LEDIT_MODE", "SPROUT_WORKSPACE", "LEDIT_WORKSPACE", "SPROUT_PROVIDER", "LEDIT_PROVIDER",
	"SPROUT_MODEL", "LEDIT_MODEL", "SPROUT_FEATURE", "LEDIT_FEATURE", "SPROUT_SESSION", "LEDIT_SESSION", "SPROUT_TAB", "LEDIT_TAB",
	"SPROUT_EDITOR", "LEDIT_EDITOR", "SPROUT_THEME", "LEDIT_THEME", "SPROUT_TERMINAL", "LEDIT_TERMINAL",
}

// secretKeywords are keywords that indicate an env var likely contains secrets
var secretKeywords = []string{
	"TOKEN", "KEY", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL",
	"PRIVATE", "AUTH", "PAT", "BEARER", "API_KEY",
}

// knownSecretVars are specific env var names that are known to be secrets
var knownSecretVars = []string{
	"GITHUB_PERSONAL_ACCESS_TOKEN",
	"OPENAI_API_KEY",
	"ANTHROPIC_API_KEY",
	"DEEPINFRA_API_KEY",
	"OPENROUTER_API_KEY",
	"LMSTUDIO_API_KEY",
	"JINAAI_API_KEY",
	"BIGQUERY_API_KEY",
	"FIREWORKS_API_KEY",
	"FIREWORKS_AI_API_KEY",
	"GOOGLE_API_KEY",
	"GOOGLE_GENERATIVE_AI_API_KEY",
	"GROQ_API_KEY",
	"MISTRAL_API_KEY",
	"DEEPSEEK_API_KEY",
	"TOGETHER_API_KEY",
	"TOGETHER_AI_API_KEY",
	"PERPLEXITY_API_KEY",
	"COHERE_API_KEY",
	"VOYAGE_API_KEY",
}

// IsSensitiveEnvName reports whether an environment variable name suggests
// it holds a credential. It reuses the heuristic from pkg/mcp but makes it
// available outside the mcp package.
//
// NOTE: Do NOT import pkg/mcp from here (would create circular dependency
// since pkg/mcp already imports pkg/credentials). Instead, duplicate the
// simple keyword-list heuristic here.
func IsSensitiveEnvName(name string) bool {
	name = strings.TrimSpace(strings.ToUpper(name))

	// Check for known secret vars first
	for _, known := range knownSecretVars {
		if name == known {
			return true
		}
	}

	// Exclude common non-secret prefixes
	for _, prefix := range commonSecretPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}

	// Check for secret keywords
	for _, keyword := range secretKeywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}

	return false
}

// RedactMap returns a copy of m where every value is replaced with its masked form.
// Values that look like credential reference placeholders (e.g., "{{credential:...}}")
// are kept as-is since they are already safe indirect references, not actual secrets.
// Uses MaskValue for each value.
//
// NOTE: MaskValue preserves the first 2–4 characters for debugging/verification
// purposes (e.g., "sk-a****"). This is intentional — it lets operators confirm the
// correct key is in place without exposing the full value. For fully opaque redaction
// (e.g., log exports), use RedactEnvMap or RedactJSONBytes instead.
func RedactMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}

	result := make(map[string]string, len(m))
	for k, v := range m {
		if isCredentialRef(v) {
			result[k] = v
		} else {
			result[k] = MaskValue(v)
		}
	}
	return result
}

// isCredentialRef returns true if the value looks like a credential reference
// placeholder (e.g., "{{credential:mcp/server/ENVVAR}}").
func isCredentialRef(value string) bool {
	return strings.Contains(value, "{{credential:") || strings.Contains(value, "{{stored}}") || value == ""
}

// RedactEnvMap returns a copy of env where values whose keys match IsSensitiveEnvName
// are replaced with "[REDACTED]". Non-sensitive values are kept as-is.
func RedactEnvMap(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}

	result := make(map[string]string, len(env))
	for k, v := range env {
		if IsSensitiveEnvName(k) {
			result[k] = "[REDACTED]"
		} else {
			result[k] = v
		}
	}
	return result
}

// RedactJSONBytes applies credential redaction to JSON-encoded data. It
// unmarshals the data, recursively redacts string values, and re-marshals
// with indentation. Two redaction strategies are applied:
//  1. Key-aware: map keys whose names match IsSensitiveEnvName have their
//     string values replaced with "[REDACTED]" wholesale.
//  2. Value-based: all string values are scanned by the secretdetect
//     scanner (gitleaks-backed) and matched secrets are replaced with
//     opaque "[REDACTED]" tokens.
//
// Returns the redacted JSON bytes or an error if the input is not valid JSON.
func RedactJSONBytes(data []byte) ([]byte, error) {
	if data == nil {
		return nil, nil
	}

	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}

	return json.MarshalIndent(redactValue(v), "", "  ")
}

// redactValue recursively redacts string values in a JSON-like structure.
// Map keys matching IsSensitiveEnvName have their string values replaced
// wholesale; other strings go through the secretdetect opaque redactor.
func redactValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return secretdetect.RedactOpaque(val)
	case map[string]interface{}:
		redacted := make(map[string]interface{}, len(val))
		for k, v := range val {
			if IsSensitiveEnvName(k) {
				if _, ok := v.(string); ok {
					redacted[k] = "[REDACTED]"
					continue
				}
			}
			redacted[k] = redactValue(v)
		}
		return redacted
	case []interface{}:
		redacted := make([]interface{}, len(val))
		for i, v := range val {
			redacted[i] = redactValue(v)
		}
		return redacted
	default:
		return v
	}
}
