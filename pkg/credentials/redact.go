package credentials

import (
	"regexp"
	"strings"
)

// commonSecretPrefixes are env var prefixes that are typically NOT secrets.
// NOTE: Keep these lists aligned with pkg/mcp/secrets.go commonSecretPrefixes,
// secretKeywords, and knownSecretVars. Due to a circular-import constraint
// (pkg/mcp → pkg/credentials), the lists are duplicated. If you change one,
// update the other.
var commonSecretPrefixes = []string{
	"PATH", "HOME", "NODE", "PYTHON", "JAVA", "GO", "GOPATH", "GOROOT",
	"NPM", "NVM", "CARGO", "RUSTUP", "LEDIT_", "MCP_",
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

// logRedactionPatterns are regex patterns that match potential credential values in log lines
var logRedactionPatterns = []*regexp.Regexp{
	// Authorization headers
	regexp.MustCompile(`(?i)"Authorization"\s*:\s*"Bearer\s+[^"]+"`),
	regexp.MustCompile(`(?i)"Authorization"\s*:\s*"Basic\s+[^"]+"`),
	regexp.MustCompile(`Authorization:\s*Bearer\s+\S+`),
	regexp.MustCompile(`Authorization:\s*Basic\s+\S+`),

	// JSON API key patterns
	regexp.MustCompile(`(?i)"api_key"\s*:\s*"[^"]+"`),
	regexp.MustCompile(`(?i)"apikey"\s*:\s*"[^"]+"`),
	regexp.MustCompile(`(?i)"token"\s*:\s*"[^"]+"`),
	regexp.MustCompile(`(?i)"secret"\s*:\s*"[^"]+"`),
	regexp.MustCompile(`(?i)"password"\s*:\s*"[^"]+"`),

	// Common API key patterns (sk-..., ghp_..., xoxb-..., etc.)
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`gho_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`ghu_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`ghs_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`ghr_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`xox[baprs]-[a-zA-Z0-9-]+`),
	regexp.MustCompile(`ghpat_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`(pika|pika_)[a-zA-Z0-9_-]{20,}`),
	regexp.MustCompile(`(?i)api[_-]?key["']?\s*[:=]\s*["']?[a-zA-Z0-9_-]{20,}`),
}

// RedactLogLine scans a single log line for potential credential values and
// replaces them with [REDACTED]. It catches:
//   - "Authorization: Bearer sk-..." or "Authorization: Basic ..."
//   - JSON fields like "api_key": "sk-..." or "token": "..."
//   - API key patterns (sk-...., ghp_...., xoxb-...., etc.)
// Returns the redacted line.
func RedactLogLine(line string) string {
	redacted := line
	for _, pattern := range logRedactionPatterns {
		redacted = pattern.ReplaceAllString(redacted, "[REDACTED]")
	}
	return redacted
}
