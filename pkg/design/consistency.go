package design

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Rule ids for the flow/wireframe bidirectionality consistency pack,
// SP-140-4 §4b. This pack is a part of the SP-140-1g validator (same
// {file, line?, severity, message, rule} findings schema, emitted through the
// standard dispatch in tree.go) rather than a separate tool; the rule ids are
// constants so findings and future tooling share one spelling.
const (
	// ruleConsistencyFlowEdgeWireframe fires when a flow edge endpoint is a
	// wireframe stem (so the flow is a screen flow) but a *non-terminal* node
	// id has no wireframe counterpart. A terminal state — a node with no
	// outgoing edge — is exempt, per §4b ("every edge has a wireframe
	// counterpart unless the node is a terminal state").
	ruleConsistencyFlowEdgeWireframe = "consistency_flow_edge_wireframe"
)

// README artifact references reuse the existing ruleManifestLinkDangling id
// (manifest.go): a README listing naming an artifact with no backing file is
// the same failure mode as a dangling markdown link ("screens referenced in
// README exist", §4b), and the critique/rubric mapping already treats that rule
// as the manifest's link/consistency class. One rule id per broken promise
// rather than a second spelling of the same check.

// consistencySeverity is the severity of the flow/wireframe bidirectionality
// findings. §4b places this pack under "Consistency checker" without spelling
// a severity per rule; these are real correctness issues (a screen flow that
// leads to a screen that does not exist, a README that promises a screen that
// was never drawn) but the tree is still renderable, so they are advisory
// warns rather than hard errors — matching the §1h warn class and keeping the
// toolkit's error/warn split ("cannot be parsed/self-contained" vs "parses but
// is inconsistent") intact.
const consistencySeverity = SeverityWarn

// manifestSectionHeadingRe matches a markdown ATX heading and captures its
// text, used to find the manifest's Screens/Flows listing sections.
var manifestSectionHeadingRe = regexp.MustCompile(`^#{1,6}\s+(.*\S)\s*$`)

// manifestBulletNameRe captures the backticked name of a manifest listing
// bullet (`- \`login\` — draft — sign-in`).
var manifestBulletNameRe = regexp.MustCompile("^[-*]\\s+`([^`]+)`")

// ValidateConsistency runs the cross-artifact consistency packs over the whole
// design tree under root, SP-140-4 §4b. It covers the cross-artifact checks the
// per-artifact validators cannot see:
//
//   - ruleConsistencyFlowEdgeWireframe: every non-terminal node referenced by
//     a flow edge has a wireframe counterpart (a terminal state is exempt);
//   - ruleManifestLinkDangling: every artifact named by the README manifest's
//     Screens/Flows listings maps to a real file (a Screens entry to a
//     wireframe or screen, a Flows entry to a flow or wireframe).
//
// The third bullet of §4b ("every data-nav target exists") is the existing
// ruleSVGDataNavDangling hard rule (svg.go); it is emitted through
// ValidateWireframesDir/ValidateWireframe and is covered by this pack's tests
// rather than re-implemented here.
//
// It also runs the screen-inventory and naming packs of §4b (orphan screens,
// screens/ ↔ wireframes/ name mismatches, duplicate screen names), which live
// in inventory_rules.go alongside this pack because they share the same
// cross-artifact dispatch point and finding schema. A workspace with no design/
// tree yields no findings. Findings are sorted by file, line, rule, message;
// the result is never nil.
func ValidateConsistency(root string) []Finding {
	findings := []Finding{}

	wireframeStems := assetStems(root, "wireframes", ".svg")
	stemSet := make(map[string]struct{}, len(wireframeStems))
	for _, s := range wireframeStems {
		stemSet[s] = struct{}{}
	}
	screenStems := assetStems(root, "screens", ".html")
	flowStems := assetStems(root, "flows", ".mmd")

	if matches, err := filepath.Glob(filepath.Join(root, DirName, "flows", "*.mmd")); err == nil {
		sort.Strings(matches)
		for _, match := range matches {
			data, err := os.ReadFile(match)
			if err != nil {
				// An unreadable flow is reported by ValidateFlowsDir's I/O
				// error path; the consistency pack skips it.
				continue
			}
			rel := relAsset(root, match)
			findings = append(findings, validateFlowWireframeBidirectionality(rel, data, stemSet)...)
		}
	}

	readmeAssets := readmeScreenAssets{
		wireframeStems: wireframeStems,
		screenStems:    screenStems,
		flowStems:      flowStems,
	}
	findings = append(findings, validateReadmeScreenRefs(root, readmeAssets)...)

	// SP-140-4 §4b screen inventory + naming packs (inventory_rules.go): orphan
	// screens (info) and screens/ ↔ wireframes/ name mismatches (warn). They
	// share this dispatch point so design_validate and the static critique pick
	// them up with the bidirectionality pack.
	findings = append(findings, ValidateInventory(root)...)
	findings = append(findings, screenSlugViolations(root)...)
	findings = append(findings, screenNameMismatches(root)...)

	sortFindings(findings)
	return findings
}

