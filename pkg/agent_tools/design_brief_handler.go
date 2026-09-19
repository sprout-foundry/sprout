//go:build !js

package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// designBriefHandler implements ToolHandler for the design_brief tool
// (SP-140-5 §5g, TODO item 5.8). It is the "brief whenever building a screen"
// step of the loop (§5d): the read-only contract an implementing agent reads
// before it builds a screen.
//
// Given a screen name (a wireframe stem) it reads the design/ tree *for that
// screen* and returns a structured brief:
//
//   - purpose            — the screen's purpose from the README Screens listing;
//   - wireframe path     — design/wireframes/<stem>.svg;
//   - flows in/out       — every flow edge touching the screen, with its trigger
//     (the mermaid edge label) and direction;
//   - token paths        — the DTCG tokens the wireframe refers to, plus the
//     token groups available to consume;
//   - open feedback      — pending §4d feedback annotations for the screen;
//   - status             — the README status marker (draft/review/ready).
//
// The brief is a *contract, not a generator* (§5g, Non-goals): it writes no
// files and produces no component code. It is purely advisory context returned
// in the ToolResult. This handler therefore does no I/O itself beyond the
// Gate-1 precheck; every read lives in the pure, deterministic core
// design.BuildScreenBrief (pkg/design/brief.go), which is unit tested without a
// ToolEnv.
//
// An unknown screen_name is a clear NOT-FOUND result (Found=false with
// guidance), never an error: the tool's job is to brief, and "no such screen"
// is reportable context the agent can act on (check the name, run
// design_assets), not a crash.
//
// The tool is pure Go with no browser/vision dependency, but SP-140 invariant 7
// keeps the Phase 4/5 design tools off the WASM roster (only design_assets and
// design_validate ship WASM variants); it is native-only with a nil-returning
// stub in design_brief_handler_js.go and a build-tagged registrar in all.go
// (mirroring design_sync_handler.go / design_export_handler.go).
type designBriefHandler struct{}

func (h *designBriefHandler) Name() string {
	return "design_brief"
}

func (h *designBriefHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_brief",
		Description: "Read the design/ tree for ONE screen and return a structured, " +
			"read-only brief to build it from (SP-140-5 §5g). Run it BEFORE a dev turn " +
			"builds a screen — it is the 'brief whenever building a screen' step of the loop. " +
			"screen_name is the wireframe stem (e.g. `login`); a path like " +
			"`design/wireframes/login.svg` is accepted and reduced to its stem. " +
			"The brief combines, for that screen: " +
			"purpose (the README Screens listing text), " +
			"wireframe path (design/wireframes/<stem>.svg) and whether it exists, " +
			"flows IN and OUT with their triggers (every flow edge touching the screen, " +
			"with the mermaid edge label as the trigger, the far endpoint, and the flow file), " +
			"token paths to consume (the `{group.token}` references the wireframe carries, " +
			"each flagged known/unknown against design/tokens/, plus the token groups " +
			"available in the tree), " +
			"open feedback annotations (the §4d feedback file for the screen with its " +
			"unresolved count and, at full depth, the unresolved notes), and " +
			"status (the README status marker: draft/review/ready). " +
			"depth=`summary` (the default) returns the condensed brief — counts and headings; " +
			"depth=`full` returns the detailed one (annotation notes, the delivered screen " +
			"file path). " +
			"IMPORTANT: this tool WRITES NO FILES. The output is advisory context for the " +
			"implementing agent, returned as text plus a structured ScreenBrief in the " +
			"ToolResult — it is a CONTRACT, NOT A GENERATOR; no component code is produced. " +
			"An unknown screen_name is reported as a clear not-found brief (found=false with " +
			"guidance), never an error. All path resolution runs the file-access precheck " +
			"(Gate-1, SP-140 invariant 7).",
		Parameters: []ParameterDef{
			{
				Name:        "screen_name",
				Type:        "string",
				Required:    true,
				Description: "The screen to brief: its wireframe stem (e.g. `login`). A design asset path (`design/wireframes/login.svg`) is accepted and reduced to the stem.",
			},
			{
				Name:        "depth",
				Type:        "string",
				Required:    false,
				Description: "`summary` (default) returns the condensed brief (counts and headings); `full` returns the detailed brief (open annotation notes, the delivered screen file path).",
			},
		},
		Required: []string{"screen_name"},
	}
}

