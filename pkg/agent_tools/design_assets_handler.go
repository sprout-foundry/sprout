package tools

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// designAssetsHandler implements ToolHandler for the design_assets tool
// (SP-140-2 §2c). It is the inventory + context entry point for the design/
// tree: a structured JSON summary (manifest, per-asset rows, token group
// counts, flow node/edge counts, validator findings) plus a workspace with no
// design/ returns an explicit {exists: false} result with scaffold guidance so
// the model never hallucinates a tree.
//
// Pure Go with no browser or vision dependencies, so it compiles on WASM and
// native builds alike and lives in the shared AllTools list rather than a
// build-tagged registration (matching design_validate). On WASM the file reads
// go through the same os.ReadDir path the rest of the tool layer uses.
type designAssetsHandler struct{}

func (h *designAssetsHandler) Name() string {
	return "design_assets"
}

func (h *designAssetsHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_assets",
		Description: "Inventory the design/ workspace tree (SP-140): a manifest summary " +
			"(frames, status markers), per-asset rows {path, kind, name, status?, summary?}, " +
			"token group counts, flow node/edge counts, and the validator findings from " +
			"design_validate (advisory, non-blocking). " +
			"Use this to see what exists before extending a design tree rather than guessing. " +
			"On a workspace with no design/ directory it returns {exists: false} plus scaffold " +
			"guidance (use the design-system skill) instead of fabricating a tree.",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    false,
				Description: "Optional design subtree to inventory (e.g. `design/wireframes` or `design/tokens`), relative to the workspace root. Omit to inventory the whole `design/` tree.",
			},
			{
				Name:        "format",
				Type:        "string",
				Required:    false,
				Description: "Optional asset-kind filter: one of manifest, token, brand, icon, wireframe, screen, flow, feedback. Omit to return every asset.",
			},
		},
		Required: nil,
	}
}

func (h *designAssetsHandler) Validate(args map[string]any) error {
	if p, exists := lookupKey(args, "path"); exists && p != nil {
		if _, ok := p.(string); !ok {
			return fmt.Errorf("parameter 'path' must be a string, got %T", p)
		}
	}
	if f, exists := lookupKey(args, "format"); exists && f != nil {
		if _, ok := f.(string); !ok {
			return fmt.Errorf("parameter 'format' must be a string, got %T", f)
		}
	}
	return nil
}

func (h *designAssetsHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	subtree := ""
	if raw, exists := lookupKey(args, "path"); exists && raw != nil {
		if s, ok := raw.(string); ok {
			subtree = strings.TrimSpace(s)
		}
	}

	formatFilter := normaliseFormatFilter(stringArg(args, "format"))

	// Gate-1 precheck (SP-140 invariant 7): every workspace path this tool
	// touches is prechecked, mirroring design_validate and
	// analyze_ui_screenshot. The path is the requested subtree when one was
	// supplied; otherwise the tool reads the whole design/ tree, so the
	// canonical design/ path is prechecked.
	gatePath := subtree
	if gatePath == "" {
		gatePath = design.DirName
	}
	preRes, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_assets", gatePath)
	if decision == "deny" {
		msg := fmt.Sprintf("design_assets blocked: %s is denied by the active file-access policy", gatePath)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_assets blocked: %s is declared denied", gatePath)
	}
	if subtree != "" && decision == "allow" && preRes != "" {
		// Map the resolved absolute path back to a workspace-relative slash
		// path when it sits under the workspace root.
		if rel, relErr := filepath.Rel(root, preRes); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			subtree = filepath.ToSlash(rel)
		}
	}

	// No design/ tree: explicit {exists: false} + scaffold guidance so the
	// model never invents a tree.
	if !design.FileExists(root) {
		out := designMissingOutput(subtree, formatFilter)
		return ToolResult{
			Output:        renderDesignAssetsSummary(out),
			StructuredOut: out,
			IsError:       false,
		}, nil
	}

	// A subtree outside design/ is a usage error — the tool inventories the
	// design/ tree.
	if subtree != "" && !isDesignSubtree(subtree) {
		msg := fmt.Sprintf("design_assets: %q is not under design/; pass a subtree like design/wireframes or omit path for the whole tree", subtree)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_assets: %s is not under design/", subtree)
	}

	inv, err := design.Scan(root)
	if err != nil {
		msg := fmt.Sprintf("design_assets failed: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_assets: %w", err)
	}

	out := buildDesignAssetsOutput(subtree, formatFilter, inv)
	return ToolResult{
		Output:        renderDesignAssetsSummary(out),
		StructuredOut: out,
		IsError:       false,
	}, nil
}

