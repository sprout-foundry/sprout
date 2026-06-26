package security

import (
	"os"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// DetectedSecret describes a single secret found in tool output.
type DetectedSecret struct {
	Type    string // e.g. "Env Var Value", "Bearer Token", "API Key"
	Snippet string // Matched text, truncated to ~40 chars for display
	Line    int    // 1-based line number where found (0 if not line-scannable)
}

// RedactionResult holds the output after redaction and any secrets detected.
type RedactionResult struct {
	Content string           // The (potentially) redacted output
	Secrets []DetectedSecret // What was found (if any)
}

// OutputRedactor scans tool output for secrets using two strategies:
//  1. Environment value matching — literal secret values from env vars
//  2. Pattern matching — regex-based detection of common credential formats
type OutputRedactor struct {
	envSecretValues   map[string]string // var name -> value (sensitive env vars only)
	envSecretSnippets map[string]string // value -> var name (reverse index for fast lookup)
	envSecretLengths  []secretLength    // sorted by value length descending (longest first)
}

// secretLength pairs a secret value with its environment variable name,
// used for ordering scans longest-first to avoid partial replacements.
type secretLength struct {
	value   string
	varName string
	length  int
}

// NewOutputRedactor constructs an OutputRedactor by scanning os.Environ()
// for env vars whose names suggest they hold credentials (per
// credentials.IsSensitiveEnvName).
func NewOutputRedactor() *OutputRedactor {
	r := &OutputRedactor{
		envSecretValues:   make(map[string]string),
		envSecretSnippets: make(map[string]string),
	}

	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || v == "" {
			continue
		}

		if !credentials.IsSensitiveEnvName(k) {
			continue
		}

		// Skip values that are too short to be meaningful secrets.
		if len(v) < 8 {
			continue
		}

		// Skip values that look like absolute file paths or URLs. We only reject
		// values that *start* with a path-shaped prefix; a "/" anywhere else in
		// the value is allowed because real secrets often contain "/" (base64
		// padding, AWS secret keys, JWT signatures).
		if strings.HasPrefix(v, "/") || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") ||
			strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			continue
		}

		r.envSecretValues[k] = v
		r.envSecretSnippets[v] = k
		r.envSecretLengths = append(r.envSecretLengths, secretLength{value: v, varName: k, length: len(v)})
	}

	// Sort longest secret first so we match e.g. "sk-abc123def456" before "sk-abc123"
	// if they happen to be overlapping (unlikely but defensive).
	sort.Slice(r.envSecretLengths, func(i, j int) bool {
		return r.envSecretLengths[i].length > r.envSecretLengths[j].length
	})

	return r
}

// RedactToolOutput scans tool output for secrets, returning a redacted version
// and any secrets detected. The toolName and toolArgs are logged but do not
// change redaction behaviour per tool; they are reserved for future context-aware
// filtering.
func (r *OutputRedactor) RedactToolOutput(output string, toolName string, toolArgs map[string]interface{}) RedactionResult {
	logger := utils.GetLogger(false)
	logger.Logf("security: redacting output from tool=%s", toolName)

	content := output
	var secrets []DetectedSecret

	// Pass 1 — environment value scan: replace literal secret values with
	// a tagged placeholder so operators can tell which var leaked.
	content, secrets = r.redactEnvValues(content)

	// Pass 2 — pattern scan via the gitleaks-backed secretdetect scanner.
	patternSecrets := r.detectAndRedactPatterns(&content)
	secrets = append(secrets, patternSecrets...)

	if len(secrets) == 0 {
		secrets = nil
	}

	return RedactionResult{
		Content: content,
		Secrets: secrets,
	}
}

// RedactFileContent is a convenience wrapper equivalent to RedactToolOutput
// but with toolName set to "read_file" and filePath attached for context.
func (r *OutputRedactor) RedactFileContent(content string, filePath string) RedactionResult {
	args := map[string]interface{}{
		"path": filePath,
	}
	return r.RedactToolOutput(content, "read_file", args)
}

// redactEnvValues scans each line for literal secret values from the
// environment and replaces them with tagged [REDACTED:VARNAME] placeholders.
func (r *OutputRedactor) redactEnvValues(content string) (string, []DetectedSecret) {
	var secrets []DetectedSecret

	// Build the replacement map in a single pass so we don't double-replace.
	// Process line-by-line to report accurate line numbers.
	lines := strings.Split(content, "\n")
	for lineIdx, line := range lines {
		for _, sl := range r.envSecretLengths {
			if strings.Contains(line, sl.value) {
				// Avoid replacing text that is already a redaction placeholder.
				if strings.Contains(line, "[REDACTED:"+sl.varName+"]") {
					continue
				}

				line = strings.ReplaceAll(line, sl.value, "[REDACTED:"+sl.varName+"]")

				snippet := sl.value
				if len(snippet) > 40 {
					snippet = snippet[:37] + "..."
				}
				secrets = append(secrets, DetectedSecret{
					Type:    "Env Var Value",
					Snippet: snippet,
					Line:    lineIdx + 1, // 1-based
				})
			}
		}
		lines[lineIdx] = line
	}

	return strings.Join(lines, "\n"), secrets
}

// detectAndRedactPatterns applies gitleaks-backed secret detection to content
// (mutating the pointer). Returns any secrets detected.
//
// Replacements use self-disclosing tokens of the form
// [REDACTED:rule=<ruleID>,len=<n>,entropy=<x.x>] so a downstream LLM reader can
// tell a display-layer redaction apart from real file content.
func (r *OutputRedactor) detectAndRedactPatterns(content *string) []DetectedSecret {
	scanner, err := secretdetect.Default()
	if err != nil || scanner == nil {
		return nil
	}

	matches := scanner.Scan(*content)
	if len(matches) == 0 {
		return nil
	}

	*content = secretdetect.Redact(*content, matches)

	secrets := make([]DetectedSecret, 0, len(matches))
	for _, m := range matches {
		snippet := m.Secret
		if snippet == "" {
			snippet = m.Match
		}
		if len(snippet) > 40 {
			snippet = snippet[:37] + "..."
		}
		secrets = append(secrets, DetectedSecret{
			Type:    m.RuleID,
			Snippet: snippet,
			Line:    m.StartLine,
		})
	}
	return secrets
}
