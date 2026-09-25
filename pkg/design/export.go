package design

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// GeneratedSubdir is the design subdirectory export writes to, under DirName
// (SP-140-5 §5a: "targets design/generated/"). It is deliberately not part of
// Subdirs — it is a generated output location, not a scaffolded source tier,
// and SP-140-1 §1h leaves it un-ignored.
const GeneratedSubdir = "generated"

// Export target identifiers. These are the exact strings the design_export_tokens
// `targets` argument accepts.
const (
	ExportTargetCSS      = "css"
	ExportTargetTS       = "ts"
	ExportTargetJSON     = "json"
	ExportTargetTailwind = "tailwind"
	ExportTargetSwift    = "swift"
	ExportTargetKotlin   = "kotlin"
)

// ExportTargetAll is the `targets` value that runs every exporter.
const ExportTargetAll = "all"

// ExportTargets is the canonical target list, in the deterministic order
// exports are generated and reported (all → css, ts, json, tailwind, swift,
// kotlin). Callers must not rely on map order, so this slice is the one
// ordering authority.
var ExportTargets = []string{
	ExportTargetCSS,
	ExportTargetTS,
	ExportTargetJSON,
	ExportTargetTailwind,
	ExportTargetSwift,
	ExportTargetKotlin,
}

// ExportFilenames maps each target to the file it writes under
// design/generated/. The names are fixed by SP-140-5 §5a so generated output
// is predictable to consumers (a webui build can hardcode tokens.css).
var ExportFilenames = map[string]string{
	ExportTargetCSS:      "tokens.css",
	ExportTargetTS:       "tokens.ts",
	ExportTargetJSON:     "tokens.json",
	ExportTargetTailwind: "tailwind.theme.css",
	ExportTargetSwift:    "tokens.swift",
	ExportTargetKotlin:   "tokens.kt",
}

// ExportTarget is one resolved export target: its identifier and its
// design-relative file name.
type ExportTarget struct {
	Name string
	File string
}

// ErrNoTokensForExport is returned when no token leaves exist under the
// workspace's design/tokens/ directory — there is nothing to export, and
// emitting target boilerplate with no tokens would be misleading. It is a
// distinct sentinel so the handler can report a usage error rather than a
// crash. (Exporting requires tokens/, which is also how the handler refuses a
// workspace with no design/ tree at all.)
var ErrNoTokensForExport = errors.New("no design tokens found to export")

// ErrDirtyAliasGraph is returned when the token tree contains at least one
// dangling or cyclic whole-value alias (SP-140-5 §5a: export "refuses dirty
// alias graphs"). It is a distinct sentinel so the handler can report the
// refusal as a structured, actionable failure and name the offending token —
// see DirtyAliasError for the per-token detail. Export never writes a partial
// or broken theme on a dirty graph.
var ErrDirtyAliasGraph = errors.New("dirty alias graph: token aliases do not resolve")

// DirtyAliasError is the structured refusal returned by ResolveExportTokens
// (wrapping ErrDirtyAliasGraph) when the alias graph is dirty. Violations are
// sorted by token path so the message and the structured detail are
// deterministic across runs and file read orders.
type DirtyAliasError struct {
	// Violations are the offending aliases, one per (rule, token) pair.
	Violations []AliasViolation
}

// AliasViolation is one dangling or cyclic alias in the token tree.
type AliasViolation struct {
	// Token is the dotted path of the token whose alias is broken
	// (e.g. "color.brand.text").
	Token string
	// Path is the workspace-relative slash path of the source document the
	// token lives in (design/tokens/color.tokens.json).
	Path string
	// Line is the 1-based source line of the token leaf's value.
	Line int
	// Rule is the validator rule id (token_alias_dangling or
	// token_alias_cycle), so the refusal uses the same vocabulary as
	// design_validate.
	Rule string
	// Kind is a short human label: "dangling" or "cycle".
	Kind string
	// Message is the validator's finding message (already names the token
	// and, for cycles, the member loop).
	Message string
}

