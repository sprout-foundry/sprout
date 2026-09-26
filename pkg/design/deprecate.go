package design

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// SP-140-9 §9a retires the wireframe tier: HTML screens
// (design/screens/<stem>.html) are the primary screen format and SVG stays
// where it is good (icons §1f, brand §1d). The spec's cutover is
// "deprecated warn for one release, then error"; this tree ships the
// deprecation at info instead — every finding (info included) suppresses
// the webui health strip's "validated clean" state, so a warn window would
// hold the whole dogfood surface in a perpetual advisory state for the
// releases 9.4 needs to land the migration. Info keeps the notice visible
// in the same channel (validate tallies, design_validate output, the
// critique static pass) while the pre-migration tree stays clean; 9.4
// migrates the tier and the severity steps up to error.
const ruleWireframeDeprecated = "wireframe_deprecated"

// ScreenRelPath returns the canonical primary-screen path for a stem:
// design/screens/<stem>.html (SP-140-9 §9a). Shared by the deprecation
// notices and the flow tooling, which name screens by this path.
func ScreenRelPath(stem string) string {
	return path.Join(DirName, "screens", stem+".html")
}

// wireframeDeprecationFinding is the §9a deprecation notice for one legacy
// wireframe file. The remedy names the migration item so a reader knows the
// conversion is scheduled work (9.4), not an error to hand-fix now.
func wireframeDeprecationFinding(relPath string) Finding {
	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".svg")
	return Finding{
		File:     relPath,
		Rule:     ruleWireframeDeprecated,
		Severity: SeverityInfo,
		Message:  fmt.Sprintf("the wireframe tier is deprecated (SP-140-9 §9a): %s should become the primary screen %s (item 9.4 migrates the tier); icons/ and brand/ SVGs are unaffected", relPath, ScreenRelPath(stem)),
	}
}

// appendWireframeDeprecations returns findings plus one deprecation notice
// per legacy wireframe path. order is the glob order the walk already
// uses (sorted); each file earns exactly one notice.
func appendWireframeDeprecations(findings []Finding, relPaths []string) []Finding {
	for _, rel := range relPaths {
		findings = append(findings, wireframeDeprecationFinding(rel))
	}
	return findings
}
