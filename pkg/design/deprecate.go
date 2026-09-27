package design

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// SP-140-9 §9a retires the wireframe tier: HTML screens
// (design/screens/<stem>.html) are the primary screen format and SVG stays
// where it is good (icons §1f, brand §1d). The spec's cutover was "deprecated
// for one release, then reject"; 9.4 shipped the migration, so the window is
// closed — a legacy wireframe's presence IS the error. The transitional
// info-era comment (every finding severity suppresses the webui health
// strip's "validated clean" state) no longer applies: the tier is gone, and
// a tree that reintroduces it must fail loudly rather than sit in a
// perpetual advisory state. flow_mmd_legacy stays info: external trees'
// hand-authored .mmd flows are still a transitional state (9.4 migrated only
// this tree).
const ruleWireframeDeprecated = "wireframe_deprecated"

// ScreenRelPath returns the canonical primary-screen path for a stem:
// design/screens/<stem>.html (SP-140-9 §9a). Shared by the deprecation
// findings and the flow tooling, which name screens by this path.
func ScreenRelPath(stem string) string {
	return path.Join(DirName, "screens", stem+".html")
}

// wireframeDeprecationFinding is the §9a deprecation finding for one legacy
// wireframe file. Post-9.4 the tier is removed, so this is an error whose
// remedy is the conversion to the named primary screen.
func wireframeDeprecationFinding(relPath string) Finding {
	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".svg")
	return Finding{
		File:     relPath,
		Rule:     ruleWireframeDeprecated,
		Severity: SeverityError,
		Message:  fmt.Sprintf("the wireframe tier is removed (SP-140-9 §9a, item 9.4 migrated it): %s must become the primary screen %s — convert it (design/runtime/base/desktop.html is the starting point); icons/ and brand/ SVGs are unaffected", relPath, ScreenRelPath(stem)),
	}
}

// appendWireframeDeprecations returns findings plus one deprecation finding
// per legacy wireframe path. order is the glob order the walk already
// uses (sorted); each file earns exactly one.
func appendWireframeDeprecations(findings []Finding, relPaths []string) []Finding {
	for _, rel := range relPaths {
		findings = append(findings, wireframeDeprecationFinding(rel))
	}
	return findings
}