// Error renders the refusal so a model reads which token is dirty without
// parsing structured data. It lists every offending token in path order.
func (e *DirtyAliasError) Error() string {
	if e == nil || len(e.Violations) == 0 {
		return ErrDirtyAliasGraph.Error()
	}
	parts := make([]string, 0, len(e.Violations))
	for _, v := range e.Violations {
		parts = append(parts, fmt.Sprintf("%s (%s)", v.Token, v.Kind))
	}
	return fmt.Sprintf("refusing to export: dirty alias graph (%d): %s; fix the token aliases and re-run",
		len(e.Violations), strings.Join(parts, ", "))
}

// Unwrap lets errors.Is(err, ErrDirtyAliasGraph) recognize the structured
// refusal, so the handler can branch on the sentinel without a type assertion.
func (e *DirtyAliasError) Unwrap() error { return ErrDirtyAliasGraph }

// checkAliasGraph parses one source document with the validator's parser and
// reports its dangling/cyclic aliases as a DirtyAliasError. It returns nil when
// the document's alias graph is clean. A document that does not parse is not
// this function's business — collectExportLeaves reports the parse failure with
// its own message — so an unparseable document yields nil here and the caller
// fails on the parse instead. $extensions subtrees are invisible to
// checkAliases by construction (SP-140-1 §1a), so references there never make
// a graph "dirty". It is the single-document convenience wrapper around
// collectAliasViolations used by tests; ResolveExportTokens aggregates across
// documents.
func checkAliasGraph(rel string, content []byte) error {
	violations := collectAliasViolations(rel, content)
	if len(violations) == 0 {
		return nil
	}
	sortAliasViolations(violations)
	return &DirtyAliasError{Violations: violations}
}

// collectAliasViolations returns one AliasViolation per dangling/cyclic alias
// in one source document, in document order (the caller sorts globally). A
// document that does not parse yields none — the parse path owns that error.
func collectAliasViolations(rel string, content []byte) []AliasViolation {
	root, parseFindings := parseTokensDocument(content)
	if root == nil {
		return nil // unparseable: reported by the parse path, not the alias path
	}
	// parseFindings can be non-empty while root is non-nil only for the
	// trailing-data case, which also means the tree is not trustworthy; let
	// the parse path report it.
	if len(parseFindings) > 0 {
		return nil
	}
	findings := checkAliases(root)
	if len(findings) == 0 {
		return nil
	}
	violations := make([]AliasViolation, 0, len(findings))
	for _, f := range findings {
		violations = append(violations, AliasViolation{
			Token:   aliasFindingToken(f),
			Path:    rel,
			Line:    f.Line,
			Rule:    f.Rule,
			Kind:    aliasViolationKind(f.Rule),
			Message: f.Message,
		})
	}
	return violations
}

// sortAliasViolations orders violations deterministically: by token path, then
// source line, then file, then rule. Two runs over the same tree (in any file
// read order) produce the same refusal.
func sortAliasViolations(violations []AliasViolation) {
	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].Token != violations[j].Token {
			return violations[i].Token < violations[j].Token
		}
		if violations[i].Line != violations[j].Line {
			return violations[i].Line < violations[j].Line
		}
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		return violations[i].Rule < violations[j].Rule
	})
}

// aliasViolationKind labels a rule id as a dangling or cyclic violation.
func aliasViolationKind(rule string) string {
	switch rule {
	case ruleTokenAliasCycle:
		return "cycle"
	case ruleTokenAliasDangling:
		return "dangling"
	default:
		return "alias"
	}
}

// aliasFindingToken extracts the offending token's dotted path from a
// checkAliases finding message. The two message shapes are fixed by
// tokens_alias.go:
//
//	dangling: alias "{path}" on token "token.path" does not resolve
//	cycle:    alias cycle: a -> b -> a
//
// A cycle's message lists members rather than anchoring on the leaf, so the
// first member is used as the representative token. A message that matches
// neither shape yields "" and the violation is reported without a token.
func aliasFindingToken(f Finding) string {
	if idx := strings.Index(f.Message, `" on token "`); idx >= 0 {
		rest := f.Message[idx+len(`" on token "`):]
		if end := strings.Index(rest, `"`); end >= 0 {
			return rest[:end]
		}
	}
	if strings.HasPrefix(f.Message, "alias cycle: ") {
		members := strings.Split(strings.TrimPrefix(f.Message, "alias cycle: "), " -> ")
		if len(members) > 0 {
			return members[0]
		}
	}
	return ""
}

