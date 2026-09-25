package design

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ValidateTree validates the whole design/ tree under root, SP-140-1 §1g:
// every §1.x whole-tree validator runs (tokens, wireframes, flows, screens,
// icons, brand, README manifest), plus the §1h git contract
// (.gitattributes diff rule, .gitignore cache policy) and the §4d human
// feedback channel (design/feedback/*.json), and the combined
// findings are sorted by file, line, rule, message. root is the workspace
// root (the parent of design/).
//
// A missing or partial design/ tree yields empty findings, not an error —
// a whole-tree run must not fail on workspaces without design assets.
// Errors are real I/O failures only (an unreadable file that matches a
// glob, for example); rule violations are findings, never errors. The
// result is never nil.
func ValidateTree(root string) ([]Finding, error) {
	var errs []error
	findings := []Finding{}

	tokens, err := ValidateTokensDir(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, tokens...)
	}

	wireframes, err := ValidateWireframesDir(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, wireframes...)
	}

	components, err := ValidateComponentsDir(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, components...)
	}

	flows, err := ValidateFlowsDir(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, flows...)
	}

	screens, err := ValidateScreensDir(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, screens...)
	}

	// SP-143 §143.5: the screen runtime's source-hash and the screens.json
	// drift are whole-tree checks (one artifact each, spanning every
	// screen); they run right after the per-screen pass that feeds them.
	index, err := ValidateScreensIndex(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, index...)
	}

	icons, err := ValidateIconsDir(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, icons...)
	}

	findings = append(findings, ValidateBrandDir(root)...)
	findings = append(findings, ValidateManifest(root)...)

	// SP-140-4 §4d: the human feedback channel (design/feedback/*.json) is
	// part of the tree, so the whole-tree run validates it too. The
	// design-system skill's loop starts any `changes-requested` target by
	// reading its feedback file, so a malformed or dangling feedback document
	// is a tree finding like any other.
	feedback, err := ValidateFeedbackDir(root)
	if err != nil {
		errs = append(errs, err)
	} else {
		findings = append(findings, feedback...)
	}

	// SP-140-4 §4b: the flow/wireframe bidirectionality consistency pack
	// (non-terminal flow edges resolve to a wireframe, README screen refs
	// exist) runs last, after the per-artifact validators, so its cross-file
	// findings are picked up by every tree-wide consumer (design_validate and
	// design_critique's static pass).
	findings = append(findings, ValidateConsistency(root)...)

	// SP-140-1 §1h: the design tree's git contract (.gitattributes diff rule,
	// .gitignore cache policy). A workspace without design/ contributes
	// nothing here, so a missing tree stays finding-free.
	findings = append(findings, ValidateGitContract(root)...)

	sortFindings(findings)
	return findings, errors.Join(errs...)
}

