package design

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// hexColorRe captures a 3/4/6/8-digit hex colour with a leading '#'.
var hexColorRe = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

// cssSpacingRe captures a bare pixel length in a CSS-ish property context —
// `padding: 13px`, `gap: 7px`, `margin-top: 22px` — the "magic spacing" case
// §5b names. Submatch 1 is the number.
var cssSpacingRe = regexp.MustCompile(`(?i)(?:padding|margin|gap|top|right|bottom|left|inset|space)[a-z-]*\s*:\s*(-?\d+(?:\.\d+)?)px`)

// spacingTokenGroups are the token groups a proposed spacing token would land
// in, in preference order.
var spacingTokenGroups = []string{"dimension.space", "space", "spacing", "dimension"}

// analyzeInferred detects inferred deltas in one touched file: raw hex colours
// and magic spacing values that have no token counterpart. Each is a proposal
// — a new token (with a suggested path), or a switch to an existing token when
// one already carries the same value.
func analyzeInferred(f SyncFileInput, tree syncTree) []SyncDelta {
	if !isCSSVarHost(f.Path) {
		return nil
	}
	content := string(f.Content)
	var deltas []SyncDelta

	// Raw hex colours. A hex directly inside a var() fallback or backed by a
	// token-comment reference is still counterpart-less unless a DTCG token
	// carries the same value; the candidate lookup handles the latter. A hex
	// that *is* the declared value of a var mapping to a DTCG token is already
	// reported by the literal pass, so it is skipped here (no double signal).
	literalSpans := map[int]bool{}
	for _, m := range cssVarDeclRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		if _, ok := tree.byCSSVar[name]; ok || looksLikeToken(tree, name) != "" {
			for i := m[4]; i < m[5]; i++ {
				literalSpans[i] = true
			}
		}
	}
	for _, m := range hexColorRe.FindAllStringSubmatchIndex(content, -1) {
		hex := strings.ToLower(content[m[0]:m[1]])
		if strings.HasPrefix(hex, "--") || literalSpans[m[0]] {
			continue
		}
		line := strings.Count(content[:m[0]], "\n") + 1
		deltas = append(deltas, inferredDelta(tree, f.Path, hex, "color", line,
			fmt.Sprintf("raw hex %s in code has no token counterpart", hex)))
	}

	// Magic spacing. A length that is the declared value of a var mapping to a
	// DTCG token is the literal pass's business.
	seen := map[string]bool{}
	for _, m := range cssSpacingRe.FindAllStringSubmatchIndex(content, -1) {
		if literalSpans[m[2]] {
			continue
		}
		num := content[m[2]:m[3]]
		if num == "0" || num == "0.0" {
			continue
		}
		value := num + "px"
		if seen[value] {
			continue
		}
		seen[value] = true
		line := strings.Count(content[:m[0]], "\n") + 1
		deltas = append(deltas, inferredDelta(tree, f.Path, value, "space", line,
			fmt.Sprintf("magic spacing %s in code has no token counterpart", value)))
	}
	return deltas
}

// inferredDelta builds one inferred (proposal) delta: it looks for an existing
// token carrying the same literal value (a "switch to existing" candidate),
// otherwise proposes a new token path in the group family for the value type.
func inferredDelta(tree syncTree, codeFile, value, family string, line int, summary string) SyncDelta {
	d := SyncDelta{
		Kind:        DeltaKindToken,
		Basis:       DeltaBasisInferred,
		Confidence:  ConfidenceLow,
		DesignFiles: []string{},
		SafeToApply: false,
		Proposal:    true,
		CodeFiles:   []string{codeFile},
		Evidence:    fmt.Sprintf("%s (line %d)", value, line),
	}
	if candidates := tree.byValue[strings.ToLower(value)]; len(candidates) > 0 {
		d.Delta = fmt.Sprintf("%s; switch to the existing token %s instead", summary, candidates[0])
		d.ProposalKind = "switch-to-existing"
		d.Candidate = candidates[0]
		d.Proposed = candidates[0]
		if tree.tokens != nil {
			if f := tokenSourceFile(tree, candidates[0]); f != "" {
				d.DesignFiles = []string{f}
			}
		}
		return d
	}

	proposed := proposeNewTokenPath(tree, family, value)
	d.Delta = fmt.Sprintf("%s; propose a new token %s", summary, proposed)
	d.ProposalKind = "new-token"
	d.Proposed = proposed
	d.DesignFiles = []string{proposedTokenFile(tree, proposed)}
	return d
}

// proposedTokenFile names the design file a *proposed* new token would go in:
// the conventional group tier (`design/tokens/<group>.tokens.json`) when that
// file exists, else the same conventional name so apply knows where to create
// it. It deliberately does not fall back to an unrelated existing tier the way
// tokenSourceFile does for an already-existing token.
func proposedTokenFile(tree syncTree, tokenPath string) string {
	group := topGroup(tokenPath)
	if group == "" {
		return tree.tokensPath
	}
	conventional := path.Join(DirName, TokenSubdir, group+".tokens.json")
	return conventional
}

// proposeNewTokenPath suggests a DTCG path for an inferred value: a color is
// proposed under `color.<slug>`; a dimension under the first free
// spacing-token group. The slug is derived from the value so the same literal
// in two files proposes the same name (determinism).
func proposeNewTokenPath(tree syncTree, family, value string) string {
	slug := valueSlug(value)
	switch family {
	case "color":
		return "color." + slug
	default:
		for _, group := range spacingTokenGroups {
			candidate := group + "." + slug
			if !tokenExists(tree, candidate) {
				return candidate
			}
		}
		return spacingTokenGroups[0] + "." + slug
	}
}

// valueSlug turns a literal value into a slug-safe name fragment:
// "#0055ff" → "0055ff", "13px" → "13px" → "13". Digits and letters only.
func valueSlug(value string) string {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "#")
	trimmed = strings.TrimSuffix(trimmed, "px")
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "value"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "v" + out
	}
	return out
}

// -----------------------------------------------------------------------------
// Deterministic ordering / dedup
// -----------------------------------------------------------------------------
