package design

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Rule ids for the mermaid flow validator, SP-140-1 §1c/§1g.
const (
	// ruleFlowchartDeclaration fires when a flow file has zero or more than
	// one flowchart/graph declaration.
	ruleFlowchartDeclaration = "flowchart_declaration"

	// ruleFlowchartSyntax fires on a statement line that cannot be read as a
	// node or edge (missing endpoint id, or a bare statement with no node).
	ruleFlowchartSyntax = "flowchart_syntax"

	// ruleFlowchartNodeStem fires when a screen-flow node id is not a
	// wireframe stem (SP-140-1 §1c).
	ruleFlowchartNodeStem = "flowchart_node_stem"
)

// flowOpRe matches mermaid connect operators. Alternatives are ordered
// longest-first so RE2's leftmost, first-matching alternative picks the full
// operator (e.g. --> before --, and == before ===-style).
var flowOpRe = regexp.MustCompile(`(-\.->|--o|--x|o--|x--|-->|===|==>|---|--|==)`)

// leadingIDRe captures the leading node identifier of a node reference.
var leadingIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+`)

// FlowNode is a single node extracted from a mermaid flowchart.
type FlowNode struct {
	ID    string
	Label string
}

// FlowEdge is a single directed/undirected edge extracted from a flowchart.
type FlowEdge struct {
	Source string
	Target string
	// Label is the |trigger| text on a labeled edge (A -->|trigger| B);
	// "" on a label-free edge. SP-140-9 §9b renders the derived .mmd's
	// triggers as edge labels, so the parser surfaces them instead of
	// dropping them.
	Label    string
	HasArrow bool
}

// Flowchart is the result of parsing a mermaid flowchart file (the
// flowchart subset — node/edge extraction, not full syntax fidelity,
// SP-140-1 §1c).
type Flowchart struct {
	Direction    string
	Declarations int // number of flowchart/graph declaration lines
	Nodes        map[string]FlowNode
	Edges        []FlowEdge
	NodeOrder    []string // first-seen order of node ids
	BadLines     []int    // 1-based line numbers of statements that could not be parsed
}

// ParseFlowchart parses a mermaid flowchart document and extracts its nodes
// and edges. It is a subset parser: it handles flowchart/graph declarations,
// node definitions (id plus any shape/label), and edge statements (including
// chained A --> B --> C forms), while skipping comments, subgraph/end, and
// styling keywords (classDef/class/style/linkStyle/click). Full syntax
// fidelity is intentionally not required (SP-140-1 §1c).
func ParseFlowchart(content string) Flowchart {
	fc := Flowchart{Nodes: map[string]FlowNode{}}
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "%%") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "flowchart") || strings.HasPrefix(lower, "graph") {
			fc.Declarations++
			if fields := strings.Fields(line); len(fields) >= 2 {
				fc.Direction = fields[1]
			}
			continue
		}
		// structural / styling keywords are not node or edge statements.
		if lower == "end" || strings.HasPrefix(lower, "subgraph") ||
			strings.HasPrefix(lower, "classdef") || strings.HasPrefix(lower, "class") ||
			strings.HasPrefix(lower, "style") || strings.HasPrefix(lower, "linkstyle") ||
			strings.HasPrefix(lower, "click") {
			continue
		}
		parseStatement(line, i+1, &fc)
	}
	return fc
}

// parseStatement reads one node/edge statement, splitting it into
// source/target segments on connect operators so chained edges (A --> B --> C)
// resolve to a full node and edge set. A |label| on an operator ("A
// -->|submit| B") becomes that edge's Label (SP-140-9 §9b: the derived .mmd
// carries triggers as edge labels); label-free parsing is unchanged.
func parseStatement(line string, lineNo int, fc *Flowchart) {
	segs, ops := splitByOperator(line)
	labels := make(map[int]string, len(ops))
	for i, op := range ops {
		labels[i] = edgeLabel(op)
		ops[i] = trimEdgeLabel(op)
	}
	ids := make([]string, 0, len(segs))
	for _, seg := range segs {
		id, label := parseNodeRef(seg)
		ids = append(ids, id)
		if id != "" {
			addNode(fc, id, label)
		}
	}
	for i, op := range ops {
		if i+1 < len(ids) && ids[i] != "" && ids[i+1] != "" {
			fc.Edges = append(fc.Edges, FlowEdge{
				Source:   ids[i],
				Target:   ids[i+1],
				Label:    labels[i],
				HasArrow: strings.Contains(op, ">"),
			})
		}
	}
	// A statement with an operator needs both a source and a target; a bare
	// statement needs at least a node id. Anything else is unparseable.
	var incomplete bool
	if len(ops) > 0 {
		incomplete = ids[0] == "" || ids[len(ids)-1] == ""
	} else {
		incomplete = ids[0] == ""
	}
	if incomplete {
		fc.BadLines = append(fc.BadLines, lineNo)
	}
}

// edgeLabel returns the trigger text between the pipes of a labeled operator
// ("-->|submit|" -> "submit"); "" when the operator carries no label.
func edgeLabel(op string) string {
	open := strings.IndexByte(op, '|')
	if open < 0 {
		return ""
	}
	rest := op[open+1:]
	if close := strings.IndexByte(rest, '|'); close >= 0 {
		return strings.TrimSpace(rest[:close])
	}
	// Unclosed label: take what is present (the line reports as an
	// incomplete statement anyway, since the target segment is swallowed).
	return strings.TrimSpace(rest)
}

// trimEdgeLabel strips a trailing |label| group off a connect operator
// ("-->|submit|" --> "-->"), so the labeled form does not leak pipe
// characters into HasArrow or the next segment. A label-free operator is
// returned unchanged.
func trimEdgeLabel(op string) string {
	if open := strings.IndexByte(op, '|'); open >= 0 {
		return op[:open]
	}
	return op
}

// splitByOperator splits a statement into node segments and the operators
// between them. For "A --> B --> C" it yields segs [A, B, C] and ops [-->, -->].
// Operators inside a node label's bracket group ([...], (...), {...}) are NOT
// separators, so "A[Step --> Process] --> B" yields segs [A[...], B] and a
// single operator. A |label| group attached to an operator ("-->|submit|") is
// part of that operator: the whole span "-->|submit|" is returned as the op,
// so the label never spawns a phantom segment or swallows the target node.
func splitByOperator(line string) (segs, ops []string) {
	depth := 0
	last := 0
	i := 0
	for i < len(line) {
		switch c := line[i]; c {
		case '[', '(', '{':
			depth++
			i++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
			i++
		case '|':
			// A label only rides an operator at bracket depth 0 (mermaid's
			// A -->|x| B); a pipe inside a node label's brackets is label
			// text, and a lone pipe with no operator before it is not label
			// syntax. A closed |...| span is consumed as part of the current
			// op (chained labels A -->|x| B -->|y| C each ride their op); an
			// unclosed one leaks into the next segment, which then parses as
			// a missing target id and reports via BadLines.
			if depth == 0 && len(ops) > 0 {
				if close := strings.IndexByte(line[i+1:], '|'); close >= 0 {
					// The span rides the current op ("-->|x|"), so the label
					// reaches the edge rather than the next segment.
					ops[len(ops)-1] += line[i : i+close+2]
					i += close + 2
					last = i // the label belongs to the op, not the next segment
					continue
				}
			}
			i++
		default:
			if depth == 0 {
				if loc := flowOpRe.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
					op := line[i : i+loc[1]-loc[0]]
					segs = append(segs, line[last:i])
					ops = append(ops, op)
					i += len(op)
					last = i
					continue
				}
			}
			i++
		}
	}
	segs = append(segs, line[last:])
	return segs, ops
}

// parseNodeRef extracts the id and (best-effort) label from a node reference
// such as "A", "A[label]", "A(label)", or "A{label}".
func parseNodeRef(s string) (id, label string) {
	s = strings.TrimSpace(s)
	id = leadingIDRe.FindString(s)
	if id == "" {
		return "", ""
	}
	rest := s[len(id):]
	if len(rest) > 0 {
		label = bracketContent(rest)
	}
	return id, label
}

// bracketContent returns the text inside the outermost bracket group after an
// id ([...], (...), {...}, and nested variants like ((...)), [[...]]).
func bracketContent(rest string) string {
	rest = strings.TrimSpace(rest)
	if len(rest) == 0 {
		return ""
	}
	open := rest[0]
	var close byte
	switch open {
	case '[':
		close = ']'
	case '(':
		close = ')'
	case '{':
		close = '}'
	default:
		return ""
	}
	depth := 0
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return strings.TrimSpace(rest[1:i])
			}
		}
	}
	// unclosed bracket: best-effort take what is present
	return strings.TrimSpace(rest[1:])
}

// addNode records a node, preserving first-seen order and keeping the label of
// the first non-empty definition.
func addNode(fc *Flowchart, id, label string) {
	if existing, ok := fc.Nodes[id]; ok {
		if existing.Label == "" && label != "" {
			fc.Nodes[id] = FlowNode{ID: id, Label: label}
		}
		return
	}
	fc.Nodes[id] = FlowNode{ID: id, Label: label}
	fc.NodeOrder = append(fc.NodeOrder, id)
}

// derivedFlowStepIDs lists the step ids a sibling flow source (.json beside
// the .mmd, SP-140-9 §9b) declares. The derived export renders steps with
// their step id as the node id, so those ids are legitimate screen-flow nodes
// for the node-stem rule. Absent/unparsable source yields nil.
func derivedFlowStepIDs(root, relPath string) []string {
	name := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".mmd")
	dir := path.Dir(filepath.ToSlash(relPath))
	srcPath := filepath.Join(root, filepath.FromSlash(path.Join(dir, name+".json")))
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return nil
	}
	src, err := ParseFlowSource(srcPath, raw)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(src.Steps))
	for _, s := range src.Steps {
		ids = append(ids, s.ID)
	}
	return ids
}

// isScreenFlow reports whether the flow looks like a screen flow: at least one
// node id is a known wireframe stem. Pure process/user flows (no node matches
// a stem) are left un-checked, per SP-140-1 §1c ("nothing cross-checks them").
func isScreenFlow(fc Flowchart, stemSet map[string]struct{}) bool {
	for _, id := range fc.NodeOrder {
		if _, ok := stemSet[id]; ok {
			return true
		}
	}
	return false
}

// ValidateFlows validates one mermaid flow file per SP-140-1 §1c/§1g,
// adjusted by SP-140-9 §9a: screen-flow node ids may equal either a wireframe
// stem or a delivered-screen stem during the format migration (9.4 collapses
// the tier; post-migration only screens remain). Hard checks (SeverityError):
// exactly one flowchart declaration, unparseable statement lines, and
// screen-flow node ids matching neither tier. The result is never nil.
func ValidateFlows(root, relPath string, content []byte, wireframeStems []string, screenStems ...string) []Finding {
	fc := ParseFlowchart(string(content))
	stemSet := make(map[string]struct{}, len(wireframeStems)+len(screenStems))
	for _, s := range wireframeStems {
		stemSet[s] = struct{}{}
	}
	for _, s := range screenStems {
		stemSet[s] = struct{}{}
	}
	// SP-140-9 §9b: a derived flow's step nodes carry the step id, not a
	// screen stem — a sibling flow-source .json names the legitimate ids.
	for _, id := range derivedFlowStepIDs(root, relPath) {
		stemSet[id] = struct{}{}
	}

	var findings []Finding

	switch {
	case fc.Declarations == 0:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleFlowchartDeclaration,
			Severity: SeverityError,
			Message:  "expected a flowchart or graph declaration (one flowchart per file)",
		})
	case fc.Declarations > 1:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleFlowchartDeclaration,
			Severity: SeverityError,
			Message:  fmt.Sprintf("one flowchart per file (found %d declarations)", fc.Declarations),
		})
	}

	for _, ln := range fc.BadLines {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     ln,
			Rule:     ruleFlowchartSyntax,
			Severity: SeverityError,
			Message:  "unparseable flowchart line (expected a node or edge)",
		})
	}

	if isScreenFlow(fc, stemSet) {
		for _, id := range fc.NodeOrder {
			if _, ok := stemSet[id]; !ok {
				findings = append(findings, Finding{
					File:     relPath,
					Rule:     ruleFlowchartNodeStem,
					Severity: SeverityError,
					Message:  fmt.Sprintf("node id %q is neither a wireframe stem (design/wireframes/%s.svg) nor a screen stem (design/screens/%s.html); screen-flow node ids must equal a known screen (SP-140-9 §9a accepts either tier during migration)", id, id, id),
				})
			}
		}
	}

	return finalizeFlowFindings(findings)
}

// ValidateFlowsDir validates every design/flows/*.mmd under root, SP-140-1
// §1c/§1g. Wireframe stems are gathered from design/wireframes/*.svg so the
// node-id == stem rule resolves. A missing or empty flows directory yields no
// findings, not an error. Findings are sorted by file, line, rule, message.
func ValidateFlowsDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "flows", "*.mmd")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	if len(matches) == 0 {
		return findings, nil
	}

	// Wireframe stems for the node-id == stem rule.
	stems := []string{}
	if wf, err := filepath.Glob(filepath.Join(root, DirName, "wireframes", "*.svg")); err == nil {
		for _, m := range wf {
			stems = append(stems, strings.TrimSuffix(path.Base(filepath.ToSlash(m)), ".svg"))
		}
	}

	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to %s: %w", match, root, err)
		}
		screenStems := assetStems(root, "screens", ".html")
		findings = append(findings, ValidateFlows(root, filepath.ToSlash(rel), data, stems, screenStems...)...)
	}
	sortFindings(findings)
	return findings, nil
}

// finalizeFlowFindings normalizes a flow finding slice: never nil and sorted
// deterministically (file, line, rule, message).
func finalizeFlowFindings(findings []Finding) []Finding {
	if findings == nil {
		findings = []Finding{}
	}
	sortFindings(findings)
	return findings
}
