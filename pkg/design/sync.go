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

// Design↔code sync, analyze half (SP-140-5 §5b, TODO item 5.3).
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

// cssVarHostExts are the code file extensions scanned for CSS custom
// properties and literal styling. Extension-based classification is the v1
// convention (SP-140-5 §5b): no parsing, no AST.
var cssVarHostExts = map[string]bool{
	".css": true, ".scss": true, ".sass": true, ".less": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".html": true, ".htm": true, ".svg": true, ".vue": true, ".svelte": true,
}

// isCSSVarHost reports whether a path is scanned for CSS variables/literals.
func isCSSVarHost(p string) bool {
	return cssVarHostExts[strings.ToLower(path.Ext(p))]
}

// routerFileRe matches the v1 router-file convention: any path segment
// containing "rout" (routes.ts, router.tsx, app-router/, ...).
var routerFileRe = regexp.MustCompile(`(?i)rout`)

// isRouterFile reports whether a path is a router file by the v1 convention.
func isRouterFile(p string) bool {
	base := path.Base(p)
	// A directory segment counts too (routes/login.tsx, router/index.ts).
	return routerFileRe.MatchString(p) && !strings.HasSuffix(base, ".css")
}

// -----------------------------------------------------------------------------
// Passthrough: literal deltas — CSS custom properties / token references
// -----------------------------------------------------------------------------

// cssVarDeclRe captures a CSS custom property declaration: `--name: value`.
// Submatch 1 = the name without the leading `--`, 2 = the value up to the
// statement terminator. It is deliberately lenient (no full CSS grammar in
// v1) and matches inside TS/TSX template strings and style objects too, which
// is exactly the point: the vocabulary is the convention.
var cssVarDeclRe = regexp.MustCompile(`--([A-Za-z0-9_-]+)\s*:\s*([^;}\n]+)`)

// cssVarRefRe captures a CSS custom property reference: `var(--name)`.
var cssVarRefRe = regexp.MustCompile(`var\(\s*--([A-Za-z0-9_-]+)\s*\)`)

// tokenRefRe captures a DTCG `{group.token}` reference in code (a comment or
// string), the same dot-path form the wireframe token-comment convention
// uses.
var tokenRefRe = regexp.MustCompile(`\{\s*([A-Za-z][A-Za-z0-9]*(?:\.[A-Za-z0-9_-]+)+)\s*\}`)

// analyzeCSSLiterals detects literal deltas: CSS custom property
// declarations/references that map to a DTCG token entry, and DTCG token
// references the code consumes. A declaration whose value differs from the
// token's current value is a *revalue*; a reference with no token counterpart
// is reported so the semantic layer can adopt it.
func analyzeCSSLiterals(f SyncFileInput, tree syncTree) []SyncDelta {
	if !isCSSVarHost(f.Path) {
		return nil
	}
	content := string(f.Content)
	var deltas []SyncDelta

	// Declarations: `--name: value`.
	for _, m := range cssVarDeclRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		value := strings.TrimSpace(content[m[4]:m[5]])
		line := strings.Count(content[:m[0]], "\n") + 1

		tokenPath, ok := tree.byCSSVar[name]
		if !ok {
			continue // declared var with no DTCG counterpart: inferred territory
		}
		designFiles, tokFile, tokEntry := tokenDesignFiles(tree, tokenPath)
		current := currentTokenValue(tree, tokenPath)
		d := SyncDelta{
			Kind:        DeltaKindToken,
			Basis:       DeltaBasisLiteral,
			Confidence:  ConfidenceHigh,
			DesignFiles: designFiles,
			SafeToApply: true,
			Token:       tokenPath,
			TokenFile:   tokFile,
			TokenEntry:  tokEntry,
			CodeFiles:   []string{f.Path},
			Evidence:    fmt.Sprintf("--%s: %s (line %d)", name, value, line),
		}
		if current != "" && !strings.EqualFold(current, value) {
			d.Delta = fmt.Sprintf("token %s revalued in code: --%s is %q but the DTCG entry is %q",
				tokenPath, name, value, current)
		} else {
			d.Delta = fmt.Sprintf("token %s declared in code as --%s: %s (matches the DTCG entry)",
				tokenPath, name, value)
		}
		deltas = append(deltas, d)
	}

	// References: `var(--name)` to a var with no declaration in this file and
	// no DTCG counterpart — the code consumes a variable the semantic layer
	// does not know about. (Declarations above cover the known ones.)
	for _, m := range cssVarRefRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		if _, known := tree.byCSSVar[name]; known {
			continue
		}
		if strings.Contains(content, "--"+name+":") {
			continue // declared in this very file; the declaration path owns it
		}
		line := strings.Count(content[:m[0]], "\n") + 1
		deltas = append(deltas, SyncDelta{
			Delta: fmt.Sprintf("code references --%s, which has no DTCG token counterpart; "+
				"add the token to design/tokens/ so export and sync agree", name),
			Kind:         DeltaKindToken,
			Basis:        DeltaBasisInferred,
			Confidence:   ConfidenceLow,
			DesignFiles:  []string{},
			SafeToApply:  false,
			Proposal:     true,
			ProposalKind: "new-token",
			Proposed:     tokenPathFromCSSVar(name),
			CodeFiles:    []string{f.Path},
			Evidence:     fmt.Sprintf("var(--%s) (line %d)", name, line),
		})
	}

	// DTCG references in code: `{color.brand.primary}`.
	for _, m := range tokenRefRe.FindAllStringSubmatchIndex(content, -1) {
		tokenPath := content[m[2]:m[3]]
		if tree.tokens == nil {
			continue // no tokens to cross-reference
		}
		if !tokenExists(tree, tokenPath) {
			line := strings.Count(content[:m[0]], "\n") + 1
			deltas = append(deltas, SyncDelta{
				Delta: fmt.Sprintf("code references token {%s}, which is not in design/tokens/; "+
					"add it (or fix the reference)", tokenPath),
				Kind:        DeltaKindToken,
				Basis:       DeltaBasisStructural,
				Confidence:  ConfidenceMedium,
				DesignFiles: []string{tree.tokensPath},
				SafeToApply: true,
				Token:       tokenPath,
				TokenEntry:  tokenPath,
				CodeFiles:   []string{f.Path},
				Evidence:    fmt.Sprintf("{%s} (line %d)", tokenPath, line),
			})
		}
	}
	return deltas
}

