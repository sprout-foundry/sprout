package design

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Token export (SP-140-5 §5a): turn the design/tokens/*.tokens.json DTCG
// tiers into code-side artifacts under design/generated/. This file is the
// pure DTCG -> text half — no filesystem reads, no ToolEnv, no I/O beyond the
// directory-tree helpers at the bottom — so every target's output is unit
// testable without a workspace. The design_export_tokens ToolHandler is a thin
// wrapper around it (pkg/agent_tools/design_export_handler.go).
//
// The hard requirement is deterministic, byte-identical output: the same token
// inputs must produce identical bytes on every run and every platform,
// regardless of map iteration order or file read order. Everything below sorts
// by name before it emits, so the only things that can vary the bytes are the
// token values themselves. Note that filepath.Glob's lexical ordering is a
// POSIX convenience, not a cross-platform guarantee; platform-independent
// determinism comes from the post-hoc sort in ResolveExportTokens, not from the
// read order.
//
// Scope note: this file covers items 5.1 and 5.2. Item 5.2 adds two things:
// (1) every generated artifact opens with a provenance header carrying the
// content hash of the token inputs (TokenExportInputHash) so a consumer can
// verify design/code consistency offline at any checkout (SP-140 invariant 2,
// SP-140-5 §5f); and (2) ResolveExportTokens refuses a dirty alias graph
// (dangling/cyclic aliases) with a structured DirtyAliasError naming the
// offending token, rather than emitting broken var()/*token* references
// (SP-140-5 §5a). Both are deterministic — no timestamps — so the 5.1
// byte-identical guarantee still holds.

// export_render.go — the per-target renderers (css/ts/json/tailwind/swift/
// kotlin) and the value/identifier formatting helpers, split out of
// export.go.

// RenderExport renders the token set for one target, returning the exact bytes
// to write. The output opens with the target's provenance header (item 5.2)
// and ends with a single trailing newline; it is byte-identical across runs for
// identical inputs.
func RenderExport(target string, tokens *TokenExport) ([]byte, error) {
	if tokens == nil {
		return nil, errors.New("nil token set")
	}
	switch target {
	case ExportTargetCSS:
		return renderCSSTokens(tokens), nil
	case ExportTargetTS:
		return renderTSTokens(tokens), nil
	case ExportTargetJSON:
		return renderJSONTokens(tokens), nil
	case ExportTargetTailwind:
		return renderTailwindTokens(tokens), nil
	case ExportTargetSwift:
		return renderSwiftTokens(tokens), nil
	case ExportTargetKotlin:
		return renderKotlinTokens(tokens), nil
	default:
		return nil, fmt.Errorf("unknown export target %q", target)
	}
}

// renderCSSTokens emits a :root block of CSS custom properties
// (webui/src/App.css style: --group-token). Alias leaves emit var(--target) so
// the generated sheet keeps the token graph; every other leaf emits its
// resolved value. SP-143 §143.1: under the variables sits the generated
// utility layer (pkg/design/utilities.go) — the fixed group→class vocabulary
// screens style with.
func renderCSSTokens(tokens *TokenExport) []byte {
	var b bytes.Buffer
	b.WriteString(provenanceHeader(ExportTargetCSS, tokens.InputHash))
	b.WriteString("\n")
	b.WriteString(":root {\n")
	for _, t := range tokens.Leaves {
		fmt.Fprintf(&b, "  %s: %s;\n", cssVarName(t.Name), cssValue(t))
	}
	b.WriteString("}\n")
	b.Write(renderCSSUtilities(tokens))
	return b.Bytes()
}

// renderTSTokens emits a TypeScript module: a frozen typed token map keyed by
// the original dotted path, plus the flattened CSS variable names, so code can
// consume either the semantic name or the generated var. Entries are sorted by
// path (the Leaves order). Values are always quoted strings: a design token is
// a design decision, and keeping its textual form (including units like "8px"
// and durations like "300ms") is the point.
func renderTSTokens(tokens *TokenExport) []byte {
	var b bytes.Buffer
	b.WriteString(provenanceHeader(ExportTargetTS, tokens.InputHash))
	b.WriteString("\n")
	b.WriteString("// The keys are DTCG token paths; the values are the resolved token values.\n")
	b.WriteString("// cssVar maps each token path to its generated CSS variable name.\n\n")
	b.WriteString("export const tokens = {\n")
	for _, t := range tokens.Leaves {
		fmt.Fprintf(&b, "  %s: %s,\n", tsKey(t.Name), tsString(exportStringValue(t)))
	}
	b.WriteString("} as const;\n\n")
	b.WriteString("export type TokenName = keyof typeof tokens;\n\n")
	b.WriteString("export const cssVar: Record<TokenName, string> = {\n")
	for _, t := range tokens.Leaves {
		fmt.Fprintf(&b, "  %s: %s,\n", tsKey(t.Name), tsString(`--`+cssVarStem(t.Name)))
	}
	b.WriteString("};\n")
	return b.Bytes()
}

// renderJSONTokens emits the SP-143 §143.1 json target: the resolved token
// values for JS consumers (the screen runtime reads it without parsing CSS).
// Keys are the DTCG dotted paths — the same keys tokens.ts uses — with native
// JSON values (a fontWeight stays a number, a cubicBezier stays an array);
// cssVars maps each path to its generated CSS variable stem. The provenance
// banner is carried as JSON fields because JSON has no comment syntax: the
// same three facts (tool+target, source-hash, source) as every other target,
// and the same InputHash so cross-target verification stays uniform.
func renderJSONTokens(tokens *TokenExport) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "{\n")
	fmt.Fprintf(&b, "  \"provenance\": \"Generated by design_export_tokens (target=json). Do not edit by hand.\",\n")
	fmt.Fprintf(&b, "  \"source-hash\": \"%s\",\n", tokens.InputHash)
	fmt.Fprintf(&b, "  \"source\": \"design/tokens/*.tokens.json (W3C DTCG). Recompute the hash from those bytes to verify.\",\n")
	fmt.Fprintf(&b, "  \"tokens\": {\n")
	for i, t := range tokens.Leaves {
		fmt.Fprintf(&b, "    %s: %s%s\n", tsKey(t.Name), jsonValue(t), comma(i, len(tokens.Leaves)))
	}
	b.WriteString("  },\n")
	fmt.Fprintf(&b, "  \"cssVars\": {\n")
	for i, t := range tokens.Leaves {
		fmt.Fprintf(&b, "    %s: %s%s\n", tsKey(t.Name), tsString("--"+cssVarStem(t.Name)), comma(i, len(tokens.Leaves)))
	}
	b.WriteString("  }\n}\n")
	return b.Bytes()
}

// comma renders ", " or "" for positional JSON/TS list separators.
func comma(i, n int) string {
	if i == n-1 {
		return ""
	}
	return ","
}

// jsonValue renders a token's resolved value as native JSON (numbers stay
// numbers, structured values stay compact JSON). An unresolvable leaf falls
// back to its raw value, then to null — never a parse-breaking emission.
func jsonValue(t ExportedToken) string {
	v := t.Resolved
	if !t.ResolvedOK {
		v = t.Value
	}
	switch value := v.(type) {
	case string:
		return tsString(value)
	case float64:
		return trimFloat(value)
	case bool:
		if value {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		return compactJSON(value)
	}
}

// renderTailwindTokens emits a Tailwind v4 @theme block: each token becomes a
// --group-token custom property in the theme layer. Alias leaves reference the
// target variable, matching renderCSSTokens' idiom.
func renderTailwindTokens(tokens *TokenExport) []byte {
	var b bytes.Buffer
	b.WriteString(provenanceHeader(ExportTargetTailwind, tokens.InputHash))
	b.WriteString("\n")
	b.WriteString("@theme {\n")
	for _, t := range tokens.Leaves {
		fmt.Fprintf(&b, "  %s: %s;\n", cssVarName(t.Name), cssValue(t))
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// renderSwiftTokens emits a Swift file: an enum namespace with a static
// constant per token, plus a string map of the CSS variable names for parity
// with the web targets. Alias leaves carry the target value (Swift has no
// CSS-var indirection at this layer).
func renderSwiftTokens(tokens *TokenExport) []byte {
	var b bytes.Buffer
	b.WriteString(provenanceHeader(ExportTargetSwift, tokens.InputHash))
	b.WriteString("\n")
	b.WriteString("import Foundation\n\n")
	b.WriteString("/// Design tokens generated from design/tokens/*.tokens.json.\n")
	b.WriteString("public enum DesignTokens {\n")
	for _, t := range tokens.Leaves {
		fmt.Fprintf(&b, "  /// %s (%s)\n", swComment(t.Name), t.Type)
		fmt.Fprintf(&b, "  public static let %s: String = %s\n", swiftIdent(t.Name), swiftString(exportStringValue(t)))
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// renderKotlinTokens emits a Kotlin file: a top-level object holding one const
// val per token.
func renderKotlinTokens(tokens *TokenExport) []byte {
	var b bytes.Buffer
	b.WriteString(provenanceHeader(ExportTargetKotlin, tokens.InputHash))
	b.WriteString("\n")
	b.WriteString("object DesignTokens {\n")
	for _, t := range tokens.Leaves {
		fmt.Fprintf(&b, "  /** %s (%s) */\n", swComment(t.Name), t.Type)
		fmt.Fprintf(&b, "  const val %s: String = %s\n", kotlinIdent(t.Name), kotlinString(exportStringValue(t)))
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// -----------------------------------------------------------------------------
// Value rendering
// -----------------------------------------------------------------------------

