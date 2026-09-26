package design

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Design↔code sync, analyze half (SP-140-5 §5b).
//
// This file is the pure analysis core of the `design_sync` tool: it takes the
// touched UI code files (their paths and current bytes) plus the design/ tree
// and returns a structured *sync report* — the semantic deltas a dev turn
// introduced, each tagged with the design files it would touch, a confidence,
// and the basis that produced it:
//
//   - literal    — a token variable was renamed or revalued in code, mapping
//     1:1 to a DTCG entry. Safe to apply mechanically.
//   - structural — a new route/screen appeared in a router file, a component
//     was added under an existing screen, or a nav target moved. Maps to
//     wireframe and/or flow changes.
//   - inferred   — styling exists with no token counterpart (raw hex, magic
//     spacing). A *proposal*: a new token, or a switch to an existing one.
//     Never safe to auto-apply.
//
// The v1 contract is deliberately shallow (SP-140-5 §5b, non-goals): the
// analysis works from file diffs and the convention vocabulary (CSS custom
// properties, Tailwind classes, router files, component file names) rather
// than parsing a source tree. There is no AST here and no code is regenerated.
//
// Everything is derived from a deterministic signature: the same touched file
// set, contents, and design tree must produce the same report, byte for byte,
// on every run. Inputs are sorted and the deltas are ordered by a canonical
// key (see CompareDeltas), never by map iteration order.
//
// The report shape is the contract the 5.4 apply half consumes:
//
//   - Mode records that this was an analyze run ("" when not set), so a caller
//     holding a stored report can tell where it came from.
//   - Deltas carries the ordered semantic deltas. Apply mode walks exactly
//     this slice and writes the literal/structural subset.
//   - Reports on each delta the design files it would touch, which is the
//     write set apply must confine to design/ (§5e: apply never rewrites the
//     implementation).
//   - Token deltas name the DTCG file and entry; structural deltas name the
//     proposed wireframe stem and/or flow edge; inferred deltas carry
//     Proposal=true (and Proposed/candidate fields), so apply can skip them.
type deltaBasis string

// DeltaBasis is the basis of one semantic delta (SP-140-5 §5b). It governs
// automation: literal and structural deltas are the safe-to-apply subset;
// inferred deltas are proposals.
type DeltaBasis = deltaBasis

// Basis values, fixed by SP-140-5 §5b.
const (
	// DeltaBasisLiteral: a token variable was renamed/revalued in code and
	// maps 1:1 to a DTCG entry. Safe to apply mechanically.
	DeltaBasisLiteral deltaBasis = "literal"
	// DeltaBasisStructural: a new route/screen/component/nav target —
	// structure, not value. Maps to wireframe/flow changes.
	DeltaBasisStructural deltaBasis = "structural"
	// DeltaBasisInferred: styling with no token counterpart. A proposal.
	DeltaBasisInferred deltaBasis = "inferred"
)

// DeltaKind is the design-tier class a delta belongs to (SP-140-5 §5b:
// token|wireframe|flow|feedback).
type DeltaKind = string

// Delta kinds, fixed by SP-140-5 §5b.
const (
	DeltaKindToken     DeltaKind = "token"
	DeltaKindWireframe DeltaKind = "wireframe"
	DeltaKindFlow      DeltaKind = "flow"
	DeltaKindFeedback  DeltaKind = "feedback"
)

// Confidence is a coarse automation gate on a delta. Literal and structural
// deltas are safe to apply; inferred deltas are proposals and carry a lower
// confidence.
type Confidence = string

// Confidence values. High means "safe to apply mechanically", medium means
// "mapped to a known design artifact with reasonable certainty", low means
// "a proposal a human or the agent should confirm".
const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// FlowDraftStatus is the status a §5b-created wireframe/flow edge carries:
// draft, so the next design turn reviews it rather than trusting it.
const FlowDraftStatus = "draft"

