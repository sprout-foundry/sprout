package design

// SP-140-9 §9b: the derived flow export's pure half. The flow source .json
// (flowsource.go) is the only flow truth; the .mmd beside it is derived
// deterministically from the .json plus the touched screens' data-nav edges
// and carries a provenance header so design_validate recomputes it and flags
// drift as an error (SP-140-5 §5f convention, shared with the token exports
// and the screens index). Hand-editing a .mmd is invalid — the edit goes
// into the .json/screens and the export regenerates.
//
// Input ordering — the offline recompute recipe the header promises: fold
// "len(bytes)\n bytes" over (1) design/flows/<name>.json, then (2)
// design/screens/<stem>.html for every screen the flow's subgraph touches
// (the step screens plus every data-nav-reachable screen), sorted by stem;
// fnv1a64 that byte stream (TokenExportInputHash). No timestamps, no paths,
// no env.
//
// §9c node-id stability: step nodes use the step id, off-path screens use
// the screen stem; a regeneration never renames a node the §7d layout
// sidecar keys on.

import (
	"fmt"
	"sort"
	"strings"
)

// FlowSourceHashLabel is the header key on the derived .mmd carrying the §9b
// input hash. The value renders with the token-export digest label
// (fnv1a64:<16 hex>), so every derived artifact in the tree verifies the same
// way.
const FlowSourceHashLabel = "flow-source-hash"

// flowMMDProvenance and flowMMDSourceFmt are the fixed banner facts of the
// derived export, worded like the screens index and the token exports.
const (
	flowMMDProvenance = "Derived from the flow source .json + the touched screens' data-nav edges (SP-140-9 §9b). Do not edit by hand; regenerate with design_export_tokens targets:flows."
	flowMMDSourceFmt  = "design/flows/%s.json, then design/screens/<stem>.html for the touched screens sorted by stem. Recompute the hash from those bytes (%s) to verify."
)

// defaultFlowDirection is the declared layout direction of a derived export.
// The §7d sidecar keys on node ids, not on direction; TD keeps the canvas
// reading top-down like every hand-authored flow today.
const defaultFlowDirection = "TD"

// FlowExportDoc is the derived graph of one flow: the walk (the steps) plus
// every data-nav-reachable screen marked off-path. It is the render model of
// the .mmd — everything RenderFlowMMD emits, nothing else.
type FlowExportDoc struct {
	// Name is the flow name (= the source document stem).
	Name string
	// Direction is the mermaid layout direction (always defaultFlowDirection
	// today).
	Direction string
	// Nodes are the graph nodes: the walk steps in source order first, then
	// the off-path screens sorted by stem.
	Nodes []FlowExportNode
	// Edges are the graph edges: the step-to-step walk edges in step order
	// first, then the off-path nav edges sorted by (source, trigger, target).
	Edges []FlowExportEdge
}

// FlowExportNode is one node of the derived graph.
type FlowExportNode struct {
	// ID is the §9c stable node id: the step id for walk nodes, the screen
	// stem for off-path nodes.
	ID string
	// Label is the rendered label: the step's label text, or the screen stem.
	Label string
	// Screen is the stem of the screen the node stands for; "" for pure walk
	// nodes whose screen is only implied by the step (kept: every v1 step
	// names a screen, so this stays empty only for hypothetical future
	// step-less nodes).
	Screen string
	// OffPath marks a data-nav-reachable screen that is not a step.
	OffPath bool
}

// FlowExportEdge is one edge of the derived graph.
type FlowExportEdge struct {
	Source string
	// Target is the target node id (a step id or an off-path screen stem).
	Target string
	// Trigger is the edge label: the step's trigger for walk edges, the
	// data-nav trigger for off-path edges; "" renders an unlabeled edge.
	Trigger string
	// OffPath marks an edge the flow steps do not account for (a data-nav
	// edge reached by closure, not by a step).
	OffPath bool
}

// DeriveFlowMMD derives the export doc for one flow source against the
// screens' data-nav graph (the parsed screens index). A step screen unknown
// to the index contributes no nav edges — the screen validator reports the
// unknown stem, and the export still renders the walk. Self-loop navs (a
// screen navigating to itself) are suppressed: they are state navigation
// inside one screen, not graph edges.
func DeriveFlowMMD(src *FlowSource, screens *ScreenIndexDoc) *FlowExportDoc {
	doc := &FlowExportDoc{Name: src.Name, Direction: defaultFlowDirection}

	for _, step := range src.Steps {
		doc.Nodes = append(doc.Nodes, FlowExportNode{ID: step.ID, Label: step.Label, Screen: step.Screen})
	}
	for _, step := range src.Steps {
		if step.Next == "" {
			continue
		}
		doc.Edges = append(doc.Edges, FlowExportEdge{Source: step.ID, Target: step.Next, Trigger: step.Trigger})
	}

	stepScreens := map[string]bool{}
	walkRank := map[string]int{} // step screen -> first step index touching it
	for i, step := range src.Steps {
		if !stepScreens[step.Screen] {
			walkRank[step.Screen] = i
			stepScreens[step.Screen] = true
		}
	}

	// Data-nav edges off each step's screen join the graph as off-path edges
	// (s1 -> some-screen|trigger|), and every reachable screen beyond the
	// step screens joins as an off-path node keyed by its stem (§9c). The
	// dedup key carries the trigger: the same target reachable by two
	// triggers is two distinct edges, the same fact twice is one.
	seenEdge := map[string]bool{}
	reachable := map[string]bool{}
	queue := make([]string, 0, len(stepScreens))
	for stem := range stepScreens {
		reachable[stem] = true
		queue = append(queue, stem)
	}
	sort.Strings(queue) // deterministic BFS seed order
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, nav := range flowScreenNavs(screens, current) {
			if nav.To == current {
				continue
			}
			edge := FlowExportEdge{Source: current, Target: nav.To, Trigger: nav.Trigger, OffPath: true}
			key := edge.Source + "\x00" + edge.Trigger + "\x00" + edge.Target
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			doc.Edges = append(doc.Edges, edge)
			if !reachable[nav.To] {
				reachable[nav.To] = true
				queue = append(queue, nav.To)
			}
		}
	}

	offStems := make([]string, 0, len(reachable))
	for stem := range reachable {
		if !stepScreens[stem] {
			offStems = append(offStems, stem)
		}
	}
	sort.Strings(offStems)
	for _, stem := range offStems {
		doc.Nodes = append(doc.Nodes, FlowExportNode{ID: stem, Label: stem, Screen: stem, OffPath: true})
	}

	flowSortOffPathEdges(doc.Edges, walkRank)
	return doc
}

