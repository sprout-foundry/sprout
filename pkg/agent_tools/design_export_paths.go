//go:build !js

// design_export_paths.go — output-path confinement + artifact-relocation helpers
// for the design_export_tokens handler, split from design_export_handler.go.
// These pure helpers keep every generated artifact inside design/ (logical
// prefix + symlink-escape checks) and rewrite the default
// design/generated/<file> paths into the requested out_dir.
package tools

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/design"
)

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