// tokenPathFromCSSVar converts a CSS variable name back to a DTCG-ish dotted
// token path: `color-brand-primary` → `color.brand.primary`. Best-effort (the
// inverse of cssVarStem is lossy), used only to *propose* a new token name.
func tokenPathFromCSSVar(name string) string {
	segs := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	if len(segs) == 0 {
		return name
	}
	return strings.Join(segs, ".")
}

// tokenDesignFiles returns the design files a token delta would touch: the
// token source file, plus the generated artifacts design_export_tokens owns
// (§5f: a token change means the theme must be regenerated). tokFile/tokEntry
// name the DTCG file and entry for the report.
func tokenDesignFiles(tree syncTree, tokenPath string) (designFiles []string, tokFile, tokEntry string) {
	files := []string{}
	if tree.tokens != nil {
		if f := tokenSourceFile(tree, tokenPath); f != "" {
			tokFile = f
			files = append(files, f)
			tokEntry = tokenPath
		}
	}
	if len(files) == 0 {
		// No tokens in the tree: point at the token tier so apply knows where
		// a new entry would go.
		files = append(files, tree.tokensPath)
		tokEntry = tokenPath
	}
	// The generated artifacts follow from the token §5a machinery.
	for _, name := range []string{"tokens.css", "tokens.ts", "tailwind.theme.css", "tokens.swift", "tokens.kt"} {
		files = append(files, path.Join(DirName, GeneratedSubdir, name))
	}
	return files, tokFile, tokEntry
}

// tokenSourceFile names the token source file a token path lives in. The DTCG
// file per tier is conventional (color.tokens.json for the color group), so
// the group name maps to `<group>.tokens.json`; when a file with that name
// exists it is preferred, and when no group-scoped file exists the first token
// file in canonical order is used so the delta still names a real source.
func tokenSourceFile(tree syncTree, tokenPath string) string {
	group := topGroup(tokenPath)
	if group == "" {
		return ""
	}
	conventional := path.Join(DirName, TokenSubdir, group+".tokens.json")
	for _, f := range tree.tokenFiles {
		if f == conventional {
			return f
		}
	}
	// The group has no tier file of its own (a project tier may hold it):
	// fall back to the first token file in canonical order, so the report
	// names a real source rather than an invented one.
	if len(tree.tokenFiles) > 0 {
		return tree.tokenFiles[0]
	}
	return conventional
}

// currentTokenValue returns the current rendered value of a DTCG token, "" when
// unknown.
func currentTokenValue(tree syncTree, tokenPath string) string {
	if tree.tokens == nil {
		return ""
	}
	for _, leaf := range tree.tokens.Leaves {
		if leaf.Name == tokenPath {
			return exportStringValue(leaf)
		}
	}
	return ""
}

// tokenExists reports whether a dotted token path resolves to a leaf.
func tokenExists(tree syncTree, tokenPath string) bool {
	if tree.tokens == nil {
		return false
	}
	for _, leaf := range tree.tokens.Leaves {
		if leaf.Name == tokenPath {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Structural deltas — routes/screens, components, nav targets
// -----------------------------------------------------------------------------

// routeObjectRe captures an object literal with both a path and an element or
// component, the shape every JS router uses:
//
//	{ path: "/login", element: <Login /> }
//	{ path: '/login', component: Login }
var routeObjectRe = regexp.MustCompile(`\{[^{}]*?(?i:path)\s*:\s*["'](/[a-z0-9][a-z0-9/_-]*)["'][^{}]*\}`)

// componentNameRe captures a JSX component identifier (`<Login />`,
// `element: <Login/>`).
var componentNameRe = regexp.MustCompile(`<\s*([A-Z][A-Za-z0-9]*)\b`)

// screenStemFromRoute derives a wireframe-stem-shaped name from a route path:
// "/check-deposit" → "check-deposit", "/settings/profile" → "settings-profile".
// It returns "" for the root route (a route of "/" names no screen of its own).
func screenStemFromRoute(route string) string {
	trimmed := strings.Trim(route, "/")
	if trimmed == "" {
		return ""
	}
	// A route parameter ("/users/:id") or catch-all ("/files/*") names no
	// screen: there is no single wireframe for it. Check before sanitising,
	// since sanitising strips the ':' that marks the parameter.
	if strings.ContainsAny(trimmed, ":*") {
		return ""
	}
	segments := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '/' })
	for i, s := range segments {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
				return r
			case r == '_' || r == ' ':
				return '-'
			default:
				return -1
			}
		}, s)
		segments[i] = s
	}
	joined := strings.Join(segments, "-")
	if joined == "" {
		return ""
	}
	if !SlugMatches(joined) {
		return ""
	}
	return joined
}

// SlugMatches reports whether a name satisfies the shared slug rule
// (SlugPattern, SP-140-1). It is exported so the sync handler/tests can assert
// a proposed stem is a legal wireframe name without duplicating the regexp.
func SlugMatches(name string) bool {
	ok, err := regexp.MatchString(SlugPattern, name)
	return err == nil && ok
}

// deliveredScreen returns the design/screens/<stem>.html path when a hi-fi
// screen has already been delivered for a stem, "" otherwise. It is the
// structural pass's "is the design tree already ahead here?" check: when a
// screen exists but no wireframe does, the proposal is to backfill the
// wireframe, and the delta names the delivered screen as adoption context.
func deliveredScreen(tree syncTree, stem string) string {
	if stem == "" {
		return ""
	}
	target := path.Join(DirName, "screens", stem+".html")
	for _, f := range tree.screenFiles {
		if f == target {
			return f
		}
	}
	return ""
}

