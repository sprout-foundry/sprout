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

// MigrateSecretsToCredentialsField migrates secrets from the Env map to the Credentials field.
// This is a one-time migration for existing configs that used Env for secrets.
// Returns the count of migrated secrets and any error.
//
// The migration:
// 1. Reads all secrets from config.Env (which may contain plaintext or placeholders)
// 2. Stores them in the credential backend (if not already stored)
// 3. Removes them from config.Env
// 4. Adds them to config.Credentials with placeholder values
func MigrateSecretsToCredentialsField(serverName string, config *MCPServerConfig) (int, error) {
	if config.Env == nil || len(config.Env) == 0 {
		return 0, nil
	}

	// Phase 1: collect secrets from Env (both plaintext and placeholders)
	pending := make(map[string]string, len(config.Env))
	for name, value := range config.Env {
		if IsSecretRef(value) {
			// Already a placeholder - extract env var name and ensure it's in credential store
			_, envVarName, ok := ParseSecretRef(value)
			if !ok {
				continue // Invalid placeholder, skip
			}
			// Check if already in Credentials
			if config.Credentials != nil && config.Credentials[envVarName] == value {
				continue // Already migrated
			}
			pending[name] = value
		} else if IsSecretEnvVar(name) && value != "" && value != "{{stored}}" {
			// Plaintext secret - needs to be stored
			pending[name] = value
		}
	}

	if len(pending) == 0 {
		return 0, nil
	}

	// Phase 2: ensure all secrets are in the credential backend
	for name, value := range pending {
		var key string
		var actualValue string

		if IsSecretRef(value) {
			// Extract env var name from placeholder
			_, envVarName, ok := ParseSecretRef(value)
			if !ok {
				continue
			}
			key = CredentialKey(serverName, envVarName)
			// Verify the credential exists in the store
			_, _, err := credentials.GetFromActiveBackend(key)
			if err != nil {
				log.Printf("[mcp-secrets] Warning: credential %s not found, skipping", key)
				continue
			}
			actualValue = value // Keep the placeholder
		} else {
			// Plaintext value - store it
			key = CredentialKey(serverName, name)
			if err := credentials.SetToActiveBackend(key, value); err != nil {
				log.Printf("[mcp-secrets] Failed to store credential %s: %v", key, err)
				return 0, err
			}
			actualValue = SecretRef(serverName, name)
		}

		// Add to Credentials map
		if config.Credentials == nil {
			config.Credentials = make(map[string]string)
		}
		config.Credentials[name] = actualValue
	}

	// Phase 3: remove migrated secrets from Env
	for name := range pending {
		delete(config.Env, name)
	}

	return len(pending), nil
}

// ResolveCredentialsForServer resolves all credential placeholders for a server
// and returns a map of env var name -> actual value.
// This is used when starting an MCP server to build the full environment.
func ResolveCredentialsForServer(serverName string, config *MCPServerConfig) (map[string]string, error) {
	if config.Credentials == nil || len(config.Credentials) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(config.Credentials))

	for envVarName, value := range config.Credentials {
		if !IsSecretRef(value) {
			// Not a placeholder, use as-is (shouldn't happen, but be safe)
			result[envVarName] = value
			continue
		}

		// Parse the placeholder
		_, actualEnvVarName, ok := ParseSecretRef(value)
		if !ok {
			log.Printf("[mcp-secrets] Invalid credential placeholder for %s/%s, skipping", serverName, envVarName)
			continue
		}

		// Get from credential store
		key := CredentialKey(serverName, actualEnvVarName)
		credValue, _, err := credentials.GetFromActiveBackend(key)
		if err != nil {
			log.Printf("[mcp-secrets] Error getting credential %s: %v", key, err)
			continue
		}

		// If empty, fall back to OS environment
		if credValue == "" {
			credValue = os.Getenv(actualEnvVarName)
			if credValue == "" {
				log.Printf("[mcp-secrets] Credential %s not found in store or OS env, skipping", key)
				continue
			}
		}

		result[envVarName] = credValue
	}

	return result, nil
}