// TokenExport is the resolved, target-neutral projection of every DTCG token
// leaf in a tier set. Leaves is sorted by Name; the validators have already
// established that each leaf has a well-formed $type, so the projection can
// assume one.
type TokenExport struct {
	Leaves []ExportedToken
	// InputHash is the §5f provenance hash of the token source documents this
	// projection was built from (see TokenExportInputHash). Every generated
	// artifact's header records it, so the artifact can be verified against
	// design/tokens/ at any checkout, offline.
	InputHash string
}

// ExportedToken is one DTCG token leaf prepared for export.
type ExportedToken struct {
	// Name is the dotted token path (e.g. "color.brand.primary").
	Name string
	// Type is the leaf's $type (one of the nine DTCG types).
	Type string
	// Value is the raw (unresolved) $value.
	Value any
	// Resolved is the transitive alias resolution of Value. For a whole-value
	// alias it is the final non-alias $value; for a plain value it is Value
	// itself; for a value with no final scalar (an alias to a group, an alias
	// chain that cannot end at a string, or a non-string structured value) it
	// is nil and ResolvedOK is false.
	Resolved any
	// ResolvedOK reports whether Resolved is usable.
	ResolvedOK bool
	// HasAlias is true when Value (or any link in its alias chain) is a
	// whole-value alias reference. Targets use it to decide between a literal
	// and a var()/token() reference.
	HasAlias bool
	// AliasPath is the dotted path of the first whole-value alias in the
	// chain (from Value itself, or the deepest resolvable link), used for
	// cross-token references. "" when HasAlias is false.
	AliasPath string
}

// ResolveExportTokens reads every design/tokens/*.tokens.json under root and
// projects each DTCG leaf into an ExportedToken. Files are read in glob order
// (filepath.Glob is lexical) and the leaves are sorted by dotted name, so the
// result is identical for identical inputs.
//
// A malformed tokens file is an error: the validator is the reporting surface
// (design_validate), but export must not silently emit a partial theme from a
// file it could not parse. A missing or empty tokens/ directory yields
// ErrNoTokensForExport.
//
// A *dirty alias graph* — any dangling or cyclic whole-value alias in any
// source file — is also an error (ErrDirtyAliasGraph, item 5.2): export
// refuses rather than emit a theme whose var()/*token* references cannot be
// resolved. The check reuses the validator's own alias machinery
// (checkAliases), so export and design_validate agree by construction on what
// "dirty" means.
func ResolveExportTokens(root string) (*TokenExport, error) {
	pattern := filepath.Join(root, DirName, TokenSubdir, "*.tokens.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	var leaves []ExportedToken
	var sources []TokenExportSource
	var violations []AliasViolation
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		// Collect every dangling/cyclic alias across the whole tier set, so
		// the refusal lists all of them rather than stopping at the first
		// file. We do not project anything yet: a dirty graph is a refusal,
		// not a partial export (item 5.2).
		rel := relTokenSource(match)
		violations = append(violations, collectAliasViolations(rel, data)...)
		sources = append(sources, TokenExportSource{
			Path:    rel,
			Name:    filepath.Base(match),
			Content: data,
		})
	}
	if len(violations) > 0 {
		sortAliasViolations(violations)
		return nil, &DirtyAliasError{Violations: violations}
	}

	// No dirty aliases: project the leaves. Parsing happened above (inside the
	// alias check) but the projection needs the parsed structure too, so this
	// is a second parse pass over each document — cheap, and it keeps the
	// refusal path from doing projection work it would throw away.
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		docLeaves, err := collectExportLeaves(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.ToSlash(match), err)
		}
		leaves = append(leaves, docLeaves...)
	}

	// Duplicate names across files resolve last-wins, matching the parser's
	// within-file duplicate rule; dropping the earlier duplicate here keeps
	// the sorted output free of repeated names. (Tiers are self-contained by
	// design — SP-140-1 §1a — so this is a defensive tie-break, not the norm.)
	deduped := make(map[string]ExportedToken, len(leaves))
	for _, leaf := range leaves {
		deduped[leaf.Name] = leaf
	}
	out := &TokenExport{Leaves: make([]ExportedToken, 0, len(deduped))}
	for _, name := range sortedKeys(deduped) {
		out.Leaves = append(out.Leaves, deduped[name])
	}
	if len(out.Leaves) == 0 {
		return nil, ErrNoTokensForExport
	}
	out.InputHash = exportTokenInputHash(sources)
	return out, nil
}

