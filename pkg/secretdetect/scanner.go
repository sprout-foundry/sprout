// Package secretdetect provides secret detection backed by the gitleaks
// detection engine. It exposes a small Scanner interface so callers can
// detect (and optionally redact) secrets in arbitrary text without depending
// directly on gitleaks types.
package secretdetect

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
)

// Match describes a single secret detected in input text.
type Match struct {
	RuleID    string // gitleaks rule ID (e.g. "openai-api-key", "generic-api-key")
	Match     string // the broader matched substring (rule-defined; may include the var name)
	Secret    string // the extracted secret value (the part to actually redact)
	StartLine int    // 1-based line number of the match
	StartCol  int
	EndLine   int
	EndCol    int
	Entropy   float32 // Shannon entropy of the secret as computed by gitleaks
}

// Scanner detects secrets in input text. Implementations must be safe for
// concurrent use after construction.
type Scanner interface {
	Scan(content string) []Match
}

type gitleaksScanner struct {
	detector *detect.Detector
}

var (
	defaultOnce    sync.Once
	defaultScanner Scanner
	defaultInitErr error
)

// Default returns a process-wide Scanner backed by gitleaks' default
// embedded ruleset. It is initialised once on first call.
func Default() (Scanner, error) {
	defaultOnce.Do(func() {
		d, err := detect.NewDetectorDefaultConfig()
		if err != nil {
			defaultInitErr = err
			return
		}
		defaultScanner = &gitleaksScanner{detector: d}
	})
	return defaultScanner, defaultInitErr
}

func (s *gitleaksScanner) Scan(content string) []Match {
	if s == nil || s.detector == nil || content == "" {
		return nil
	}
	findings := s.detector.DetectString(content)
	if len(findings) == 0 {
		return nil
	}
	out := make([]Match, 0, len(findings))
	for _, f := range findings {
		out = append(out, fromFinding(f))
	}
	return out
}

func fromFinding(f report.Finding) Match {
	return Match{
		RuleID:    f.RuleID,
		Match:     f.Match,
		Secret:    f.Secret,
		StartLine: f.StartLine,
		StartCol:  f.StartColumn,
		EndLine:   f.EndLine,
		EndCol:    f.EndColumn,
		Entropy:   f.Entropy,
	}
}

// Redact returns content with each match's secret value replaced by a
// self-disclosing token of the form
//
//	[REDACTED:rule=<ruleID>,len=<n>,entropy=<x.x>]
//
// The token carries enough metadata that a downstream LLM reader can
// distinguish "this file actually contained a secret" from "the display layer
// matched a pattern that happened to look like a secret." Replacements are
// applied longest-first so overlapping secrets don't produce partial matches.
//
// If a match's Secret is empty (rule has no SecretGroup), Match is used.
func Redact(content string, matches []Match) string {
	if len(matches) == 0 || content == "" {
		return content
	}

	type replacement struct {
		needle string
		token  string
	}

	reps := make([]replacement, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		needle := m.Secret
		if needle == "" {
			needle = m.Match
		}
		if needle == "" {
			continue
		}
		if _, ok := seen[needle]; ok {
			continue
		}
		seen[needle] = struct{}{}
		reps = append(reps, replacement{
			needle: needle,
			token:  formatToken(m.RuleID, len(needle), m.Entropy),
		})
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

func formatToken(ruleID string, length int, entropy float32) string {
	return fmt.Sprintf("[REDACTED:rule=%s,len=%d,entropy=%.1f]", ruleID, length, entropy)
}
