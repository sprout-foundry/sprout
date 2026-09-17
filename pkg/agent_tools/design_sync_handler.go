//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// designSyncHandler implements ToolHandler for the design_sync tool
// (SP-140-5 §5b). It is the code→design half of the loop: the tool a UI-affecting
// dev turn runs at its end so the semantic layer (design/) adopts what the
// implementation learned.
//
// Both halves of §5b are implemented here:
//
//   - Analyze mode (item 5.3) reads the touched UI code files and the design/
//     tree and returns a structured sync report: the detected semantic deltas,
//     each {delta, kind, basis, confidence, designFiles} with the three bases
//     §5b fixes — literal (a token var renamed/revalued; maps 1:1 to a DTCG
//     entry, safe to apply), structural (a new route/screen/nav target; maps to
//     wireframe/flow changes), and inferred (raw hex / magic spacing with no
//     token counterpart; a proposal).
//
//   - Apply mode (item 5.4) writes the report's *safe subset* into design/:
//     literal token renames/revalues (the referenced DTCG entry), structural
//     skeleton wireframes (draft status) and flow-edge additions. Inferred
//     deltas are marked as proposals and NOT auto-applied.
//
// §5e is enforced structurally: apply writes design files and NEVER rewrites
// the implementation. Every path in the plan is design/-confined, the handler
// refuses (rather than writes) any path that would escape design/, and there is
// no operation anywhere on this path that edits implementation code. The
// semantic layer is *invited to adopt*; enforcement lives in review/critique,
// not here.
//
// The pure halves live in pkg/design (AnalyzeTouchedFiles, PlanSyncApply); this
// handler is the thin ToolEnv-facing wrapper: it resolves the touched-file set
// (the explicit `files` argument, else the turn's ChangeTracker set via the
// sanctioned env.ResolveToolFuncs().ListChanges seam), runs Gate-1
// PrecheckFileAccess on every path it reads *and writes*, loads each file's
// bytes, and — for apply — performs the plan's writes through the
// workspace-confined resolver (filesystem.SafeResolvePathForWriteWithBypass), so
// they are ordinary ChangeTracker-visible, revertible workspace edits.
//
// The tool is pure Go with no browser/vision dependency, but SP-140 invariant 7
// keeps only design_assets and design_validate on the WASM roster; it is
// native-only with a nil-returning stub in design_sync_handler_js.go and a
// build-tagged registrar in all.go (mirroring design_export_handler.go).
type designSyncHandler struct{}

func (h *designSyncHandler) Name() string {
	return "design_sync"
}

// SyncModeAnalyze / SyncModeApply are the `mode` argument values (§5b).
const (
	SyncModeAnalyze = "analyze"
	SyncModeApply   = "apply"
)

func (h *designSyncHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_sync",
		Description: "Design↔code sync (SP-140-5 §5b): detect the semantic deltas a " +
			"UI-affecting dev turn introduced, so the design/ tree (the semantic source of " +
			"truth) can adopt them. Run it at the END of a turn that changed UI code — the " +
			"way a turn that edits code ends with tests. " +
			"Analyze mode (the default) reads the touched UI code files AND the design/ tree " +
			"and returns a structured sync report: each detected delta is " +
			"{delta, kind: token|wireframe|flow|feedback, basis: literal|structural|inferred, " +
			"confidence, designFiles}. Bases: `literal` — a token variable was renamed/revalued " +
			"in code and maps 1:1 to a DTCG entry (the report names the design/tokens file and " +
			"entry); `structural` — a new route/screen appeared in a router file, a component " +
			"was added, or a nav target moved (the report names the proposed wireframe stem " +
			"and/or flow edge, draft status); `inferred` — styling with no token counterpart " +
			"(raw hex, magic spacing) reported as a *proposal* (a new token, or a switch to an " +
			"existing one), never auto-applied. Confidence governs automation: literal and " +
			"structural deltas are safe to apply; inferred ones are proposals. " +
			"The analysis is diff + convention based (CSS custom properties, Tailwind/DTCG " +
			"token references, router files, component file names) — no AST/deep static analysis " +
			"in v1. " +
			"mode=analyze writes no files and reads only: it runs the file-access precheck on " +
			"every path it touches. " +
			"mode=apply writes the report's SAFE SUBSET into design/: literal token " +
			"renames/revalues (rewriting the referenced DTCG entry in design/tokens/), " +
			"structural skeleton wireframes with `draft` status, and flow-edge additions. " +
			"Inferred deltas are marked as proposals and NOT auto-applied — it never creates " +
			"tokens without the literal/structural confidence bar. Apply writes are confined " +
			"to design/ (SP-140-5 §5e: it never rewrites the implementation to match design/); " +
			"a write that would leave design/ is refused. All writes are ordinary workspace " +
			"file edits — ChangeTracker-visible and revertible.",
		Parameters: []ParameterDef{
			{
				Name:        "files",
				Type:        "string",
				Required:    false,
				Description: "The turn's touched code files to analyse: a comma- or newline-separated list of workspace paths. Omit to use the turn's ChangeTracker set (the files changed this session).",
			},
			{
				Name:        "mode",
				Type:        "string",
				Required:    false,
				Description: "`analyze` (default) returns the sync report and writes nothing. `apply` writes the report's safe subset (literal token renames/revalues, structural wireframe/flow additions) into design/ and reports inferred deltas as proposals; writes are confined to design/ and never touch implementation code.",
			},
		},
		Required: nil,
	}
}

