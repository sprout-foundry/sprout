package tools

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	stdsort "sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/design"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// designArtifactDirName is the derived-artifact cache root, relative to the
// design/ tree (SP-140 invariant 2 + SP-140-1 §1h: .gitignore carries
// `design/.cache/`, so everything here is regenerable and never a source).
const designArtifactDirName = ".cache"

// designRenderArtifactDirName is the subdirectory rendered PNGs land in
// (§4a: "cache under design/.cache/renders/").
const designRenderArtifactDirName = "renders"

// designCritiqueStemSeparator joins a target stem to the comparison stem in a
// delta-review artifact name (login~home.png). `~` cannot appear in a design
// slug (design.SlugPattern), so the composed stem stays unambiguous.
const designCritiqueStemSeparator = "~"

// designCritiqueDefaultMaxScreens is the §4e whole-tree cost cap: one
// design_critique run over the whole design/ tree may rasterize (and, with a
// vision tier, critique) at most this many screens, so a single turn cannot
// fire one vision call per file in a large tree. It is a *tool argument*
// (max_screens) with this default, never a CLI flag — the cap has to travel
// with the call that spends the budget.
//
// The spec's "cap at 20 screens per run" is this number; the cap applies to
// the whole-tree target only, because a critique the caller narrowed to one
// screen is already the narrow critique the notice tells them to run.
const designCritiqueDefaultMaxScreens = 20

// designCritiqueCapNoticePrefix opens the explicit notice appended to a capped
// run's Note (and therefore its human-readable summary). It names the cap, the
// number of targets left uncovered, and the remedy, so a truncated critique is
// never silently mistaken for a complete one (§4e "with explicit notice").
const designCritiqueCapNoticePrefix = "critique capped at "

// critiqueTarget is one renderable unit of the critique: where it came from
// (Source, workspace-relative, "" for a synthesized target), what it is
// (Kind/Screen/Stage), and which file the browser rasterizes (RenderSource).
type critiqueTarget struct {
	// Label is the workspace-relative path the finding's `target` field
	// carries. It is Source when the caller named a file, and the source the
	// target was derived from when they named a slug or the tree.
	Label string
	// Source is the workspace-relative path the target was derived from ("").
	Source string
	// RenderSource is the local file handed to the browser.
	RenderSource string
	// Kind is the classifyDesignSource verdict (browser / mermaid).
	Kind designRenderKind
	// Screen is the screen slug when the target is a screen ("" otherwise).
	Screen string
	// Stage names which critique step this target belongs to:
	// "target", "compare", or "tree".
	Stage string
	// CompareLabel is the baseline target label a comparison target is
	// reviewed against ("" unless Stage == "compare"). It sets the artifact
	// name so a target and its comparison never collide.
	CompareLabel string
}

// discoverCritiqueTargets resolves the target argument to the concrete set of
// renderable units. Accepted forms:
//
//   - a workspace-relative source path (design/wireframes/login.svg, …),
//   - a bare slug (login → the wireframe, and its screen when one exists),
//   - a design/ subtree (design/screens → every screen in it),
//   - the design root or "design/" ("whole tree" per §4a).
//
// A target that resolves to nothing is a usage error (tool failure), never an
// empty success — a critique that silently critiqued nothing is worse than no
// critique.
//
// Every directory this walk reads is prechecked first (SP-140 invariant 7),
// because a tree/subtree target enumerates paths the caller never named.
func discoverCritiqueTargets(ctx context.Context, env ToolEnv, requested string) ([]critiqueTarget, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	clean := strings.TrimSuffix(filepath.ToSlash(path.Clean(strings.TrimSpace(requested))), "/")

	// Bare design root / whole tree.
	if clean == design.DirName || clean == "." || clean == "" {
		if err := precheckCritiquePath(ctx, env, design.DirName); err != nil {
			return nil, err
		}
		return discoverTreeTargets(root)
	}

	// A design/ subtree that is a canonical subdirectory.
	if sub, ok := designSubdirOf(clean); ok {
		if err := precheckCritiquePath(ctx, env, clean); err != nil {
			return nil, err
		}
		return discoverSubtreeTargets(root, sub)
	}

	// An explicit renderable source path. An absolute path is used as-is: it
	// may be inside the workspace (the common case) or outside it, in which
	// case Gate 1 decides — discovery must not silently rewrite it.
	if isRenderableCritiqueSource(clean) {
		renderSource := filepath.FromSlash(clean)
		if !filepath.IsAbs(renderSource) {
			renderSource = filepath.Join(root, renderSource)
		}
		if _, statErr := os.Stat(renderSource); statErr == nil {
			return []critiqueTarget{{
				Label:        clean,
				Source:       clean,
				RenderSource: renderSource,
				Kind:         classifyDesignSource(clean),
				Screen:       screenSlugFor(clean),
				Stage:        "target",
			}}, nil
		}
		// The path names an extension we render but the file is absent: fall
		// through to slug resolution so `design/wireframes/login.svg` and
		// `login` behave the same way when only one of them exists.
	}

	// A bare slug.
	if isSlug(clean) {
		if t, ok := targetForSlug(root, clean); ok {
			return []critiqueTarget{t}, nil
		}
	}

	return nil, critiqueUsageError(requested)
}