// flowBidirectionalityFindings is the single-file entry point for the
// flow/wireframe bidirectionality rule (SP-140-4 §4b), used by ValidateFile's
// flow dispatch so a one-file flow validation surfaces the same consistency
// findings a whole-tree run does. The result is never nil and is sorted.
func flowBidirectionalityFindings(relPath string, content []byte, wireframeStems []string) []Finding {
	stemSet := make(map[string]struct{}, len(wireframeStems))
	for _, s := range wireframeStems {
		stemSet[s] = struct{}{}
	}
	findings := validateFlowWireframeBidirectionality(relPath, content, stemSet)
	if findings == nil {
		findings = []Finding{}
	}
	sortFindings(findings)
	return findings
}

// validateFlowWireframeBidirectionality flags every non-terminal node reachable
// through a flow edge that has no wireframe counterpart, SP-140-4 §4b.
// wireframeStems is the set of wireframe stems (without .svg).
//
// Like the §1c node-stem rule, the check applies only to screen flows (at
// least one edge endpoint is a wireframe stem); a pure process/user flow has
// no wireframes to be bidirectional with and is left alone. A node is exempt
// when it is terminal: it is the target of an edge but the source of none
// (an end state, per §4b). Source-only nodes are flagged too — the edge
// leaves a screen that does not exist, which is the same broken
// bidirectionality from the other side. Findings are deduplicated per node id
// and carry the 1-based line of the node's first mention.
func validateFlowWireframeBidirectionality(relPath string, content []byte, wireframeStems map[string]struct{}) []Finding {
	fc := ParseFlowchart(string(content))
	findings := []Finding{}

	hasSource := map[string]bool{}
	hasTarget := map[string]bool{}
	edgeTouchesStem := false
	for _, e := range fc.Edges {
		hasSource[e.Source] = true
		hasTarget[e.Target] = true
		if _, ok := wireframeStems[e.Source]; ok {
			edgeTouchesStem = true
		}
		if _, ok := wireframeStems[e.Target]; ok {
			edgeTouchesStem = true
		}
	}
	if !edgeTouchesStem {
		return findings
	}

	edgeNodes := map[string]bool{}
	for _, e := range fc.Edges {
		edgeNodes[e.Source] = true
		edgeNodes[e.Target] = true
	}

	for _, id := range fc.NodeOrder {
		if !edgeNodes[id] {
			// A node not referenced by any edge is not this rule's business.
			continue
		}
		if _, ok := wireframeStems[id]; ok {
			continue
		}
		if hasTarget[id] && !hasSource[id] {
			// Terminal state (no outgoing edge): exempt per §4b.
			continue
		}
		findings = append(findings, Finding{
			File:     relPath,
			Line:     nodeFirstLine(content, id),
			Rule:     ruleConsistencyFlowEdgeWireframe,
			Severity: consistencySeverity,
			Message:  fmt.Sprintf("flow edge references node %q (non-terminal) with no wireframe counterpart (expected design/wireframes/%s.svg); add the wireframe or mark the node terminal", id, id),
		})
	}

	return findings
}

// readmeScreenAssets is the set of design file stems a README listing can
// resolve against: wireframe/screen stems for a Screens entry, flow stems for
// a Flows entry.
type readmeScreenAssets struct {
	wireframeStems []string
	screenStems    []string
	flowStems      []string
}