// designAssetsOutput is the JSON-friendly structured result of one
// design_assets run. Exists is false for a workspace without design/, in which
// case the counters are zero and Guidance carries the scaffold instructions.
type designAssetsOutput struct {
	Exists bool `json:"exists"`

	// Path is the subtree inventoried ("" for the whole design/ tree).
	Path string `json:"path,omitempty"`
	// Format is the active asset-kind filter ("" for none).
	Format string `json:"format,omitempty"`

	Manifest    design.ManifestSummary   `json:"manifest"`
	Assets      []design.AssetRow        `json:"assets"`
	TokenGroups []design.TokenGroupCount `json:"tokenGroups"`
	Flows       []design.FlowCounts      `json:"flows"`

	Findings   []findingOut   `json:"findings"`
	BySeverity map[string]int `json:"bySeverity"`

	// Guidance is the scaffold text for a missing design/ tree ("" otherwise).
	Guidance string `json:"guidance,omitempty"`
}

// designMissingOutput builds the {exists: false} result plus scaffold guidance
// for a workspace without a design/ tree.
func designMissingOutput(subtree, formatFilter string) designAssetsOutput {
	return designAssetsOutput{
		Exists:      false,
		Path:        subtree,
		Format:      formatFilter,
		Manifest:    design.ManifestSummary{Frames: []design.Frame{}, Status: []string{}},
		Assets:      []design.AssetRow{},
		TokenGroups: []design.TokenGroupCount{},
		Flows:       []design.FlowCounts{},
		Findings:    []findingOut{},
		BySeverity:  map[string]int{"error": 0, "warn": 0, "info": 0, "fix": 0},
		Guidance: "No design/ directory found. To start a design workspace, " +
			"activate the design-system skill and scaffold the tree in this order: " +
			"design/README.md (manifest: purpose, status markers draft/review/ready, " +
			"a frames: block), then design/tokens/ (W3C DTCG *.tokens.json), then " +
			"design/wireframes/ (one SVG per screen), then design/flows/ (mermaid .mmd), " +
			"then design/screens/ (self-contained HTML). Run design_validate after each step.",
	}
}

// buildDesignAssetsOutput filters the scanned inventory by the optional
// subtree and format filter and converts the findings to the shared
// findingOut shape.
func buildDesignAssetsOutput(subtree, formatFilter string, inv *design.Inventory) designAssetsOutput {
	out := designAssetsOutput{
		Exists:      true,
		Path:        subtree,
		Format:      formatFilter,
		Manifest:    inv.Manifest,
		Assets:      filterAssets(inv.Assets, subtree, formatFilter),
		TokenGroups: inv.TokenGroups,
		Flows:       inv.Flows,
		BySeverity:  map[string]int{"error": 0, "warn": 0, "info": 0, "fix": 0},
	}

	// A subtree narrows token groups and flows to the matching class. The
	// design/ root and the canonical design/tokens, design/flows subtree
	// paths keep their respective counts; any other subtree drops them.
	if subtree != "" {
		sub := strings.TrimSuffix(filepath.ToSlash(path.Clean(subtree)), "/")
		atRoot := sub == design.DirName
		if !atRoot && !strings.HasSuffix(sub, "/"+design.TokenSubdir) {
			out.TokenGroups = []design.TokenGroupCount{}
		}
		if !atRoot && !strings.HasSuffix(sub, "/"+design.FlowSubdir) {
			out.Flows = []design.FlowCounts{}
		}
	}
	if formatFilter != "" {
		if formatFilter != design.KindToken {
			out.TokenGroups = []design.TokenGroupCount{}
		}
		if formatFilter != design.KindFlow {
			out.Flows = []design.FlowCounts{}
		}
	}

	// Findings are always reported whole-tree (they are the validator's
	// view, not the subtree's). Tally them for the summary line.
	out.Findings = make([]findingOut, 0, len(inv.Findings))
	for _, f := range inv.Findings {
		out.Findings = append(out.Findings, findingOut{
			File:     f.File,
			Line:     f.Line,
			Severity: f.Severity.String(),
			Message:  f.Message,
			Rule:     f.Rule,
		})
		out.BySeverity[f.Severity.String()]++
	}
	return out
}