func (h *designBriefHandler) Validate(args map[string]any) error {
	v, exists := lookupKey(args, "screen_name")
	if !exists || v == nil || strings.TrimSpace(stringArg(args, "screen_name")) == "" {
		return fmt.Errorf("parameter 'screen_name' is required (the wireframe stem, e.g. 'login')")
	}
	if _, ok := v.(string); !ok {
		return fmt.Errorf("parameter 'screen_name' must be a string, got %T", v)
	}
	if d, exists := lookupKey(args, "depth"); exists && d != nil {
		if _, ok := d.(string); !ok {
			return fmt.Errorf("parameter 'depth' must be a string, got %T", d)
		}
	}
	return nil
}

// designBriefOutput is the JSON-friendly structured result of one design_brief
// run. It wraps the pure §5g brief with the run-level provenance (where the run
// looked) so a model reads the shape without re-deriving paths.
type designBriefOutput struct {
	// Brief is the pure §5g screen brief (design.ScreenBrief).
	Brief *design.ScreenBrief `json:"brief"`
	// ScreenName echoes the normalised request.
	ScreenName string `json:"screenName"`
	// Depth is the effective depth (summary|full) after the default is applied.
	Depth string `json:"depth"`
	// DesignTreeExists reports whether the workspace has a design/ tree at all
	// (a missing tree is a reportable state, not an error).
	DesignTreeExists bool `json:"designTreeExists"`
	// WritesNothing restates the §5g contract at the result level: the brief is
	// read-only advisory context and never a generator. Always true.
	WritesNothing bool `json:"writesNothing"`
	// Guidance is the not-found/scaffold helper ("" when the screen was found).
	Guidance string `json:"guidance,omitempty"`
}

func (h *designBriefHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	screenName := stringArg(args, "screen_name")
	if strings.TrimSpace(screenName) == "" {
		msg := "design_brief: `screen_name` is required (the wireframe stem, e.g. `login`)"
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_brief: missing screen_name")
	}
	depth := stringArg(args, "depth")

	// Gate-1 precheck (SP-140 invariant 7): every path this tool resolves is
	// prechecked before any I/O. The tool reads the whole design/ tree for the
	// screen, so the canonical design/ path is what is prechecked.
	gatePath := design.DirName
	if _, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_brief", gatePath); decision == "deny" {
		msg := fmt.Sprintf("design_brief blocked: %s is denied by the active file-access policy", gatePath)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_brief blocked: %s is declared denied", gatePath)
	}

	brief, err := design.BuildScreenBrief(design.ScreenBriefInput{
		Root:       root,
		ScreenName: screenName,
		Depth:      depth,
	})
	if err != nil {
		msg := fmt.Sprintf("design_brief failed: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_brief: %w", err)
	}

	out := designBriefOutput{
		Brief:            brief,
		ScreenName:       brief.ScreenName,
		Depth:            brief.Depth,
		DesignTreeExists: design.FileExists(root),
		WritesNothing:    true,
	}
	if !brief.Found {
		out.Guidance = brief.Guidance
	}

	// The human/agent-readable block leads with the structured summary the core
	// renders; an unknown screen stays a successful (not-found) result.
	return ToolResult{
		Output:        design.RenderScreenBrief(brief),
		StructuredOut: out,
		IsError:       false,
	}, nil
}

func (h *designBriefHandler) Aliases() []string      { return nil }
func (h *designBriefHandler) Timeout() time.Duration { return 60 * time.Second }
func (h *designBriefHandler) MaxResultSize() int     { return 0 }
func (h *designBriefHandler) SafeForParallel() bool  { return true }
func (h *designBriefHandler) Interactive() bool      { return false }

// registerDesignBriefTools registers the design_brief tool. It is pure Go, but
// SP-140 invariant 7 keeps only design_assets and design_validate on the WASM
// roster, so this is registered from a build-tagged registrar (excluded from
// WASM via design_brief_handler_js.go, which returns nil).
func registerDesignBriefTools() []ToolHandler {
	return []ToolHandler{
		&designBriefHandler{},
	}
}