// analyzeStructural detects structural deltas in one touched file: a new
// route/screen in a router file, and nav targets whose wireframe counterpart
// is missing.
func analyzeStructural(f SyncFileInput, tree syncTree) []SyncDelta {
	content := string(f.Content)
	var deltas []SyncDelta

	if isRouterFile(f.Path) {
		for _, m := range routeObjectRe.FindAllStringSubmatchIndex(content, -1) {
			block := content[m[0]:m[1]]
			route := content[m[2]:m[3]]
			stem := screenStemFromRoute(route)
			if stem == "" {
				continue
			}
			component := firstComponentName(block)
			if tree.wireframeStems[stem] {
				// The wireframe exists; the only structural question is the
				// nav edge, which the nav pass below covers. Skip.
				continue
			}
			line := strings.Count(content[:m[0]], "\n") + 1
			wireframePath := path.Join(DirName, "wireframes", stem+".svg")
			designFiles := []string{wireframePath}
			what := fmt.Sprintf("new route %q%s with no wireframe", route, componentSuffix(component))
			if screen := deliveredScreen(tree, stem); screen != "" {
				// The semantic layer is partly ahead: a hi-fi screen is
				// already delivered, so the proposal is to backfill the
				// wireframe the screen implies. The delivered screen is part
				// of the delta's design-file read set.
				designFiles = append(designFiles, screen)
				what = fmt.Sprintf("new route %q%s has a delivered screen %s but no wireframe",
					route, componentSuffix(component), screen)
			}
			d := SyncDelta{
				Delta:         fmt.Sprintf("%s; propose %s", what, wireframePath),
				Kind:          DeltaKindWireframe,
				Basis:         DeltaBasisStructural,
				Confidence:    ConfidenceMedium,
				DesignFiles:   designFiles,
				SafeToApply:   true,
				WireframeStem: stem,
				Status:        FlowDraftStatus,
				CodeFiles:     []string{f.Path},
				Evidence:      fmt.Sprintf("%s (line %d)", strings.TrimSpace(route), line),
			}
			deltas = append(deltas, d)

			// The flow edge that gets the screen into the graph. The source
			// is the route's parent segment when there is one, else the
			// screen is an entry point and the edge is proposed into the
			// first existing flow file (or a new one when none exists).
			edge, flowFile := proposeFlowEdge(stem, tree)
			if edge != "" {
				deltas = append(deltas, SyncDelta{
					Delta: fmt.Sprintf("new screen %q is not in any flow; propose edge %q in %s",
						stem, edge, flowFile),
					Kind:          DeltaKindFlow,
					Basis:         DeltaBasisStructural,
					Confidence:    ConfidenceMedium,
					DesignFiles:   []string{flowFile},
					SafeToApply:   true,
					WireframeStem: stem,
					FlowEdge:      edge,
					FlowFile:      flowFile,
					Status:        FlowDraftStatus,
					CodeFiles:     []string{f.Path},
					Evidence:      fmt.Sprintf("route %q", route),
				})
			}
		}
	}

	// Nav targets: `data-nav="login"` or `to="/login"` values that name a
	// screen whose wireframe is missing.
	for _, target := range navTargets(content) {
		stem := screenStemFromRoute("/" + strings.Trim(target, "/"))
		if stem == "" || tree.wireframeStems[stem] {
			continue
		}
		wireframePath := path.Join(DirName, "wireframes", stem+".svg")
		deltas = append(deltas, SyncDelta{
			Delta: fmt.Sprintf("nav target %q has no wireframe; propose %s",
				target, wireframePath),
			Kind:          DeltaKindWireframe,
			Basis:         DeltaBasisStructural,
			Confidence:    ConfidenceMedium,
			DesignFiles:   []string{wireframePath},
			SafeToApply:   true,
			WireframeStem: stem,
			Status:        FlowDraftStatus,
			CodeFiles:     []string{f.Path},
			Evidence:      target,
		})
	}
	return deltas
}

// firstComponentName returns the first JSX component identifier in a route
// object block, "" when none.
func firstComponentName(block string) string {
	if m := componentNameRe.FindStringSubmatch(block); m != nil {
		return m[1]
	}
	return ""
}

// componentSuffix renders " for component Login" (or "" when unknown) for a
// delta description.
func componentSuffix(component string) string {
	if component == "" {
		return ""
	}
	return " for component " + component
}

// navAttrRe captures a nav target attribute value: data-nav="login" or
// to="/login" (the React-router Link form).
var navAttrRe = regexp.MustCompile(`(?i)\b(?:data-nav|to|href)\s*=\s*["'](/[A-Za-z0-9/_-]*|[a-z0-9][a-z0-9_-]*)["']`)

