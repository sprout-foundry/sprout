package secretdetect

import (
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