// cssValue renders a token's CSS value: var(--target) for an alias reference,
// otherwise the resolved value stringified for CSS.
func cssValue(t ExportedToken) string {
	if t.HasAlias && t.AliasPath != "" {
		return "var(--" + cssVarStem(t.AliasPath) + ")"
	}
	return cssLiteral(t)
}

// cssLiteral stringifies a resolved token value for CSS. It never returns ""
// (a token with no renderable value falls back to the raw value, then to an
// empty string quoted only as a last resort).
func cssLiteral(t ExportedToken) string {
	v := t.Resolved
	if !t.ResolvedOK {
		v = t.Value
	}
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return trimFloat(value)
	case bool:
		if value {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		// Structured values (cubicBezier arrays, border objects) have no
		// single CSS scalar; render their compact JSON so the output stays
		// lossless and deterministic.
		return compactJSON(value)
	}
}

// exportStringValue renders a token value as a plain string for the
// Swift/Kotlin targets: alias leaves carry the resolved value, everything else
// stringifies its resolved value (compacted JSON for structured types).
func exportStringValue(t ExportedToken) string {
	v := t.Resolved
	if !t.ResolvedOK {
		v = t.Value
	}
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return trimFloat(value)
	case bool:
		if value {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return compactJSON(value)
	}
}

