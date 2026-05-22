package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewOutputRedactor verifies construction and that it populates
// envSecretValues from os.Environ() for sensitive env vars.
func TestNewOutputRedactor(t *testing.T) {
	// Set a sensitive env var before constructing the redactor.
	t.Setenv("TEST_SECRET_KEY_FOR_LEdit", "sk-test-long-secret-value-12345")

	r := NewOutputRedactor()

	assert.NotNil(t, r)
	assert.NotNil(t, r.envSecretValues)
	// The redactor should have picked up our test env var.
	val, ok := r.envSecretValues["TEST_SECRET_KEY_FOR_LEdit"]
	assert.True(t, ok, "expected TEST_SECRET_KEY_FOR_LEdit in envSecretValues")
	assert.Equal(t, "sk-test-long-secret-value-12345", val)
}

// TestRedactToolOutput_NoSecrets verifies that output with no secrets
// passes through unchanged and reports no detected secrets.
func TestRedactToolOutput_NoSecrets(t *testing.T) {
	r := NewOutputRedactor()

	output := "The quick brown fox jumps over the lazy dog.\nNo secrets here."
	result := r.RedactToolOutput(output, "shell", nil)

	assert.Equal(t, output, result.Content)
	assert.Empty(t, result.Secrets)
}

// TestRedactToolOutput_EnvVarValue verifies that an env var value is
// redacted with a tagged placeholder when it appears in output.
func TestRedactToolOutput_EnvVarValue(t *testing.T) {
	const varName = "LEdit_TEST_API_KEY"
	const varValue = "sk-unique-test-key-x9y8z7w6v5u4t3s2"
	t.Setenv(varName, varValue)

	r := NewOutputRedactor()

	output := "Connection established with key: " + varValue + " and it works."
	result := r.RedactToolOutput(output, "shell", nil)

	assert.Contains(t, result.Content, "[REDACTED:"+varName+"]")
	assert.NotContains(t, result.Content, varValue)
	assert.NotEmpty(t, result.Secrets)

	found := false
	for _, s := range result.Secrets {
		if s.Type == "Env Var Value" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected an Env Var Value secret")
}

// TestRedactToolOutput_APIKeyPattern verifies that pattern-based detection
// catches realistic API key shapes in tool output and replaces them with
// the self-disclosing [REDACTED:rule=...] token.
func TestRedactToolOutput_APIKeyPattern(t *testing.T) {
	r := NewOutputRedactor()

	output := "api_key=" + realisticOpenAIKey
	result := r.RedactToolOutput(output, "shell", nil)

	assert.Contains(t, result.Content, "[REDACTED:rule=")
	assert.NotContains(t, result.Content, realisticOpenAIKey)
	assert.NotEmpty(t, result.Secrets)

	hasPatternSecret := false
	for _, s := range result.Secrets {
		if s.Type != "Env Var Value" {
			hasPatternSecret = true
		}
	}
	assert.True(t, hasPatternSecret, "expected a pattern-based secret type")
}

// TestRedactToolOutput_APIKeyPatternJSON verifies JSON-quoted API keys
// are caught by the gitleaks-backed scanner.
func TestRedactToolOutput_APIKeyPatternJSON(t *testing.T) {
	r := NewOutputRedactor()

	output := `{"api_key": "` + realisticOpenAIKey + `"}`
	result := r.RedactToolOutput(output, "shell", nil)

	assert.Contains(t, result.Content, "[REDACTED:rule=")
	assert.NotContains(t, result.Content, realisticOpenAIKey)
}

// TestRedactToolOutput_BearerToken verifies Authorization: Bearer tokens
// are detected. The token uses a realistic OpenAI key shape so the
// gitleaks openai-api-key rule fires.
func TestRedactToolOutput_BearerToken(t *testing.T) {
	r := NewOutputRedactor()

	output := "Authorization: Bearer " + realisticOpenAIKey
	result := r.RedactToolOutput(output, "shell", nil)

	assert.Contains(t, result.Content, "[REDACTED:rule=")
	assert.NotContains(t, result.Content, realisticOpenAIKey)
	assert.NotEmpty(t, result.Secrets)
}

// TestRedactToolOutput_MixedSecrets verifies redaction when both env var
// values and pattern-based secrets are present.
func TestRedactToolOutput_MixedSecrets(t *testing.T) {
	const varName = "LEdit_MIXED_TOKEN"
	// Avoid substrings "abc", "test", "demo" etc. (case-insensitive) in env var
	// value that DetectSecurityConcerns would filter out on pattern match.
	// Use only chars D-Z and digits 4-9 to avoid triggering false-positive filters.
	const varValue = "SUPERSECRETVALUEDFFGHJKLMNQPRSTUVWXYZDFFGHJKLMNQP"
	t.Setenv(varName, varValue)

	r := NewOutputRedactor()

	// Bearer token: use chars/digits that don't form "abc" or "123" substrings.
	bearerToken := "sk-proj-DFFGHJKLMNQPRSTUVWXYZDFFGHJKLMNQPRSTUVWXYZDFFG"
	output := "Using secret " + varValue + " with Authorization: Bearer " + bearerToken
	result := r.RedactToolOutput(output, "shell", nil)

	// The env var value should be tagged-redacted.
	assert.Contains(t, result.Content, "[REDACTED:"+varName+"]")
	assert.NotContains(t, result.Content, varValue)

	// There should be at least one detected secret (the env var secret,
	// and possibly the bearer token pattern secret).
	assert.NotEmpty(t, result.Secrets)

	// Ensure env var secret is present.
	hasEnvVarSecret := false
	for _, s := range result.Secrets {
		if s.Type == "Env Var Value" {
			hasEnvVarSecret = true
		}
	}
	assert.True(t, hasEnvVarSecret, "expected Env Var Value secret in mixed output")
}

// TestRedactFileContent is a convenience wrapper test — should behave the
// same as RedactToolOutput with toolName="read_file".
func TestRedactFileContent(t *testing.T) {
	const varName = "LEdit_FILECONTENT_SECRET"
	const varValue = "filecontentsecretvaluezyxwvutsrqponmlkjihgfedcba0"
	t.Setenv(varName, varValue)

	r := NewOutputRedactor()

	content := "some config file\nsecret_key = " + varValue + "\nend of file"

	resultDirect := r.RedactToolOutput(content, "read_file", map[string]interface{}{"path": "/tmp/config.cfg"})
	resultFile := r.RedactFileContent(content, "/tmp/config.cfg")

	assert.Equal(t, resultDirect.Content, resultFile.Content)
	assert.Equal(t, resultDirect.Secrets, resultFile.Secrets)
	assert.Contains(t, resultFile.Content, "[REDACTED:"+varName+"]")
}

// TestRedactToolOutput_PathValuesSkipped verifies that env var values
// containing "/" are NOT scanned (path-like values are skipped).
func TestRedactToolOutput_PathValuesSkipped(t *testing.T) {
	// "SECRET" keyword is in the name so IsSensitiveEnvName → true,
	// but the value contains "/" so it should be skipped.
	t.Setenv("LEdit_PATHLIKE_SECRET", "/usr/local/bin/secret-tool")

	r := NewOutputRedactor()

	// The path-like value should NOT be in the redactor's env secret values.
	_, ok := r.envSecretValues["LEdit_PATHLIKE_SECRET"]
	assert.False(t, ok, "path-like env var values should be skipped")

	// And if the value appears in output it should NOT be redacted as env var.
	output := "The path is /usr/local/bin/secret-tool"
	result := r.RedactToolOutput(output, "shell", nil)
	assert.Contains(t, result.Content, "/usr/local/bin/secret-tool")
}

// TestRedactToolOutput_ShortValuesSkipped verifies that env var values
// shorter than 8 chars are NOT scanned.
func TestRedactToolOutput_ShortValuesSkipped(t *testing.T) {
	t.Setenv("LEdit_SHORT_SECRET", "abc123")

	r := NewOutputRedactor()

	_, ok := r.envSecretValues["LEdit_SHORT_SECRET"]
	assert.False(t, ok, "env var values shorter than 8 chars should be skipped")

	output := "The short value is abc123"
	result := r.RedactToolOutput(output, "shell", nil)
	// "abc123" is only 6 chars, too short for any pattern match.
	assert.Equal(t, output, result.Content)
}

// TestRedactToolOutput_SlashInValueIsScanned verifies that env var values
// containing "/" but not starting with a path/URL prefix ARE scanned.
// This is the AWS-secret / base64-with-padding / JWT-signature shape that
// the old "skip anything with /" heuristic would silently miss.
func TestRedactToolOutput_SlashInValueIsScanned(t *testing.T) {
	const varName = "LEdit_AWS_SECRET_ACCESS_KEY"
	// Realistic AWS-secret-key shape (40 chars, mixed letters/digits, contains "/").
	const varValue = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	t.Setenv(varName, varValue)

	r := NewOutputRedactor()

	v, ok := r.envSecretValues[varName]
	assert.True(t, ok, "AWS-shape secret containing / should be tracked")
	assert.Equal(t, varValue, v)

	output := "credentials: " + varValue + " (do not log)"
	result := r.RedactToolOutput(output, "shell", nil)
	assert.Contains(t, result.Content, "[REDACTED:"+varName+"]")
	assert.NotContains(t, result.Content, varValue)
}

// TestRedactToolOutput_HTTPValuesSkipped verifies that env var values
// starting with http:// or https:// are NOT scanned.
func TestRedactToolOutput_HTTPValuesSkipped(t *testing.T) {
	t.Setenv("LEdit_HTTP_SECRET", "https://api.example.com/v1/secret-key-here")

	r := NewOutputRedactor()

	_, ok := r.envSecretValues["LEdit_HTTP_SECRET"]
	assert.False(t, ok, "URL env var values should be skipped")

	output := "The URL is https://api.example.com/v1/secret-key-here"
	result := r.RedactToolOutput(output, "shell", nil)
	assert.Contains(t, result.Content, "https://api.example.com/v1/secret-key-here")
}
