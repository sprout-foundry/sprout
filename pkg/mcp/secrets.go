// Package mcp provides secret management for MCP server environment variables.
// It handles detecting, storing, and resolving sensitive environment variables
// using the credentials backend for secure storage.
package mcp

import (
	"log"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/credentials"
)

// commonSecretPrefixes are env var prefixes that are typically NOT secrets
var commonSecretPrefixes = []string{
	"PATH", "HOME", "NODE", "PYTHON", "JAVA", "GO", "GOPATH", "GOROOT",
	"NPM", "NVM", "CARGO", "RUSTUP", "LEDIT_", "MCP_",
}

// secretKeywords are keywords that indicate an env var likely contains secrets
var secretKeywords = []string{
	"TOKEN", "KEY", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL",
	"PRIVATE", "AUTH", "PAT", "BEARER",
}

// knownSecretVars are specific env var names that are known to be secrets
var knownSecretVars = []string{
	"GITHUB_PERSONAL_ACCESS_TOKEN",
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
func MigrateEnvSecrets(serverName string, env map[string]string) (map[string]string, int, error) {
	result := make(map[string]string, len(env))
	migrated := 0

	for name, value := range env {
		// Skip already-migrated and pass through unchanged
		if IsSecretRef(value) {
			result[name] = value
			continue
		}
		// Skip display-only sentinel and pass through
		if value == "{{stored}}" {
			result[name] = value
			continue
		}
		if value == "" {
			result[name] = value
			continue
		}

		// Check if this looks like a secret
		if IsSecretEnvVar(name) {
			key := CredentialKey(serverName, name)
			if err := credentials.SetToActiveBackend(key, value); err != nil {
				log.Printf("[mcp-secrets] Failed to store credential %s: %v", key, err)
				return result, migrated, err
			}

			log.Printf("[mcp-secrets] Migrated secret %s for server %s to credential store", name, serverName)
			result[name] = SecretRef(serverName, name)
			migrated++
		} else {
			// Non-secret value passes through unchanged
			result[name] = value
		}
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

// MigrateEnvSecretsFromServer migrates plaintext secrets in an MCPServerConfig's Env map
// to the credential store, replacing values with placeholders.
// Returns the count of migrated secrets and any error.
//
// The migration attempts to be atomic: all secrets are collected first and stored in
// the credential backend before any config.Env values are mutated. If any store fails,
// the config.Env map is left untouched.
func MigrateEnvSecretsFromServer(serverName string, config *MCPServerConfig) (int, error) {
	if config.Env == nil || len(config.Env) == 0 {
		return 0, nil
	}

	// Phase 1: collect secrets to migrate (name → plaintext value)
	pending := make(map[string]string, len(config.Env))
	for name, value := range config.Env {
		if IsSecretRef(value) {
			continue // Already migrated
		}
		if value == "{{stored}}" {
			continue // Display-only sentinel from the frontend — keep existing value
		}
		if !IsSecretEnvVar(name) {
			continue // Not a secret
		}
		if value == "" {
			continue // Empty value
		}
		pending[name] = value
	}

	if len(pending) == 0 {
		return 0, nil
	}

	// Phase 2: store all secrets in the credential backend.
	// If any fails, do NOT mutate config.Env — the backend entry will be
	// orphaned but the config is not left in an inconsistent state.
	for name, value := range pending {
		key := CredentialKey(serverName, name)
		if err := credentials.SetToActiveBackend(key, value); err != nil {
			log.Printf("[mcp-secrets] Failed to store credential %s: %v", key, err)
			return 0, err
		}
	}

	// Phase 3: all writes succeeded — now mutate the config.
	for name := range pending {
		config.Env[name] = SecretRef(serverName, name)
		log.Printf("[mcp-secrets] Migrated secret %s for server %s to credential store", name, serverName)
	}

	return len(pending), nil
}


