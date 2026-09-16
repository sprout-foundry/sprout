package design

import (
	"fmt"
	"slices"
	"strings"
)

// maxAliasChain caps the number of hops when following an alias chain.
// It is a safety valve, not a real limit: well-formed chains end at a
// non-alias node and broken ones end at a dangling reference, so a walk
// should always terminate on its own — but a pathological tree must not
// be able to spin the validator, so the walk stops silently at the cap.
const maxAliasChain = 128

// checkAliases reports dangling and cyclic {group.token} alias
// references across the parsed tree (SP-140-1 §1a: the validator must
// resolve every reference and reject cycles and dangling paths). Only
// whole-value aliases resolve: a string whose entire text is
// `{` + inner + `}`; multi-part strings like "{a} / {b}" are plain
// values, and aliasing them is not specified (deliberate for this
// item). Resolution is per-file against the file's own root — SP-140-1
// has no cross-file aliasing — and walks only node.child maps, so
// $extensions subtrees (never parsed into nodes) are invisible here by
// construction.
//
// Findings carry only Rule, Line, and Message — the caller adds File
// and sorts.
func checkAliases(root *tokenNode) []Finding {
	var findings []Finding
	seenCycles := map[string]struct{}{}
	var walk func(n *tokenNode)
	walk = func(n *tokenNode) {
		if n.nonObject {
			return
		}
		if n.leaf {
			path, isAlias := aliasPath(n.valueStr)
			if !n.valueIsStr || !isAlias {
				return
			}
			target := resolveAlias(root, path)
			if target == nil {
				findings = append(findings, Finding{
					Rule:    ruleTokenAliasDangling,
					Line:    n.line,
					Message: fmt.Sprintf("alias %q on token %q does not resolve", "{"+path+"}", n.path),
				})
				return
			}
			reportAliasCycle(n, target, root, &findings, seenCycles)
			return
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(root)
	return findings
}

// aliasPath parses one whole-value alias: the string must start with
// '{', end with '}', and carry a non-empty inner text free of braces.
// The inner text is used untrimmed as the dot path — "{ a.b }" is an
// alias whose path " a.b " cannot resolve, which surfaces as dangling.
// "{}}", "{a{b}}", and "{a}b}" fail the inner-no-braces or whole-text
// test and stay plain strings.
func aliasPath(v string) (path string, ok bool) {
	if len(v) < 3 || v[0] != '{' || v[len(v)-1] != '}' {
		return "", false
	}
	inner := v[1 : len(v)-1]
	if inner == "" || strings.ContainsAny(inner, "{}") {
		return "", false
	}
	return inner, true
}

// resolveAlias resolves one alias dot path against the file's root
// node, returning the target or nil. Every segment must resolve to any
// node — group, leaf, or malformed nonObject node: existence is enough,
// the value's well-formedness is the structure rules' business.
func resolveAlias(root *tokenNode, path string) *tokenNode {
	current := root
	for _, segment := range strings.Split(path, ".") {
		next, ok := current.child[segment]
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

// reportAliasCycle follows the alias chain from leaf's target and
// reports a cycle when the chain revisits a node. The chain ends at a
// dangling reference (already reported), a group or nonObject node, or
// a leaf whose value is not a whole-value alias; a repeat before any
// of those means the aliases form a loop. Each distinct cycle reports
// once, keyed by its member paths — cycleKey quotes each member, so
// paths containing NUL or any delimiter cannot collide — so N aliases
// on one loop yield N walks but a single finding — anchored at the
// leaf whose walk found the repeat, with members listed from the
// repeated node onward.
func reportAliasCycle(leaf, target, root *tokenNode, findings *[]Finding, seenCycles map[string]struct{}) {
	visited := []string{leaf.path}
	current := target
	for range maxAliasChain {
		visited = append(visited, current.path)
		if !current.leaf {
			return
		}
		path, isAlias := aliasPath(current.valueStr)
		if !current.valueIsStr || !isAlias {
			return
		}
		next := resolveAlias(root, path)
		if next == nil {
			return // dangling further along; already reported at its own node
		}
		if slices.Contains(visited, next.path) {
			start := slices.Index(visited, next.path)
			members := visited[start:]
			key := cycleKey(members)
			if _, seen := seenCycles[key]; seen {
				return
			}
			seenCycles[key] = struct{}{}
			*findings = append(*findings, Finding{
				Rule:    ruleTokenAliasCycle,
				Line:    leaf.line,
				Message: "alias cycle: " + strings.Join(members, " -> "),
			})
			return
		}
		current = next
	}
}

// cycleKey builds the seenCycles dedup key from one cycle's member
// paths. Members are sorted (order-independent identity for a cycle)
// and each is quoted with %q before joining: %q never emits a raw
// NUL, quote, or separator look-alike, so distinct member sets cannot
// collapse to the same key — a plain join on a delimiter would, since
// JSON keys may legally contain any byte (e.g. "\u0000").
func cycleKey(members []string) string {
	quoted := make([]string, len(members))
	for i, m := range slices.Sorted(slices.Values(members)) {
		quoted[i] = fmt.Sprintf("%q", m)
	}
	return strings.Join(quoted, "\x00")
}
