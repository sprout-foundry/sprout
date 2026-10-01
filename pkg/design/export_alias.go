package design

import (
	"errors"
	"fmt"
	"sort"
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

// export_alias.go — the DTCG alias-graph validation half of token export:
// DirtyAliasError / AliasViolation and the dirty-alias detection helpers.

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