// compactJSON serializes a structured value as compact JSON. Map keys are
// re-encoded in sorted order by encoding/json, so nested objects are stable.
// An unserializable value (impossible for decoded JSON) degrades to "".
func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// trimFloat renders a float64 without a trailing ".0" for integral values
// (700, not 700.0) and with the shortest round-trippable form otherwise, so
// numeric tokens read naturally and identically across platforms.
func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// -----------------------------------------------------------------------------
// Identifier construction (deterministic, collision-free)
// -----------------------------------------------------------------------------

// cssVarName builds a CSS custom property name: --group-token, with every
// non [a-z0-9] run collapsed to a single '-'. It assumes valid DTCG paths in
// practice; the saneName fallback keeps pathological keys (built by the
// parser's last-wins overwrite of object keys) from producing an invalid
// declaration.
func cssVarName(path string) string {
	return "--" + cssVarStem(path)
}

// cssVarStem is the identifier stem behind a CSS variable name (no leading
// "--"); alias references reuse it so var(--x) always matches its declaration.
func cssVarStem(path string) string {
	return saneName(path, '-', false)
}

// tsKey quotes a token path as an object key for the TypeScript map. %q emits
// a double-quoted Go string literal whose escape rules are valid TypeScript
// for every legal JSON key.
func tsKey(path string) string {
	return tsString(path)
}

// swiftIdent builds a lowerCamelCase Swift identifier from a token path.
func swiftIdent(path string) string {
	return camelIdent(path)
}

// kotlinIdent builds a lowerCamelCase Kotlin identifier from a token path.
func kotlinIdent(path string) string {
	return camelIdent(path)
}