// navTargets extracts the nav target values in a file, deduped and sorted.
// Route-absolute values ("/login") and bare stems ("login") both qualify;
// external hrefs ("https://…") and anchors ("#…") do not.
func navTargets(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range navAttrRe.FindAllStringSubmatch(content, -1) {
		v := strings.TrimSpace(m[1])
		if v == "" || strings.HasPrefix(v, "//") || strings.Contains(v, "://") {
			continue
		}
		if strings.HasPrefix(v, "#") || strings.HasPrefix(v, ".") {
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// proposeFlowEdge proposes a flow edge for a newly detected screen. The source
// is derived from the screen's name when it has a parent segment
// ("settings-profile" → settings → profile); otherwise the edge is proposed
// from an existing wireframe that links to it (the code's nav targets), and
// failing that from the first existing wireframe stem so the graph stays
// connected. It returns ("", "") when no edge can be proposed (no wireframes
// and no flow file to host it).
func proposeFlowEdge(stem string, tree syncTree) (edge, flowFile string) {
	flowFile = ""
	if len(tree.flowFiles) > 0 {
		flowFile = tree.flowFiles[0]
	} else {
		flowFile = path.Join(DirName, FlowSubdir, stem+".mmd")
	}

	target := stem
	source := ""
	// Prefer an explicit parent segment in a compound stem.
	if i := strings.LastIndex(stem, "-"); i > 0 {
		candidate := stem[:i]
		if tree.wireframeStems[candidate] {
			source = candidate
		}
	}
	if source == "" {
		// Fall back to any existing wireframe (sorted, for determinism).
		stems := make([]string, 0, len(tree.wireframeStems))
		for s := range tree.wireframeStems {
			stems = append(stems, s)
		}
		sort.Strings(stems)
		if len(stems) > 0 {
			source = stems[0]
		}
	}
	if source == "" || source == target {
		return "", ""
	}
	edge = source + " --> " + target
	if tree.flowEdges[edge] {
		return "", ""
	}
	return edge, flowFile
}

// -----------------------------------------------------------------------------
// Inferred deltas — raw hex / magic spacing with no token counterpart
// -----------------------------------------------------------------------------

// hexColorRe captures a 3/4/6/8-digit hex colour with a leading '#'.
var hexColorRe = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

// cssSpacingRe captures a bare pixel length in a CSS-ish property context —
// `padding: 13px`, `gap: 7px`, `margin-top: 22px` — the "magic spacing" case
// §5b names. Submatch 1 is the number.
var cssSpacingRe = regexp.MustCompile(`(?i)(?:padding|margin|gap|top|right|bottom|left|inset|space)[a-z-]*\s*:\s*(-?\d+(?:\.\d+)?)px`)

// spacingTokenGroups are the token groups a proposed spacing token would land
// in, in preference order.
var spacingTokenGroups = []string{"dimension.space", "space", "spacing", "dimension"}

// analyzeInferred detects inferred deltas in one touched file: raw hex colours
// and magic spacing values that have no token counterpart. Each is a proposal
// — a new token (with a suggested path), or a switch to an existing token when
// one already carries the same value.
func analyzeInferred(f SyncFileInput, tree syncTree) []SyncDelta {
	if !isCSSVarHost(f.Path) {
		return nil
	}
	content := string(f.Content)
	var deltas []SyncDelta

	// Raw hex colours. A hex directly inside a var() fallback or backed by a
	// token-comment reference is still counterpart-less unless a DTCG token
	// carries the same value; the candidate lookup handles the latter. A hex
	// that *is* the declared value of a var mapping to a DTCG token is already
	// reported by the literal pass, so it is skipped here (no double signal).
	literalSpans := map[int]bool{}
	for _, m := range cssVarDeclRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		if _, ok := tree.byCSSVar[name]; ok {
			for i := m[4]; i < m[5]; i++ {
				literalSpans[i] = true
			}
		}
	}
	for _, m := range hexColorRe.FindAllStringSubmatchIndex(content, -1) {
		hex := strings.ToLower(content[m[0]:m[1]])
		if strings.HasPrefix(hex, "--") || literalSpans[m[0]] {
			continue
		}
		line := strings.Count(content[:m[0]], "\n") + 1
		deltas = append(deltas, inferredDelta(tree, f.Path, hex, "color", line,
			fmt.Sprintf("raw hex %s in code has no token counterpart", hex)))
	}

	// Magic spacing. A length that is the declared value of a var mapping to a
	// DTCG token is the literal pass's business.
	seen := map[string]bool{}
	for _, m := range cssSpacingRe.FindAllStringSubmatchIndex(content, -1) {
		if literalSpans[m[2]] {
			continue
		}
		num := content[m[2]:m[3]]
		if num == "0" || num == "0.0" {
			continue
		}
		value := num + "px"
		if seen[value] {
			continue
		}
		seen[value] = true
		line := strings.Count(content[:m[0]], "\n") + 1
		deltas = append(deltas, inferredDelta(tree, f.Path, value, "space", line,
			fmt.Sprintf("magic spacing %s in code has no token counterpart", value)))
	}
	return deltas
}

// inferredDelta builds one inferred (proposal) delta: it looks for an existing
// token carrying the same literal value (a "switch to existing" candidate),
// otherwise proposes a new token path in the group family for the value type.
func inferredDelta(tree syncTree, codeFile, value, family string, line int, summary string) SyncDelta {
	d := SyncDelta{
		Kind:        DeltaKindToken,
		Basis:       DeltaBasisInferred,
		Confidence:  ConfidenceLow,
		DesignFiles: []string{},
		SafeToApply: false,
		Proposal:    true,
		CodeFiles:   []string{codeFile},
		Evidence:    fmt.Sprintf("%s (line %d)", value, line),
	}
	if candidates := tree.byValue[strings.ToLower(value)]; len(candidates) > 0 {
		d.Delta = fmt.Sprintf("%s; switch to the existing token %s instead", summary, candidates[0])
		d.ProposalKind = "switch-to-existing"
		d.Candidate = candidates[0]
		d.Proposed = candidates[0]
		if tree.tokens != nil {
			if f := tokenSourceFile(tree, candidates[0]); f != "" {
				d.DesignFiles = []string{f}
			}
		}
		return d
	}

	proposed := proposeNewTokenPath(tree, family, value)
	d.Delta = fmt.Sprintf("%s; propose a new token %s", summary, proposed)
	d.ProposalKind = "new-token"
	d.Proposed = proposed
	d.DesignFiles = []string{proposedTokenFile(tree, proposed)}
	return d
}

// proposedTokenFile names the design file a *proposed* new token would go in:
// the conventional group tier (`design/tokens/<group>.tokens.json`) when that
// file exists, else the same conventional name so apply knows where to create
// it. It deliberately does not fall back to an unrelated existing tier the way
// tokenSourceFile does for an already-existing token.
func proposedTokenFile(tree syncTree, tokenPath string) string {
	group := topGroup(tokenPath)
	if group == "" {
		return tree.tokensPath
	}
	conventional := path.Join(DirName, TokenSubdir, group+".tokens.json")
	return conventional
}

// proposeNewTokenPath suggests a DTCG path for an inferred value: a color is
// proposed under `color.<slug>`; a dimension under the first free
// spacing-token group. The slug is derived from the value so the same literal
// in two files proposes the same name (determinism).
func proposeNewTokenPath(tree syncTree, family, value string) string {
	slug := valueSlug(value)
	switch family {
	case "color":
		return "color." + slug
	default:
		for _, group := range spacingTokenGroups {
			candidate := group + "." + slug
			if !tokenExists(tree, candidate) {
				return candidate
			}
		}
		return spacingTokenGroups[0] + "." + slug
	}
}

// valueSlug turns a literal value into a slug-safe name fragment:
// "#0055ff" → "0055ff", "13px" → "13px" → "13". Digits and letters only.
func valueSlug(value string) string {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "#")
	trimmed = strings.TrimSuffix(trimmed, "px")
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "value"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "v" + out
	}
	return out
}

