package design

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// cssVarHostExts are the code file extensions scanned for CSS custom
// properties and literal styling. Extension-based classification is the v1
// convention (SP-140-5 §5b): no parsing, no AST.
var cssVarHostExts = map[string]bool{
	".css": true, ".scss": true, ".sass": true, ".less": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".html": true, ".htm": true, ".svg": true, ".vue": true, ".svelte": true,
}

// isCSSVarHost reports whether a path is scanned for CSS variables/literals.
func isCSSVarHost(p string) bool {
	return cssVarHostExts[strings.ToLower(path.Ext(p))]
}

// routerFileRe matches the v1 router-file convention: any path segment
// containing "rout" (routes.ts, router.tsx, app-router/, ...).
var routerFileRe = regexp.MustCompile(`(?i)rout`)

// isRouterFile reports whether a path is a router file by the v1 convention.
func isRouterFile(p string) bool {
	base := path.Base(p)
	// A directory segment counts too (routes/login.tsx, router/index.ts).
	return routerFileRe.MatchString(p) && !strings.HasSuffix(base, ".css")
}

// -----------------------------------------------------------------------------
// Passthrough: literal deltas — CSS custom properties / token references
// -----------------------------------------------------------------------------

// cssVarDeclRe captures a CSS custom property declaration: `--name: value`.
// Submatch 1 = the name without the leading `--`, 2 = the value up to the
// statement terminator. It is deliberately lenient (no full CSS grammar in
// v1) and matches inside TS/TSX template strings and style objects too, which
// is exactly the point: the vocabulary is the convention.
var cssVarDeclRe = regexp.MustCompile(`--([A-Za-z0-9_-]+)\s*:\s*([^;}\n]+)`)

// cssVarRefRe captures a CSS custom property reference: `var(--name)`.
var cssVarRefRe = regexp.MustCompile(`var\(\s*--([A-Za-z0-9_-]+)\s*\)`)

// tokenRefRe captures a DTCG `{group.token}` reference in code (a comment or
// string), the same dot-path form the wireframe token-comment convention
// uses.
var tokenRefRe = regexp.MustCompile(`\{\s*([A-Za-z][A-Za-z0-9]*(?:\.[A-Za-z0-9_-]+)+)\s*\}`)

// analyzeCSSLiterals detects literal deltas: CSS custom property
// declarations/references that map to a DTCG token entry, and DTCG token
// references the code consumes. A declaration whose value differs from the
// token's current value is a *revalue*; a reference with no token counterpart
// is reported so the semantic layer can adopt it.
func analyzeCSSLiterals(f SyncFileInput, tree syncTree) []SyncDelta {
	if !isCSSVarHost(f.Path) {
		return nil
	}
	content := string(f.Content)
	var deltas []SyncDelta

	// Declarations: `--name: value`.
	for _, m := range cssVarDeclRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		value := strings.TrimSpace(content[m[4]:m[5]])
		line := strings.Count(content[:m[0]], "\n") + 1

		tokenPath, ok := tree.byCSSVar[name]
		if !ok {
			// The declaration may spell the same token differently: the export's
			// identifier rule collapses every non-alphanumeric run in the DTCG
			// path to '-', so `--color_brand_primary` and `--color-brand-primary`
			// are the same token. Resolve through the normal form before giving
			// up — otherwise a renamed var falls through to the inferred pass,
			// which reports "no token counterpart" for a token that exists.
			if normal := looksLikeToken(tree, name); normal != "" {
				tokenPath, ok = normal, true
			}
		}
		if !ok {
			continue // declared var with no DTCG counterpart: inferred territory
		}
		designFiles, tokFile, tokEntry := tokenDesignFiles(tree, tokenPath)
		current := currentTokenValue(tree, tokenPath)
		d := SyncDelta{
			Kind:        DeltaKindToken,
			Basis:       DeltaBasisLiteral,
			Confidence:  ConfidenceHigh,
			DesignFiles: designFiles,
			SafeToApply: true,
			Token:       tokenPath,
			TokenFile:   tokFile,
			TokenEntry:  tokEntry,
			CodeFiles:   []string{f.Path},
			Evidence:    fmt.Sprintf("--%s: %s (line %d)", name, value, line),
		}
		if current != "" && !strings.EqualFold(current, value) {
			d.Delta = fmt.Sprintf("token %s revalued in code: --%s is %q but the DTCG entry is %q",
				tokenPath, name, value, current)
		} else {
			d.Delta = fmt.Sprintf("token %s declared in code as --%s: %s (matches the DTCG entry)",
				tokenPath, name, value)
		}
		deltas = append(deltas, d)
	}

	// References: `var(--name)` to a var with no declaration in this file and
	// no DTCG counterpart — the code consumes a variable the semantic layer
	// does not know about. (Declarations above cover the known ones.)
	for _, m := range cssVarRefRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		if _, known := tree.byCSSVar[name]; known {
			continue
		}
		// Same separator-spelling tolerance as the declaration pass: a var that
		// resolves to an existing token is not "unknown".
		if looksLikeToken(tree, name) != "" {
			continue
		}
		if strings.Contains(content, "--"+name+":") {
			continue // declared in this very file; the declaration path owns it
		}
		line := strings.Count(content[:m[0]], "\n") + 1
		deltas = append(deltas, SyncDelta{
			Delta: fmt.Sprintf("code references --%s, which has no DTCG token counterpart; "+
				"add the token to design/tokens/ so export and sync agree", name),
			Kind:         DeltaKindToken,
			Basis:        DeltaBasisInferred,
			Confidence:   ConfidenceLow,
			DesignFiles:  []string{},
			SafeToApply:  false,
			Proposal:     true,
			ProposalKind: "new-token",
			Proposed:     tokenPathFromCSSVar(name),
			CodeFiles:    []string{f.Path},
			Evidence:     fmt.Sprintf("var(--%s) (line %d)", name, line),
		})
	}

	// DTCG references in code: `{color.brand.primary}`.
	for _, m := range tokenRefRe.FindAllStringSubmatchIndex(content, -1) {
		tokenPath := content[m[2]:m[3]]
		if tree.tokens == nil {
			continue // no tokens to cross-reference
		}
		if !tokenExists(tree, tokenPath) {
			line := strings.Count(content[:m[0]], "\n") + 1
			deltas = append(deltas, SyncDelta{
				Delta: fmt.Sprintf("code references token {%s}, which is not in design/tokens/; "+
					"add it (or fix the reference)", tokenPath),
				Kind:        DeltaKindToken,
				Basis:       DeltaBasisStructural,
				Confidence:  ConfidenceMedium,
				DesignFiles: []string{tree.tokensPath},
				SafeToApply: true,
				Token:       tokenPath,
				TokenEntry:  tokenPath,
				CodeFiles:   []string{f.Path},
				Evidence:    fmt.Sprintf("{%s} (line %d)", tokenPath, line),
			})
		}
	}
	return deltas
}