func (h *designSyncHandler) Validate(args map[string]any) error {
	if v, exists := lookupKey(args, "files"); exists && v != nil {
		switch v.(type) {
		case string, []any, []string:
			// accepted
		default:
			return fmt.Errorf("parameter 'files' must be a string or a list of strings, got %T", v)
		}
	}
	if v, exists := lookupKey(args, "mode"); exists && v != nil {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("parameter 'mode' must be a string, got %T", v)
		}
	}
	return nil
}

// designSyncOutput is the JSON-friendly structured result of one design_sync
// run. It wraps the pkg/design report so the handler can add the resolved
// touched-file context (where the set came from, what was read) without
// polluting the pure analysis result.
type designSyncOutput struct {
	// Report is the pure analyze result (SP-140-5 §5b): the ordered semantic
	// deltas and their design-file work set.
	Report *design.SyncReport `json:"report"`
	// TouchedSource records where the touched-file set came from:
	// "argument" (explicit `files`) or "changes" (the turn's ChangeTracker set
	// via list_changes).
	TouchedSource string `json:"touchedSource"`
	// TouchedFiles are the analysed code paths, sorted (the report's own list
	// is per-delta; this is the run-level view).
	TouchedFiles []string `json:"touchedFiles"`
	// Apply is the apply-mode result (item 5.4): the plan of safe-subset writes
	// and the proposals left alone. It is nil for an analyze run.
	Apply *designSyncApplyOutput `json:"apply,omitempty"`
	// Notes carries run-level caveats (e.g. "no changed files this session").
	Notes []string `json:"notes,omitempty"`
}

// designSyncApplyOutput is the apply-mode structured result (SP-140-5 §5b
// apply half): the safe-subset plan, the design files actually written, and the
// proposals apply deliberately left to the agent/user.
type designSyncApplyOutput struct {
	// Plan is the pure apply plan (writes + proposals, design/-confined).
	Plan *design.SyncApplyPlan `json:"plan"`
	// Applied are the design/ paths written, sorted. Empty when there was
	// nothing safe to apply — a second apply after a first is a no-op.
	Applied []string `json:"applied"`
	// ProposalCount is the number of deltas left as proposals (inferred ones,
	// plus any unsafe/unplannable delta); also on the plan, surfaced here so a
	// consumer reading only the top level sees the split.
	ProposalCount int `json:"proposalCount"`
	// OutsideDesign is the set of paths the plan refused because a write would
	// have escaped design/ (§5e). Always empty in practice; present so a caller
	// can assert the invariant.
	OutsideDesign []string `json:"outsideDesign,omitempty"`
	// ImplementationUntouched states the §5e invariant explicitly: apply writes
	// design files and never rewrites the implementation. Always true.
	ImplementationUntouched bool `json:"implementationUntouched"`
}