// -----------------------------------------------------------------------------
// Deterministic ordering / dedup
// -----------------------------------------------------------------------------

// basisOrder ranks the bases for the canonical delta ordering: literal first
// (the safe mechanical subset), then structural, then inferred proposals.
func basisOrder(b deltaBasis) int {
	switch b {
	case DeltaBasisLiteral:
		return 0
	case DeltaBasisStructural:
		return 1
	case DeltaBasisInferred:
		return 2
	default:
		return 3
	}
}

// CompareDeltas is the canonical delta ordering: by basis, then kind, then
// code file, then the delta text. It is the determinism guarantee for the
// report — two runs over the same inputs emit the same Deltas slice.
func CompareDeltas(a, b SyncDelta) bool {
	if basisOrder(a.Basis) != basisOrder(b.Basis) {
		return basisOrder(a.Basis) < basisOrder(b.Basis)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if firstString(a.CodeFiles) != firstString(b.CodeFiles) {
		return firstString(a.CodeFiles) < firstString(b.CodeFiles)
	}
	if a.Delta != b.Delta {
		return a.Delta < b.Delta
	}
	return a.Token < b.Token
}

// firstString returns the first element of a slice, "" when empty.
func firstString(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// sortAndDedupDeltas orders the deltas canonically and coalesces exact
// duplicates (same basis, kind, description, token, and code files) so a value
// repeated across two files is reported once per file but not twice per match.
func sortAndDedupDeltas(deltas []SyncDelta) []SyncDelta {
	seen := map[string]bool{}
	out := make([]SyncDelta, 0, len(deltas))
	for _, d := range deltas {
		if d.DesignFiles == nil {
			d.DesignFiles = []string{}
		}
		if d.CodeFiles == nil {
			d.CodeFiles = []string{}
		}
		sort.Strings(d.DesignFiles)
		sort.Strings(d.CodeFiles)
		key := strings.Join([]string{
			string(d.Basis), d.Kind, d.Delta, d.Token, d.Proposed,
			strings.Join(d.CodeFiles, ","), strings.Join(d.DesignFiles, ","),
		}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareDeltas(out[i], out[j]) })
	return out
}

// -----------------------------------------------------------------------------
// Report summary
// -----------------------------------------------------------------------------

// RenderSyncSummary is the human/agent-readable one-liner for a report: the
// touched-file count and the delta split by basis, plus a pointer to the safe
// subset apply can write. Aimed at the model reading the ToolResult text.
func RenderSyncSummary(r *SyncReport) string {
	if r == nil {
		return "design_sync: no report."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "design_sync (analyze): %d touched file(s), %d semantic delta(s)",
		r.TouchedCount, r.DeltaCount)
	if r.DeltaCount > 0 {
		parts := []string{}
		for _, b := range []deltaBasis{DeltaBasisLiteral, DeltaBasisStructural, DeltaBasisInferred} {
			if n := r.ByBasis[string(b)]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, b))
			}
		}
		fmt.Fprintf(&sb, " (%s)", strings.Join(parts, ", "))
	}
	sb.WriteString(".")
	if safe := safeDeltaCount(r); safe > 0 {
		fmt.Fprintf(&sb, " %d safe to apply (run mode=apply to write them into design/).", safe)
	}
	if r.ByBasis[string(DeltaBasisInferred)] > 0 {
		fmt.Fprintf(&sb, " %d inferred proposal(s) need review.", r.ByBasis[string(DeltaBasisInferred)])
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&sb, " Skipped %d unreadable path(s).", len(r.Skipped))
	}
	return sb.String()
}

// safeDeltaCount counts the deltas apply may write mechanically.
func safeDeltaCount(r *SyncReport) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, d := range r.Deltas {
		if d.SafeToApply {
			n++
		}
	}
	return n
}

// -----------------------------------------------------------------------------
// Apply half — SP-140-5 §5b apply mode (TODO item 5.4)
// -----------------------------------------------------------------------------
//
// The apply half is the pure planning core for `design_sync` mode=apply. It
// takes a report (as produced by AnalyzeTouchedFiles) plus a reader for the
// current bytes of an existing design file, and returns a *SyncApplyPlan*: the
// ordered design-file writes the safe subset requires.
//
// The §5e rule is baked into the type system here, not just the prose:
//
//   - design_sync --apply writes design files; it NEVER rewrites the
//     implementation. The plan carries only design/-confined writes — the
//     handler is the one that performs I/O, and it refuses any plan whose
//     write set escapes design/ (see plan.IsConfinedToDesign).
//   - The semantic layer is *invited to adopt*, never auto-enforced onto code:
//     there is no "edit the implementation to match design/" operation in
//     this file at all, and none is reachable from the plan.
//
// The safe subset §5b names is exactly the deltas with SafeToApply set:
//
//   - literal token renames/revalues → rewrite the referenced DTCG entry
//     (revalue) or add the missing renamed entry, in the delta's TokenFile.
//   - structural new route/screen → create a skeleton wireframe SVG with
//     `draft` status, and append the proposed flow edge to the flow file.
//   - wireframe attribute/sidecar updates → the skeleton wireframe carries the
//     `draft` status marker and the proposed stem.
//
// Inferred deltas are *proposals*: the plan records them in Proposals and
// writes nothing for them (§5b "inferred ones are proposals"; AC "apply does
// not auto-create tokens without the literal/structural confidence bar").
//
// Everything is deterministic: the plan's writes and proposals are ordered by
// design path, and each write's bytes are a pure function of the delta and the
// file's current bytes.

// SyncPlanningOp is the kind of write a plan step performs.
type SyncPlanningOp = string