// BuildFullEnvForServer combines non-secret Env vars with resolved credentials.
// Returns the complete environment map to use when starting the MCP server.
// It resolves both the Credentials map and any placeholder refs that remain in Env
// (e.g. when MigrateEnvSecretsFromServer was called but MigrateSecretsToCredentialsField
// has not yet moved the placeholders from Env to Credentials).
func BuildFullEnvForServer(serverName string, config *MCPServerConfig) (map[string]string, error) {
	// Start with non-secret Env vars
	result := make(map[string]string)
	if config.Env != nil {
		for k, v := range config.Env {
			result[k] = v
		}
	}

	// Resolve credentials from the Credentials map
	creds, err := ResolveCredentialsForServer(serverName, config)
	if err != nil {
		return result, err
	}

	for k, v := range creds {
		result[k] = v
	}

	// Also resolve any placeholder refs that remain in result (from Env map)
	// where Credentials may not have been populated yet.
	for k, v := range result {
		if IsSecretRef(v) {
			_, actualEnvVarName, ok := ParseSecretRef(v)
			if !ok {
				continue
			}
			key := CredentialKey(serverName, actualEnvVarName)
			credValue, _, getErr := credentials.GetFromActiveBackend(key)
			if getErr != nil {
				log.Printf("[mcp-secrets] Error resolving env placeholder %s: %v", key, getErr)
				continue
			}
			if credValue == "" {
				credValue = os.Getenv(actualEnvVarName)
			}
			if credValue != "" {
				result[k] = credValue
			} else {
				log.Printf("[mcp-secrets] Credential %s not found in store or OS env", key)
			}
		}
	}

	return result, nil
}

// buildAuthHeaders returns HTTP headers to set based on resolved credentials.
// It iterates through all resolved env/credential values and maps them to HTTP headers:
// - Authorization or GITHUB_PERSONAL_ACCESS_TOKEN -> "Authorization: Bearer {value}"
// - Other credentials containing "-" (like X-API-Key, X-Auth-Token) -> header name as-is
// This allows users to configure arbitrary auth headers for HTTP MCP servers
// through the credential management UI.
func buildAuthHeaders(serverName string, config *MCPServerConfig) (map[string]string, error) {
	// Get all resolved environment variables (Env + Credentials)
	resolvedEnv, err := BuildFullEnvForServer(serverName, config)
	if err != nil {
		return nil, err
	}

	headers := make(map[string]string)

	for envVarName, value := range resolvedEnv {
		if value == "" {
			continue
		}

		envVarUpper := strings.ToUpper(envVarName)

		// Handle Authorization header
		if envVarUpper == "AUTHORIZATION" || envVarUpper == "GITHUB_PERSONAL_ACCESS_TOKEN" {
			headers["Authorization"] = "Bearer " + value
		} else if strings.Contains(envVarUpper, "-") {
			// Handle header-like env vars (e.g., X-API-Key, X-Auth-Token)
			// Normalize: convert env var name to HTTP header format
			// e.g., "X_API_KEY" -> "X-Api-Key", "x_auth_token" -> "X-Auth-Token"
			headerName := normalizeHeaderName(envVarName)
			headers[headerName] = value
		}
	}

	return headers, nil
}

// normalizeHeaderName converts an environment variable name to HTTP header format.
// Splits on both underscores and hyphens, capitalizes each segment, and joins with hyphens.
// e.g., "X_API_KEY" -> "X-Api-Key", "x-auth_token" -> "X-Auth-Token", "X-API-Key" -> "X-Api-Key"
func normalizeHeaderName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-'
	})
	if len(parts) == 0 {
		return ""
	}

	// Capitalize first letter and lowercase the rest of each part, then join with hyphens
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}

	return strings.Join(parts, "-")
}


