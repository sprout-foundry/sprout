//go:build !js

package tools

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// designExportHandler implements ToolHandler for the design_export_tokens tool
// (SP-140-5 §5a). It reads the DTCG token tiers under design/tokens/ and
// writes generated code-side artifacts to design/generated/: CSS custom
// properties, a TypeScript token map, a Tailwind v4 @theme block, and Swift /
// Kotlin constants. Output is deterministic and byte-identical for identical
// inputs.
//
// The DTCG -> text logic lives in pkg/design/export.go (pure, unit testable);
// this handler is the thin ToolEnv-facing wrapper: it resolves arguments,
// runs Gate-1 PrecheckFileAccess on every path it reads or writes, confines
// writes to design/generated/, and reports a structured summary.
//
// The export is pure Go with no browser/vision dependency, but SP-140
// invariant 7 keeps the tool off the WASM roster (only design_assets and
// design_validate ship WASM variants); it is native-only with a nil-returning
// stub in design_export_handler_js.go and a build-tagged registrar in all.go.
type designExportHandler struct{}

func (h *designExportHandler) Name() string {
	return "design_export_tokens"
}

func (h *designExportHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_export_tokens",
		Description: "Export the design tokens (design/tokens/*.tokens.json, W3C DTCG) into " +
			"code-side artifacts under design/generated/: " +
			"`css` → tokens.css (CSS custom properties PLUS the generated utility layer: " +
			".bg-*/.text-*/.border-*, .p-*/.m-*/.gap-*, .font-*/.text-*-size/.text-*-weight, " +
			".rounded-*, .shadow-* — style screens with these, SP-143), `ts` → tokens.ts (typed " +
			"token map + cssVar lookup), `json` → tokens.json (resolved token values + cssVar map " +
			"for JS, e.g. the screen runtime), `tailwind` → tailwind.theme.css (a Tailwind v4 " +
			"@theme block), `swift` → tokens.swift, `kotlin` → tokens.kt. " +
			"Additionally `screens` → screens.json (SP-143): the machine-readable screen graph " +
			"derived from design/screens/*.html data-attributes (per stem: device/frame, declared " +
			"states, data-nav edges with triggers), provenance-hashed over the screen bytes. It is " +
			"NOT part of `all` — request it explicitly (targets:screens) after adding/changing " +
			"screens; the validator flags drift as an error. " +
			"Additionally `flows` (SP-140-9 §9b): regenerate every derived flow export " +
			"design/flows/<name>.mmd from its flow source .json plus the touched screens — the " +
			"same explicit-only rule (targets:flows, never mixed with the token targets, no " +
			"out_dir); the validator flags a stale or hand-edited .mmd as flow_mmd_drift. Flows " +
			"are authored as .json sources; the .mmd is never hand-edited. " +
			"Utilities come only from the known groups (color, space, font/typography, radius, " +
			"shadow); unknown groups stay variables-only. " +
			"Output is deterministic and byte-identical for the same tokens, so re-running the " +
			"export after no token change is a no-op. Every generated file opens with a " +
			"provenance header carrying `source-hash: fnv1a64:<hex>` — a content hash of the " +
			"token inputs (design/tokens/*.tokens.json) — so design/code consistency can be " +
			"verified offline at any checkout by recomputing the hash from those bytes (no " +
			"timestamp, fully deterministic). DTCG aliases emit a var(--target) " +
			"reference in the CSS/Tailwind targets and the resolved value elsewhere. " +
			"If the token tree has a dirty alias graph — a dangling alias referencing a " +
			"nonexistent token, or a cyclic alias — the tool REFUSES to export and reports " +
			"the offending token(s) instead of emitting broken output. " +
			"This is the design→code half of the loop: run it before UI work so the " +
			"implementation consumes the current theme, then let design_sync bring dev-side " +
			"changes back. It only ever writes under design/generated/ — the token sources are " +
			"never modified.",
		Parameters: []ParameterDef{
			{
				Name:        "targets",
				Type:        "string",
				Required:    false,
				Description: "Which exporters to run: `all` (default — the token targets), one target, or a comma-separated list. Targets: css, ts, json, tailwind, swift, kotlin, screens (the SP-143 screens.json index), flows (the SP-140-9 derived flow .mmd exports). screens and flows are explicit only, never in `all`, and each must be requested alone.",
			},
			{
				Name:        "out_dir",
				Type:        "string",
				Required:    false,
				Description: "Optional output directory for generated files, relative to the workspace root. Defaults to `design/generated/`. Must be inside `design/`; the token sources under `design/tokens/` are never written to.",
			},
		},
		Required: nil,
	}
}

