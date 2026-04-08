package credentials

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// IsSensitiveEnvName
// ---------------------------------------------------------------------------

func TestIsSensitiveEnvName(t *testing.T) {
	t.Run("known_secret_vars", func(t *testing.T) {
		knownSecrets := []string{
			"GITHUB_PERSONAL_ACCESS_TOKEN",
			"OPENAI_API_KEY",
			"ANTHROPIC_API_KEY",
			"DEEPINFRA_API_KEY",
			"OPENROUTER_API_KEY",
			"LMSTUDIO_API_KEY",
			"JINAAI_API_KEY",
		}
		for _, name := range knownSecrets {
			t.Run(name, func(t *testing.T) {
				assert.True(t, IsSensitiveEnvName(name),
					"expected %q to be identified as sensitive", name)
			})
		}
	})

	t.Run("secret_keywords", func(t *testing.T) {
		cases := []struct {
			name string
		}{
			{"MY_TOKEN"},
			{"MY_SECRET"},
			{"MY_PASSWORD"},
			{"MY_AUTH"},
			{"MY_PRIVATE_KEY"},
			{"PAT_VALUE"},
			{"BEARER_TOKEN"},
			{"CREDENTIAL_FILE"},
			{"MY_PRIVATE"},
			{"PASSWD_FILE"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				assert.True(t, IsSensitiveEnvName(tc.name),
					"expected %q to be identified as sensitive via keyword match", tc.name)
			})
		}
	})

	t.Run("non_secret_prefixes_excluded", func(t *testing.T) {
		// These may contain keywords like KEY but the prefixes should shield them.
		cases := []struct {
			name string
		}{
			{"PATH_EXTRA"},
			{"HOME_DIR"},
			{"NODE_ENV"},
			{"PYTHON_VERSION"},
			{"JAVA_HOME"},
			{"GOARCH"},
			{"GOPATH"},
			{"GOROOT"},
			{"NPM_CONFIG"},
			{"LEDIT_MODE"},
			{"MCP_DEBUG"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				assert.False(t, IsSensitiveEnvName(tc.name),
					"expected %q to NOT be identified as sensitive (prefix exclusion)", tc.name)
			})
		}
	})

	t.Run("ordinary_env_vars", func(t *testing.T) {
		ordinary := []string{
			"DISPLAY",
			"TERM",
			"SHELL",
			"LANG",
			"EDITOR",
			"USER",
			"HOSTNAME",
			"PWD",
			"OLDPWD",
			"SHLVL",
		}
		for _, name := range ordinary {
			t.Run(name, func(t *testing.T) {
				assert.False(t, IsSensitiveEnvName(name),
					"expected %q to NOT be identified as sensitive", name)
			})
		}
	})

	t.Run("case_insensitive", func(t *testing.T) {
		assert.True(t, IsSensitiveEnvName("openai_api_key"))
		assert.True(t, IsSensitiveEnvName("OpenAI_Api_Key"))
		assert.True(t, IsSensitiveEnvName("my_token"))
	})

	t.Run("whitespace_handling", func(t *testing.T) {
		assert.True(t, IsSensitiveEnvName("  OPENAI_API_KEY  "),
			"whitespace-padded known secret should still be detected")
		assert.True(t, IsSensitiveEnvName("\tMY_TOKEN\n"),
			"whitespace-padded keyword match should still be detected")
	})

	t.Run("edge_cases", func(t *testing.T) {
		assert.False(t, IsSensitiveEnvName(""),
			"empty string should not be sensitive")
		// A var like "KEYNOTE" could be ambiguous; the keyword "KEY" without a
		// prefix exclusion means it IS flagged as sensitive currently.
		assert.True(t, IsSensitiveEnvName("MY_KEYNOTE"),
			"env var containing keyword KEY is flagged as sensitive")
	})
}

// ---------------------------------------------------------------------------
// RedactMap
// ---------------------------------------------------------------------------