// Planning ops, fixed by the apply half.
const (
	// SyncPlanningUpdateToken rewrites an existing DTCG entry's value in place
	// (a literal token revalue). Delta is the token path.
	SyncPlanningUpdateToken SyncPlanningOp = "update-token"
	// SyncPlanningAddToken adds a DTCG entry that does not yet exist (a literal
	// token rename/addition the code introduced). Delta is the token path.
	SyncPlanningAddToken SyncPlanningOp = "add-token"
	// SyncPlanningAddWireframe creates a skeleton wireframe SVG with draft
	// status. Delta is the wireframe stem.
	SyncPlanningAddWireframe SyncPlanningOp = "add-wireframe"
	// SyncPlanningAddFlowEdge appends the proposed edge to a flow file. Delta is
	// the edge ("login --> home").
	SyncPlanningAddFlowEdge SyncPlanningOp = "add-flow-edge"
)

// SyncApplyWrite is one design-file write the apply half will perform. Path is
// workspace-relative, slash-separated, and always confined to design/ (§5e).
// Content is the full new file bytes (the write is a whole-file replace, which
// is what makes the ChangeTracker see an ordinary workspace edit).
type SyncApplyWrite struct {
	// Path is the design/ file to write, workspace-relative.
	Path string `json:"path"`
	// Op is the planning operation that produced this write.
	Op SyncPlanningOp `json:"op"`
	// Kind is the delta kind (token|wireframe|flow) this write serves, so a
	// consumer can group the run by the §5b vocabulary.
	Kind DeltaKind `json:"kind"`
	// Delta is the semantic delta description this write applies.
	Delta string `json:"delta"`
	// Token is the DTCG token path the write concerns ("" otherwise).
	Token string `json:"token,omitempty"`
	// Stem is the wireframe stem the write concerns ("" otherwise).
	Stem string `json:"stem,omitempty"`
	// Edge is the flow edge the write adds ("" otherwise).
	Edge string `json:"edge,omitempty"`
	// Status is the status marker the artifact carries (draft for §5b-created
	// artifacts; "" for a token revalue).
	Status string `json:"status,omitempty"`
	// Created is true when the write creates a file that did not exist.
	Created bool `json:"created"`
	// Content is the full new file bytes. It is deliberately JSON-omitted:
	// the plan's JSON shape names *what* will be written, and the handler is
	// the one that writes it; a consumer needing the bytes reads the file
	// after apply.
	Content []byte `json:"-"`
}

// SyncApplyProposal records a delta the apply half deliberately did NOT write:
// an inferred delta (a proposal §5b leaves to the agent/user via normal file
// edits), or a malformed/unsafe delta that could not be applied mechanically.
type SyncApplyProposal struct {
	// Delta is the semantic delta description.
	Delta string `json:"delta"`
	// Kind is the delta kind.
	Kind DeltaKind `json:"kind"`
	// Basis is literal|structural|inferred.
	Basis deltaBasis `json:"basis"`
	// Reason is why apply left it alone: "inferred" (§5b proposal),
	// "outside-design" (§5e refusal), or a specific refusal message.
	Reason string `json:"reason"`
	// ProposalKind / Proposed carry the §5b proposal fields when the delta is
	// an inferred proposal.
	ProposalKind string `json:"proposalKind,omitempty"`
	Proposed     string `json:"proposed,omitempty"`
	// CodeFiles are the touched code files the delta came from.
	CodeFiles []string `json:"codeFiles"`
}

// SyncApplyPlan is the pure result of planning a report's safe subset. The
// handler performs the Writes; nothing here touches the filesystem.
type SyncApplyPlan struct {
	// Mode is always "apply" (this is the apply half's plan).
	Mode string `json:"mode"`
	// Report is the analyze report the plan was derived from, so a consumer can
	// see the full delta set behind a partial apply.
	Report *SyncReport `json:"report"`
	// Writes is the ordered safe-subset write set (deterministic, design/-only).
	Writes []SyncApplyWrite `json:"writes"`
	// Proposals are the deltas apply deliberately left unwritten (inferred
	// proposals, plus any unsafe/unplannable delta).
	Proposals []SyncApplyProposal `json:"proposals"`
	// Refused are design-file writes that were suppressed because a write would
	// have escaped design/ (§5e). Always empty in a plan produced by
	// PlanSyncApply, which drops such writes; kept so a consumer can distinguish
	// "nothing to apply" from "something was refused".
	Refused []string `json:"refused,omitempty"`
	// AppliedCount / ProposalCount are the write and proposal tallies.
	AppliedCount  int `json:"appliedCount"`
	ProposalCount int `json:"proposalCount"`
	// WritePaths is the sorted set of distinct design/ paths the plan writes,
	// surfaced so a caller can assert design/-confinement without walking Writes.
	WritePaths []string `json:"writePaths"`
	// Notes carries run-level caveats (e.g. a malformed token file a write
	// could not be planned from).
	Notes []string `json:"notes,omitempty"`
}

// SyncFileReader returns the current bytes of an existing design file (path is
// workspace-relative, slash-separated). It returns (nil, false) when the file
// does not exist (a create) or cannot be read (a note, not a failure). The
// handler supplies a workspace-confined reader; the pure core never does I/O.
type SyncFileReader func(rel string) ([]byte, bool)

// IsConfinedToDesign reports whether every write in the plan targets a file
// inside design/ (§5e hard assertion). An empty plan is trivially confined.
func (p *SyncApplyPlan) IsConfinedToDesign() bool {
	if p == nil {
		return false
	}
	for _, w := range p.Writes {
		if !designConfinedPath(w.Path) {
			return false
		}
	}
	for _, path := range p.WritePaths {
		if !designConfinedPath(path) {
			return false
		}
	}
	return true
}

// designConfinedPath reports whether a slash path is inside design/ (the design
// dir itself or a descendant). The pure-core counterpart of the handler's
// designSyncDesignFile guard; kept here so the plan can self-check.
func designConfinedPath(p string) bool {
	clean := strings.TrimSuffix(path.Clean(strings.TrimSpace(p)), "/")
	if clean == "" || clean == "." {
		return false
	}
	return clean == DirName || strings.HasPrefix(clean, DirName+"/")
}