// SyncReport is the structured result of one `design_sync` analyze run
// (SP-140-5 §5b). It is deliberately the shape apply mode consumes: Deltas is
// the ordered work list, each delta naming the design files it would touch.
type SyncReport struct {
	// Mode is the mode the report was produced in ("" for a bare analyze
	// call; apply mode is item 5.4). It is advisory metadata for a stored
	// report.
	Mode string `json:"mode,omitempty"`
	// TokensPath / WireframeDir / FlowsDir are the design-tier directories the
	// run cross-referenced, slash-separated and workspace-relative. They are
	// always present so the report reads as a design-tree lookup rather than a
	// bag of deltas.
	TokensPath   string `json:"tokensPath"`
	WireframeDir string `json:"wireframesPath"`
	FlowsDir     string `json:"flowsPath"`
	// TouchedCount is the number of touched code files analysed.
	TouchedCount int `json:"touchedCount"`
	// DeltaCount is the number of deltas; also broken out by Basis and Kind
	// so a model reads the automation split without counting.
	DeltaCount int            `json:"deltaCount"`
	ByBasis    map[string]int `json:"byBasis"`
	ByKind     map[string]int `json:"byKind"`
	// Deltas is the ordered semantic-delta work list (deterministic).
	Deltas []SyncDelta `json:"deltas"`
	// WritesNothing states the §5e invariant explicitly: analyze mode writes
	// no files, and apply (5.4) is confined to design/. Always true here.
	WritesNothing bool `json:"writesNothing"`
	// NextStep is a one-line pointer to the apply half / the skill loop, so a
	// model knows analyze is the first half.
	NextStep string `json:"nextStep"`
	// Skipped are touched paths the analysis could not read (missing on disk,
	// a directory, or not a recognised UI file). Advisory.
	Skipped []string `json:"skipped,omitempty"`
	// Notes carries human-readable caveats (e.g. "no design/ tree, so
	// literal deltas have no DTCG counterpart"). Deterministic order.
	Notes []string `json:"notes,omitempty"`
}

// SyncDelta is one detected semantic delta (SP-140-5 §5b). Delta is the
// human/agent-readable description; Kind/Basis/Confidence govern what apply
// may do with it; DesignFiles names the design/ paths it would touch.
type SyncDelta struct {
	// Delta is the one-line description of the detected change.
	Delta string `json:"delta"`
	// Kind is token|wireframe|flow|feedback.
	Kind DeltaKind `json:"kind"`
	// Basis is literal|structural|inferred.
	Basis deltaBasis `json:"basis"`
	// Confidence is high|medium|low.
	Confidence Confidence `json:"confidence"`
	// DesignFiles are the design/ paths this delta would touch, sorted and
	// slash-separated, workspace-relative. Always non-nil (possibly empty) so
	// the JSON shape is stable; empty means "the semantic layer has no
	// counterpart yet" (apply's job to create one, subject to basis).
	DesignFiles []string `json:"designFiles"`
	// SafeToApply is the automation gate: true for literal and structural
	// deltas, false for inferred (and for any delta whose basis could not be
	// established). Redundant with Basis but explicit for a model reading the
	// report.
	SafeToApply bool `json:"safeToApply"`
	// Proposal marks an inferred delta: a proposal (new token, or switch to an
	// existing one), never auto-applied.
	Proposal bool `json:"proposal,omitempty"`
	// ProposalKind is new-token|switch-to-existing for an inferred delta.
	ProposalKind string `json:"proposalKind,omitempty"`
	// Proposed is the proposed new token path (inferred, new token) or the
	// design/ path proposal ("" otherwise).
	Proposed string `json:"proposed,omitempty"`
	// Candidate is the existing DTCG token path an inferred delta could switch
	// to, when one matches the literal value ("" otherwise).
	Candidate string `json:"candidate,omitempty"`
	// Token is the DTCG token path a token delta concerns ("" otherwise).
	Token string `json:"token,omitempty"`
	// TokenFile is the design/tokens/*.tokens.json the token lives in
	// ("" otherwise), from the token export machinery.
	TokenFile string `json:"tokenFile,omitempty"`
	// TokenEntry is the DTCG entry key as it appears in TokenFile: the
	// dotted token path for new/revalued entries, or the pre-change alias
	// target for a literal rename ("" otherwise).
	TokenEntry string `json:"tokenEntry,omitempty"`
	// WireframeStem is the proposed wireframe stem for a structural delta
	// ("" otherwise).
	WireframeStem string `json:"wireframeStem,omitempty"`
	// FlowEdge is the proposed flow edge ("login --> home") for a structural
	// delta ("" otherwise).
	FlowEdge string `json:"flowEdge,omitempty"`
	// FlowFile is the design/flows/*.mmd the edge would be added to
	// ("" otherwise).
	FlowFile string `json:"flowFile,omitempty"`
	// Status is the status a created artifact would carry (draft for
	// §5b-created wireframes/edges).
	Status string `json:"status,omitempty"`
	// CodeFiles are the touched code files this delta was detected in, sorted
	// and slash-separated.
	CodeFiles []string `json:"codeFiles"`
	// Evidence is the offending code snippet/line, trimmed (best-effort).
	Evidence string `json:"evidence,omitempty"`
}

