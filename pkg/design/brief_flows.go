package design

// brief_flows.go — the flow-edge extraction half of the screen brief:
// briefFlowEdges (dir walk) + the mermaid label parsing (briefFlowEdgesInFile,
// briefAddStatementEdges, briefSegmentLabel, the label regexes), split out
// of brief.go.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// briefFlowEdges parses every design/flows/*.mmd file and returns the edges
// touching the screen, with triggers, sorted deterministically. The screen is
// a node id (== wireframe stem, per SP-140-1 §1c). A flow file that cannot be
// read is skipped (the validator reports it), never a hard error.
func briefFlowEdges(root, screen string) ([]BriefFlowEdge, error) {
	matches, err := filepath.Glob(filepath.Join(root, DirName, FlowSubdir, "*.mmd"))
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", filepath.Join(root, DirName, FlowSubdir), err)
	}
	sort.Strings(matches)

	edges := []BriefFlowEdge{}
	for _, match := range matches {
		data, readErr := os.ReadFile(match)
		if readErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(root, match)
		if relErr != nil {
			rel = match
		}
		rel = filepath.ToSlash(rel)
		flowName := assetName(path.Base(rel))

		fc := ParseFlowchart(string(data))
		for _, e := range briefFlowEdgesInFile(rel, flowName, string(data), fc) {
			if e.Source != screen && e.Target != screen {
				continue
			}
			switch {
			case e.Source == screen && e.Target == screen:
				e.Direction = "both"
			case e.Source == screen:
				e.Direction = "out"
				e.OtherStem = e.Target
				e.OtherLabel = fc.Nodes[e.Target].Label
			default:
				e.Direction = "in"
				e.OtherStem = e.Source
				e.OtherLabel = fc.Nodes[e.Source].Label
			}
			edges = append(edges, e)
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Flow != b.Flow {
			return a.Flow < b.Flow
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Trigger < b.Trigger
	})
	return edges, nil
}

// briefLabelSegmentRe matches a label segment between two edge operators: a
// quoted label (`-- "tap Submit" --`) or a bare one (`-- label --`). It is
// applied to the *segments* splitByOperator already produced (the shared,
// tested split the validator uses), so a plain `A --> B` yields no label and a
// chained `A --> B --> C` cannot leak operator text into the label.
var briefLabelSegmentRe = regexp.MustCompile(`^\s*(?:"([^"]*)"|([^"|]+?))\s*$`)

// briefPipeLabelRe matches the `|label|` form inside a target segment
// (`-->|tap Submit| home`).
var briefPipeLabelRe = regexp.MustCompile(`^\s*\|([^|]*)\|\s*(.*)$`)

// briefFlowEdgesInFile extracts the edges of one flow file, resolving any
// mermaid edge label as the trigger. It reuses the shared splitByOperator
// (flowchart.go) the validator uses for the edge split, then reads the label
// from the labelled forms the manifest contract documents — `A -- "tap Submit"
// --> B` and `A -->|tap Submit| B`. A plain `A --> B`, a chained
// `A --> B --> C`, and an unknown/opaque form all yield a "" trigger and never
// a spurious label.
//
// The line scan is authoritative (it sees labels the subset parser does not);
// the parser's own edge list is merged afterwards to pick up any edge shape the
// line scan could not split (an unlabelled statement is already covered, so
// this is defensive for the odd operator form).
func briefFlowEdgesInFile(rel, flowName, content string, fc Flowchart) []BriefFlowEdge {
	scanned := []BriefFlowEdge{}
	seen := map[string]bool{}

	record := func(dst *[]BriefFlowEdge, src, tgt, trigger string) {
		if src == "" || tgt == "" {
			return
		}
		key := src + "\x00" + tgt
		if seen[key] {
			return
		}
		seen[key] = true
		*dst = append(*dst, BriefFlowEdge{
			Flow:     rel,
			FlowName: flowName,
			Source:   src,
			Target:   tgt,
			Trigger:  strings.TrimSpace(trigger),
		})
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "flowchart") || strings.HasPrefix(lower, "graph") {
			continue
		}
		if lower == "end" || strings.HasPrefix(lower, "subgraph") ||
			strings.HasPrefix(lower, "classdef") || strings.HasPrefix(lower, "class") ||
			strings.HasPrefix(lower, "style") || strings.HasPrefix(lower, "linkstyle") ||
			strings.HasPrefix(lower, "click") {
			continue
		}

		// A labelled `A -- "label" --> B` statement splits into alternating
		// endpoint/label segments (src, label, tgt) with an operator between
		// each pair; a plain statement splits into consecutive endpoints
		// (src, tgt, ...). Classify each segment as an endpoint or a label and
		// walk the alternating list, so both shapes and the chained
		// `A --> B --> C` form resolve correctly.
		segs, ops := splitByOperator(line)
		if len(ops) == 0 || len(segs) < 2 {
			continue
		}
		briefAddStatementEdges(&scanned, seen, rel, flowName, segs)
	}

	// Merge the subset parser's edges for any pair the scan did not produce
	// (defensive: an operator shape the scan could not split).
	for _, e := range fc.Edges {
		record(&scanned, e.Source, e.Target, "")
	}
	return scanned
}