// designSubdirOf reports whether p is exactly a canonical design/ subdirectory
// tracked by the design contract (design/wireframes, design/screens, …).
func designSubdirOf(p string) (string, bool) {
	rest, ok := strings.CutPrefix(p, design.DirName+"/")
	if !ok {
		return "", false
	}
	for _, sub := range design.Subdirs {
		if rest == sub {
			return sub, true
		}
	}
	return "", false
}

// isRenderableCritiqueSource reports whether p carries an extension the render
// helper can rasterize (SVG/HTML directly, .mmd through the mermaid page).
func isRenderableCritiqueSource(p string) bool {
	return classifyDesignSource(p) != renderKindUnknown
}

// screenSlugFor returns the screen slug a design/ source path belongs to, or
// "" when the path is not a screen asset. It prefers the screens/ subdirectory
// (a hi-fi screen), then the wireframes/ one (a wireframe for that screen).
func screenSlugFor(p string) string {
	base := GetBaseName(p)
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	stem := strings.ToLower(base)
	if !isSlug(stem) {
		return ""
	}
	slash := filepath.ToSlash(p)
	switch {
	case strings.HasPrefix(slash, design.DirName+"/screens/"):
		return stem
	case strings.HasPrefix(slash, design.DirName+"/wireframes/"):
		return stem
	default:
		return ""
	}
}

// targetForSlug resolves a bare slug to a critique target, preferring the
// hi-fi screen (design/screens/<slug>.html) over the wireframe
// (design/wireframes/<slug>.svg). Only an existing file is returned.
func targetForSlug(root, slug string) (critiqueTarget, bool) {
	candidates := []struct {
		rel  string
		kind designRenderKind
	}{
		{path.Join(design.DirName, "screens", slug+".html"), renderKindBrowser},
		{path.Join(design.DirName, "screens", slug+".htm"), renderKindBrowser},
		{path.Join(design.DirName, "wireframes", slug+".svg"), renderKindBrowser},
		{path.Join(design.DirName, "flows", slug+".mmd"), renderKindMermaid},
	}
	for _, c := range candidates {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(c.rel))); statErr == nil {
			return critiqueTarget{
				Label:        c.rel,
				RenderSource: filepath.Join(root, filepath.FromSlash(c.rel)),
				Kind:         c.kind,
				Screen:       screenSlugFor(c.rel),
				Stage:        "target",
			}, true
		}
	}
	return critiqueTarget{}, false
}

// discoverSubtreeTargets enumerates every renderable target under a canonical
// design/ subdirectory, in a deterministic order.
//
// Only the wireframes, screens, and flows subdirectories hold renderable
// sources; tokens/brand/icons/feedback either are not renderable as whole
// screens or are documented as other assets, so they return a usage error
// rather than an empty critique.
func discoverSubtreeTargets(root, sub string) ([]critiqueTarget, error) {
	dir := filepath.Join(root, design.DirName, sub)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, critiqueUsageError(path.Join(design.DirName, sub))
	}

	var targets []critiqueTarget
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		rel := path.Join(design.DirName, sub, e.Name())
		if !isRenderableCritiqueSource(rel) {
			continue
		}
		targets = append(targets, critiqueTarget{
			Label:        rel,
			Source:       rel,
			RenderSource: filepath.Join(root, filepath.FromSlash(rel)),
			Kind:         classifyDesignSource(rel),
			Screen:       screenSlugFor(rel),
			Stage:        "tree",
		})
	}
	if len(targets) == 0 {
		return nil, critiqueUsageError(path.Join(design.DirName, sub))
	}
	return targets, nil
}