// tokenPathFromCSSVar converts a CSS variable name back to a DTCG-ish dotted
// token path: `color-brand-primary` → `color.brand.primary`. Best-effort (the
// inverse of cssVarStem is lossy), used only to *propose* a new token name.
func tokenPathFromCSSVar(name string) string {
	segs := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	if len(segs) == 0 {
		return name
	}
	return strings.Join(segs, ".")
}

// looksLikeToken resolves a CSS var name that was spelled differently from the
// export's canonical form back to an existing DTCG entry, or "" when the name
// matches no token.
//
// The export identifier rule (cssVarStem) collapses every non-alphanumeric run
// in a token path to '-', so the canonical spelling of `color.brand.primary` is
// `color-brand-primary`. A declaration written `--color_brand_primary` or
// `--color.brand.primary` denotes the same token; without this the exact map
// lookup misses and the delta is misreported as having no counterpart.
//
// Only spellings that differ *solely* in separator characters are accepted, so
// a genuinely new name (e.g. `color-brand-primary-alt`) stays a proposal.
func looksLikeToken(tree syncTree, name string) string {
	normalised := tokenPathFromCSSVar(name)
	if tree.tokens != nil {
		for _, candidate := range tree.tokens.Leaves {
			if tokenPathFromCSSVar(cssVarStem(candidate.Name)) == normalised {
				return candidate.Name
			}
		}
	}
	if tree.tokensPath != "" && tokenExists(tree, normalised) {
		return normalised
	}
	return ""
}

// tokenDesignFiles returns the design files a token delta would touch: the
// token source file, plus the generated artifacts design_export_tokens owns
// (§5f: a token change means the theme must be regenerated). tokFile/tokEntry
// name the DTCG file and entry for the report.
func tokenDesignFiles(tree syncTree, tokenPath string) (designFiles []string, tokFile, tokEntry string) {
	files := []string{}
	if tree.tokens != nil {
		if f := tokenSourceFile(tree, tokenPath); f != "" {
			tokFile = f
			files = append(files, f)
			tokEntry = tokenPath
		}
	}
	if len(files) == 0 {
		// No tokens in the tree: point at the token tier so apply knows where
		// a new entry would go.
		files = append(files, tree.tokensPath)
		tokEntry = tokenPath
	}
	// The generated artifacts follow from the token §5a machinery.
	for _, name := range []string{"tokens.css", "tokens.ts", "tokens.json", "tailwind.theme.css", "tokens.swift", "tokens.kt"} {
		files = append(files, path.Join(DirName, GeneratedSubdir, name))
	}
	return files, tokFile, tokEntry
}

// tokenSourceFile names the token source file a token path lives in. The DTCG
// file per tier is conventional (color.tokens.json for the color group), so
// the group name maps to `<group>.tokens.json`; when a file with that name
// exists it is preferred, and when no group-scoped file exists the first token
// file in canonical order is used so the delta still names a real source.
func tokenSourceFile(tree syncTree, tokenPath string) string {
	group := topGroup(tokenPath)
	if group == "" {
		return ""
	}
	conventional := path.Join(DirName, TokenSubdir, group+".tokens.json")
	for _, f := range tree.tokenFiles {
		if f == conventional {
			return f
		}
	}
	// The group has no tier file of its own (a project tier may hold it):
	// fall back to the first token file in canonical order, so the report
	// names a real source rather than an invented one.
	if len(tree.tokenFiles) > 0 {
		return tree.tokenFiles[0]
	}
	return conventional
}

// currentTokenValue returns the current rendered value of a DTCG token, "" when
// unknown.
func currentTokenValue(tree syncTree, tokenPath string) string {
	if tree.tokens == nil {
		return ""
	}
	for _, leaf := range tree.tokens.Leaves {
		if leaf.Name == tokenPath {
			return exportStringValue(leaf)
		}
	}
	return ""
}

// tokenExists reports whether a dotted token path resolves to a leaf.
func tokenExists(tree syncTree, tokenPath string) bool {
	if tree.tokens == nil {
		return false
	}
	for _, leaf := range tree.tokens.Leaves {
		if leaf.Name == tokenPath {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Structural deltas — routes/screens, components, nav targets
// -----------------------------------------------------------------------------
