package secretdetect

import (
	"strings"
	"testing"
)

// These tests verify the failure modes that motivated bringing in gitleaks:
// the old regex-based detector flagged any string matching "API_KEY=<20+chars>"
// regardless of context. gitleaks' default config combines per-rule regexes
// with keyword pre-filters, entropy thresholds, stopwords, and a global
// allowlist of placeholder shapes — collectively they should reject the cases
// below without false positives.

// realisticOpenAIKey is a syntactically valid OpenAI key shape (the literal
// "T3BlbkFJ" marker is required by gitleaks' openai-api-key rule, plus
// exactly 20 alphanumeric chars on each side).
// This is NOT a live key — it's a static fake constructed to match the rule.
const realisticOpenAIKey = "sk-AbCdEfGhIjKlMnOpQrStT3BlbkFJ1234567890abcdefghij"

func TestScanner_DoesNotRedact_HTMLLabelLikeContent(t *testing.T) {
	s, err := Default()
	if err != nil {
		t.Fatalf("Default() failed: %v", err)
	}

	cases := []string{
		`<input value="OPENAI_API_KEY=" placeholder="OPENAI_API_KEY">`,
		`<input type="text" value="xai_API_KEY=" placeholder="enter xai API key">`,
		`<label for="key">OPENAI_API_KEY:</label>`,
		`# OPENAI_API_KEY=your_key_here`,
		`OPENAI_API_KEY=PLACEHOLDER_TEXT_GOES_HERE`,
		`<code>OPENAI_API_KEY=&lt;your-key&gt;</code>`,
		`Example: OPENAI_API_KEY=sk-...`,
		`The OPENAI_API_KEY environment variable should be set.`,
	}

	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			matches := s.Scan(in)
			if len(matches) > 0 {
				t.Errorf("expected no matches, got %d:", len(matches))
				for _, m := range matches {
					t.Errorf("  rule=%s match=%q secret=%q entropy=%.2f",
						m.RuleID, m.Match, m.Secret, m.Entropy)
				}
			}
		})
	}
}

func TestScanner_Detects_RealisticOpenAIKey(t *testing.T) {
	s, err := Default()
	if err != nil {
		t.Fatalf("Default() failed: %v", err)
	}

	cases := []string{
		realisticOpenAIKey,
		`OPENAI_API_KEY="` + realisticOpenAIKey + `"`,
		`Authorization: Bearer ` + realisticOpenAIKey,
		`{"api_key": "` + realisticOpenAIKey + `"}`,
	}

	for _, in := range cases {
		t.Run(in[:min(60, len(in))], func(t *testing.T) {
			matches := s.Scan(in)
			if len(matches) == 0 {
				t.Fatalf("expected detection, got none for input: %q", in)
			}
		})
	}
}

func TestRedact_ReplacesSecretWithSelfDisclosingToken(t *testing.T) {
	s, err := Default()
	if err != nil {
		t.Fatalf("Default() failed: %v", err)
	}

	input := `config: OPENAI_API_KEY="` + realisticOpenAIKey + `"`
	matches := s.Scan(input)
	if len(matches) == 0 {
		t.Fatalf("precondition: expected the realistic key to be detected")
	}

	out := Redact(input, matches)

	if strings.Contains(out, realisticOpenAIKey) {
		t.Errorf("output still contains the secret value: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:") {
		t.Errorf("expected self-disclosing redaction token in output, got: %s", out)
	}
	if !strings.Contains(out, "rule=") {
		t.Errorf("expected rule= metadata in token, got: %s", out)
	}
	if !strings.Contains(out, "len=") {
		t.Errorf("expected len= metadata in token, got: %s", out)
	}
}

func TestRedact_EmptyMatchesIsNoOp(t *testing.T) {
	in := `hello world`
	if got := Redact(in, nil); got != in {
		t.Errorf("expected unchanged content, got %q", got)
	}
}

func TestScanner_NilSafeAndEmptyInput(t *testing.T) {
	var s *gitleaksScanner
	if got := s.Scan("anything"); got != nil {
		t.Errorf("nil receiver should return nil, got %v", got)
	}

	real, err := Default()
	if err != nil {
		t.Fatalf("Default() failed: %v", err)
	}
	if got := real.Scan(""); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}