func (h *designExportHandler) Validate(args map[string]any) error {
	if v, exists := lookupKey(args, "targets"); exists && v != nil {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("parameter 'targets' must be a string, got %T", v)
		}
	}
	if v, exists := lookupKey(args, "out_dir"); exists && v != nil {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("parameter 'out_dir' must be a string, got %T", v)
		}
	}
	return nil
}

// designExportOutput is the JSON-friendly structured result of one
// design_export_tokens run.
type designExportOutput struct {
	// Exists is false when the workspace has no design/ tree at all, in which
	// case there is nothing to export and Guidance carries scaffold text.
	Exists bool `json:"exists"`
	// TokensPath is the directory the token tiers were read from
	// (design/tokens), slash-separated and workspace-relative.
	TokensPath string `json:"tokensPath"`
	// OutDir is the directory the artifacts were written to, slash-separated
	// and workspace-relative.
	OutDir string `json:"outDir"`
	// TargetCount is the number of exporters run. Redundant with len(Files)
	// but explicit for a model reading the summary.
	TargetCount int `json:"targetCount"`
	// TokenCount is the number of DTCG token leaves exported.
	TokenCount int `json:"tokenCount"`
	// SourceHash is the §5f provenance hash of the token inputs, echoed from
	// the artifacts' headers so a caller can verify the export offline without
	// opening a file. Empty when nothing was exported.
	SourceHash string `json:"sourceHash,omitempty"`
	// Files are the written artifacts, in canonical target order.
	Files []designExportFile `json:"files"`
	// Refused, when non-empty, names the alias violations that made the export
	// refuse (item 5.2). The run is an error; this carries the per-token detail
	// a model reads to fix the graph.
	Refused []designExportViolation `json:"refused,omitempty"`
	// Guidance is the scaffold text for a missing design/ tree ("" otherwise).
	Guidance string `json:"guidance,omitempty"`
}

