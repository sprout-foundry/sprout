package secretdetect

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// RedactTagged scans content with the default scanner and replaces every
// detected secret with [REDACTED:<rule-id>]. Use this for log files and
// debugging contexts where it's useful to know the *kind* of secret that was
// present without exposing its value, length, or entropy.
//
// If the default scanner cannot be initialised, content is returned
// unchanged.
func RedactTagged(content string) string {
	return redactReplace(content, func(m Match) string {
		return fmt.Sprintf("[REDACTED:%s]", m.RuleID)
	})
}

// RedactOpaque scans content with the default scanner and replaces every
// detected secret with the literal token [REDACTED]. Use this for log files,
// training exports, CLI display, and other paths where the consumer is a
// human (or non-LLM tool) and self-disclosing metadata would either be noise
// or a leak of original-value shape information.
//
// For tool output that an LLM agent will read, use Redact instead so the
// agent can distinguish display-layer redactions from on-disk content.
//
// If the default scanner cannot be initialised, content is returned
// unchanged.
func RedactOpaque(content string) string {
	return redactReplace(content, func(Match) string { return "[REDACTED]" })
}

// RedactOpaqueJSON redacts a serialized JSON payload and guarantees the result
// is still valid JSON. It re-encodes the document and redacts each string
// VALUE in its decoded form, so a secret whose scan boundary would otherwise
// split a JSON escape (\", \\, \uXXXX) can never orphan the escape and corrupt
// the document — the failure mode RedactOpaque's byte-level replacement risks
// when a gitleaks capture group swallows an escape adjacent to the secret.
//
// When no secret is found the input is returned byte-for-byte unchanged (the
// re-encoding is discarded), so a clean payload is never reformatted. When
// content is not valid JSON (or is empty) it falls back to RedactOpaque,
// preserving today's behaviour for non-JSON callers.
//
// Intended for outbound LLM request bodies (the egress redaction backstop);
// RedactOpaque remains the choice for free text.
func RedactOpaqueJSON(content string) string {
	if content == "" {
		return content
	}
	if !json.Valid([]byte(content)) {
		return RedactOpaque(content)
	}
	var doc any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return RedactOpaque(content)
	}
	redacted, changed := redactJSONValue(doc)
	if !changed {
		return content
	}
	out, err := json.Marshal(redacted)
	if err != nil {
		return RedactOpaque(content)
	}
	return string(out)
}

// redactJSONValue walks a decoded JSON value, replacing any secret found in a
// string with the opaque token. It reports whether any value changed, so the
// caller can skip re-encoding an untouched document. Keys are left untouched
// (redacting a key would change the schema the provider sees); only leaf
// string values are redacted.
func redactJSONValue(v any) (any, bool) {
	switch t := v.(type) {
	case string:
		r := RedactOpaque(t)
		return r, r != t
	case []any:
		changed := false
		for i, e := range t {
			nv, c := redactJSONValue(e)
			if c {
				t[i] = nv
				changed = true
			}
		}
		return t, changed
	case map[string]any:
		changed := false
		for k, e := range t {
			nv, c := redactJSONValue(e)
			if c {
				t[k] = nv
				changed = true
			}
		}
		return t, changed
	default:
		return v, false
	}
}

// redactReplace is the shared scan + longest-first replacement loop used by
// RedactOpaque and RedactTagged. The replacement string for each match is
// produced by tokenFor.
func redactReplace(content string, tokenFor func(Match) string) string {
	if content == "" {
		return content
	}
	s, err := Default()
	if err != nil || s == nil {
		return content
	}
	matches := s.Scan(content)
	if len(matches) == 0 {
		return content
	}

	type rep struct {
		needle string
		token  string
	}
	reps := make([]rep, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		n := m.Secret
		if n == "" {
			n = m.Match
		}
		if n == "" {
			continue
		}
		// Boundary-safe needle: gitleaks capture groups can swallow the
		// backslash of an escaped JSON quote when a secret sits inside a
		// serialized JSON string. Replacing a
		// needle that starts/ends with a backslash deletes the escape and
		// orphans the quote, corrupting the payload. No valid secret of the
		// rules in use has a boundary backslash — when gitleaks captures one
		// it is a JSON escape, not credential material. Re-evaluate if adding
		// a custom rule whose valid secrets can start/end with a backslash.
		n = strings.Trim(n, "\\")
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		reps = append(reps, rep{needle: n, token: tokenFor(m)})
	}

	sort.SliceStable(reps, func(i, j int) bool {
		return len(reps[i].needle) > len(reps[j].needle)
	})

	out := content
	for _, r := range reps {
		out = strings.ReplaceAll(out, r.needle, r.token)
	}
	return out
}