// briefAddStatementEdges resolves the edges of one split statement, handling
// both the plain `A --> B` and the labelled `A -- "label" --> B` shapes (and
// their chained variants). segs are the operator-separated segments from
// splitByOperator. A segment is an endpoint when it parses as a node reference;
// a segment between two endpoints that does not is a label belonging to the
// following edge.
func briefAddStatementEdges(edges *[]BriefFlowEdge, seen map[string]bool, rel, flowName string, segs []string) {
	// Normalise each segment: strip a leading `|label|`, record the node id,
	// and keep a labelled form for the label case.
	type seg struct {
		raw     string
		node    string
		pipe    string // `|label|` text, when present
		isLabel bool
	}
	norm := make([]seg, 0, len(segs))
	for _, s := range segs {
		pipe := ""
		body := s
		if m := briefPipeLabelRe.FindStringSubmatch(s); m != nil {
			pipe = strings.TrimSpace(m[1])
			body = m[2]
		}
		node, _ := parseNodeRef(body)
		norm = append(norm, seg{raw: s, node: node, pipe: pipe})
	}
	// A segment with no node id is a label (it sits between two operators).
	for i := range norm {
		norm[i].isLabel = norm[i].node == "" && norm[i].pipe == ""
	}

	for i := 0; i < len(norm); i++ {
		src := norm[i]
		if src.isLabel || src.node == "" {
			continue
		}
		// The next endpoint may be the immediate next segment, or the segment
		// after a label.
		next := i + 1
		trigger := src.pipe
		if trigger == "" && next < len(norm) && norm[next].isLabel {
			trigger = briefSegmentLabel(norm[next].raw)
			next++
		}
		if next >= len(norm) {
			continue
		}
		tgt := norm[next]
		if trigger == "" {
			trigger = tgt.pipe
		}
		if tgt.node == "" {
			continue
		}
		key := src.node + "\x00" + tgt.node
		if seen[key] {
			continue
		}
		seen[key] = true
		*edges = append(*edges, BriefFlowEdge{
			Flow:     rel,
			FlowName: flowName,
			Source:   src.node,
			Target:   tgt.node,
			Trigger:  strings.TrimSpace(trigger),
		})
	}
}

// briefSegmentLabel reads a standalone label segment (the middle segment of a
// `src -- "label" --> tgt` statement): a quoted string, a bare label, or "".
func briefSegmentLabel(seg string) string {
	s := strings.TrimSpace(seg)
	if s == "" {
		return ""
	}
	// A bare operator fragment (e.g. a leftover from a `-->` inside the
	// segment) is not a label.
	if strings.ContainsAny(s, "<>=") {
		return ""
	}
	if m := briefLabelSegmentRe.FindStringSubmatch(s); m != nil {
		if m[1] != "" {
			return m[1]
		}
		return strings.TrimSpace(m[2])
	}
	return ""
}
