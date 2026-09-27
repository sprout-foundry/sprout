package design

// SP-140-9 §9b: the derived flow export's filesystem half — the artifact the
// design_export_tokens `flows` target writes, and the validator checks the
// §9d ripples wire in: derived-.mmd drift (recompute + hash, error),
// header-less .mmd (transitional info pointing at 9.4), and the off-path
// data-nav cross-check (warning, scoped to .json-bearing trees).

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Rule ids for the SP-140-9 §9b/§9d flow-tier checks.
const (
	// ruleFlowMMDDrift fires when a derived .mmd does not match the
	// recompute from its .json source + the touched screens' bytes (stale or
	// hand-edited), or parses as a derived header with an unverifiable hash.
	ruleFlowMMDDrift = "flow_mmd_drift"

	// ruleFlowMMDLegacy is the transitional info on a flows/*.mmd with no
	// derived header — a hand-authored legacy flow. Item 9.4 migrates these
	// to .json sources + regenerated exports; until then the notice is the
	// same transitional queue the wireframe deprecation rides (info, so the
	// pre-migration tree stays 0-error).
	ruleFlowMMDLegacy = "flow_mmd_legacy"

	// ruleFlowOffPathNav fires (warn) when a data-nav edge from a touched
	// screen is not represented in the derived .mmd of any flow source — an
	// off-path navigation, often intentional (a global nav), never an error.
	// Scoped to trees that carry flow sources.
	ruleFlowOffPathNav = "flow_off_path_nav"
)

// FlowMMDFilename returns the derived export path for a flow name.
func FlowMMDFilename(name string) string {
	return flowMMDRelPath(name)
}

// ErrNoFlowSources is returned when the `flows` export target is requested
// but design/flows/ holds no flow source documents — there is nothing to
// derive, and an empty export would be misleading. A distinct sentinel so
// the handler reports a usage error, mirroring ErrNoScreensForIndex.
var ErrNoFlowSources = errors.New("no flow sources found under design/flows")

// FlowExportedArtifact is one rendered derived .mmd ready to write.
type FlowExportedArtifact struct {
	// Name is the flow name (the .json stem).
	Name string
	// RelPath is the workspace-relative slash path (design/flows/<name>.mmd).
	RelPath string
	// Content is the exact byte content to write.
	Content []byte
	// Hash is the §9b input hash carried in the provenance header.
	Hash string
}

// RenderFlowMDMArtifact derives one flow's export artifact under root: read
// the flow source, read the screens, derive, render, hash.
func RenderFlowMDMArtifact(root string, name string) (*FlowExportedArtifact, error) {
	srcFile, err := readOneFlowSource(root, name)
	if err != nil {
		return nil, err
	}
	screens, err := ScreensIndexSources(root)
	if err != nil {
		return nil, err
	}
	indexDoc, err := screensDocFromSources(screens, root)
	if err != nil {
		return nil, err
	}
	touched := FlowTouchedScreens(srcFile.Source, indexDoc)
	hash := FlowMMDInputHash(srcFile.Raw, srcFile.RelPath, screens, touched)
	doc := DeriveFlowMMD(srcFile.Source, indexDoc)
	return &FlowExportedArtifact{
		Name:    name,
		RelPath: FlowMMDFilename(name),
		Content: RenderFlowMMD(doc, hash),
		Hash:    hash,
	}, nil
}

// RenderAllFlowMDMArtifacts derives the export artifact for every flow source
// under root, in flow-name order.
func RenderAllFlowMDMArtifacts(root string) ([]FlowExportedArtifact, error) {
	sources, err := ReadFlowSources(root)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, ErrNoFlowSources
	}
	out := make([]FlowExportedArtifact, 0, len(sources))
	for _, src := range sources {
		artifact, err := RenderFlowMDMArtifact(root, src.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, *artifact)
	}
	return out, nil
}

// screensDocFromSources parses every screen source into the index shape the
// flow derivation consumes. Re-deriving from the screen bytes (rather than
// reading design/generated/screens.json) keeps the export self-contained: it
// does not couple to the screens target's regen cycle, and the bytes are the
// same provenance inputs the header hashes, so one read serves both.
func screensDocFromSources(sources []TokenExportSource, root string) (*ScreenIndexDoc, error) {
	frames := manifestFrames(root)
	doc := &ScreenIndexDoc{}
	for _, s := range sources {
		entry := ParseScreenHTML(s.Content, frames)
		entry.Stem = s.Name
		doc.Screens = append(doc.Screens, entry)
	}
	return doc, nil
}