// applySyncPlan plans and performs the report's safe subset (§5b apply half).
//
// It reads current design-file bytes through a workspace-confined reader (so
// the planned content is derived from exactly what is on disk), plans the
// writes with the pure core, refuses any plan write outside design/ (§5e), then
// performs the writes as ordinary workspace file edits. Inferred deltas are
// left as proposals and nothing is written for them.
func (h *designSyncHandler) applySyncPlan(ctx context.Context, env ToolEnv, root string, report *design.SyncReport, out designSyncOutput) (ToolResult, error) {
	reader := syncDesignFileReader(root)
	plan := design.PlanSyncApply(report, reader)

	if !plan.IsConfinedToDesign() {
		msg := "design_sync: refusing to apply — the plan would write outside design/ (SP-140-5 §5e)"
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_sync: apply plan escapes design/")
	}

	applied, writeErr := writeSyncPlan(ctx, env, plan)

	// On failure writeSyncPlan rolled back whatever it had applied, so nothing
	// remains in the tree: report an empty applied set so the structured result
	// matches reality (the plan still shows what was intended).
	reported := applied
	if writeErr != nil {
		reported = nil
	}
	applyOut := &designSyncApplyOutput{
		Plan:                    plan,
		Applied:                 reported,
		ProposalCount:           plan.ProposalCount,
		OutsideDesign:           plan.Refused,
		ImplementationUntouched: true,
	}
	out.Apply = applyOut

	if writeErr != nil {
		// A failed write is a hard failure: the plan is reported so the caller
		// can see what was intended and that the tree was rolled back.
		msg := fmt.Sprintf("design_sync apply failed (rolled back): %v", writeErr)
		return ToolResult{Output: msg, StructuredOut: out, IsError: true}, fmt.Errorf("design_sync: %w", writeErr)
	}

	return ToolResult{
		Output:        renderDesignSyncApplySummary(out),
		StructuredOut: out,
		IsError:       false,
	}, nil
}

// syncDesignFileReader returns the workspace-confined design-file reader the
// pure apply core uses for current bytes. A design/ path is read from disk; a
// path outside design/ (the plan never emits one, but the reader stays
// defensive) is treated as absent, so no non-design bytes can ever reach a
// plan. A missing file reports (nil,false) — a create.
func syncDesignFileReader(root string) design.SyncFileReader {
	return func(rel string) ([]byte, bool) {
		if !designSyncDesignFile(rel) {
			return nil, false
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return nil, false
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			return nil, false
		}
		return data, true
	}
}

func (h *designSyncHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	mode := normaliseSyncMode(stringArg(args, "mode"))
	switch mode {
	case SyncModeAnalyze, SyncModeApply:
		// Both halves are implemented (analyze item 5.3, apply item 5.4).
	default:
		msg := fmt.Sprintf("design_sync: unknown mode %q (want %q or %q)", mode, SyncModeAnalyze, SyncModeApply)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_sync: unknown mode %q", mode)
	}

	// Resolve the touched-file set: explicit `files` wins, else the turn's
	// ChangeTracker set through the sanctioned ListChanges seam.
	touched, source, resolveErr := resolveSyncTouched(ctx, env, args)
	if resolveErr != nil {
		msg := fmt.Sprintf("design_sync: %v", resolveErr)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_sync: %w", resolveErr)
	}

	// Gate-1 precheck (SP-140 invariant 7): every path the tool touches — the
	// touched code paths it reads and the design/ tree it reads (and, in apply
	// mode, writes) — is prechecked before any I/O. The design/ root is checked
	// first so a workspace-level deny is caught even before probing the tree.
	gatePaths := append([]string{design.DirName}, touched...)
	for _, gate := range gatePaths {
		_, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_sync", gate)
		if decision == "deny" {
			msg := fmt.Sprintf("design_sync blocked: %s is denied by the active file-access policy", gate)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_sync blocked: %s is declared denied", gate)
		}
	}

	// Load each touched file's bytes. A file that cannot be read (missing, a
	// directory, or outside the workspace) is skipped with a note rather than
	// failing the run: analyze reports on what it can see.
	inputs, skipped := loadSyncInputs(root, touched)

	report, err := design.AnalyzeTouchedFiles(design.SyncInput{Root: root, Touched: inputs})
	if err != nil {
		msg := fmt.Sprintf("design_sync failed: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_sync: %w", err)
	}
	report.Mode = mode

	out := designSyncOutput{
		Report:        report,
		TouchedSource: source,
		TouchedFiles:  make([]string, 0, len(touched)),
	}
	for _, in := range inputs {
		out.TouchedFiles = append(out.TouchedFiles, in.Path)
	}
	sort.Strings(out.TouchedFiles)
	if len(touched) == 0 {
		out.Notes = append(out.Notes,
			"No touched code files to analyse. Pass `files` explicitly, or run design_sync at the end of a UI-affecting turn.")
	}
	if len(skipped) > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("Skipped %d unreadable path(s): %s",
			len(skipped), strings.Join(skipped, ", ")))
	}

	if mode == SyncModeApply {
		return h.applySyncPlan(ctx, env, root, report, out)
	}

	return ToolResult{
		Output:        renderDesignSyncSummary(out),
		StructuredOut: out,
		IsError:       false,
	}, nil
}