// PlanSyncApply is the pure apply core: given a report and a reader for the
// current design-file bytes, it returns the plan of safe-subset writes plus the
// proposals apply leaves alone. It performs no writes and never plans a write
// outside design/.
//
// A nil report (or one with no deltas) yields an empty, valid plan — apply on
// an already-synced tree is a no-op.
func PlanSyncApply(report *SyncReport, read SyncFileReader) *SyncApplyPlan {
	plan := &SyncApplyPlan{
		Mode:       SyncModeApplyConst,
		Report:     report,
		Writes:     []SyncApplyWrite{},
		Proposals:  []SyncApplyProposal{},
		WritePaths: []string{},
	}
	if report == nil {
		plan.Notes = append(plan.Notes, "No report to apply.")
		return plan
	}
	if read == nil {
		read = func(string) ([]byte, bool) { return nil, false }
	}

	// Dedup guards: two deltas may name the same file/entry (a value repeated
	// across two code files, a nav target and a route naming the same stem).
	// The plan writes each file once, folding later contributions in.
	writesByPath := map[string]int{}  // path -> index into plan.Writes
	tokenUpdates := map[string]bool{} // "file\x00token" -> already planned
	edgesByFile := map[string]map[string]bool{}
	wireframes := map[string]bool{}

	addWrite := func(w SyncApplyWrite) {
		if !designConfinedPath(w.Path) {
			// §5e: never plan a write outside design/. Surface the refusal
			// rather than silently dropping it.
			plan.Refused = append(plan.Refused, w.Path)
			return
		}
		plan.Writes = append(plan.Writes, w)
	}

	for _, d := range report.Deltas {
		if d.Proposal || !d.SafeToApply || d.Basis == DeltaBasisInferred {
			// §5b: inferred deltas (and anything not safe to apply) are
			// proposals the agent/user resolve via normal file edits.
			plan.Proposals = append(plan.Proposals, proposalFromDelta(d))
			continue
		}

		switch d.Kind {
		case DeltaKindToken:
			w, ok, note := planTokenWrite(d, read)
			if note != "" {
				plan.Notes = append(plan.Notes, note)
			}
			if !ok {
				plan.Proposals = append(plan.Proposals, SyncApplyProposal{
					Delta:     d.Delta,
					Kind:      d.Kind,
					Basis:     d.Basis,
					Reason:    note,
					CodeFiles: append([]string{}, d.CodeFiles...),
				})
				continue
			}
			key := w.Path + "\x00" + w.Token
			if tokenUpdates[key] {
				continue
			}
			tokenUpdates[key] = true
			if idx, exists := writesByPath[w.Path]; exists {
				// Fold into the existing write for this file: append this
				// token's edit onto the already-planned content by re-running
				// the token rewrite over the planned bytes.
				plan.Writes[idx].Content = w.Content
				continue
			}
			writesByPath[w.Path] = len(plan.Writes)
			addWrite(w)

		case DeltaKindWireframe:
			if d.WireframeStem == "" || wireframes[d.WireframeStem] {
				continue
			}
			wireframes[d.WireframeStem] = true
			wfPath := syncWireframePath(d)
			if !designConfinedPath(wfPath) {
				plan.Refused = append(plan.Refused, wfPath)
				continue
			}
			_, exists := read(wfPath)
			addWrite(SyncApplyWrite{
				Path:    wfPath,
				Op:      SyncPlanningAddWireframe,
				Kind:    DeltaKindWireframe,
				Delta:   d.Delta,
				Stem:    d.WireframeStem,
				Status:  FlowDraftStatus,
				Created: !exists,
				Content: []byte(skeletonWireframeSVG(d.WireframeStem)),
			})

		case DeltaKindFlow:
			if d.FlowEdge == "" || d.FlowFile == "" {
				continue
			}
			if edgesByFile[d.FlowFile] == nil {
				edgesByFile[d.FlowFile] = map[string]bool{}
			}
			if edgesByFile[d.FlowFile][d.FlowEdge] {
				continue
			}
			edgesByFile[d.FlowFile][d.FlowEdge] = true
			current, exists := read(d.FlowFile)
			var content []byte
			if exists {
				content = appendFlowEdge(current, d.FlowEdge)
			} else {
				content = []byte("flowchart TD\n  " + d.FlowEdge + "\n")
			}
			plan.Writes = append(plan.Writes, SyncApplyWrite{
				Path:    d.FlowFile,
				Op:      SyncPlanningAddFlowEdge,
				Kind:    DeltaKindFlow,
				Delta:   d.Delta,
				Edge:    d.FlowEdge,
				Stem:    d.WireframeStem,
				Status:  FlowDraftStatus,
				Created: !exists,
				Content: content,
			})

		default:
			// A feedback or unknown-kind safe delta: nothing mechanical to
			// write; leave it to the agent/user.
			plan.Proposals = append(plan.Proposals, SyncApplyProposal{
				Delta:     d.Delta,
				Kind:      d.Kind,
				Basis:     d.Basis,
				Reason:    "no mechanical write for this delta kind",
				CodeFiles: append([]string{}, d.CodeFiles...),
			})
		}
	}

	plan.AppliedCount = len(plan.Writes)
	plan.ProposalCount = len(plan.Proposals)
	plan.WritePaths = distinctSortedPaths(plan.Writes)
	return plan
}

// SyncModeApplyConst mirrors the handler's mode constant without importing the
// handler (the pure core must not depend on pkg/agent_tools).
const SyncModeApplyConst = "apply"

// proposalFromDelta turns an inferred delta into an apply proposal record.
func proposalFromDelta(d SyncDelta) SyncApplyProposal {
	reason := "inferred: a proposal for the agent/user to resolve via normal file edits"
	if d.Basis != DeltaBasisInferred {
		reason = "not safe to apply: left to the agent/user"
	}
	return SyncApplyProposal{
		Delta:        d.Delta,
		Kind:         d.Kind,
		Basis:        d.Basis,
		Reason:       reason,
		ProposalKind: d.ProposalKind,
		Proposed:     d.Proposed,
		CodeFiles:    append([]string{}, d.CodeFiles...),
	}
}