// validateReadmeScreenRefs flags every listing in the README manifest's
// Screens/Flows sections that names an artifact with no real file, SP-140-4 §4b
// ("screens referenced in README exist"). The two sections are checked against
// the artifact they actually name:
//
//   - a Screens entry names a screen, resolvable as a wireframe stem
//     (design/wireframes/<name>.svg) or a delivered screen file
//     (design/screens/<name>.html);
//   - a Flows entry names a flow, resolvable as design/flows/<name>.mmd (or,
//     because a screen flow and its wireframe share the stem, a wireframe).
//
// A missing README yields no findings.
func validateReadmeScreenRefs(root string, assets readmeScreenAssets) []Finding {
	rel := path.Join(DirName, ManifestName)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}

	stems := func(list []string) map[string]struct{} {
		m := make(map[string]struct{}, len(list))
		for _, s := range list {
			m[s] = struct{}{}
		}
		return m
	}
	wireframes := stems(assets.wireframeStems)
	screens := stems(assets.screenStems)
	flows := stems(assets.flowStems)

	var findings []Finding
	for _, ref := range readmeScreenRefs(string(data)) {
		var expected string
		switch ref.section {
		case "Flows":
			expected = fmt.Sprintf("design/flows/%s.mmd", ref.name)
			if _, ok := flows[ref.name]; ok {
				continue
			}
			// A screen flow and its wireframe share a stem; a listing that
			// names a screen flow is satisfied by the wireframe too.
			if _, ok := wireframes[ref.name]; ok {
				continue
			}
		default: // Screens
			expected = fmt.Sprintf("design/wireframes/%s.svg (or design/screens/%s.html)", ref.name, ref.name)
			if _, ok := wireframes[ref.name]; ok {
				continue
			}
			if _, ok := screens[ref.name]; ok {
				continue
			}
		}
		findings = append(findings, Finding{
			File:     rel,
			Line:     ref.line,
			Rule:     ruleManifestLinkDangling,
			Severity: consistencySeverity,
			Message:  fmt.Sprintf("README %s listing references %q, which has no %s file", ref.section, ref.name, expected),
		})
	}
	return findings
}

// readmeScreenRef is one screen named by a README Screens or Flows listing.
type readmeScreenRef struct {
	name    string
	section string // "Screens" or "Flows"
	line    int
}

// readmeScreenRefs extracts the screen names referenced by the manifest's
// Screens and Flows listing bullets, in document order. Only listings inside a
// Screens or Flows section are considered (the frames block, status-marker
// prose, and link lists carry no screen names), matching the sections the
// design workspace template documents (SP-140-2 §2c). A name seen twice in the
// same section is reported once, at its first line; the same name may appear
// in both sections (a flow file and its wireframe share a stem) and yields one
// finding per section.
func readmeScreenRefs(text string) []readmeScreenRef {
	var refs []readmeScreenRef
	section := ""
	inFence := false
	seen := map[string]bool{}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := manifestSectionHeadingRe.FindStringSubmatch(line); m != nil {
			section = manifestSectionKind(m[1])
			continue
		}
		// A new heading-like marker (a "## Screens" reset) or any other
		// non-listing line inside a section keeps the section until the next
		// heading; only listing bullets are read.
		if section == "" {
			continue
		}
		m := manifestBulletNameRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		key := section + "\x00" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		refs = append(refs, readmeScreenRef{name: name, section: section, line: i + 1})
	}
	return refs
}

// manifestSectionKind classifies a heading text as the "Screens" or "Flows"
// listing section; other headings return "".
func manifestSectionKind(heading string) string {
	trimmed := strings.TrimSpace(heading)
	switch {
	case strings.HasPrefix(strings.ToLower(trimmed), "screen"):
		return "Screens"
	case strings.HasPrefix(strings.ToLower(trimmed), "flow"):
		return "Flows"
	default:
		return ""
	}
}

// relAsset resolves an absolute asset path to its workspace-relative slash path.
func relAsset(root, abs string) string {
	if rel, err := filepath.Rel(root, abs); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}

// nodeFirstLine returns the 1-based line of the first statement mentioning the
// node id, or 0 when the id cannot be located. Best-effort line support.
func nodeFirstLine(content []byte, id string) int {
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(id) + `([^A-Za-z0-9_-]|$)`)
	for i, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "flowchart") || strings.HasPrefix(lower, "graph") {
			continue
		}
		if re.MatchString(line) {
			return i + 1
		}
	}
	return 0
}