// writeSyncPlan performs the plan's writes as ordinary workspace file edits and
// returns the applied-path set. Every write path is re-checked against Gate-1
// (a deny OR an unresolved prompt is a hard refusal, not a skip) and resolved
// through the workspace-confined resolver, so a plan can never write outside the
// workspace or outside design/ (§5e). The parent directory is created as needed
// (a new wireframe/flow file). Files are written whole, which is what makes the
// edit ChangeTracker-visible and revertible.
//
// Each write is recorded with the agent's ChangeTracker through the same
// TrackFileWrite seam write_file uses (§5b: "All writes are ordinary workspace
// file edits — ChangeTracker-visible, revertible"). Tracking is best-effort: a
// tracking failure must not fail the write itself.
//
// On a failure part-way through, the writes already applied are rolled back
// (originals restored, created files removed) so the tree is never left in a
// half-applied state; the error names the failing path.
//
// A path that would escape design/ is refused here as a second, independent
// guard (PlanSyncApply already drops such writes): §5e is enforced at both
// layers, and this one is the one that performs I/O.
func writeSyncPlan(ctx context.Context, env ToolEnv, plan *design.SyncApplyPlan) ([]string, error) {
	applied := make([]string, 0, len(plan.Writes))
	// restore records what to undo for each applied write: a created file is
	// deleted, an updated file is rewritten with its captured original.
	type undo struct {
		abs      string
		created  bool
		original []byte
	}
	var undos []undo

	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.created {
				_ = os.Remove(u.abs)
				continue
			}
			_ = os.WriteFile(u.abs, u.original, 0o644)
		}
	}

	for _, w := range plan.Writes {
		if !designSyncDesignFile(w.Path) {
			rollback()
			return applied, fmt.Errorf("refusing to write %q: design_sync --apply is confined to design/ (§5e)", w.Path)
		}
		// Gate-1 on every write path (the dispatch-level precheck covers
		// design/ wholesale and the touched code paths; this covers the exact
		// file, including one created by the plan). Mirrors write_file's
		// contract: a "deny" is a hard refusal; a "prompt" falls through to the
		// workspace-confined resolver below, which is the enforcement point for
		// paths with no verdict (a design/ path is inside the workspace, so the
		// resolver admits it; an escaping path is refused by the resolver).
		_, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_sync", w.Path)
		if decision == "deny" {
			rollback()
			return applied, fmt.Errorf("design_sync blocked: %s is denied by the active file-access policy", w.Path)
		}
		abs, resolveErr := filesystem.SafeResolvePathForWriteWithBypass(ctx, w.Path)
		if resolveErr != nil {
			rollback()
			return applied, fmt.Errorf("resolving %s for write: %w", w.Path, resolveErr)
		}
		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			rollback()
			return applied, fmt.Errorf("creating directory for %s: %w", w.Path, mkErr)
		}
		// Capture pre-write content for change tracking BEFORE the write
		// mutates the file, so recovery can restore it. A read miss (new file)
		// is the create case: original stays empty. Read unconditionally so the
		// rollback has the bytes even without a tracker.
		var original []byte
		created := true
		if data, readErr := os.ReadFile(abs); readErr == nil {
			original = data
			created = false
		}
		if writeErr := os.WriteFile(abs, w.Content, 0o644); writeErr != nil {
			rollback()
			return applied, fmt.Errorf("writing %s: %w", w.Path, writeErr)
		}
		undos = append(undos, undo{abs: abs, created: created, original: original})
		// Session change tracking (best-effort), mirroring write_file.
		if fn := env.ResolveToolFuncs().TrackFileWrite; fn != nil {
			if trackErr := fn(abs, string(original), string(w.Content)); trackErr != nil {
				log.Printf("[design_sync] change tracking failed for %q: %v", w.Path, trackErr)
			}
		}
		applied = append(applied, w.Path)
	}
	return applied, nil
}

