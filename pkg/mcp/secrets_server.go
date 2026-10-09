package mcp

// secrets_server.go — the server-side secret layer: migrating env
// secrets into the credentials backend (MigrateEnvSecretsFromServer,
// MigrateSecretsToCredentialsField), resolving credentials per server
// (ResolveCredentialsForServer, BuildFullEnvForServer), and auth-header
// building (buildAuthHeaders, normalizeHeaderName). Split out of secrets.go.
import (
	"log"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

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

// buildRequestHeaders returns the HTTP headers to send on every request:
//  1. The server's configured Headers map (values may be credential
//     placeholders — {{credential:...}} — which resolve from the store).
//  2. Auth headers derived from resolved credentials: Authorization or
//     GITHUB_PERSONAL_ACCESS_TOKEN -> Bearer; hyphenated names
//     (X-API-Key...) -> a header of the same name. Kept for configs written
//     before the Headers field existed.
//  3. A Bearer fallback: a credential named like an API token
//     (FIGMA_TOKEN, API_KEY...) that would otherwise silently go nowhere
//     on an HTTP server is sent as "Authorization: Bearer <value>".
//
// Explicit Headers always win over derived ones.
func buildRequestHeaders(serverName string, config *MCPServerConfig) (map[string]string, error) {
	resolvedEnv, err := BuildFullEnvForServer(serverName, config)
	if err != nil {
		return nil, err
	}

	headers := make(map[string]string)

	// (1) Explicit headers, resolving credential placeholders in values.
	for name, value := range config.Headers {
		if value == "" {
			continue
		}
		if IsSecretRef(value) {
			_, envVarName, ok := ParseSecretRef(value)
			if !ok {
				continue
			}
			key := CredentialKey(serverName, envVarName)
			stored, _, getErr := credentials.GetFromActiveBackend(key)
			if getErr != nil || stored == "" {
				stored = os.Getenv(envVarName)
			}
			if stored == "" {
				log.Printf("[mcp] header %s for %s references credential %s which is not set", name, serverName, key)
				continue
			}
			value = stored
		}
		headers[name] = value
	}

	derived := make(map[string]string)
	for envVarName, value := range resolvedEnv {
		if value == "" {
			continue
		}

		envVarUpper := strings.ToUpper(envVarName)

		// Handle Authorization header
		if envVarUpper == "AUTHORIZATION" || envVarUpper == "GITHUB_PERSONAL_ACCESS_TOKEN" {
			derived["Authorization"] = "Bearer " + value
		} else if strings.Contains(envVarUpper, "-") {
			// Handle header-like env vars (e.g., X-API-Key, X-Auth-Token)
			// Normalize: convert env var name to HTTP header format
			// e.g., "X_API_KEY" -> "X-Api-Key", "x_auth_token" -> "X-Auth-Token"
			headerName := normalizeHeaderName(envVarName)
			derived[headerName] = value
		} else if looksLikeTokenEnvVar(envVarUpper) {
			// (3) Fallback: a token-shaped credential with no header
			// mapping would otherwise be silently unused by HTTP servers.
			derived["Authorization"] = "Bearer " + value
		}
	}

	for name, value := range derived {
		if _, exists := headers[name]; !exists {
			headers[name] = value
		}
	}

	return headers, nil
}

// looksLikeTokenEnvVar reports whether the (uppercased) env var name names a
// bearer-style API token. Only used for HTTP servers, where a bare env var
// has no other way to reach the server.
func looksLikeTokenEnvVar(upper string) bool {
	switch upper {
	case "API_TOKEN", "API_KEY", "ACCESS_TOKEN", "AUTH_TOKEN", "BEARER_TOKEN",
		"MCP_AUTH_TOKEN", "PERSONAL_ACCESS_TOKEN":
		return true
	}
	return strings.HasSuffix(upper, "_TOKEN") || strings.HasSuffix(upper, "_API_KEY")
}

// buildAuthHeaders is the pre-Headers-field entry point, kept for callers
// that predate buildRequestHeaders.
func buildAuthHeaders(serverName string, config *MCPServerConfig) (map[string]string, error) {
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