func TestRedactMap(t *testing.T) {
	t.Run("nil_input", func(t *testing.T) {
		assert.Nil(t, RedactMap(nil))
	})

	t.Run("empty_map", func(t *testing.T) {
		result := RedactMap(map[string]string{})
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("values_are_masked", func(t *testing.T) {
		input := map[string]string{
			"openai":     "sk-abcdef1234567890",
			"short_val":  "ab",
			"medium_val": "abcd",
		}
		result := RedactMap(input)

		// len 19 >= 8 → first 4 + "****"
		assert.Equal(t, "sk-a****", result["openai"])
		// len 2 < 4 → "****"
		assert.Equal(t, "****", result["short_val"])
		// len 4 >= 4 → first 2 + "****"
		assert.Equal(t, "ab****", result["medium_val"])
	})

	t.Run("credential_reference_kept", func(t *testing.T) {
		input := map[string]string{
			"env1": "{{credential:mcp/server/OPENAI_API_KEY}}",
			"env2": "{{stored}}",
		}
		result := RedactMap(input)

		assert.Equal(t, "{{credential:mcp/server/OPENAI_API_KEY}}", result["env1"],
			"credential reference placeholder should be preserved")
		assert.Equal(t, "{{stored}}", result["env2"],
			"{{stored}} placeholder should be preserved")
	})

	t.Run("empty_string_values_kept", func(t *testing.T) {
		input := map[string]string{
			"empty": "",
		}
		result := RedactMap(input)
		assert.Equal(t, "", result["empty"],
			"empty string values should be kept as-is")
	})

	t.Run("keys_are_preserved", func(t *testing.T) {
		input := map[string]string{
			"server":   "localhost",
			"port":     "8080",
			"password": "super-secret",
		}
		result := RedactMap(input)

		require.Len(t, result, 3)
		assert.Contains(t, result, "server")
		assert.Contains(t, result, "port")
		assert.Contains(t, result, "password")
	})

	t.Run("original_map_not_modified", func(t *testing.T) {
		input := map[string]string{
			"key": "value12345",
		}
		originalCopy := map[string]string{"key": "value12345"}

		_ = RedactMap(input)

		assert.Equal(t, originalCopy, input,
			"RedactMap should not mutate the input map")
	})

	t.Run("mixed_values", func(t *testing.T) {
		input := map[string]string{
			"secret":       "sk-abcdef1234567890abcdef",
			"cred_ref":     "{{credential:mcp/server/ENV}}",
			"stored_ref":   "{{stored}}",
			"empty_val":    "",
			"short_secret": "x",
		}
		result := RedactMap(input)

		assert.Equal(t, "sk-a****", result["secret"])
		assert.Equal(t, "{{credential:mcp/server/ENV}}", result["cred_ref"])
		assert.Equal(t, "{{stored}}", result["stored_ref"])
		assert.Equal(t, "", result["empty_val"])
		assert.Equal(t, "****", result["short_secret"])
	})
}

// ---------------------------------------------------------------------------
// RedactEnvMap
// ---------------------------------------------------------------------------

func TestRedactEnvMap(t *testing.T) {
	t.Run("nil_input", func(t *testing.T) {
		assert.Nil(t, RedactEnvMap(nil))
	})

	t.Run("empty_map", func(t *testing.T) {
		result := RedactEnvMap(map[string]string{})
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("sensitive_keys_redacted", func(t *testing.T) {
		input := map[string]string{
			"API_TOKEN":   "secret123",
			"MY_PASSWORD": "hunter2",
			"AUTH_KEY":    "abcdefg",
		}
		result := RedactEnvMap(input)

		assert.Equal(t, "[REDACTED]", result["API_TOKEN"])
		assert.Equal(t, "[REDACTED]", result["MY_PASSWORD"])
		assert.Equal(t, "[REDACTED]", result["AUTH_KEY"])
	})

	t.Run("non_sensitive_keys_preserved", func(t *testing.T) {
		input := map[string]string{
			"PATH_EXTRA":   "/usr/bin",
			"HOME_DIR":     "/home/user",
			"DISPLAY":      ":0",
			"EDITOR":       "vim",
		}
		result := RedactEnvMap(input)

		assert.Equal(t, "/usr/bin", result["PATH_EXTRA"])
		assert.Equal(t, "/home/user", result["HOME_DIR"])
		assert.Equal(t, ":0", result["DISPLAY"])
		assert.Equal(t, "vim", result["EDITOR"])
	})

	t.Run("mixed_map", func(t *testing.T) {
		input := map[string]string{
			"OPENAI_API_KEY": "sk-proj-abc123",
			"PATH":            "/usr/local/bin:/usr/bin:/bin",
			"MY_SECRET":       "top-secret-value",
			"NODE_ENV":        "production",
			"TERM":            "xterm-256color",
		}
		result := RedactEnvMap(input)

		assert.Equal(t, "[REDACTED]", result["OPENAI_API_KEY"])
		assert.Equal(t, "/usr/local/bin:/usr/bin:/bin", result["PATH"])
		assert.Equal(t, "[REDACTED]", result["MY_SECRET"])
		assert.Equal(t, "production", result["NODE_ENV"])
		assert.Equal(t, "xterm-256color", result["TERM"])
	})

	t.Run("original_map_not_modified", func(t *testing.T) {
		input := map[string]string{
			"MY_TOKEN": "secret-value",
			"EDITOR":   "vim",
		}
		original := map[string]string{
			"MY_TOKEN": "secret-value",
			"EDITOR":   "vim",
		}

		_ = RedactEnvMap(input)

		assert.Equal(t, original["MY_TOKEN"], input["MY_TOKEN"],
			"original sensitive value should still be present")
		assert.Equal(t, original["EDITOR"], input["EDITOR"],
			"original non-sensitive value should still be present")
	})

	t.Run("known_secret_vars_redacted", func(t *testing.T) {
		knownVars := []string{
			"GITHUB_PERSONAL_ACCESS_TOKEN",
			"OPENAI_API_KEY",
			"ANTHROPIC_API_KEY",
			"DEEPINFRA_API_KEY",
			"OPENROUTER_API_KEY",
		}
		input := make(map[string]string, len(knownVars))
		for _, v := range knownVars {
			input[v] = "some-secret-value"
		}

		result := RedactEnvMap(input)
		for _, v := range knownVars {
			assert.Equal(t, "[REDACTED]", result[v],
				"expected known secret var %q to be redacted", v)
		}
	})
}

// ---------------------------------------------------------------------------
// RedactLogLine
// ---------------------------------------------------------------------------

func TestRedactLogLine(t *testing.T) {
	t.Run("noop_on_normal_lines", func(t *testing.T) {
		const line = "normal log message about processing request"
		assert.Equal(t, line, RedactLogLine(line))
	})

	t.Run("authorization_bearer_header", func(t *testing.T) {
		const input = "Authorization: Bearer sk-abc12345678901234567"
		result := RedactLogLine(input)
		// The regex matches the entire "Authorization: Bearer sk-..." so the whole
		// thing (including the label) is replaced with [REDACTED].
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("authorization_basic_header", func(t *testing.T) {
		const input = "Authorization: Basic dXNlcjpwYXNz"
		result := RedactLogLine(input)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("json_api_key", func(t *testing.T) {
		const input = `"api_key": "sk-abc12345678901234567"`
		result := RedactLogLine(input)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("json_apikey", func(t *testing.T) {
		const input = `"apikey": "sk-abc12345678901234567"`
		result := RedactLogLine(input)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("json_token", func(t *testing.T) {
		const input = `"token": "ghp_abcdefghijklmnopqrstuvwxyz123456"`
		result := RedactLogLine(input)
		assert.Equal(t, `[REDACTED]`, result)
	})

	t.Run("json_secret", func(t *testing.T) {
		const input = `"secret": "mysecret"`
		result := RedactLogLine(input)
		assert.Equal(t, `[REDACTED]`, result)
	})

	t.Run("json_password", func(t *testing.T) {
		const input = `"password": "mypass"`
		result := RedactLogLine(input)
		assert.Equal(t, `[REDACTED]`, result)
	})

	t.Run("openai_key_pattern", func(t *testing.T) {
		const input = "using key sk-abcdef1234567890abcdef1234567890 in request"
		result := RedactLogLine(input)
		assert.Contains(t, result, "[REDACTED]",
			"sk-... pattern should be redacted")
		assert.NotContains(t, result, "sk-abcdef",
			"original key should be removed")
	})

	t.Run("github_pat_pattern", func(t *testing.T) {
		// ghp_ pattern requires 36+ trailing chars per regex
		const input = "token: ghp_abcdefghijklmnopqrstuvwxyz1234567890"
		result := RedactLogLine(input)
		assert.Contains(t, result, "[REDACTED]")
		assert.NotContains(t, result, "ghp_abcdefghijklmnopqrstuvwxyz1234567890")
	})

	t.Run("slack_token_pattern", func(t *testing.T) {
		const input = "xoxb-123456789012-123456789012-abcdefghijklmnop"
		result := RedactLogLine(input)
		assert.Contains(t, result, "[REDACTED]")
		assert.NotContains(t, result, "xoxb-123456789012")
	})

	t.Run("case_insensitive", func(t *testing.T) {
		const input = `"API_KEY": "sk-abc12345678901234567"`
		result := RedactLogLine(input)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("multiple_patterns_in_one_line", func(t *testing.T) {
		input := `Authorization: Bearer sk-abc12345678901234567, "token": "ghp_abcdefghijklmnopqrstuvwxyz1234567890"`
		result := RedactLogLine(input)
		// Both patterns should be redacted
		assert.NotContains(t, result, "sk-abc12345678901234567",
			"first sensitive value should be redacted")
		assert.NotContains(t, result, "ghp_abcdefghijklmnopqrstuvwxyz1234567890",
			"second sensitive value should be redacted")
		assert.True(t, strings.Count(result, "[REDACTED]") >= 2,
			"expected at least 2 [REDACTED] markers, got: %s", result)
	})

	t.Run("no_false_positives", func(t *testing.T) {
		// A line with the word "key" in a non-credential context should be preserved
		const input = "the key is to be found in the documentation"
		result := RedactLogLine(input)
		assert.Equal(t, input, result,
			"non-credential usage of 'key' should not be redacted")
	})

	t.Run("json_authorization_bearer", func(t *testing.T) {
		const input = `"Authorization": "Bearer sk-abc12345678901234567"`
		result := RedactLogLine(input)
		assert.Equal(t, "[REDACTED]", result,
			"JSON-formatted Authorization Bearer should be redacted")
	})

	t.Run("json_authorization_basic", func(t *testing.T) {
		const input = `"Authorization": "Basic dXNlcjpwYXNz"`
		result := RedactLogLine(input)
		assert.Equal(t, "[REDACTED]", result,
			"JSON-formatted Authorization Basic should be redacted")
	})

	t.Run("log_line_with_surrounding_context", func(t *testing.T) {
		const input = `[INFO] sending request with Authorization: Bearer sk-abc12345678901234567 to endpoint`
		result := RedactLogLine(input)
		assert.NotContains(t, result, "sk-abc12345678901234567",
			"token should be redacted even with surrounding text")
		assert.Contains(t, result, "[INFO]",
			"non-sensitive parts of the log should be preserved")
		assert.Contains(t, result, "to endpoint",
			"non-sensitive parts of the log should be preserved")
	})

	t.Run("apikey_key_equals_pattern", func(t *testing.T) {
		const input = `apikey=sk-abc12345678901234567`
		result := RedactLogLine(input)
		assert.Contains(t, result, "[REDACTED]",
			"apikey=value pattern should be redacted")
		assert.NotContains(t, result, "sk-abc12345678901234567")
	})

	t.Run("github_other_prefixes", func(t *testing.T) {
		// gh{o,u,s,r}_ patterns require 36+ trailing chars per regex
		suffix := "abcdefghijklmnopqrstuvwxyz1234567890" // 36 chars
		cases := []struct {
			name  string
			input string
		}{
			{"gho", "gho_" + suffix},
			{"ghu", "ghu_" + suffix},
			{"ghs", "ghs_" + suffix},
			{"ghr", "ghr_" + suffix},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				result := RedactLogLine(tc.input)
				assert.NotContains(t, result, suffix,
					"gh* pattern should be redacted")
			})
		}
	})

	t.Run("xox_slack_prefixes", func(t *testing.T) {
		cases := []struct {
			name  string
			input string
		}{
			{"xoxp", "xoxp-123456789012-123456789012-abcdefghijklmnop"},
			{"xoxa", "xoxa-123456789012-123456789012-abcdefghijklmnop"},
			{"xoxr", "xoxr-123456789012-123456789012-abcdefghijklmnop"},
			{"xoxs", "xoxs-123456789012-123456789012-abcdefghijklmnop"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				result := RedactLogLine(tc.input)
				assert.NotContains(t, result, "abcdefghijklmnop",
					"Slack xox* pattern %q should be redacted", tc.name)
			})
		}
	})
}

// ---------------------------------------------------------------------------
// isCredentialRef (unexported, tested indirectly via RedactMap, but let's
// write a focused unit test to lock down its contract).
// ---------------------------------------------------------------------------

func TestIsCredentialRef(t *testing.T) {
	t.Run("credential_template", func(t *testing.T) {
		assert.True(t, isCredentialRef("{{credential:mcp/server/OPENAI_API_KEY}}"))
	})

	t.Run("stored_template", func(t *testing.T) {
		assert.True(t, isCredentialRef("{{stored}}"))
	})

	t.Run("empty_string", func(t *testing.T) {
		assert.True(t, isCredentialRef(""))
	})

	t.Run("plain_value", func(t *testing.T) {
		assert.False(t, isCredentialRef("sk-abc123"))
		assert.False(t, isCredentialRef("some random text"))
	})
}