// normaliseSyncMode trims and lowercases the mode argument, defaulting to
// analyze (§5b: "mode (analyze | apply, default analyze)").
func normaliseSyncMode(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		return SyncModeAnalyze
	}
	return m
}

// resolveSyncTouched determines the touched-file set and its provenance. An
// explicit `files` argument (string or list) overrides the default; otherwise
// the turn's ChangeTracker set is read through the sanctioned
// env.ResolveToolFuncs().ListChanges seam — string output, parsed (there is no
// typed ChangeTracker accessor; §5b names this seam explicitly).
func resolveSyncTouched(ctx context.Context, env ToolEnv, args map[string]any) (paths []string, source string, err error) {
	if v, exists := lookupKey(args, "files"); exists && v != nil {
		explicit := syncArgPaths(v)
		if len(explicit) > 0 {
			return explicit, "argument", nil
		}
		// An explicitly-passed but empty `files` is a usage error: the caller
		// asked for a specific set and named none.
		if _, ok := v.(string); ok && strings.TrimSpace(syncArgString(v)) == "" {
			return nil, "argument", fmt.Errorf("`files` was passed but names no paths")
		}
	}

	fn := env.ResolveToolFuncs().ListChanges
	if fn == nil {
		return nil, "changes", nil
	}
	raw, callErr := fn(ctx, map[string]any{})
	if callErr != nil {
		return nil, "changes", fmt.Errorf("listing the turn's changed files: %w", callErr)
	}
	return parseListChangesPaths(raw), "changes", nil
}

// syncArgPaths extracts a path list from a `files` argument value: a
// comma/newline-separated string, or a []any/[]string list. Entries are
// trimmed; empties dropped; duplicates removed; the result is sorted.
func syncArgPaths(v any) []string {
	var raw []string
	switch val := v.(type) {
	case string:
		raw = splitPathList(val)
	case []string:
		raw = append(raw, val...)
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// syncArgString renders a string arg for emptiness checks.
func syncArgString(v any) string {
	s, _ := v.(string)
	return s
}

// splitPathList splits a comma-, newline-, or semicolon-separated path list,
// trimmed and with empties dropped.
func splitPathList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// listChangesEnvelope is the subset of the list_changes JSON output the sync
// tool reads: the per-file rows' `path` field, plus a bulk row's nested
// `bulk_items[].path` list (a shell command that touched many files is one
// `bulk` row carrying its members). The full envelope is wider (diffs,
// timestamps); only the paths matter here, so the parse is deliberately
// tolerant of the rest.
type listChangesEnvelope struct {
	Files []struct {
		Path      string `json:"path"`
		Op        string `json:"op"`
		BulkItems []struct {
			Path string `json:"path"`
		} `json:"bulk_items"`
	} `json:"files"`
}

// parseListChangesPaths extracts the changed paths from a list_changes JSON
// string (§5b: the sanctioned seam is "string output, parsed"). It is
// deliberately tolerant: a non-JSON or unexpected payload yields no paths
// rather than an error, because analyze mode degrades to "nothing to analyse"
// better than it fails a turn. Paths are deduped and sorted.
func parseListChangesPaths(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var env listChangesEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		// Tolerant by design: a malformed payload yields no paths (an empty
		// touched set), because analyze mode degrades to "nothing to analyse"
		// rather than failing a dev turn.
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(env.Files))
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, f := range env.Files {
		// A bulk entry (a shell command that touched many files) lists its
		// members in nested bulk_items; those are the real paths, and the
		// row's own `path` is empty for a bulk row.
		for _, b := range f.BulkItems {
			add(b.Path)
		}
		if len(f.BulkItems) > 0 {
			continue
		}
		add(f.Path)
	}
	sort.Strings(out)
	return out
}

// loadSyncInputs reads each touched path's bytes, returning the analysis inputs
// plus the paths that could not be read. A path is skipped when it is empty, a
// directory, missing, or resolves outside the workspace root (Gate-1 is the
// enforcement point; this is the read-side guard so an out-of-workspace path
// can never leak bytes into the report).
func loadSyncInputs(root string, touched []string) (inputs []design.SyncFileInput, skipped []string) {
	inputs = make([]design.SyncFileInput, 0, len(touched))
	for _, p := range touched {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, filepath.FromSlash(p))
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			skipped = append(skipped, p)
			continue
		}
		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() {
			skipped = append(skipped, p)
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			skipped = append(skipped, p)
			continue
		}
		inputs = append(inputs, design.SyncFileInput{Path: filepath.ToSlash(rel), Content: data})
	}
	sort.Strings(skipped)
	return inputs, skipped
}