// SyncInput is the analyze-mode input: the touched code files and their
// current contents, plus the workspace root so the design/ tree can be read.
type SyncInput struct {
	// Root is the workspace root (the parent of design/).
	Root string
	// Touched are the code files to analyse. Paths may be absolute or
	// workspace-relative; Contents maps each path (as given) to its bytes.
	// A path with no content entry is skipped (advisory, named in Skipped).
	Touched []SyncFileInput
}

// SyncFileInput is one touched code file: its path as the caller named it and
// its current content.
type SyncFileInput struct {
	Path    string
	Content []byte
}

// AnalyzeTouchedFiles is the pure analyze core (no I/O beyond the design/ tree
// read): given the touched code files and the workspace root, it returns the
// §5b sync report. It never writes anything.
//
// The analysis is convention-based, in three passes over the touched files:
//
//	literal    — CSS custom properties and token references, cross-referenced
//	             against the DTCG export projection.
//	structural — router routes/screens, component file names, and nav targets.
//	inferred   — raw hex colours and magic spacing values with no token
//	             counterpart, emitted as proposals.
//
// The result is deterministic: touched paths and every emitted slice are
// sorted, and the delta order is fixed by CompareDeltas.
func AnalyzeTouchedFiles(in SyncInput) (*SyncReport, error) {
	root := in.Root
	if root == "" {
		root = "."
	}

	report := &SyncReport{
		TokensPath:    path.Join(DirName, TokenSubdir),
		WireframeDir:  path.Join(DirName, "wireframes"),
		FlowsDir:      path.Join(DirName, FlowSubdir),
		ByBasis:       map[string]int{},
		ByKind:        map[string]int{},
		Deltas:        []SyncDelta{},
		WritesNothing: true,
		NextStep: "Analyze is read-only. Run design_sync with mode=apply to write the " +
			"safe subset (literal token renames/revalues, wireframe/flow additions) into design/; " +
			"inferred deltas stay proposals.",
	}

	// Normalise + sort the touched set so the report is order-independent.
	touched := normaliseTouched(root, in.Touched)
	report.TouchedCount = len(touched.files)
	report.Skipped = touched.skipped

	// The design tree: what exists, what tokens exist. Both are best-effort —
	// a workspace with no design/ yields an empty context and the report says
	// so rather than failing (the tool's job is to report, not to gate).
	tree := loadSyncTree(root)
	if !tree.exists {
		report.Notes = append(report.Notes,
			"No design/ tree found — literal and structural deltas have no semantic counterpart yet; "+
				"scaffold design/ (design-system skill) and re-run, or apply will create the missing artifacts.")
	}

	var deltas []SyncDelta
	for _, f := range touched.files {
		deltas = append(deltas, analyzeCSSLiterals(f, tree)...)
		deltas = append(deltas, analyzeStructural(f, tree)...)
		deltas = append(deltas, analyzeInferred(f, tree)...)
	}

	// Coalesce, dedup, and order deterministically.
	deltas = sortAndDedupDeltas(deltas)
	report.Deltas = deltas
	report.DeltaCount = len(deltas)
	for _, d := range deltas {
		report.ByBasis[string(d.Basis)]++
		report.ByKind[d.Kind]++
	}
	return report, nil
}

// -----------------------------------------------------------------------------
// Touched-file normalisation
// -----------------------------------------------------------------------------

// touchedSet is the normalised, deduped, sorted touched-file set.
type touchedSet struct {
	files   []SyncFileInput
	skipped []string
}