// designExportViolation is one dangling/cyclic alias in the structured
// refusal. Field names mirror design.Finding so a model reads one vocabulary.
type designExportViolation struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Token   string `json:"token"`
	Kind    string `json:"kind"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// designExportFile is one generated artifact in the structured output.
type designExportFile struct {
	Target      string `json:"target"`
	Path        string `json:"path"`
	Bytes       int    `json:"bytes"`
	ContentHash string `json:"contentHash"`
	// SourceHash is the provenance hash carried in this artifact's header
	// (= the run's SourceHash). Recorded per file so a caller checking one
	// artifact does not have to trust the run-level field.
	SourceHash string `json:"sourceHash"`
}

func (h *designExportHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	// Resolve the requested targets before touching the filesystem: a typo is
	// a usage error, not an empty export.
	targets, err := design.ResolveExportTargets(stringArg(args, "targets"))
	if err != nil {
		msg := fmt.Sprintf("design_export_tokens: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	outDir, err := resolveExportOutDir(root, stringArg(args, "out_dir"))
	if err != nil {
		msg := fmt.Sprintf("design_export_tokens: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	tokensPath := path.Join(design.DirName, design.TokenSubdir)

	// Gate-1 precheck (SP-140 invariant 7): the token source directory and
	// the output directory are both workspace paths this tool reads/writes,
	// so both are prechecked before any I/O. The design/ root is prechecked
	// as well so a workspace-level deny is caught even before the tree probe.
	for _, gate := range []string{design.DirName, tokensPath, outDir} {
		_, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_export_tokens", gate)
		if decision == "deny" {
			msg := fmt.Sprintf("design_export_tokens blocked: %s is denied by the active file-access policy", gate)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens blocked: %s is declared denied", gate)
		}
	}

	// No design/ tree: explicit {exists: false} + scaffold guidance, so the
	// model never invents one (mirrors design_assets).
	if !design.FileExists(root) {
		out := designExportOutput{
			Exists:     false,
			TokensPath: tokensPath,
			OutDir:     filepath.ToSlash(outDir),
			Files:      []designExportFile{},
			Guidance: "No design/ directory found. Scaffold the tree with the design-system " +
				"skill (design/tokens/ first: W3C DTCG *.tokens.json) before exporting tokens.",
		}
		return ToolResult{Output: renderDesignExportSummary(out), StructuredOut: out, IsError: false}, nil
	}

	// SP-143 §143.5: the screens target derives from design/screens/*.html,
	// not the token sources — it may not be mixed with the token targets
	// (a screens-only run must not demand tokens, and a token run must not
	// silently rewrite the screen graph).
	screensOnly := false
	flowsOnly := false
	for _, t := range targets {
		switch t.Name {
		case design.ExportTargetScreens:
			if len(targets) > 1 {
				msg := "design_export_tokens: the screens target derives from design/screens/*.html, not the token sources — request it alone (targets:screens), not mixed with the token targets."
				return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %s target must be requested alone", design.ExportTargetScreens)
			}
			screensOnly = true
		case design.ExportTargetFlows:
			if len(targets) > 1 {
				msg := "design_export_tokens: the flows target derives from design/flows/*.json + the touched screens, not the token sources — request it alone (targets:flows), not mixed with the token targets."
				return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %s target must be requested alone", design.ExportTargetFlows)
			}
			flowsOnly = true
		}
	}
	if flowsOnly {
		return exportFlowsTarget(ctx, env, root, outDir)
	}
	if screensOnly {
		artifact, err := design.RenderScreensIndexArtifact(root)
		if err != nil {
			if errors.Is(err, design.ErrNoScreensForIndex) {
				out := designExportOutput{
					Exists:     true,
					TokensPath: tokensPath,
					OutDir:     filepath.ToSlash(outDir),
					Files:      []designExportFile{},
				}
				msg := "design_export_tokens: no screens found under " + path.Join(design.DirName, "screens") + "/ — add a screen document first."
				return ToolResult{Output: msg, StructuredOut: out, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
			}
			msg := fmt.Sprintf("design_export_tokens failed: %v", err)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
		}
		if _, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_export_tokens", artifact.RelPath); decision == "deny" {
			msg := fmt.Sprintf("design_export_tokens blocked: %s is denied by the active file-access policy", artifact.RelPath)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens blocked: %s is declared denied", artifact.RelPath)
		}
		relocated, err := relocateArtifacts([]design.ExportedArtifact{artifact}, outDir)
		if err != nil {
			msg := fmt.Sprintf("design_export_tokens: %v", err)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
		}
		if err := design.WriteExportedArtifactsAt(root, relocated); err != nil {
			msg := fmt.Sprintf("design_export_tokens failed: %v", err)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
		}
		out := designExportOutput{
			Exists:      true,
			TokensPath:  tokensPath,
			OutDir:      filepath.ToSlash(outDir),
			TargetCount: 1,
			Files:       make([]designExportFile, 0, 1),
		}
		for _, a := range relocated {
			out.Files = append(out.Files, designExportFile{
				Target:      a.Target,
				Path:        a.RelPath,
				Bytes:       len(a.Content),
				ContentHash: a.Hash,
			})
		}
		return ToolResult{Output: renderDesignExportSummary(out), StructuredOut: out, IsError: false}, nil
	}

	// Physical containment (defence in depth beyond the logical design/
	// prefix check): if the output directory — or any existing ancestor of
	// it — is a symlink pointing outside the design tree, refuse before any
	// write. Logical confinement alone would let `design/generated ->
	// /elsewhere` pass the prefix test and write outside the workspace.
	if err := verifyOutputDirPhysical(root, outDir); err != nil {
		msg := fmt.Sprintf("design_export_tokens: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	tokens, err := design.ResolveExportTokens(root)
	if err != nil {
		if err == design.ErrNoTokensForExport {
			out := designExportOutput{
				Exists:     true,
				TokensPath: tokensPath,
				OutDir:     filepath.ToSlash(outDir),
				Files:      []designExportFile{},
			}
			msg := "design_export_tokens: no tokens found under " + tokensPath + "/ — add a W3C DTCG *.tokens.json file first."
			return ToolResult{Output: msg, StructuredOut: out, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
		}
		// Dirty alias graph (item 5.2): the tool refuses rather than emit
		// broken output. Report the per-token violations structured, plus a
		// summary naming the offending tokens, so the model can fix the
		// aliases and re-run. This is a hard failure.
		var dirty *design.DirtyAliasError
		if errors.As(err, &dirty) {
			out := designExportOutput{
				Exists:     true,
				TokensPath: tokensPath,
				OutDir:     filepath.ToSlash(outDir),
				Files:      []designExportFile{},
				Refused:    make([]designExportViolation, 0, len(dirty.Violations)),
			}
			for _, v := range dirty.Violations {
				out.Refused = append(out.Refused, designExportViolation{
					File:    v.Path,
					Line:    v.Line,
					Token:   v.Token,
					Kind:    v.Kind,
					Rule:    v.Rule,
					Message: v.Message,
				})
			}
			msg := "design_export_tokens refused: " + dirty.Error()
			return ToolResult{Output: msg, StructuredOut: out, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
		}
		msg := fmt.Sprintf("design_export_tokens failed: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	// Render every artifact in memory first, so a renderer failure writes
	// nothing (no half-exported theme).
	artifacts, err := design.RenderArtifacts(tokens, targets)
	if err != nil {
		msg := fmt.Sprintf("design_export_tokens failed: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	// The out_dir override relocates the artifacts; the default writes them
	// straight to design/generated/. Rewriting RelPath keeps the confinement
	// check meaningful for an override too.
	relocated, err := relocateArtifacts(artifacts, outDir)
	if err != nil {
		msg := fmt.Sprintf("design_export_tokens: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	// Gate-1 on every concrete output file, then write. Each target path is
	// prechecked individually (not just its directory) so a per-file deny is
	// honoured.
	for _, a := range relocated {
		_, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_export_tokens", a.RelPath)
		if decision == "deny" {
			msg := fmt.Sprintf("design_export_tokens blocked: %s is denied by the active file-access policy", a.RelPath)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens blocked: %s is declared denied", a.RelPath)
		}
	}

	if err := design.WriteExportedArtifactsAt(root, relocated); err != nil {
		msg := fmt.Sprintf("design_export_tokens failed: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	out := designExportOutput{
		Exists:      true,
		TokensPath:  tokensPath,
		OutDir:      filepath.ToSlash(outDir),
		TargetCount: len(relocated),
		TokenCount:  len(tokens.Leaves),
		SourceHash:  tokens.InputHash,
		Files:       make([]designExportFile, 0, len(relocated)),
	}
	for _, a := range relocated {
		out.Files = append(out.Files, designExportFile{
			Target:      a.Target,
			Path:        a.RelPath,
			Bytes:       len(a.Content),
			ContentHash: a.Hash,
			SourceHash:  tokens.InputHash,
		})
	}
	return ToolResult{
		Output:        renderDesignExportSummary(out),
		StructuredOut: out,
		IsError:       false,
	}, nil
}

// resolveExportOutDir validates the optional out_dir override and returns the
// workspace-relative slash path it names. The default is design/generated/. An
// override must sit inside design/ (and may not be the token-source directory
// or anything under it) — export generates, it never rewrites sources.
func resolveExportOutDir(root, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return path.Join(design.DirName, design.GeneratedSubdir), nil
	}
	clean := strings.TrimSuffix(path.Clean(filepath.ToSlash(raw)), "/")
	if clean == "." || clean == "" {
		return "", fmt.Errorf("out_dir must be a directory inside %s/", design.DirName)
	}
	if clean != design.DirName && !strings.HasPrefix(clean, design.DirName+"/") {
		return "", fmt.Errorf("out_dir %q must be inside %s/ (the token sources are never written to)", raw, design.DirName)
	}
	// Refuse design/ itself, design/tokens/, and any other non-generated
	// design subtree: export only ever writes generated artifacts. This keeps
	// `out_dir: design/wireframes` (a foot-gun that would drop tokens.css
	// into the wireframe tier) a usage error.
	sub := strings.TrimPrefix(strings.TrimPrefix(clean, design.DirName), "/")
	if sub == "" {
		return "", fmt.Errorf("out_dir must be a subdirectory of %s/, not %s/ itself", design.DirName, design.DirName)
	}
	// The token tier is named explicitly (not only via design.Subdirs) so a
	// future refactor of Subdirs can never quietly make the source tier a
	// valid output directory. The first segment is what matters: an override
	// into any nested path under a source tier is refused too.
	firstSegment := strings.SplitN(sub, "/", 2)[0]
	if firstSegment == design.TokenSubdir {
		return "", fmt.Errorf("out_dir %q targets the token source tier %s/%s; export writes generated output only", raw, design.DirName, design.TokenSubdir)
	}
	for _, tier := range design.Subdirs {
		if sub == tier || strings.HasPrefix(sub, tier+"/") {
			return "", fmt.Errorf("out_dir %q targets the source tier %s/%s; export writes generated output only", raw, design.DirName, tier)
		}
	}
	return clean, nil
}

// verifyOutputDirPhysical checks that the output directory (locating the
// deepest existing ancestor when it does not yet exist) resolves, after
// symlink evaluation, to a path still inside the workspace's design/ tree. It
// catches the case the logical prefix check cannot: `design/generated` (or any
// ancestor under design/) being a symlink that points outside.
func verifyOutputDirPhysical(root, outDir string) error {
	workspaceRoot := root
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	designRoot := filepath.Join(workspaceRoot, design.DirName)
	realDesignRoot, err := filepath.EvalSymlinks(designRoot)
	if err != nil {
		// design/ is not resolvable (missing/unreadable); the logical checks
		// and the write itself will surface a clearer error. Nothing to
		// verify physically.
		return nil
	}

	target := filepath.Join(workspaceRoot, filepath.FromSlash(outDir))
	realTarget, err := evalSymlinksDeepestAncestor(target)
	if err != nil {
		return fmt.Errorf("cannot resolve output directory %s: %w", outDir, err)
	}

	rel, err := filepath.Rel(realDesignRoot, realTarget)
	if err != nil {
		return fmt.Errorf("output directory %s is not inside %s/", outDir, design.DirName)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("output directory %s resolves outside %s/ (symlink escape refused)", outDir, design.DirName)
	}
	return nil
}

// evalSymlinksDeepestAncestor resolves p with symlinks evaluated, walking up
// to the deepest existing ancestor when p (or part of it) does not exist, then
// re-joining the non-existent tail. This mirrors the resolution strategy Gate-1
// classification uses for paths that may not exist yet.
func evalSymlinksDeepestAncestor(p string) (string, error) {
	clean := filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved, nil
	}
	dir, base := filepath.Split(clean)
	if dir == "" || dir == clean {
		// Reached the filesystem root without an existing ancestor.
		return clean, nil
	}
	resolvedDir, err := evalSymlinksDeepestAncestor(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedDir, base), nil
}

// relocateArtifacts rewrites each artifact's path from the default
// design/generated/<file> into the requested output directory. The paths are
// confined to design/ by resolveExportOutDir before this runs; the check here
// is the belt-and-braces assertion that no artifact escapes.
func relocateArtifacts(artifacts []design.ExportedArtifact, outDir string) ([]design.ExportedArtifact, error) {
	relocated := make([]design.ExportedArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		base := path.Base(a.RelPath)
		rel := path.Join(outDir, base)
		if !isInsideDesign(rel) {
			return nil, fmt.Errorf("refusing to write outside %s/: %s", design.DirName, rel)
		}
		a.RelPath = rel
		relocated = append(relocated, a)
	}
	return relocated, nil
}

// isInsideDesign reports whether a slash path names a path under design/
// (strictly, not the design/ root itself).
func isInsideDesign(rel string) bool {
	clean := strings.TrimSuffix(path.Clean(rel), "/")
	return strings.HasPrefix(clean, design.DirName+"/")
}

// renderDesignExportSummary builds the human-readable one-liner naming the
// files written, so a model reads the result without parsing the JSON.
func renderDesignExportSummary(out designExportOutput) string {
	if !out.Exists {
		return "design_export_tokens: no design/ directory — " + out.Guidance
	}
	if len(out.Files) == 0 {
		return fmt.Sprintf("design_export_tokens: no artifacts written (no tokens under %s/).", out.TokensPath)
	}
	parts := make([]string, 0, len(out.Files))
	// Files are already in canonical target order; keep that order for the
	// summary so two runs with the same target set read identically.
	ordered := append([]designExportFile(nil), out.Files...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	for _, f := range ordered {
		parts = append(parts, fmt.Sprintf("%s (%d bytes)", f.Path, f.Bytes))
	}
	summary := fmt.Sprintf("design_export_tokens: %d token(s) → %d file(s) in %s/: %s.",
		out.TokenCount, out.TargetCount, out.OutDir, strings.Join(parts, ", "))
	if out.SourceHash != "" {
		summary += " source-hash: " + out.SourceHash + "."
	}
	return summary
}

func (h *designExportHandler) Aliases() []string      { return nil }
func (h *designExportHandler) Timeout() time.Duration { return 60 * time.Second }
func (h *designExportHandler) MaxResultSize() int     { return 0 }
func (h *designExportHandler) SafeForParallel() bool  { return false }
func (h *designExportHandler) Interactive() bool      { return false }

// registerDesignExportTools registers the design_export_tokens tool. It is
// pure Go, but SP-140 invariant 7 keeps only design_assets and design_validate
// on the WASM roster, so this is registered from a build-tagged registrar
// (excluded from WASM via design_export_handler_js.go, which returns nil).
func registerDesignExportTools() []ToolHandler {
	return []ToolHandler{
		&designExportHandler{},
	}
}