// relTokenSource renders a matched token path the way errors and headers name
// it: design-relative, slash-separated (design/tokens/color.tokens.json). When
// the path cannot be made relative to dirname(dirname(match)) it degrades to
// the base name rather than leaking an absolute path into an error message or
// (worse) a hash input.
func relTokenSource(match string) string {
	name := filepath.Base(match)
	dir := filepath.Base(filepath.Dir(match)) // tokens/
	if dir == "" || dir == "." {
		return name
	}
	return DirName + "/" + dir + "/" + name
}

// collectExportLeaves parses one DTCG document (via the validator's parser, so
// the two agree on what a token leaf is) and resolves every leaf's alias chain
// against the document root. Leaves are returned in document order; the caller
// re-sorts globally.
//
// Structured $values (objects/arrays — the norm for color, border, and
// cubicBezier) are not retained by the streaming parser, so the document is
// also decoded into a plain map and each leaf's dotted path is looked up there.
func collectExportLeaves(content []byte) ([]ExportedToken, error) {
	root, parseFindings := parseTokensDocument(content)
	if root == nil {
		return nil, fmt.Errorf("invalid token document: %s", firstFindingMessage(parseFindings))
	}

	var raw map[string]any
	if err := json.Unmarshal(content, &raw); err != nil {
		// parseTokensDocument already validated the JSON; this only guards
		// against a decoder discrepancy.
		return nil, fmt.Errorf("invalid token document: %w", err)
	}

	var leaves []ExportedToken
	var walk func(n *tokenNode)
	walk = func(n *tokenNode) {
		if n.nonObject {
			return
		}
		if n.leaf {
			if !n.hasType || !n.typeIsStr {
				// The validator reports this; export skips the leaf rather
				// than emitting an untyped variable.
				return
			}
			leaves = append(leaves, resolveExportLeaf(n, root, lookupRawValue(raw, n.path)))
			return
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(root)
	return leaves, nil
}

// lookupRawValue walks a decoded JSON document by a dotted token path. DTCG
// metadata keys ("$value", "$type") are skipped, matching the parser's
// structural path. It returns the value at the path, or nil when absent (a
// leaf whose value the streaming parser saw but the map lookup misses — a
// duplicate-key artifact — degrades to no value).
func lookupRawValue(doc map[string]any, path string) any {
	var current any = doc
	for _, segment := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		next, ok := obj[segment]
		if !ok {
			return nil
		}
		current = next
	}
	if obj, ok := current.(map[string]any); ok {
		if v, ok := obj["$value"]; ok {
			return v
		}
		return nil
	}
	return nil
}

// resolveExportLeaf resolves one leaf's alias chain. It records the first
// alias path in the chain (for cross-token references) and the final resolved
// value where one exists.
func resolveExportLeaf(n *tokenNode, root *tokenNode, rawValue any) ExportedToken {
	leaf := ExportedToken{Name: n.path, Type: n.typeName, Value: rawValue}
	if !n.valueIsStr {
		// Non-string $values (numbers, booleans, structured objects/arrays)
		// are not whole-value aliases; they resolve to themselves.
		leaf.Resolved = rawValue
		leaf.ResolvedOK = rawValue != nil || n.rawPresent
		return leaf
	}
	if path, ok := aliasPath(n.valueStr); ok {
		leaf.HasAlias = true
		leaf.AliasPath = path
		resolved, resolvedOK := resolveAliasChain(root, path)
		if resolvedOK {
			leaf.Resolved = resolved
			leaf.ResolvedOK = true
		}
		return leaf
	}
	leaf.Resolved = n.valueStr
	leaf.ResolvedOK = true
	return leaf
}

// resolveAliasChain follows a whole-value alias chain from a path to the first
// non-alias JSON value, mirroring the validator's cycle cap so a pathological
// tree cannot spin. It returns false for a dangling reference, a chain that
// ends at a group or malformed node, or a cycle. A leaf with a structured
// $value ends the chain with a nil-but-present resolved value; the boolean
// signals that the endpoint is a leaf, and the caller renders nil as "".
func resolveAliasChain(root *tokenNode, path string) (any, bool) {
	current := resolveAlias(root, path)
	if current == nil {
		return nil, false
	}
	for range maxAliasChain {
		if !current.leaf {
			return nil, false
		}
		if !current.valueIsStr {
			// A leaf with a structured/number $value is a valid endpoint.
			// Its exact value is resolved separately by the caller (which
			// holds the decoded document); nil here just marks "leaf, not
			// an alias".
			return current.rawValue, true
		}
		next, isAlias := aliasPath(current.valueStr)
		if !isAlias {
			return current.valueStr, true
		}
		current = resolveAlias(root, next)
		if current == nil {
			return nil, false
		}
	}
	return nil, false
}

// firstFindingMessage renders the first finding's message for an error that
// wraps a parse failure; it never returns "".
func firstFindingMessage(findings []Finding) string {
	if len(findings) == 0 {
		return "unparseable token document"
	}
	return findings[0].Message
}

// -----------------------------------------------------------------------------
// Target resolution
// -----------------------------------------------------------------------------

// ResolveExportTargets maps a `targets` argument value to the ordered target
// list to run. An empty value or "all" selects every target in ExportTargets
// order. A value may be a single target ("css") or a comma-separated list
// ("css, ts"); each entry is trimmed and lowercased. Unknown targets are an
// error naming the accepted set, so a typo never silently exports nothing.
func ResolveExportTargets(raw string) ([]ExportTarget, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == ExportTargetAll {
		return exportTargetList(ExportTargets), nil
	}

	seen := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if name == ExportTargetAll {
			return exportTargetList(ExportTargets), nil
		}
		// The screens index is a recognized explicit target but never part
		// of `all`: it derives from design/screens/*.html, not the token
		// sources, so a token re-theme must not rewrite the screen graph.
		if name != ExportTargetScreens {
			if _, ok := ExportFilenames[name]; !ok {
				return nil, fmt.Errorf("unknown export target %q (want one of: %s, %s)", name, strings.Join(ExportTargets, ", "), ExportTargetScreens)
			}
		}
		seen[name] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("no export targets given (want one of: %s, %s)", strings.Join(ExportTargets, ", "), ExportTargetScreens)
	}

	// Emit in canonical order, not argument order: identical target sets must
	// produce identical artifacts and identical summaries regardless of how
	// the caller spelled them. The screens target is appended last: it is not
	// part of the token-export pipeline and its file is resolved separately.
	targets := make([]ExportTarget, 0, len(seen))
	for _, name := range ExportTargets {
		if _, ok := seen[name]; ok {
			targets = append(targets, ExportTarget{Name: name, File: ExportFilenames[name]})
		}
	}
	if _, ok := seen[ExportTargetScreens]; ok {
		targets = append(targets, ExportTarget{Name: ExportTargetScreens, File: ScreensIndexFilename})
	}
	return targets, nil
}