// readOneFlowSource reads and parses one flow source document by flow name.
func readOneFlowSource(root, name string) (*FlowSourceFile, error) {
	rel := FlowSourceRelPath(name)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rel, err)
	}
	src, err := ParseFlowSource(rel, raw)
	if err != nil {
		return nil, err
	}
	return &FlowSourceFile{RelPath: rel, Name: name, Raw: raw, Source: src}, nil
}

// flowSourceNames globs the flow source names under root (the .layout.json
// sidecars excluded), sorted.
func flowSourceNames(root string) ([]string, error) {
	paths, err := FlowSourcePaths(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, strings.TrimSuffix(path.Base(filepath.ToSlash(p)), ".json"))
	}
	return names, nil
}

// ValidateFlowsTree runs the §9b whole-tree flow checks on top of the
// per-file flow rules: for every flow source document, the .json schema
// rules; for every flows/*.mmd, the derived-drift check (a source + a
// header-less or stale .mmd) or the legacy notice; and the off-path
// cross-check for .json-bearing trees. Called from ValidateTree after the
// per-artifact passes. The result is never nil and sorted.
func ValidateFlowsTree(root string) []Finding {
	findings := []Finding{}

	names, err := flowSourceNames(root)
	if err != nil {
		return findings
	}

	// The .json schema rules.
	stems := append(assetStems(root, "wireframes", ".svg"), assetStems(root, "screens", ".html")...)
	for _, name := range names {
		rel := FlowSourceRelPath(name)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue // the I/O path reports this in the dir-level validator
		}
		findings = append(findings, ValidateFlowSource(rel, data, stems)...)
	}

	// The drift + legacy checks ride the per-.mmd entry point so a
	// single-file validate surfaces them too; the tree pass just collects.
	findings = append(findings, flowMMDDriftFindings(root)...)
	findings = append(findings, flowOffPathFindings(root)...)

	sortFindings(findings)
	return findings
}

// flowMMDDriftFindings checks every flows/*.mmd: a .mmd with a .json source
// must carry a parsable derived header and verify against the recompute
// (drift = error); a .mmd with no .json source is a transitional legacy
// notice (info, 9.4 migrates).
func flowMMDDriftFindings(root string) []Finding {
	findings := []Finding{}
	matches, err := filepath.Glob(filepath.Join(root, DirName, FlowSubdir, "*.mmd"))
	if err != nil {
		return findings
	}
	for _, match := range matches {
		rel, err := filepath.Rel(root, match)
		if err != nil {
			rel = filepath.ToSlash(match)
		}
		data, err := os.ReadFile(match)
		if err != nil {
			continue // the dir-level validator's I/O path reports it
		}
		findings = append(findings, flowMMDChecks(root, filepath.ToSlash(rel), data)...)
	}
	return findings
}

// flowMMDChecks is the single-file form of the §9b .mmd checks (drift when a
// .json source exists, legacy notice when it does not), so a single-file
// validate surfaces the same findings a whole-tree run does.
func flowMMDChecks(root, rel string, data []byte) []Finding {
	name := strings.TrimSuffix(path.Base(rel), ".mmd")
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(FlowSourceRelPath(name)))); err != nil {
		return []Finding{flowLegacyFinding(rel)}
	}
	return flowDriftFinding(root, rel, name, data)
}

// flowDriftFinding recomputes one derived .mmd's provenance and compares: a
// missing or unparsable header is invalid (the generator always writes one),
// a header whose hash differs from the recompute is stale or hand-edited —
// either way an error with the regeneration as the remedy.
func flowDriftFinding(root, rel, name string, data []byte) []Finding {
	recorded, ok := flowRecordedSourceHash(data)
	if !ok {
		return []Finding{{
			File:     rel,
			Line:     1,
			Rule:     ruleFlowMMDDrift,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s sits beside %s but carries no parsable %s header; the .mmd is a derived export — regenerate with design_export_tokens targets:flows, never hand-edit it", rel, FlowSourceRelPath(name), FlowSourceHashLabel),
		}}
	}
	artifact, err := RenderFlowMDMArtifact(root, name)
	if err != nil {
		return []Finding{{
			File:     rel,
			Line:     1,
			Rule:     ruleFlowMMDDrift,
			Severity: SeverityError,
			Message:  fmt.Sprintf("the derived export could not be recomputed from %s (%v); fix the flow source, then regenerate with design_export_tokens targets:flows", FlowSourceRelPath(name), err),
		}}
	}
	if recorded != artifact.Hash {
		return []Finding{{
			File:     rel,
			Line:     1,
			Rule:     ruleFlowMMDDrift,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is stale or hand-edited: its %s %s does not match the recompute %s; regenerate with design_export_tokens targets:flows", rel, FlowSourceHashLabel, recorded, artifact.Hash),
		}}
	}
	return nil
}