// normaliseTouched trims, sorts, and dedups the touched set; the result is
// independent of the caller's argument order (the §5b determinism guarantee).
// Duplicate paths keep their first content. A path with no bytes that is not
// an existing directory entry is skipped (advisory).
func normaliseTouched(root string, in []SyncFileInput) touchedSet {
	seen := map[string]bool{}
	set := touchedSet{}
	for _, f := range in {
		rel := syncRelPath(root, f.Path)
		if rel == "" {
			continue
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if len(f.Content) == 0 {
			// A touched path with no content: skip it rather than reporting
			// phantom deltas from nothing.
			set.skipped = append(set.skipped, rel)
			continue
		}
		set.files = append(set.files, SyncFileInput{Path: rel, Content: f.Content})
	}
	sort.Slice(set.files, func(i, j int) bool { return set.files[i].Path < set.files[j].Path })
	sort.Strings(set.skipped)
	return set
}

// syncRelPath renders a touched path workspace-relative and slash-separated,
// so the report never leaks an absolute path. An already-relative path is
// cleaned; an absolute path outside root keeps its cleaned slash form (the
// handler's Gate-1 check is what refuses those, not this function).
func syncRelPath(root, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if filepath.IsAbs(p) {
		if rel, err := filepath.Rel(root, p); err == nil {
			clean = filepath.ToSlash(rel)
		}
	}
	clean = strings.TrimSuffix(clean, "/")
	if clean == "." || clean == "" {
		return ""
	}
	return clean
}

// -----------------------------------------------------------------------------
// Design-tree context
// -----------------------------------------------------------------------------

// syncTree is the read-only design-tree context the analysis cross-references:
// what exists and which DTCG tokens exist.
type syncTree struct {
	exists bool
	// tokensPath is the workspace-relative token tier (design/tokens), so a
	// proposal with no source file still points at the right directory.
	tokensPath string
	// tokenFiles are the token source files, workspace-relative, sorted — the
	// lookup set for naming the DTCG file a token lives in.
	tokenFiles []string
	// tokens is the DTCG export projection (sorted leaves), nil when the tree
	// is missing or has no tokens.
	tokens *TokenExport
	// byCSSVar maps a generated CSS custom property name (e.g.
	// "--color-brand-primary") to its DTCG token path.
	byCSSVar map[string]string
	// byValue maps a literal token value (lowercased) to the DTCG token paths
	// carrying it, sorted — the inferred "switch to an existing token"
	// candidate index.
	byValue map[string][]string
	// wireframeStems is the set of design/wireframes/*.svg stems.
	wireframeStems map[string]bool
	// flowFiles are the design/flows/*.mmd paths, sorted.
	flowFiles []string
	// flowEdges is the set of existing "source --> target" edges across the
	// flow files, for "nav target moved" detection.
	flowEdges map[string]bool
	// screenFiles are the design/screens/*.html paths, sorted.
	screenFiles []string
}

// loadSyncTree reads the design tree context. A missing tree yields
// {exists:false} plus empty indexes — never an error: the tool's job is to
// report drift, and "no design/ yet" is a reportable state, not a failure.
func loadSyncTree(root string) syncTree {
	tree := syncTree{
		tokensPath:     path.Join(DirName, TokenSubdir),
		byCSSVar:       map[string]string{},
		byValue:        map[string][]string{},
		wireframeStems: map[string]bool{},
		flowEdges:      map[string]bool{},
	}
	if !FileExists(root) {
		return tree
	}
	tree.exists = true

	// Token source files, workspace-relative and sorted, so token->file
	// naming is exact and deterministic.
	if matches, err := filepath.Glob(filepath.Join(root, DirName, TokenSubdir, "*.tokens.json")); err == nil {
		sort.Strings(matches)
		for _, m := range matches {
			if rel, relErr := filepath.Rel(root, m); relErr == nil {
				tree.tokenFiles = append(tree.tokenFiles, filepath.ToSlash(rel))
			}
		}
	}

	// Tokens: reuse the export projection so the CSS-var names and values the
	// report names are exactly the ones design_export_tokens generates.
	if tokens, err := ResolveExportTokens(root); err == nil && tokens != nil {
		tree.tokens = tokens
		for _, leaf := range tokens.Leaves {
			name := strings.TrimPrefix(cssVarName(leaf.Name), "--")
			tree.byCSSVar[name] = leaf.Name
			if v := strings.ToLower(strings.TrimSpace(exportStringValue(leaf))); v != "" {
				tree.byValue[v] = append(tree.byValue[v], leaf.Name)
			}
		}
		for v := range tree.byValue {
			sort.Strings(tree.byValue[v])
		}
	}

	// Wireframe stems.
	if matches, err := filepath.Glob(filepath.Join(root, DirName, "wireframes", "*.svg")); err == nil {
		for _, m := range matches {
			tree.wireframeStems[strings.TrimSuffix(path.Base(filepath.ToSlash(m)), ".svg")] = true
		}
	}
	// Flow files + their existing edges.
	if matches, err := filepath.Glob(filepath.Join(root, DirName, FlowSubdir, "*.mmd")); err == nil {
		sort.Strings(matches)
		for _, m := range matches {
			rel, relErr := filepath.Rel(root, m)
			if relErr != nil {
				continue
			}
			tree.flowFiles = append(tree.flowFiles, filepath.ToSlash(rel))
			if data, readErr := os.ReadFile(m); readErr == nil {
				fc := ParseFlowchart(string(data))
				for _, e := range fc.Edges {
					tree.flowEdges[e.Source+" --> "+e.Target] = true
				}
			}
		}
	}
	// Screen files.
	if matches, err := filepath.Glob(filepath.Join(root, DirName, "screens", "*.html")); err == nil {
		sort.Strings(matches)
		for _, m := range matches {
			if rel, relErr := filepath.Rel(root, m); relErr == nil {
				tree.screenFiles = append(tree.screenFiles, filepath.ToSlash(rel))
			}
		}
	}
	return tree
}
