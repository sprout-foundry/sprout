//go:build !js

package tools

// design_sync_execute.go — the execution half of the design-sync
// handler: Execute, the sync-plan writer (writeSyncPlan), the plan
// applier (applySyncPlan), and the two summary renderers. Split out of
// design_sync_handler.go.
import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/design"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

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