// renderDesignSyncApplySummary builds the human/agent-readable summary line for
// an apply run: what was written into design/, and what was left as a proposal.
func renderDesignSyncApplySummary(out designSyncOutput) string {
	var sb strings.Builder
	sb.WriteString("design_sync (apply): ")
	if out.Apply == nil {
		sb.WriteString("no apply result.")
		return sb.String()
	}
	applied := out.Apply.Applied
	if len(applied) == 0 {
		sb.WriteString("nothing to apply — design/ is already in sync with the safe subset of the report.")
	} else {
		fmt.Fprintf(&sb, "wrote %d design file(s) in design/", len(applied))
		sb.WriteString(" (")
		if len(applied) <= 5 {
			sb.WriteString(strings.Join(applied, ", "))
		} else {
			sb.WriteString(strings.Join(applied[:5], ", "))
			fmt.Fprintf(&sb, ", +%d more", len(applied)-5)
		}
		sb.WriteString(").")
	}
	if n := out.Apply.ProposalCount; n > 0 {
		fmt.Fprintf(&sb, " %d inferred proposal(s) left for the agent/user to resolve via normal file edits.", n)
	}
	sb.WriteString(" Apply writes design files only — the implementation is never rewritten (SP-140-5 §5e).")
	if len(out.Notes) > 0 {
		sb.WriteString(" " + strings.Join(out.Notes, " "))
	}
	return sb.String()
}

// renderDesignSyncSummary builds the human/agent-readable summary line for the
// structured result: the analyze report line plus the touched-set provenance,
// so a model reads where the file set came from without parsing JSON.
func renderDesignSyncSummary(out designSyncOutput) string {
	var sb strings.Builder
	sb.WriteString(design.RenderSyncSummary(out.Report))
	if out.TouchedSource == "changes" {
		fmt.Fprintf(&sb, " Touched set: the turn's changed files (%d).", len(out.TouchedFiles))
	} else {
		fmt.Fprintf(&sb, " Touched set: explicit files argument (%d).", len(out.TouchedFiles))
	}
	if len(out.Notes) > 0 {
		sb.WriteString(" " + strings.Join(out.Notes, " "))
	}
	return sb.String()
}

// designSyncDesignFile guards the report's §5e invariant at the handler
// boundary: every design file a delta names is inside design/. It is used by
// the tests and by item 5.4's apply path; analyze mode already guarantees it by
// construction (the pure analysis only ever emits design/-prefixed paths).
func designSyncDesignFile(p string) bool {
	clean := strings.TrimSuffix(path.Clean(filepath.ToSlash(p)), "/")
	return clean == design.DirName || strings.HasPrefix(clean, design.DirName+"/")
}

func (h *designSyncHandler) Aliases() []string      { return nil }
func (h *designSyncHandler) Timeout() time.Duration { return 60 * time.Second }
func (h *designSyncHandler) MaxResultSize() int     { return 0 }
func (h *designSyncHandler) SafeForParallel() bool  { return false }
func (h *designSyncHandler) Interactive() bool      { return false }

// registerDesignSyncTools registers the design_sync tool. It is pure Go, but
// SP-140 invariant 7 keeps only design_assets and design_validate on the WASM
// roster, so this is registered from a build-tagged registrar (excluded from
// WASM via design_sync_handler_js.go, which returns nil).
func registerDesignSyncTools() []ToolHandler {
	return []ToolHandler{
		&designSyncHandler{},
	}
}
