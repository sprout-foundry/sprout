package secretdetect

import (
	"sort"
	"strings"
)

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

	needles := make([]string, 0, len(matches))
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
		needles = append(needles, n)
	}

	sort.SliceStable(needles, func(i, j int) bool {
		return len(needles[i]) > len(needles[j])
	})

	out := content
	for _, n := range needles {
		out = strings.ReplaceAll(out, n, "[REDACTED]")
	}
	return out
}
