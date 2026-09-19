package design

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Rule ids for the token validator, SP-140-1 §1a. Constants rather than
// literals so findings and future tooling share one spelling.
const (
	ruleTokenJSONParse      = "token_json_parse"
	ruleTokenLeafStructure  = "token_leaf_structure"
	ruleTokenTypeMembership = "token_type_membership"
	ruleTokenAliasDangling  = "token_alias_dangling"
	ruleTokenAliasCycle     = "token_alias_cycle"
)

// tokenTypes is the closed set of $type values the validator accepts,
// SP-140-1 §1a: color, dimension, fontFamily, fontWeight, number,
// duration, cubicBezier, strokeStyle, border.
var tokenTypes = map[string]struct{}{
	"color":       {},
	"dimension":   {},
	"fontFamily":  {},
	"fontWeight":  {},
	"number":      {},
	"duration":    {},
	"cubicBezier": {},
	"strokeStyle": {},
	"border":      {},
}

// isTokenType reports whether name is one of the nine accepted DTCG
// types (SP-140-1 §1a). Unknown types are an error, never a silent pass.
func isTokenType(name string) bool {
	_, ok := tokenTypes[name]
	return ok
}

// ValidateTokens validates one DTCG token file per SP-140-1 §1a: JSON
// parse, leaf/group structure, $type membership, and within-file alias
// resolution. Alias checks are per-file by design: tiers are
// self-contained, and cross-file aliasing is not specified.
//
// The returned findings carry File=relPath and SeverityError — every
// token rule is a hard violation — sorted by file, line, rule, message.
// The result is never nil.
func ValidateTokens(relPath string, content []byte) []Finding {
	root, parseFindings := parseTokensDocument(content)
	if root == nil {
		return finalizeFindings(parseFindings, relPath)
	}
	findings := structureFindings(root)
	findings = append(findings, checkAliases(root)...)
	return finalizeFindings(findings, relPath)
}

// structureFindings walks the parsed tree in document order and reports
// structural violations: structural values that are not objects, leaves
// missing $type, non-string or unknown $type values, and $type/$value
// shape mismatches. A node carrying $type without $value still
// traverses as a group; a leaf ($value present) is never descended
// into — its children are malformed and get their own findings only
// via the leaf-with-children report.
func structureFindings(node *tokenNode) []Finding {
	var findings []Finding
	var walk func(n *tokenNode)
	walk = func(n *tokenNode) {
		if n.nonObject {
			findings = append(findings, Finding{
				Rule:    ruleTokenLeafStructure,
				Line:    n.line,
				Message: fmt.Sprintf("token %q must be an object", n.path),
			})
			return
		}
		if n.leaf {
			if !n.hasType {
				findings = append(findings, Finding{
					Rule:    ruleTokenLeafStructure,
					Line:    n.line,
					Message: fmt.Sprintf("token %q is missing $type", n.path),
				})
			}
			if n.hasType && !n.typeIsStr {
				findings = append(findings, Finding{
					Rule:    ruleTokenTypeMembership,
					Line:    n.line,
					Message: fmt.Sprintf("$type on %q must be a string", n.path),
				})
			}
			if n.typeIsStr && !isTokenType(n.typeName) {
				findings = append(findings, Finding{
					Rule:    ruleTokenTypeMembership,
					Line:    n.line,
					Message: fmt.Sprintf("unknown $type %q on token %q", n.typeName, n.path),
				})
			}
			if len(n.children) > 0 {
				findings = append(findings, Finding{
					Rule:    ruleTokenLeafStructure,
					Line:    n.line,
					Message: fmt.Sprintf("token %q has child nodes but also $value", n.path),
				})
			}
			return
		}
		if n.hasType {
			findings = append(findings, Finding{
				Rule:    ruleTokenLeafStructure,
				Line:    n.line,
				Message: fmt.Sprintf("token %q has $type but no $value", n.path),
			})
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(node)
	return findings
}

// ValidateTokensDir validates every design/tokens/*.tokens.json under
// root, SP-140-1 §1a. A missing or empty tokens directory yields no
// findings, not an error — a whole-tree validator run must not fail on
// workspaces without tokens (or without design/ at all).
//
// Files are validated in glob order (filepath.Glob is lexical); the
// combined findings are sorted by file, line, rule, message. Errors are
// I/O failures only — per-file rule violations are findings, not errors.
func ValidateTokensDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "tokens", "*.tokens.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to %s: %w", match, root, err)
		}
		findings = append(findings, ValidateTokens(filepath.ToSlash(rel), data)...)
	}
	sortFindings(findings)
	return findings, nil
}

// finalizeFindings stamps relPath and SeverityError onto every finding
// and sorts — the token rules are all hard violations. The result is
// never nil, so callers can distinguish "validated, clean" from "no
// validation ran".
func finalizeFindings(findings []Finding, relPath string) []Finding {
	if findings == nil {
		findings = []Finding{}
	}
	for i := range findings {
		findings[i].File = relPath
		findings[i].Severity = SeverityError
	}
	sortFindings(findings)
	return findings
}

// sortFindings orders findings by File, then Line, then Rule, then
// Message — plain string/int compares keep the order fully
// deterministic across runs.
func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
}