// planTokenWrite plans the DTCG write a literal token delta requires: revalue
// the referenced entry when it exists, add it when the code introduced a new
// spelling of an existing token. ok is false (with a note) when no write can be
// planned — e.g. no token file is named, or the file cannot be parsed.
func planTokenWrite(d SyncDelta, read SyncFileReader) (SyncApplyWrite, bool, string) {
	// TokenFile is the DTCG file the delta names. A delta that names none (a
	// bare token reference the analyzer could only point at the token *tier*)
	// falls back to a conventional per-group file under design/tokens/. Never
	// treat TokenEntry as a path: it is an entry key, not a file.
	tokenFile := d.TokenFile
	if tokenFile == "" {
		for _, f := range d.DesignFiles {
			if strings.HasPrefix(f, path.Join(DirName, TokenSubdir)+"/") &&
				strings.HasSuffix(f, ".tokens.json") {
				tokenFile = f
				break
			}
		}
	}
	tokenPath := tokenTokenPath(d)
	if tokenFile == "" && tokenPath != "" {
		// Conventional per-group file: design/tokens/<group>.tokens.json, the
		// same convention tokenSourceFile/proposedTokenFile use.
		if group := topGroup(tokenPath); group != "" {
			tokenFile = path.Join(DirName, TokenSubdir, group+".tokens.json")
		}
	}
	if tokenFile == "" || tokenPath == "" {
		return SyncApplyWrite{}, false, "token delta names no DTCG file/entry; left as a proposal"
	}
	if !designConfinedPath(tokenFile) {
		return SyncApplyWrite{}, false, "token delta's file is outside design/; refused"
	}
	// A mechanical token write needs a concrete literal value. A bare
	// `{token.path}` reference (a structural token delta) carries none, so
	// inventing an entry would write an empty value — that is a proposal for
	// the agent/user, not a mechanical write.
	if !hasLiteralTokenValue(d) {
		return SyncApplyWrite{}, false, "token delta carries no concrete value to write; left as a proposal"
	}

	current, exists := read(tokenFile)
	if !exists || len(current) == 0 {
		// The token file does not exist yet: create it with just this entry.
		return SyncApplyWrite{
			Path:    tokenFile,
			Op:      SyncPlanningAddToken,
			Kind:    DeltaKindToken,
			Delta:   d.Delta,
			Token:   tokenPath,
			Created: true,
			Content: createTokenDocument(tokenPath, d),
		}, true, ""
	}

	newContent, applied, err := rewriteDTCEntry(current, tokenPath, d)
	if err != nil {
		return SyncApplyWrite{}, false, fmt.Sprintf("%s: %v", tokenFile, err)
	}
	if !applied {
		// The entry is not present (the code renamed/introduced it): add it.
		newContent, err = addDTCEntry(current, tokenPath, d)
		if err != nil {
			return SyncApplyWrite{}, false, fmt.Sprintf("%s: %v", tokenFile, err)
		}
		return SyncApplyWrite{
			Path:    tokenFile,
			Op:      SyncPlanningAddToken,
			Kind:    DeltaKindToken,
			Delta:   d.Delta,
			Token:   tokenPath,
			Created: false,
			Content: newContent,
		}, true, ""
	}
	return SyncApplyWrite{
		Path:    tokenFile,
		Op:      SyncPlanningUpdateToken,
		Kind:    DeltaKindToken,
		Delta:   d.Delta,
		Token:   tokenPath,
		Created: false,
		Content: newContent,
	}, true, ""
}

// tokenTokenPath is the DTCG entry a token delta concerns: the entry when set,
// else the token.
func tokenTokenPath(d SyncDelta) string {
	if d.TokenEntry != "" {
		return d.TokenEntry
	}
	return d.Token
}

// syncWireframePath is the wireframe path a structural delta targets: the delta's
// designFiles entry ending in .svg under design/wireframes/, else the
// conventional path from the stem.
func syncWireframePath(d SyncDelta) string {
	for _, f := range d.DesignFiles {
		if strings.HasPrefix(f, path.Join(DirName, "wireframes")+"/") &&
			strings.HasSuffix(f, ".svg") {
			return f
		}
	}
	if d.WireframeStem != "" {
		return path.Join(DirName, "wireframes", d.WireframeStem+".svg")
	}
	return ""
}

// skeletonWireframeSVG renders the §5b skeleton wireframe for a new screen: a
// viewBox-only SVG carrying the draft status marker (as a comment, the same
// convention the manifest status markers and token-usage comments use) and the
// screen's stem as the root id. It is deliberately structure-only — a draft the
// next design turn fleshes out — never code.
//
// The comment carries the status so the artifact is self-describing; the frame
// matches the mobile frame the fixture tree uses (390x844).
func skeletonWireframeSVG(stem string) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">` + "\n")
	fmt.Fprintf(&b, "  <!-- status: %s (created by design_sync apply from a new route/screen in code) -->\n", FlowDraftStatus)
	fmt.Fprintf(&b, "  <!-- screen: %s -->\n", stem)
	fmt.Fprintf(&b, "  <g id=\"%s\">\n", stem)
	b.WriteString(`    <rect x="0" y="0" width="390" height="844" fill="none" />` + "\n")
	b.WriteString("  </g>\n")
	b.WriteString("</svg>\n")
	return b.String()
}

// appendFlowEdge appends an edge statement to a flow document, preserving the
// existing declaration if there is one and adding one when there is not. It is
// idempotent: an edge already present (as a parsed edge) is not re-added.
func appendFlowEdge(content []byte, edge string) []byte {
	text := string(content)
	fc := ParseFlowchart(text)
	for _, e := range fc.Edges {
		if e.Source+" --> "+e.Target == edge {
			return content
		}
	}
	if len(content) > 0 && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if fc.Declarations == 0 {
		text = "flowchart TD\n" + text
	}
	return []byte(text + "  " + edge + "\n")
}

// distinctSortedPaths returns the sorted distinct write paths.
func distinctSortedPaths(writes []SyncApplyWrite) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(writes))
	for _, w := range writes {
		if seen[w.Path] {
			continue
		}
		seen[w.Path] = true
		out = append(out, w.Path)
	}
	sort.Strings(out)
	return out
}