// flowSortOffPathEdges moves the off-path edges after the walk edges and
// orders them by (source walk rank then source id, trigger, target), so the
// rendered bytes never depend on map or discovery order.
func flowSortOffPathEdges(edges []FlowExportEdge, walkRank map[string]int) {
	var walk, off []FlowExportEdge
	for _, e := range edges {
		if e.OffPath {
			off = append(off, e)
		} else {
			walk = append(walk, e)
		}
	}
	sort.SliceStable(off, func(i, j int) bool {
		si, sj := off[i].Source, off[j].Source
		ri, rj := walkRank[si], walkRank[sj]
		if ri != rj {
			return ri < rj
		}
		if si != sj {
			return si < sj
		}
		if off[i].Trigger != off[j].Trigger {
			return off[i].Trigger < off[j].Trigger
		}
		return off[i].Target < off[j].Target
	})
	copy(edges, append(walk, off...))
}

// flowScreenNavs returns one screen's nav edges from the parsed index, or nil
// when the screen is unknown to it.
func flowScreenNavs(screens *ScreenIndexDoc, stem string) []ScreenIndexNav {
	if screens == nil {
		return nil
	}
	for _, entry := range screens.Screens {
		if entry.Stem == stem {
			return entry.Nav
		}
	}
	return nil
}

// FlowTouchedScreens returns the flow's transitive data-nav closure: the step
// screens plus every screen reachable from them. Sorted; the off-path cross
// check and the hash-input assembly share this one closure definition.
func FlowTouchedScreens(src *FlowSource, screens *ScreenIndexDoc) []string {
	touched := map[string]bool{}
	queue := make([]string, 0, len(src.Steps))
	for _, step := range src.Steps {
		if !touched[step.Screen] {
			touched[step.Screen] = true
			queue = append(queue, step.Screen)
		}
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, nav := range flowScreenNavs(screens, current) {
			if !touched[nav.To] {
				touched[nav.To] = true
				queue = append(queue, nav.To)
			}
		}
	}
	out := make([]string, 0, len(touched))
	for stem := range touched {
		out = append(out, stem)
	}
	sort.Strings(out)
	return out
}

// FlowMMDInputHash computes the §9b provenance hash over one flow's inputs in
// the documented order: the flow source bytes first, then the touched
// screens' bytes in stem order. allScreens are the raw screen documents (as
// ScreensIndexSources read them); screens the flow touches but the tree does
// not carry are skipped (unknown stems are the screen validator's finding).
func FlowMMDInputHash(srcRaw []byte, srcRelPath string, allScreens []TokenExportSource, touched []string) string {
	byStem := make(map[string][]byte, len(allScreens))
	for _, s := range allScreens {
		byStem[s.Name] = s.Content
	}
	inputs := make([]TokenExportSource, 0, 1+len(touched))
	inputs = append(inputs, TokenExportSource{Path: srcRelPath, Name: srcRelPath, Content: srcRaw})
	for _, stem := range touched {
		if content, ok := byStem[stem]; ok {
			inputs = append(inputs, TokenExportSource{
				Path:    ScreenRelPath(stem),
				Name:    ScreenRelPath(stem),
				Content: content,
			})
		}
	}
	return TokenExportInputHash(inputs)
}

// RenderFlowMMD renders the derived .mmd bytes: the provenance banner, the
// declaration, node definitions (walk then off-path), then edges (walk then
// off-path), triggers as |label| edge labels. Deterministic by construction
// — the doc's slices are ordered by DeriveFlowMMD, the hash is an input.
func RenderFlowMMD(doc *FlowExportDoc, hash string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "%%%% %s — derived flow export (SP-140-9 §9b). %s\n", doc.Name, flowMMDProvenance)
	fmt.Fprintf(&b, "%%%% %s: %s\n", FlowSourceHashLabel, hash)
	fmt.Fprintf(&b, "%%%% Source: %s\n", fmt.Sprintf(flowMMDSourceFmt, doc.Name, TokenExportSourceHashLabel))
	fmt.Fprintf(&b, "flowchart %s\n", doc.Direction)
	for _, n := range doc.Nodes {
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", n.ID, flowMermaidEscape(n.Label))
	}
	for _, e := range doc.Edges {
		if e.Trigger != "" {
			fmt.Fprintf(&b, "  %s -->|%s| %s\n", e.Source, flowMermaidEscape(e.Trigger), e.Target)
			continue
		}
		fmt.Fprintf(&b, "  %s --> %s\n", e.Source, e.Target)
	}
	return []byte(b.String())
}

// flowMermaidEscape renders s as the inside of a mermaid double-quoted
// string. The one in-quote hazard is a double quote itself (#quot; is
// mermaid's escape); backslashes are inert in quoted labels and pass through.
func flowMermaidEscape(s string) string {
	return strings.ReplaceAll(s, `"`, "#quot;")
}
