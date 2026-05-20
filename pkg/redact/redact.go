// Package redact provides functions to remove secrets from byte slices.
// It covers common credential patterns (AWS keys, GitHub tokens, API keys,
// private keys, and env-style assignments) and replaces matches with
// [REDACTED:<kind>] tokens.
package redact

import (
	"regexp"
)

// pattern defines a single redaction rule.
type pattern struct {
	re   *regexp.Regexp
	kind string
}

// patterns is the ordered list of redaction rules. Order matters:
// more-specific patterns should come before catch-all ones so that they get
// the right label.
var patterns = []pattern{
	// ── PEM private key blocks ───────────────────────────────────
	{
		regexp.MustCompile(`(?s)-----BEGIN\s+[A-Z0-9 ]*PRIVATE KEY\s*-----.*?-----END\s+[A-Z0-9 ]*PRIVATE KEY\s*-----`),
		"private-key",
	},

	// ── AWS access keys ──────────────────────────────────────────
	{
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		"aws-access-key",
	},
	{
		regexp.MustCompile(`ABIA[0-9A-Z]{16}`),
		"aws-assume-role",
	},
	{
		regexp.MustCompile(`ASIA[0-9A-Z]{16}`),
		"aws-temporary",
	},

	// ── AWS secret keys (40-char base64 after known prefix) ──────
	{
		regexp.MustCompile(`(?i)(?:aws_secret_access_key|aws_secret_key)\s*[=:]\s*\S+`),
		"aws-secret-key",
	},

	// ── GitHub tokens ────────────────────────────────────────────
	{
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,255}`),
		"github-token",
	},

	// ── Slack tokens ─────────────────────────────────────────────
	{
		regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`),
		"slack-token",
	},

	// ── OpenAI / Anthropic / generic sk-… keys ───────────────────
	{
		regexp.MustCompile(`sk-[A-Za-z0-9\-_]{20,}`),
		"api-key",
	},

	// ── Authorization / API-key headers ──────────────────────────
	{
		regexp.MustCompile(`(?i)(?:Authorization|X-API-Key)\s*:\s*\S+`),
		"http-auth-header",
	},

	// ── Generic env-style assignments ────────────────────────────
	// Matches KEY=VALUE where the key name ends with a known secret suffix.
	{
		regexp.MustCompile(`(?im)(?:^|[\s"'` + "`" + `])([A-Za-z_][A-Za-z0-9_]*(?:TOKEN|KEY|SECRET|PASSWORD|PASSWD|PASS|CREDENTIAL|AUTH_TOKEN|API_KEY))\s*[=:]\s*\S+`),
		"env-secret",
	},
	// Bare PASSWD/PASS without prefix (e.g. PASSWD=hunter2)
	{
		regexp.MustCompile(`(?im)(?:^|[\s"'` + "`" + `])(PASSWD|PASSWORD|PASS)\s*[=:]\s*\S+`),
		"env-secret",
	},
}

// Apply returns a copy of data with all recognised secret patterns replaced
// by [REDACTED:<kind>] tokens. The original slice is not modified.
func Apply(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)

	for _, p := range patterns {
		out = p.re.ReplaceAll(out, []byte("[REDACTED:"+p.kind+"]"))
	}
	return out
}

// String is a convenience wrapper around Apply for string input.
func String(s string) string {
	return string(Apply([]byte(s)))
}