// flowRecordedSourceHash extracts the recorded digest from the derived
// header: the "%% flow-source-hash: fnv1a64:<hex>" line. Unparsable (or
// absent) yields false — the generator always writes the line, so a .mmd
// without it is not a derived artifact.
func flowRecordedSourceHash(data []byte) (string, bool) {
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(raw, "%%"))
		if !strings.HasPrefix(line, FlowSourceHashLabel+":") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, FlowSourceHashLabel+":"))
		if value == "" {
			return "", false
		}
		return value, true
	}
	return "", false
}

// flowLegacyFinding is the transitional header-less .mmd notice (9.4
// migrates it, mirroring the wireframe deprecation's transitional pattern).
func flowLegacyFinding(rel string) Finding {
	stem := strings.TrimSuffix(path.Base(rel), ".mmd")
	return Finding{
		File:     rel,
		Rule:     ruleFlowMMDLegacy,
		Severity: SeverityInfo,
		Message:  fmt.Sprintf("flows/%s.mmd is hand-authored: the flow tier is derived (SP-140-9 §9b) — add %s and regenerate the export (item 9.4 migrates the legacy flows)", stem, FlowSourceRelPath(stem)),
	}
}

// flowOffPathFindings runs the §9b cross-check on .json-bearing trees: a
// data-nav edge from a touched screen that no derived .mmd accounts for is a
// warning (off-path navigation, often intentional — a global nav), never an
// error. Scoped to trees that carry flow sources, so legacy .mmd-only trees
// do not newly warn.
//
// The comparison reads the rendered exports: every derived .mmd's off-path
// and walk edges are parsed back (ParseFlowchart), and a touched screen's
// data-nav edge counts as accounted when some flow renders an edge from a
// node standing for that screen with the same trigger and target. An edge
// from a screen no flow step touches is out of scope entirely (not part of
// any flow's subgraph).
func flowOffPathFindings(root string) []Finding {
	findings := []Finding{}
	if !hasFlowSources(root) {
		return findings
	}
	sources, err := ReadFlowSources(root)
	if err != nil {
		return findings // reported as a schema finding by ValidateFlowsTree
	}
	screens, err := ScreensIndexSources(root)
	if err != nil {
		return findings
	}
	indexDoc, err := screensDocFromSources(screens, root)
	if err != nil {
		return findings
	}

	accounted := map[string]bool{}
	for _, src := range sources {
		doc := DeriveFlowMMD(src.Source, indexDoc)
		for _, e := range doc.Edges {
			sourceScreen := flowNodeScreen(doc, e.Source)
			if sourceScreen == "" {
				continue
			}
			accounted[flowEdgeKey(sourceScreen, e.Trigger, e.Target)] = true
		}
	}

	for _, entry := range indexDoc.Screens {
		for _, nav := range entry.Nav {
			if nav.To == entry.Stem {
				// Self-navigation is in-screen state (a section switch), not
				// a graph edge — the same suppression DeriveFlowMMD applies,
				// so the cross-check never demands a flow step for it.
				continue
			}
			if accounted[flowEdgeKey(entry.Stem, nav.Trigger, nav.To)] {
				continue
			}
			findings = append(findings, Finding{
				File:     ScreenRelPath(entry.Stem),
				Rule:     ruleFlowOffPathNav,
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("data-nav %q from screen %q is not accounted for by any flow source (off-path navigation — often intentional, e.g. a global nav); add a step for it or leave it as a deliberate off-path edge", nav.Trigger+" -> "+nav.To, entry.Stem),
			})
		}
	}
	return findings
}

// flowNodeScreen returns the screen stem a derived node stands for.
func flowNodeScreen(doc *FlowExportDoc, id string) string {
	for _, n := range doc.Nodes {
		if n.ID == id {
			return n.Screen
		}
	}
	// An off-path edge from a STEP screen renders with the stem as its node
	// id; no node is declared for it (the step nodes already stand for the
	// screen), so the id itself is the screen.
	return id
}

// flowEdgeKey is the dedup key for the off-path cross-check.
func flowEdgeKey(fromScreen, trigger, toScreen string) string {
	return fromScreen + "\x00" + trigger + "\x00" + toScreen
}