// ValidateFile validates one design asset under root, SP-140-1 §1g.
// relPath is workspace-relative (e.g. "design/wireframes/login.svg"); a
// path that does not start with design/ is treated as relative to design/
// (so "wireframes/login.svg" also resolves). The path must stay under
// design/ and the file must exist.
//
// Dispatch is by location + extension: tokens/*.tokens.json, wireframes/*.svg,
// components/*.svg, icons/*.svg, flows/*.mmd, screens/*.html, README.md,
// brand/brand.md, and the repository-level git-contract files .gitattributes
// and .gitignore (§1h). Anything else is an error, not a silent pass. The
// result is never nil.
func ValidateFile(root, relPath string) ([]Finding, error) {
	rel := path.Clean(filepath.ToSlash(strings.TrimSpace(relPath)))
	if rel == "" || rel == "." {
		return nil, fmt.Errorf("design asset path is empty")
	}
	if strings.HasPrefix(rel, "/") {
		return nil, fmt.Errorf("design asset path %q must be relative to the workspace root", relPath)
	}

	// SP-140-1 §1h: the repository-level git-contract files live outside
	// design/ but are legitimate single-file targets.
	if isGitContractRelPath(rel) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("design asset %s: %w", rel, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s is a directory; validate one asset per path argument", rel)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", abs, err)
		}
		if rel == GitContractFile {
			return ValidateGitAttributesContent(rel, data), nil
		}
		return ValidateGitIgnoreContent(rel, data), nil
	}

	if !strings.HasPrefix(rel, DirName+"/") {
		rel = DirName + "/" + rel
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return nil, fmt.Errorf("design asset path %q must stay under %s/", relPath, DirName)
		}
	}

	abs := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("design asset %s: %w", rel, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory; validate one asset per path argument, or the whole %s/ tree when called with no path", rel, DirName)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", abs, err)
	}

	switch {
	case rel == DirName+"/"+ManifestName:
		return ValidateManifest(root), nil

	case rel == DirName+"/brand/brand.md":
		return ValidateBrandFile(rel, data), nil

	case strings.HasPrefix(rel, DirName+"/tokens/") && strings.HasSuffix(rel, ".tokens.json"):
		return ValidateTokens(rel, data), nil

	case strings.HasPrefix(rel, DirName+"/wireframes/") && strings.HasSuffix(rel, ".svg"):
		return ValidateWireframe(rel, data, assetStems(root, "wireframes", ".svg"), manifestFrames(root)), nil

	case strings.HasPrefix(rel, DirName+"/components/") && strings.HasSuffix(rel, ".svg"):
		return ValidateComponent(rel, data), nil

	case strings.HasPrefix(rel, DirName+"/icons/") && strings.HasSuffix(rel, ".svg"):
		isSprite := strings.TrimSuffix(path.Base(rel), ".svg") == iconSpriteName
		return validateIconSVG(rel, data, isSprite), nil

	case strings.HasPrefix(rel, DirName+"/flows/") && strings.HasSuffix(rel, ".mmd"):
		// §1c per-file rules, then the §4b bidirectionality pack so a
		// single-file flow run surfaces its non-terminal edge findings too.
		stems := assetStems(root, "wireframes", ".svg")
		findings := ValidateFlows(rel, data, stems)
		findings = append(findings, flowBidirectionalityFindings(rel, data, stems)...)
		return findings, nil

	case strings.HasPrefix(rel, DirName+"/screens/") && strings.HasSuffix(rel, ".html"):
		// SP-143 §143.5 graph rules run with the per-file rules so a
		// single-file run surfaces nav-target/state findings too.
		findings := validateScreen(rel, data, manifestFrames(root))
		findings = append(findings, validateScreenIndexGraph(rel, data, screenStemSet(root))...)
		return findings, nil

	case strings.HasPrefix(rel, DirName+"/"+RuntimeSubdir+"/"):
		// SP-143 §143.5: the runtime tier is recognized — tool-owned fixed
		// assets, never authored. The runtime's copy is checked by its
		// self-zeroing source-hash; the other assets (chrome.css, the base
		// documents) have no hash contract and validate as present.
		if path.Base(rel) == RuntimeFilename {
			return validateRuntimeAsset(root), nil
		}
		return []Finding{}, nil

	case rel == DirName+"/"+GeneratedSubdir+"/"+ScreensIndexFilename:
		// SP-143 §143.5: the derived index is a validatable artifact of the
		// tree, so a single-file run can check its drift directly.
		if err := validateScreensIndexArtifact(root, data); err != nil {
			return nil, err
		}
		return ValidateScreensIndex(root)

	default:
		return nil, fmt.Errorf("%s is not a recognized design asset (expected a tokens/*.tokens.json, wireframes/*.svg, components/*.svg, icons/*.svg, flows/*.mmd, screens/*.html, runtime/* (SP-143), %s, or brand/brand.md under %s/)",
			rel, ManifestName, DirName)
	}
}

// screenStemSet is the map form of assetStems(root, "screens", ".html") for
// the data-nav target resolution (SP-143 §143.5 rule a).
func screenStemSet(root string) map[string]bool {
	stems := map[string]bool{}
	for _, stem := range assetStems(root, "screens", ".html") {
		stems[stem] = true
	}
	return stems
}

// validateScreensIndexArtifact keeps the single-file screens.json run honest:
// the validated bytes must be the artifact design/generated/ actually holds,
// not a path that merely routes there.
func validateScreensIndexArtifact(root string, data []byte) error {
	existing, err := os.ReadFile(filepath.Join(root, DirName, GeneratedSubdir, ScreensIndexFilename))
	if err != nil {
		return fmt.Errorf("reading %s: %w", ScreensIndexFilename, err)
	}
	if !bytes.Equal(existing, data) {
		return fmt.Errorf("validated content does not match %s on disk", ScreensIndexFilename)
	}
	return nil
}

// manifestFrames reads the device frames declared in the design README so
// single-file runs can run the advisory frame-match checks. A missing README
// or an unparsable frames: block yields nil — those are advisory checks, and
// the README's own problems are reported by ValidateManifest.
func manifestFrames(root string) []Frame {
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return nil
	}
	frames, err := ParseFrames(string(data))
	if err != nil {
		return nil
	}
	return frames
}

// FileExists reports whether the workspace at root has a design/ directory.
// The design_validate handler uses it to distinguish "clean tree" from "no
// design/ at all" when a run yields zero findings.
func FileExists(root string) bool {
	info, err := os.Stat(filepath.Join(root, DirName))
	return err == nil && info.IsDir()
}

// assetStems returns the file stems (name without the extension) of the files
// in root/design/subdir ending in ext, for cross-file reference resolution
// (e.g. data-nav targets and flow node ids resolve against wireframe stems).
// A missing subdirectory yields nil.
func assetStems(root, subdir, ext string) []string {
	dir := filepath.Join(root, DirName, subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var stems []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		stems = append(stems, strings.TrimSuffix(e.Name(), ext))
	}
	return stems
}