// exportTargetList builds the ExportTarget rows for a name list.
func exportTargetList(names []string) []ExportTarget {
	targets := make([]ExportTarget, 0, len(names))
	for _, name := range names {
		targets = append(targets, ExportTarget{Name: name, File: ExportFilenames[name]})
	}
	return targets
}

// -----------------------------------------------------------------------------
// Emission — one renderer per target
// -----------------------------------------------------------------------------

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

// provenanceHeader renders the SP-140 invariant 2 provenance banner for one
// generated artifact, in the target's comment syntax. It is a fixed,
// newline-terminated block: an opening line naming the tool and target, the
// source-hash line carrying the token-input content hash (§5f: recomputable
// offline at any checkout), and a fixed terminator so a reader can extract it
// without parsing the language.
//
// Determinism is absolute: every line is either a constant or the input hash.
// There is deliberately no timestamp, host, or version — the same token inputs
// must produce byte-identical files on every run and every machine, and a
// timestamp would break both the byte-identity guarantee and the offline
// recomputation the header exists to enable.
//
// Style per target: /* ... */ block comments for the CSS targets (which open
// with "/*" and close on the terminator line), "//" line comments for TS,
// Swift, and Kotlin. Both are valid in the artifact's own language, so the
// generated file still compiles/parses with the banner in place.
func provenanceHeader(target, inputHash string) string {
	if inputHash == "" {
		inputHash = "unhashed"
	}
	prefix := headerCommentPrefix(target)
	open, close := "//", ""
	if prefix == "/*" {
		open, close = "/*", " */"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s Generated by design_export_tokens (target=%s). Do not edit by hand.%s\n",
		open, target, close)
	fmt.Fprintf(&b, "%s source-hash: %s%s\n", prefix, inputHash, close)
	fmt.Fprintf(&b, "%s Source: design/tokens/*.tokens.json (W3C DTCG). Recompute the hash from those bytes to verify.%s\n",
		prefix, close)
	if close != "" {
		// The block comment already closed on the last content line; nothing
		// more to emit.
		return b.String()
	}
	fmt.Fprintf(&b, "%s sprout:end-provenance\n", prefix)
	return b.String()
}

