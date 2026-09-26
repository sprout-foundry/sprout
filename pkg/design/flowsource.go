package design

// SP-140-9 §9b: the structured flow source. One design/flows/<name>.json per
// process, human-authored, is the only flow truth: a linear, ordered step
// list whose steps name screens and the trigger that moves to the next step.
// The .mmd beside it is a derived export (flowsource_mmd.go) — deterministic,
// provenance-hashed over these bytes plus the relevant screens' data-nav
// bytes, never hand-edited.
//
// v1 schema is exactly the spec's example — {"name", "steps":[{id, label,
// screen, trigger?, next?}]} — parsed strictly (unknown fields are errors, so
// a schema extension cannot silently pass). Condition and parallel branches
// are deferred: they are documented in the roadmap note, not invented here.
//
// This file is the pure half (schema, parse, validate, hash inputs); the
// filesystem-facing generator and the drift validator live in
// flowsource_mmd.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// flowSourceRule names the rule id every flow-source (.json) finding carries,
// so design_validate and the export refusal share one spelling.
const ruleFlowSourceSchema = "flow_source_schema"

// layoutSidecarSuffix marks the §7d flow layout sidecar (per-node drag
// positions, <name>.layout.json). It is derived canvas state, not a flow
// source, and every flows/*.json rule must skip it.
const layoutSidecarSuffix = ".layout.json"

// FlowSource is the parsed v1 flow source document.
type FlowSource struct {
	// Name is the flow's slug; it must equal the document's file stem
	// (design/flows/sign-up.json names itself "sign-up"), the same
	// identity-by-location rule the rest of the tree uses.
	Name string `json:"name"`
	// Steps are the process steps in walk order. The first step is the
	// entry; a step with an empty Next is terminal.
	Steps []FlowStep `json:"steps"`
}

// FlowStep is one step of a FlowSource.
type FlowStep struct {
	// ID is the step's stable node id (§9c): the derived .mmd and the layout
	// sidecar key on it, and a regeneration must not change it.
	ID string `json:"id"`
	// Label is the human step name rendered as the node label.
	Label string `json:"label"`
	// Screen is the stem of the screen the step happens on. It resolves to
	// a screen stem or — during the §9a migration — a wireframe stem.
	Screen string `json:"screen"`
	// Trigger is the label on the edge to the next step; "" on a terminal
	// step (and rendered as a plain unlabeled edge when Next is set without
	// a trigger).
	Trigger string `json:"trigger,omitempty"`
	// Next is the id of the following step; "" marks the step terminal.
	Next string `json:"next,omitempty"`
}

// ParseFlowSource parses one flow source document strictly: it must be a
// JSON object with exactly the v1 fields, a slug name, and at least one step.
// Structural problems (bad JSON, unknown fields, empty required fields) come
// back as a single error whose message names the fault — the validator
// renders it as a finding; the export refuses on it. Name-vs-filename is the
// caller's check (it owns the path).
func ParseFlowSource(relPath string, content []byte) (*FlowSource, error) {
	fault := func(format string, args ...any) error {
		return fmt.Errorf("%s: %s", relPath, fmt.Sprintf(format, args...))
	}
	dec := json.NewDecoder(strings.NewReader(string(content)))
	dec.DisallowUnknownFields()
	var src FlowSource
	if err := dec.Decode(&src); err != nil {
		return nil, fault("invalid flow source document (%v); the v1 schema is {\"name\", \"steps\":[{id, label, screen, trigger?, next?}]} (SP-140-9 §9b)", err)
	}
	if src.Name == "" {
		return nil, fault(`"name" is required`)
	}
	if len(src.Steps) == 0 {
		return nil, fault(`"steps" must carry at least one step`)
	}
	for i, step := range src.Steps {
		where := fmt.Sprintf("steps[%d]", i)
		switch {
		case step.ID == "":
			return nil, fault("%s: \"id\" is required", where)
		case step.Label == "":
			return nil, fault("%s (id %q): \"label\" is required", where, step.ID)
		case step.Screen == "":
			return nil, fault("%s (id %q): \"screen\" is required", where, step.ID)
		}
	}
	return &src, nil
}