// filterAssets applies the optional subtree and format filters to the asset
// rows. Both filters are ANDed; an empty filter is a no-op. The result is
// always non-nil (deterministically ordered by Scan).
func filterAssets(rows []design.AssetRow, subtree, formatFilter string) []design.AssetRow {
	out := make([]design.AssetRow, 0, len(rows))
	prefix := ""
	if subtree != "" {
		prefix = strings.TrimSuffix(filepath.ToSlash(path.Clean(subtree)), "/")
	}
	for _, r := range rows {
		if prefix != "" && r.Path != prefix && !strings.HasPrefix(r.Path, prefix+"/") {
			continue
		}
		if formatFilter != "" && r.Kind != formatFilter {
			continue
		}
		out = append(out, r)
	}
	return out
}

// renderDesignAssetsSummary builds the human-readable summary line for the
// structured result. It always names counts so the model can read the shape
// without parsing the JSON.
func renderDesignAssetsSummary(out designAssetsOutput) string {
	if !out.Exists {
		return "design_assets: no design/ directory — " + out.Guidance
	}

	var sb strings.Builder
	scope := "design/"
	if out.Path != "" {
		scope = out.Path
	}
	fmt.Fprintf(&sb, "design_assets: %s — %d asset(s), %d token group(s), %d flow(s)",
		scope, len(out.Assets), len(out.TokenGroups), len(out.Flows))
	if out.Format != "" {
		fmt.Fprintf(&sb, " (format=%s)", out.Format)
	}
	sb.WriteString(".")

	if len(out.Findings) > 0 {
		parts := []string{}
		for _, sev := range []string{"error", "warn", "info", "fix"} {
			if n := out.BySeverity[sev]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, sev))
			}
		}
		fmt.Fprintf(&sb, " Findings: %d (%s).", len(out.Findings), strings.Join(parts, ", "))
	} else {
		sb.WriteString(" No validator findings.")
	}
	return sb.String()
}

// isDesignSubtree reports whether a slash path names the design/ root or a
// path under it.
func isDesignSubtree(p string) bool {
	clean := strings.TrimSuffix(filepath.ToSlash(path.Clean(strings.TrimSpace(p))), "/")
	return clean == design.DirName || strings.HasPrefix(clean, design.DirName+"/")
}

// normaliseFormatFilter lowercases and trims a format filter, mapping the
// plural subdirectory names the model may pass (wireframes, tokens, flows)
// to their singular asset-kind form.
func normaliseFormatFilter(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	switch f {
	case "":
		return ""
	case "tokens", "token":
		return design.KindToken
	case "brand":
		return design.KindBrand
	case "icons", "icon":
		return design.KindIcon
	case "wireframes", "wireframe":
		return design.KindWireframe
	case "screens", "screen":
		return design.KindScreen
	case "flows", "flow":
		return design.KindFlow
	case "feedback":
		return design.KindFeedback
	case "manifest":
		return design.KindManifest
	default:
		return f
	}
}

// stringArg extracts a string argument by key, returning "" when absent or of
// the wrong type.
func stringArg(args map[string]any, key string) string {
	if v, exists := lookupKey(args, key); exists && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (h *designAssetsHandler) Aliases() []string      { return nil }
func (h *designAssetsHandler) Timeout() time.Duration { return 60 * time.Second }
func (h *designAssetsHandler) MaxResultSize() int     { return 0 }
func (h *designAssetsHandler) SafeForParallel() bool  { return true }
func (h *designAssetsHandler) Interactive() bool      { return false }
