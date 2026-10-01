package mcp

import (
	"log"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// commonSecretPrefixes are env var prefixes that are typically NOT secrets
var commonSecretPrefixes = []string{
	"PATH", "HOME", "NODE", "PYTHON", "JAVA", "GO", "GOPATH", "GOROOT",
	"NPM", "NVM", "CARGO", "RUSTUP", "MCP_",
	"SPROUT_CONFIG", "SPROUT_CONFIG", "SPROUT_MODE", "SPROUT_MODE", "SPROUT_WORKSPACE", "SPROUT_WORKSPACE", "SPROUT_PROVIDER", "SPROUT_PROVIDER",
	"SPROUT_MODEL", "SPROUT_MODEL", "SPROUT_FEATURE", "SPROUT_FEATURE", "SPROUT_SESSION", "SPROUT_SESSION", "SPROUT_TAB", "SPROUT_TAB",
	"SPROUT_EDITOR", "SPROUT_EDITOR", "SPROUT_THEME", "SPROUT_THEME", "SPROUT_TERMINAL", "SPROUT_TERMINAL",
}

// secretKeywords are keywords that indicate an env var likely contains secrets.
// NOTE: Keep this list aligned with pkg/credentials/redact.go secretKeywords.
// Due to a circular-import constraint (pkg/mcp → pkg/credentials), the list is
// duplicated. If you add a keyword here, also add it there.
var secretKeywords = []string{
	"TOKEN", "KEY", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL",
	"PRIVATE", "AUTH", "PAT", "BEARER", "API_KEY",
}

// knownSecretVars are specific env var names that are known to be secrets.
// NOTE: Keep this list aligned with pkg/credentials/redact.go knownSecretVars.
// Due to a circular-import constraint (pkg/mcp → pkg/credentials), the list is
// duplicated. If you add a var here, also add it there.
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

// IsSecretEnvVar returns true if the env var name looks like it contains credentials.
// It checks for common secret keywords and excludes known non-secret prefixes.
func IsSecretEnvVar(name string) bool {
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

// CredentialKey returns the credential store key for an MCP server's environment variable.
// Format: "mcp/{server}/{envvar}"
func CredentialKey(serverName, envVarName string) string {
	return "mcp/" + serverName + "/" + envVarName
}

// IsValidEnvVarName reports whether name is a usable environment variable
// name: [A-Za-z_][A-Za-z0-9_]*, at most 256 chars. The settings API's
// credential endpoints enforce the same rule; this is the canonical copy.
func IsValidEnvVarName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for i, c := range name {
		isAlpha := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		isDigitOrUnderscore := (c >= '0' && c <= '9') || c == '_'
		if i == 0 {
			if !isAlpha && c != '_' {
				return false
			}
			continue
		}
		if !isAlpha && !isDigitOrUnderscore {
			return false
		}
	}
	return true
}

// SecretRef returns the placeholder string for a credential reference.
// Format: "{{credential:mcp/{server}/{envvar}}}"
func SecretRef(serverName, envVarName string) string {
	return "{{credential:" + CredentialKey(serverName, envVarName) + "}}"
}

// IsSecretRef returns true if the value matches the credential placeholder pattern.
func IsSecretRef(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "{{credential:") && strings.HasSuffix(value, "}}")
}

// ParseSecretRef parses a credential placeholder and returns its components.
// The value between "{{credential:" and "}}" is split on "/" - first part is ignored ("mcp"),
// second is server name, third is env var name.
func ParseSecretRef(value string) (serverName, envVarName string, ok bool) {
	value = strings.TrimSpace(value)
	if !IsSecretRef(value) {
		return "", "", false
	}

	// Remove the "{{credential:" prefix and "}}" suffix
	inner := strings.TrimPrefix(value, "{{credential:")
	inner = strings.TrimSuffix(inner, "}}")

	// Split on "/" - expected format is "mcp/{server}/{envvar}"
	parts := strings.Split(inner, "/")
	if len(parts) != 3 || parts[0] != "mcp" {
		return "", "", false
	}

	return parts[1], parts[2], true
}