// discoverTreeTargets enumerates every renderable target in the design/ tree
// across the renderable subdirectories, in contract order and — within a
// directory — lexicographically, so a whole-tree critique is reproducible.
//
// §4a's "whole tree" is the union of the screen-bearing subtrees. A tree with
// no design/ directory at all, or with nothing renderable, is a usage error
// carrying scaffold guidance (the same posture as design_assets' {exists:
// false}).
func discoverTreeTargets(root string) ([]critiqueTarget, error) {
	if !design.FileExists(root) {
		return nil, critiqueUsageError(design.DirName)
	}

	var targets []critiqueTarget
	// Wireframes first, then screens, then flows: the order a design is built
	// in, so a truncated run (item 4.3's cap) reports structure before polish.
	for _, sub := range []string{"wireframes", "screens", "flows"} {
		dir := filepath.Join(root, design.DirName, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // absent subdirectory is normal
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sortStrings(names)
		for _, name := range names {
			rel := path.Join(design.DirName, sub, name)
			if !isRenderableCritiqueSource(rel) {
				continue
			}
			targets = append(targets, critiqueTarget{
				Label:        rel,
				Source:       rel,
				RenderSource: filepath.Join(root, filepath.FromSlash(rel)),
				Kind:         classifyDesignSource(rel),
				Screen:       screenSlugFor(rel),
				Stage:        "tree",
			})
		}
	}
	if len(targets) == 0 {
		return nil, critiqueUsageError(design.DirName)
	}
	return targets, nil
}

// critiqueUsageError builds the shared "nothing to critique here" failure.
func critiqueUsageError(requested string) error {
	msg := fmt.Sprintf("design_critique: target %q resolved to no renderable design target — "+
		"pass a wireframe/screen/flow path, a screen slug, or design/ for the whole tree "+
		"(renderable sources are %s)",
		requested, strings.Join(renderableCritiqueExtensions(), ", "))
	return agenterrors.NewTool("design_critique", msg, nil)
}

// renderableCritiqueExtensions lists the renderable source extensions for the
// usage-error message, sorted for stability.
func renderableCritiqueExtensions() []string {
	return []string{".html/.htm", ".mmd", ".svg"}
}

// sortStrings is sort.Strings kept local so the deterministic ordering rule is
// explicit at each call site (the package sorts in several places).
func sortStrings(s []string) { stdsort.Strings(s) }

// ---------------------------------------------------------------------------
// Gate-1 precheck
// ---------------------------------------------------------------------------

// precheckCritiquePath runs Gate 1 for a caller-supplied path — the requested
// target (or compare_to) before discovery touches it. It shares the deny/prompt
// contract with precheckCritiqueTarget.
func precheckCritiquePath(ctx context.Context, env ToolEnv, p string) error {
	if strings.TrimSpace(p) == "" {
		return nil
	}
	resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_critique", p)
	if decision == "deny" {
		return fmt.Errorf("design_critique blocked: %s is not accessible from this session", p)
	}
	if decision == "prompt" && env.FileAccessPrompter != nil {
		if _, approved := promptForOffWorkspacePath(ctx, env, "design_critique", p, resolvedPath, "read"); !approved {
			return fmt.Errorf("design_critique blocked: off-workspace access to %s was not approved", p)
		}
	}
	return nil
}

// precheckCritiqueTarget runs Gate 1 for one discovered target. The requested
// target is already prechecked by the caller; this covers the children tree
// expansion discovers, so no file is read before it is classified.
func precheckCritiqueTarget(ctx context.Context, env ToolEnv, t critiqueTarget) error {
	// Path relative to the workspace root, which is the form the classifier
	// and the VFS resolver agree on.
	rel := t.Source
	if rel == "" {
		rel = t.Label
	}
	if rel == "" {
		return nil
	}
	return precheckCritiquePath(ctx, env, rel)
}

// ---------------------------------------------------------------------------
// Render + artifact
// ---------------------------------------------------------------------------

// targetIsWholeTree reports whether the requested target names the whole
// design/ tree (the §4e cap's scope). Bare `design`, `design/`, `.`, and the
// empty string all resolve to the tree (see discoverCritiqueTargets).
func targetIsWholeTree(clean string) bool {
	return clean == design.DirName || clean == "." || clean == ""
}

// cleanCritiqueTarget normalizes a requested target the same way
// discoverCritiqueTargets does, so the cap's scope test agrees with discovery.
func cleanCritiqueTarget(requested string) string {
	return strings.TrimSuffix(filepath.ToSlash(path.Clean(strings.TrimSpace(requested))), "/")
}

// critiqueMaxScreensArg extracts the optional max_screens tool argument with
// the §4e default, rejecting a non-integer or non-positive value so a typo
// cannot quietly lower or disable the cost cap.
func critiqueMaxScreensArg(args map[string]any) (int, error) {
	v, exists := lookupKey(args, "max_screens")
	if !exists || v == nil {
		return designCritiqueDefaultMaxScreens, nil
	}
	var n int
	switch t := v.(type) {
	case int:
		n = t
	case int64:
		n = int(t)
	case float64:
		if t != float64(int(t)) {
			return 0, fmt.Errorf("parameter 'max_screens' must be a whole number, got %v", t)
		}
		n = int(t)
	default:
		return 0, fmt.Errorf("parameter 'max_screens' must be an integer, got %T", v)
	}
	if n < 1 {
		return 0, fmt.Errorf("parameter 'max_screens' must be at least 1, got %d", n)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Findings + output
// ---------------------------------------------------------------------------
