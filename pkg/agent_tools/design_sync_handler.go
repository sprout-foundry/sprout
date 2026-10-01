//go:build !js

package tools

import (
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
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

func (h *designSyncHandler) Aliases() []string { return nil }

func (h *designSyncHandler) Timeout() time.Duration { return 60 * time.Second }

func (h *designSyncHandler) MaxResultSize() int { return 0 }

func (h *designSyncHandler) SafeForParallel() bool { return false }

func (h *designSyncHandler) Interactive() bool { return false }

// registerDesignSyncTools registers the design_sync tool. It is pure Go, but
// SP-140 invariant 7 keeps only design_assets and design_validate on the WASM
// roster, so this is registered from a build-tagged registrar (excluded from
// WASM via design_sync_handler_js.go, which returns nil).
func registerDesignSyncTools() []ToolHandler {
	return []ToolHandler{
		&designSyncHandler{},
	}
}
