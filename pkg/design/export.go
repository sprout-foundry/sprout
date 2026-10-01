package design

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// export.go — the token-export core: export target consts, the
// TokenExport / ExportedToken types, DTCG resolution (ResolveExportTokens),
// and target resolution. Alias validation, rendering, and artifact writing
// live in export_alias.go / export_render.go / export_artifacts.go.

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

// ExportTargetFlows is the SP-140-9 §9b derived-flow export target: it
// regenerates design/flows/<name>.mmd from each flow source .json plus the
// touched screens. Like the screens index it is explicit-only — never part of
// `all` — because it derives from the flow sources, not the token sources, so
// a token re-theme must not rewrite the flow exports.
const ExportTargetFlows = "flows"

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
		// The screens index and the derived flow exports are recognized
		// explicit targets but never part of `all`: they derive from
		// design/screens/*.html and design/flows/*.json respectively, not
		// the token sources, so a token re-theme must not rewrite them.
		if name != ExportTargetScreens && name != ExportTargetFlows {
			if _, ok := ExportFilenames[name]; !ok {
				return nil, fmt.Errorf("unknown export target %q (want one of: %s, %s, or %s)", name, strings.Join(ExportTargets, ", "), ExportTargetScreens, ExportTargetFlows)
			}
		}
		seen[name] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("no export targets given (want one of: %s, %s, or %s)", strings.Join(ExportTargets, ", "), ExportTargetScreens, ExportTargetFlows)
	}

	// Emit in canonical order, not argument order: identical target sets must
	// produce identical artifacts and identical summaries regardless of how
	// the caller spelled them. The screens and flows targets are appended
	// last: they are not part of the token-export pipeline and their files
	// are resolved separately.
	targets := make([]ExportTarget, 0, len(seen))
	for _, name := range ExportTargets {
		if _, ok := seen[name]; ok {
			targets = append(targets, ExportTarget{Name: name, File: ExportFilenames[name]})
		}
	}
	if _, ok := seen[ExportTargetScreens]; ok {
		targets = append(targets, ExportTarget{Name: ExportTargetScreens, File: ScreensIndexFilename})
	}
	if _, ok := seen[ExportTargetFlows]; ok {
		targets = append(targets, ExportTarget{Name: ExportTargetFlows, File: FlowSubdir})
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