// headerCommentPrefix returns the comment opener for a target's comment style.
func headerCommentPrefix(target string) string {
	switch target {
	case ExportTargetCSS, ExportTargetTailwind:
		return "/*"
	default:
		return "//"
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

// ExportedArtifact is one written (or to-be-written) generated file.
type ExportedArtifact struct {
	// Target is the exporter that produced it (css, ts, ...).
	Target string
	// RelPath is the workspace-relative slash path (design/generated/<file>).
	RelPath string
	// Content is the exact byte content written.
	Content []byte
	// Hash is the deterministic content hash (see tokenExportArtifactHash).
	Hash string
}

// RenderArtifacts renders every target into memory without touching the
// filesystem, in canonical target order. The handler uses this to precheck and
// write each file; tests use it to assert byte-identical output.
func RenderArtifacts(tokens *TokenExport, targets []ExportTarget) ([]ExportedArtifact, error) {
	artifacts := make([]ExportedArtifact, 0, len(targets))
	for _, target := range targets {
		content, err := RenderExport(target.Name, tokens)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, ExportedArtifact{
			Target:  target.Name,
			RelPath: filepath.ToSlash(filepath.Join(DirName, GeneratedSubdir, target.File)),
			Content: content,
			Hash:    tokenExportArtifactHash(content),
		})
	}
	return artifacts, nil
}

// TokenExportSourceHash is the label prefix on every content hash this package
// emits. It names the algorithm in-band so a consumer reading a header knows
// how to recompute the digest without reading the implementation.
const TokenExportSourceHashLabel = "fnv1a64"

// tokenExportHash computes the FNV-1a 64-bit digest of data and renders it as
// "fnv1a64:<16 hex digits>". FNV-1a is deliberate (SP-140-5 §5f): it is
// dependency-free, stable across runs, platforms, and Go versions — a consumer
// at an arbitrary checkout can recompute the same digest offline from the
// tree alone — and this is a staleness/consistency check, not a security
// boundary, so a cryptographic digest buys nothing.
func tokenExportHash(data []byte) string {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	var h uint64 = offset64
	for _, c := range data {
		h ^= uint64(c)
		h *= prime64
	}
	return fmt.Sprintf("%s:%016x", TokenExportSourceHashLabel, h)
}

// TokenExportInputHash hashes the *token inputs* — the DTCG source documents
// the export read, not the rendered artifact. It is the authoritative
// provenance hash recorded in every generated artifact's header
// (SP-140 invariant 2, SP-140-5 §5f "provenance at any ref").
//
// Why inputs and not artifact bytes: the header must be identical across the
// five targets (only the rendering differs, the semantic content does not), and
// it has to be recomputable without re-running the export. A consumer at an
// arbitrary checkout recomputes it with:
//
//	exportTokenSourceHash(dir(os.ReadFile(design/tokens/*.tokens.json)))
//
// — i.e. concat the raw bytes of design/tokens/*.tokens.json sorted by
// basename, hash that. Nothing else (no timestamps, no paths, no env) feeds
// the digest, so it is deterministic across machines and checkouts.
//
// Each document is folded in as "len(bytes)\n bytes" so two different file
// partitions cannot concatenate to the same byte stream and collide.
func TokenExportInputHash(docs []TokenExportSource) string {
	var b bytes.Buffer
	for _, doc := range docs {
		fmt.Fprintf(&b, "%d\n", len(doc.Content))
		b.Write(doc.Content)
	}
	return tokenExportHash(b.Bytes())
}

// TokenExportSource is one DTCG token source document as read from disk: its
// workspace-relative slash path (design/tokens/<name>.tokens.json) and its
// exact bytes. Collected by ResolveExportTokens and retained on the
// TokenExport so the provenance header can be built from the same read that
// produced the leaves (no second filesystem pass, no chance of drift).
type TokenExportSource struct {
	// Path is the workspace-relative slash path of the source document.
	Path string
	// Name is the source document's base name (color.tokens.json); it is the
	// stable sort key, so the hash does not depend on the workspace root.
	Name string
	// Content is the document's exact bytes.
	Content []byte
}

// exportTokenInputHash computes the provenance hash over a set of source
// documents in name order, so two read orders cannot produce two hashes. It is
// the single place the canonical ordering lives.
func exportTokenInputHash(sources []TokenExportSource) string {
	ordered := append([]TokenExportSource(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	return TokenExportInputHash(ordered)
}

// tokenExportArtifactHash is the content hash of a *rendered artifact's* bytes
// (including its provenance header). It is the deterministic identity of the
// emitted file — two runs over the same tokens hash equal, and any single-byte
// change to the output moves it — which is what the handler reports per file
// (designExportFile.ContentHash) so a caller can tell whether a write actually
// changed anything.
func tokenExportArtifactHash(content []byte) string {
	return tokenExportHash(content)
}

// WriteExportedArtifacts writes each artifact (already rendered and carrying
// its final design-relative slash path in RelPath) under root, creating parent
// directories as needed. It returns an error on the first failure.
//
// Every RelPath must be confined to design/ — the handler asserts this before
// calling (relocateArtifacts + resolveExportOutDir), and this function
// re-checks it so a future caller cannot use it to escape the tree. Writes are
// plain file writes; the handler has prechecked every target path already.
func WriteExportedArtifacts(root string, artifacts []ExportedArtifact) error {
	return WriteExportedArtifactsAt(root, artifacts)
}

// WriteExportedArtifactsAt is WriteExportedArtifacts under its explicit name:
// it writes artifacts at their own RelPath, which the handler has already
// relocated to the requested output directory. Keeping both names lets the
// handler read naturally while the default-path helper stays for tests.
func WriteExportedArtifactsAt(root string, artifacts []ExportedArtifact) error {
	for _, a := range artifacts {
		clean := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(a.RelPath)), "/")
		if clean != DirName && !strings.HasPrefix(clean, DirName+"/") {
			return fmt.Errorf("refusing to write outside %s/: %s", DirName, a.RelPath)
		}
		abs := filepath.Join(root, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(abs), err)
		}
		if err := os.WriteFile(abs, a.Content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", clean, err)
		}
	}
	return nil
}

// sortedKeys returns a map's keys in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