// ResolveEnvVars resolves credential placeholders in an environment map.
// For each entry:
// - If it's a placeholder, resolve it from the credential store
// - If the credential store has an empty value, fall back to os.Getenv()
// - Non-placeholder values pass through unchanged
func ResolveEnvVars(serverName string, env map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(env))

	for name, value := range env {
		if IsSecretRef(value) {
			// Parse the placeholder to get the credential key
			_, envVarName, ok := ParseSecretRef(value)
			if !ok {
				log.Printf("[mcp-secrets] Invalid credential placeholder for %s/%s, skipping", serverName, name)
				continue
			}

			// Try to get from credential store
			key := CredentialKey(serverName, envVarName)
			credValue, _, err := credentials.GetFromActiveBackend(key)
			if err != nil {
				log.Printf("[mcp-secrets] Error getting credential %s: %v", key, err)
				continue
			}

			// If empty, fall back to OS environment
			if credValue == "" {
				credValue = os.Getenv(envVarName)
				if credValue == "" {
					log.Printf("[mcp-secrets] Credential %s not found in store or OS env, skipping", key)
					continue
				}
			}

			result[name] = credValue
		} else {
			// Non-secret value passes through unchanged
			result[name] = value
		}
	}

	return result, nil
}

// MigrateEnvSecrets detects plaintext secrets in an environment map and migrates them
// to the credential store, replacing them with placeholders.
// Returns the updated map and count of migrated secrets.
//
// The migration is two-phase: all secrets are stored in the credential backend first;
// only if all writes succeed is the returned map updated with placeholder values.
func MigrateEnvSecrets(serverName string, env map[string]string) (map[string]string, int, error) {
	result := make(map[string]string, len(env))

	// Phase 1: collect plaintext secrets to migrate
	pending := make(map[string]string, len(env))
	for name, value := range env {
		if IsSecretRef(value) {
			result[name] = value // Already migrated
			continue
		}
		if value == "{{stored}}" || value == "" {
			result[name] = value
			continue
		}
		if IsSecretEnvVar(name) {
			pending[name] = value
		} else {
			result[name] = value
		}
	}

	if len(pending) == 0 {
		return result, 0, nil
	}

	// Phase 2: store all secrets in the credential backend
	for name, value := range pending {
		key := CredentialKey(serverName, name)
		if err := credentials.SetToActiveBackend(key, value); err != nil {
			log.Printf("[mcp-secrets] Failed to store credential %s: %v", key, err)
			return result, 0, err // result has plaintext values for pending items
		}
	}

	// Phase 3: all writes succeeded — update result with refs
	migrated := len(pending)
	for name := range pending {
		result[name] = SecretRef(serverName, name)
		log.Printf("[mcp-secrets] Migrated secret %s for server %s to credential store", name, serverName)
	}

	return result, migrated, nil
}

// MaskEnvValue returns a masked version of a value for safe display.
// Secrets are shown as first 4 chars + "****", placeholders show as "{{stored}}".
func MaskEnvValue(value string) string {
	value = strings.TrimSpace(value)

	// Check if it's a credential placeholder
	if IsSecretRef(value) {
		return "{{stored}}"
	}

	// Mask the value: first 4 chars + "****"
	if len(value) <= 4 {
		return "****"
	}
	return value[:4] + "****"
}

// MaskEnvVars returns a copy of the env map with secret values masked.
// Values that are credential placeholders are shown as "{{stored}}",
// and values that look like secrets (per IsSecretEnvVar) are masked.
func MaskEnvVars(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}

	result := make(map[string]string, len(env))
	for name, value := range env {
		if IsSecretRef(value) {
			result[name] = "{{stored}}"
		} else if IsSecretEnvVar(name) {
			result[name] = MaskEnvValue(value)
		} else {
			result[name] = value
		}
	}
	return result
}
