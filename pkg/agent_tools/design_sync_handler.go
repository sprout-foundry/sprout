//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// designSyncHandler implements ToolHandler for the design_sync tool
// (SP-140-5 §5b). It is the code→design half of the loop: the tool a UI-affecting
// dev turn runs at its end so the semantic layer (design/) adopts what the
// implementation learned.
//
// Scope note — this file is TODO item 5.3, the ANALYZE half only. Analyze mode
// reads the touched UI code files and the design/ tree and returns a structured
// sync report: the detected semantic deltas, each
// {delta, kind, basis, confidence, designFiles} with the three bases §5b fixes —
// literal (a token var renamed/revalued; maps 1:1 to a DTCG entry, safe to
// apply), structural (a new route/screen/nav target; maps to wireframe/flow
// changes), and inferred (raw hex / magic spacing with no token counterpart; a
// proposal). The report shape is what item 5.4's apply mode consumes: it carries
// the design files each delta would touch, and marks inferred deltas as
// proposals.
//
// Apply mode (5.4) is deliberately NOT implemented here. `mode=apply` is
// rejected with an explicit, actionable error naming the item that owns it,
// rather than silently doing nothing or, worse, half-writing the tree.
//
// The pure analysis lives in pkg/design/sync.go (AnalyzeTouchedFiles); this
// handler is the thin ToolEnv-facing wrapper: it resolves the touched-file set
// (the explicit `files` argument, else the turn's ChangeTracker set via the
// sanctioned env.ResolveToolFuncs().ListChanges seam), runs Gate-1
// PrecheckFileAccess on every path it reads, loads each file's bytes, and hands
// the analysis its input. Analyze mode writes nothing.
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
			"mode=apply (which writes the safe subset into design/) is a separate item and is " +
			"not available yet; calling it returns a clear error saying so.",
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
				Description: "`analyze` (default) returns the sync report and writes nothing. `apply` (writes the safe subset into design/) is a separate implementation item and is not available yet.",
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
	// deltas and their design-file work set. Item 5.4's apply mode consumes
	// exactly this.
	Report *design.SyncReport `json:"report"`
	// TouchedSource records where the touched-file set came from:
	// "argument" (explicit `files`) or "changes" (the turn's ChangeTracker set
	// via list_changes).
	TouchedSource string `json:"touchedSource"`
	// TouchedFiles are the analysed code paths, sorted (the report's own list
	// is per-delta; this is the run-level view).
	TouchedFiles []string `json:"touchedFiles"`
	// Notes carries run-level caveats (e.g. "no changed files this session").
	Notes []string `json:"notes,omitempty"`
}

func (h *designSyncHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	mode := normaliseSyncMode(stringArg(args, "mode"))
	switch mode {
	case SyncModeAnalyze:
		// The item-5.3 path.
	case SyncModeApply:
		// Item 5.4 owns apply. Refuse explicitly rather than pretending.
		msg := "design_sync: mode=apply is not available yet — apply mode (writing the safe " +
			"subset into design/) is a separate implementation item (SP-140-5 §5b apply half). " +
			"Run mode=analyze (the default) to get the report the apply half will consume."
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_sync: apply mode not implemented")
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

	// Gate-1 precheck (SP-140 invariant 7): analyze mode reads the touched code
	// paths and the design/ tree, so every one of them is prechecked before any
	// I/O. The design/ root is checked first so a workspace-level deny is
	// caught even before probing the tree.
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
	report.Mode = SyncModeAnalyze

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

	return ToolResult{
		Output:        renderDesignSyncSummary(out),
		StructuredOut: out,
		IsError:       false,
	}, nil
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