// ValidateFlowSource validates one flow source document (SP-140-9 §9b).
// relPath is the workspace-relative slash path; stems is the transitional
// screen-stem union from 9.1 (wireframe OR screen stems) that step screens
// resolve against. Hard checks (SeverityError): the document parses strictly,
// the name is a slug matching the file stem, step ids are unique, every
// non-empty next targets an existing step, a step is terminal exactly when
// its next is empty, and every screen resolves to a known stem. The result is
// never nil and sorted.
func ValidateFlowSource(relPath string, content []byte, stems []string) []Finding {
	findings := []Finding{}
	fail := func(line int, format string, args ...any) {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     line,
			Rule:     ruleFlowSourceSchema,
			Severity: SeverityError,
			Message:  fmt.Sprintf(format, args...),
		})
	}

	src, err := ParseFlowSource(relPath, content)
	if err != nil {
		fail(1, "%s; fix the flow source before exporting (design_export_tokens targets:flows)", err)
		return finalizeFlowFindings(findings)
	}

	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".json")
	if stem != src.Name {
		fail(1, "flow source name %q must equal its file stem %q (design/flows/%s.json)", src.Name, stem, stem)
	}
	if !flowSlugRe.MatchString(src.Name) {
		fail(1, "flow source name %q is not a slug (%s)", src.Name, SlugPattern)
	}

	stemSet := make(map[string]struct{}, len(stems))
	for _, s := range stems {
		stemSet[s] = struct{}{}
	}

	seen := make(map[string]bool, len(src.Steps))
	for _, step := range src.Steps {
		if seen[step.ID] {
			fail(1, "duplicate step id %q; step ids are the §9c node ids and must be unique", step.ID)
		}
		seen[step.ID] = true
	}
	// Second pass: forward references are legal (a step may name its target
	// before it appears), so `next` is checked once every id is known. A
	// self-loop is allowed.
	for _, step := range src.Steps {
		if step.Next != "" && !seen[step.Next] {
			fail(1, "step %q next %q does not target a step in this flow", step.ID, step.Next)
		}
		if _, ok := stemSet[step.Screen]; !ok {
			fail(1, "step %q screen %q resolves to neither a screen stem (design/screens/%s.html) nor a wireframe stem (design/wireframes/%s.svg)", step.ID, step.Screen, step.Screen, step.Screen)
		}
	}

	return finalizeFlowFindings(findings)
}

// flowSlugRe applies the tree's slug rule to a flow name.
var flowSlugRe = regexp.MustCompile(SlugPattern)

// FlowSourceFile is one flow source document as read from disk: its
// workspace-relative slash path, its stem (the flow name), its exact bytes,
// and the parsed source. Produced by ReadFlowSources.
type FlowSourceFile struct {
	RelPath string
	Name    string
	Raw     []byte
	Source  *FlowSource
}

// FlowSourceRelPath returns the canonical flow source path for a flow name:
// design/flows/<name>.json (SP-140-9 §9b).
func FlowSourceRelPath(name string) string {
	return path.Join(DirName, FlowSubdir, name+".json")
}

// flowMMDRelPath returns the canonical derived export path for a flow name:
// design/flows/<name>.mmd (SP-140-9 §9b).
func flowMMDRelPath(name string) string {
	return path.Join(DirName, FlowSubdir, name+".mmd")
}

// FlowSourcePaths globs the flow source documents under root
// (design/flows/*.json, the §7d .layout.json sidecars excluded), in sorted
// order. A missing or empty flows directory yields an empty slice.
func FlowSourcePaths(root string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(root, DirName, FlowSubdir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", filepath.Join(DirName, FlowSubdir, "*.json"), err)
	}
	sort.Strings(matches)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if strings.HasSuffix(match, layoutSidecarSuffix) {
			continue
		}
		out = append(out, match)
	}
	return out, nil
}

// hasFlowSources reports whether the tree carries at least one flow source
// document. The §9b off-path cross-check is scoped to .json-bearing trees so
// legacy .mmd-only trees do not newly warn.
func hasFlowSources(root string) bool {
	paths, err := FlowSourcePaths(root)
	return err == nil && len(paths) > 0
}

// ReadFlowSources reads and parses every flow source under root, sorted by
// flow name. A malformed document is an error naming the file — the export
// refuses on it; the validator reports it as a finding instead (via
// ValidateFlowSource) and never needs this strictness.
func ReadFlowSources(root string) ([]FlowSourceFile, error) {
	paths, err := FlowSourcePaths(root)
	if err != nil {
		return nil, err
	}
	out := make([]FlowSourceFile, 0, len(paths))
	for _, p := range paths {
		raw, err := readFileForHash(p)
		if err != nil {
			return nil, err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = path.Base(filepath.ToSlash(p))
		}
		src, err := ParseFlowSource(filepath.ToSlash(rel), raw)
		if err != nil {
			return nil, err
		}
		out = append(out, FlowSourceFile{
			RelPath: filepath.ToSlash(rel),
			Name:    src.Name,
			Raw:     raw,
			Source:  src,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// readFileForHash reads one file's exact bytes for hashing; the label only
// decorates the error.
func readFileForHash(p string) ([]byte, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filepath.ToSlash(p), err)
	}
	return data, nil
}