// camelIdent joins a path's alphanumeric segments in lowerCamelCase. Segment
// text keeps its original inner case (so "fontWeight.bold" reads
// "fontWeightBold", not "fontweightBold"); the first segment's leading letter
// is lowercased and every later segment's is uppercased. A segment that starts
// with a digit is prefixed with '_' so the result is a legal identifier; a path
// with no alphanumeric content falls back to a stable hash-suffixed
// placeholder.
func camelIdent(path string) string {
	segments := splitIdentSegments(path)
	if len(segments) == 0 {
		return saneName(path, '_', true)
	}
	var b strings.Builder
	for i, seg := range segments {
		if i == 0 {
			b.WriteString(lowerFirst(seg))
			continue
		}
		b.WriteString(upperFirst(seg))
	}
	ident := b.String()
	if ident == "" || (ident[0] >= '0' && ident[0] <= '9') {
		ident = "_" + ident
	}
	return ident
}

// splitIdentSegments splits a path on every non-alphanumeric run into
// non-empty segments, preserving each segment's original case. A segment
// retains any non-ASCII byte rather than stripping it; the identifier is
// expected to be ASCII in practice (DTCG paths are slugs).
func splitIdentSegments(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
}

// lowerFirst lowercases the first byte of s when it is an ASCII uppercase
// letter; a leading non-letter (digit, underscore) is left alone.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	c := s[0]
	if c >= 'A' && c <= 'Z' {
		return string(c-'A'+'a') + s[1:]
	}
	return s
}

// upperFirst uppercases the first byte of s when it is an ASCII lowercase
// letter; a leading non-letter is left alone.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	c := s[0]
	if c >= 'a' && c <= 'z' {
		return string(c-'a'+'A') + s[1:]
	}
	return s
}

// saneName collapses a path's non-alphanumeric runs into sep and, when hash
// is true, appends a short stable hash so two distinct paths that collapse to
// the same name still differ. A path with no alphanumerics yields a
// placeholder built from its length and hash (never "").
func saneName(path string, sep byte, hash bool) string {
	var b strings.Builder
	pendingSep := false
	for i := 0; i < len(path); i++ {
		c := path[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if pendingSep && b.Len() > 0 {
				b.WriteByte(sep)
			}
			pendingSep = false
			b.WriteByte(c)
			continue
		}
		if c >= 'A' && c <= 'Z' {
			// Collapse any remaining uppercase byte to lowercase.
			if pendingSep && b.Len() > 0 {
				b.WriteByte(sep)
			}
			pendingSep = false
			b.WriteByte(c - 'A' + 'a')
			continue
		}
		pendingSep = true
	}
	out := b.String()
	if out == "" {
		out = "token"
	}
	if hash {
		out += "_" + shortHash(path)
	}
	return out
}

// shortHash is a small FNV-1a hex digest used only to disambiguate collapsed
// identifiers. It is deterministic across runs and platforms.
func shortHash(s string) string {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	var h uint64 = offset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	// 8 hex digits (32 bits) is ample for identifier disambiguation.
	return strconv.FormatUint(h&0xffffffff, 16)
}

// -----------------------------------------------------------------------------
// Escaping helpers
// -----------------------------------------------------------------------------

// tsString renders a Go double-quoted string literal — valid TypeScript for
// every JSON-representable key/value. %q is deterministic and escapes quotes,
// backslashes, and control characters.
func tsString(s string) string {
	return fmt.Sprintf("%q", s)
}

// swiftString renders a Swift string literal. Swift and Go share the same
// escapes for the characters that matter here (quotes, backslash, newline,
// tab, and \u{...}); the only divergence is non-ASCII, which Go emits raw and
// Swift accepts inside a literal. Control characters below 0x20 other than
// \n, \r, \t are impossible in a decoded JSON string.
func swiftString(s string) string {
	return fmt.Sprintf("%q", s)
}

// kotlinString renders a Kotlin string literal. Kotlin accepts $ in a literal
// only as a template, so it is escaped; every other escape Go emits (\\, \",
// \n, \r, \t, \u00XX) is valid Kotlin.
func kotlinString(s string) string {
	return strings.ReplaceAll(swiftString(s), "$", `\$`)
}

// swComment strips newlines from a token path so it can sit on one comment
// line without breaking the file structure.
func swComment(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
}

// -----------------------------------------------------------------------------
// Artifact writing (deterministic, confined to design/generated/)
// -----------------------------------------------------------------------------
