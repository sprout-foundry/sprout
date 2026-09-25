package design

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Rule ids for the screens (HTML) validator, SP-140-1 §1i/§1g.
const (
	// ruleScreenExternalRef fires (hard) on a network/CDN resource reference
	// (external script src, link href, or CSS @import/url).
	ruleScreenExternalRef = "screen_external_ref"

	// ruleScreenSlugName fires (hard) when the screen file stem breaks the
	// slug rule shared with wireframes.
	ruleScreenSlugName = "screen_slug_name"

	// ruleScreenDeviceFrame is advisory: a container width that does not
	// match any declared device frame.
	ruleScreenDeviceFrame = "screen_device_frame"

	// SP-143 §143.5 screen-graph rules (the runtime contract the kit's
	// navigation and state machinery depends on).
	//
	// ruleScreenNavTarget (hard): a data-nav `to:<stem>` that resolves to no
	// screen stem in design/screens/ — the runtime would 404 the swap.
	ruleScreenNavTarget = "screen_nav_target"
	// ruleScreenNavFormat (hard): a data-nav value with no parsable
	// to:<stem> segment.
	ruleScreenNavFormat = "screen_nav_format"
	// ruleScreenStateDeclared (hard): a data-state section on a screen whose
	// <html> does not declare that name in data-states — the runtime would
	// hide it (or leave it) forever.
	ruleScreenStateDeclared = "screen_state_declared"

	// SP-143 §143.5 index/runtime rules (the generated-contract checks).
	//
	// ruleScreenIndexDrift (hard): design/generated/screens.json is missing
	// (a screens tier exists) or differs from the derived index — a
	// hand-edit or staleness; the remedy is regeneration, not editing.
	ruleScreenIndexDrift = "screen_index_drift"
	// ruleScreenRuntimeHash (hard): design/runtime/sprout-screens.js is
	// present and its self-zeroing source-hash does not verify — a
	// hand-edited or truncated runtime copy. A missing runtime is info only
	// (old trees stay clean).
	ruleScreenRuntimeHash = "screen_runtime_hash"
	// ruleScreenRuntimeMissing (info): the screens tier has screens but no
	// design/runtime/sprout-screens.js — the kit is not scaffolded yet.
	ruleScreenRuntimeMissing = "screen_runtime_missing"
)

// RuntimeFilename is the screen runtime's fixed asset name under
// design/runtime/ (SP-143 §2).
const RuntimeFilename = "sprout-screens.js"

// runtimeSelfZeroedSourceHash recomputes the runtime's source-hash the way
// its header documents (SP-143 §3): fnv1a64 over the bytes with the 16 hex
// digits of the source-hash line replaced by the zero digest, removing the
// circularity of hashing a file that contains its own hash. Shared by the
// validator (this check) and the runtime stamp pins.
func runtimeSelfZeroedSourceHash(content []byte) string {
	return tokenExportHash(runtimeSourceHashZeroRegexp.ReplaceAll(content,
		[]byte("source-hash: "+TokenExportSourceHashLabel+":0000000000000000")))
}

// runtimeSourceHashZeroRegexp matches the runtime's source-hash value for
// the self-zeroing recompute; submatch 1 is the recorded digest.
var runtimeSourceHashZeroRegexp = regexp.MustCompile(`source-hash: (` + regexp.QuoteMeta(TokenExportSourceHashLabel) + `:[0-9a-f]{16})`)

// runtimeRecordedSourceHash extracts the recorded digest (fnv1a64:<hex>)
// from a runtime copy; "" when the file carries no parsable source-hash line.
func runtimeRecordedSourceHash(content []byte) string {
	m := runtimeSourceHashZeroRegexp.FindSubmatch(content)
	if m == nil || len(m) < 2 {
		return ""
	}
	return string(m[1])
}

// validateRuntimeAsset checks the design/runtime/sprout-screens.js copy when
// present (SP-143 §143.5 rule c): its self-zeroing source-hash must verify.
// A hand-edited or truncated runtime would silently change navigation or
// state behavior on every screen that references it. A missing runtime is
// not an error here — trees predating the kit validate clean — but a
// screens tier with screens and no runtime earns an info pointing at the
// scaffold. The result is never nil.
func validateRuntimeAsset(root string) []Finding {
	findings := []Finding{}
	abs := filepath.Join(root, DirName, RuntimeSubdir, RuntimeFilename)
	data, err := os.ReadFile(abs)
	if err != nil {
		if screensTierHasScreens(root) {
			findings = append(findings, Finding{
				File:     path.Join(DirName, RuntimeSubdir, RuntimeFilename),
				Rule:     ruleScreenRuntimeMissing,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("no %s/%s runtime found; screens will render but not navigate in place (scaffold the screen kit to add it)", RuntimeSubdir, RuntimeFilename),
			})
		}
		return findings
	}

	recorded := runtimeRecordedSourceHash(data)
	if recorded == "" {
		findings = append(findings, Finding{
			File:     path.Join(DirName, RuntimeSubdir, RuntimeFilename),
			Rule:     ruleScreenRuntimeHash,
			Severity: SeverityError,
			Message:  "runtime copy carries no parsable source-hash header; restore the fixed asset (scaffold the screen kit or copy from the skill templates)",
		})
		return findings
	}
	if recorded != runtimeSelfZeroedSourceHash(data) {
		findings = append(findings, Finding{
			File:     path.Join(DirName, RuntimeSubdir, RuntimeFilename),
			Rule:     ruleScreenRuntimeHash,
			Severity: SeverityError,
			Message:  fmt.Sprintf("runtime source-hash does not verify (%s is not the self-zeroed recompute of these bytes); the runtime is a fixed asset — restore it instead of editing it", recorded),
		})
	}
	return findings
}

// screensTierHasScreens reports whether design/screens/ holds at least one
// .html screen.
func screensTierHasScreens(root string) bool {
	matches, err := filepath.Glob(filepath.Join(root, DirName, "screens", "*.html"))
	return err == nil && len(matches) > 0
}

// ValidateScreensIndex runs the SP-143 §143.5 index/runtime checks: the
// screens.json drift (rule d) and the runtime source-hash (rule c). It is a
// whole-tree check (the index is one artifact spanning every screen), called
// from ValidateTree; a tree with no screens tier contributes nothing.
func ValidateScreensIndex(root string) ([]Finding, error) {
	findings := []Finding{}

	sources, err := ScreensIndexSources(root)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return findings, nil
	}

	// Runtime source-hash check (rule c).
	findings = append(findings, validateRuntimeAsset(root)...)

	// Index drift (rule d): derive the current index from the screen bytes
	// and compare against the artifact on disk.
	derived, err := DeriveScreensIndex(root)
	if err != nil {
		return nil, err
	}
	if _, err := RenderScreensIndex(derived); err != nil {
		return nil, err
	}
	indexPath := filepath.Join(root, DirName, GeneratedSubdir, ScreensIndexFilename)
	existing, err := os.ReadFile(indexPath)
	if err != nil {
		findings = append(findings, Finding{
			File:     path.Join(DirName, GeneratedSubdir, ScreensIndexFilename),
			Rule:     ruleScreenIndexDrift,
			Severity: SeverityError,
			Message:  fmt.Sprintf("no design/generated/%s for %d screen(s); the index is generated — run design_export_tokens targets:screens, never hand-write it", ScreensIndexFilename, len(sources)),
		})
		return findings, nil
	}
	parsed, parseErr := ReadScreensIndex(existing)
	switch {
	case parseErr != nil:
		findings = append(findings, Finding{
			File:     path.Join(DirName, GeneratedSubdir, ScreensIndexFilename),
			Rule:     ruleScreenIndexDrift,
			Severity: SeverityError,
			Message:  fmt.Sprintf("design/generated/%s does not parse (%v); it is generated — regenerate with design_export_tokens targets:screens", ScreensIndexFilename, parseErr),
		})
	case derived.SourceHash != parsed.SourceHash || !screensIndexGraphEqual(derived.Screens, parsed.Screens):
		findings = append(findings, Finding{
			File:     path.Join(DirName, GeneratedSubdir, ScreensIndexFilename),
			Rule:     ruleScreenIndexDrift,
			Severity: SeverityError,
			Message:  fmt.Sprintf("design/generated/%s is stale or hand-edited: it does not match the screens' data-attributes (index hash %s, derived %s); regenerate with design_export_tokens targets:screens", ScreensIndexFilename, parsed.SourceHash, derived.SourceHash),
		})
	}
	return findings, nil
}

// screensIndexGraphEqual compares the derived and parsed screen graphs so a
// drift finding can distinguish "stale" from "hand-edited but hash-matching"
// (the latter being a provenance lie the hash alone would miss). Order and
// content must both match; the field sets are fixed by the format.
func screensIndexGraphEqual(derived, parsed []ScreenIndexEntry) bool {
	if len(derived) != len(parsed) {
		return false
	}
	for i := range derived {
		if derived[i].Stem != parsed[i].Stem || derived[i].Device != parsed[i].Device ||
			derived[i].Frame != parsed[i].Frame {
			return false
		}
		if len(derived[i].States) != len(parsed[i].States) {
			return false
		}
		for j := range derived[i].States {
			if derived[i].States[j] != parsed[i].States[j] {
				return false
			}
		}
		if len(derived[i].Nav) != len(parsed[i].Nav) {
			return false
		}
		for j := range derived[i].Nav {
			if derived[i].Nav[j] != parsed[i].Nav[j] {
				return false
			}
		}
	}
	return true
}

// validateScreenIndexGraph runs the SP-143 §143.5 per-screen graph rules
// against one screen: (a) every data-nav `to:<stem>` resolves to a screen
// stem in the tree, and (b) every data-state section is declared in the
// screen's data-states. Both are hard errors: the runtime's in-place swap
// and state toggling silently do nothing useful otherwise. findings is the
// running slice; the result is never nil.
func validateScreenIndexGraph(relPath string, content []byte, allStems map[string]bool) []Finding {
	findings := []Finding{}

	for _, nav := range extractDataNavs(content) {
		if allStems[nav.value] {
			continue
		}
		findings = append(findings, Finding{
			File:     relPath,
			Line:     nav.line,
			Rule:     ruleScreenNavTarget,
			Severity: SeverityError,
			Message:  fmt.Sprintf("data-nav target %q does not resolve to a screen in %s/screens/; add the target screen or fix the stem", nav.value, DirName),
		})
	}
	for _, occ := range screenAttrOccurrences(string(content), "data-nav") {
		if dataNavSpecRe.FindStringSubmatch(occ.value) != nil {
			continue
		}
		findings = append(findings, Finding{
			File:     relPath,
			Line:     occ.line,
			Rule:     ruleScreenNavFormat,
			Severity: SeverityError,
			Message:  fmt.Sprintf("data-nav %q carries no to:<stem>; the runtime cannot navigate without a target", occ.value),
		})
	}

	declared := htmlElementAttrs(content)["data-states"]
	declaredSet := map[string]bool{}
	for _, name := range strings.Split(declared, ",") {
		declaredSet[strings.TrimSpace(name)] = true
	}
	for _, used := range collectUsedStates(content) {
		if declaredSet[used.value] {
			continue
		}
		if declared == "" {
			findings = append(findings, Finding{
				File:     relPath,
				Line:     used.line,
				Rule:     ruleScreenStateDeclared,
				Severity: SeverityError,
				Message:  fmt.Sprintf("data-state %q on a screen whose <html> declares no data-states; declare it (data-states=\"%s\") or drop the section", used.value, used.value),
			})
			continue
		}
		findings = append(findings, Finding{
			File:     relPath,
			Line:     used.line,
			Rule:     ruleScreenStateDeclared,
			Severity: SeverityError,
			Message:  fmt.Sprintf("data-state %q is not declared in this screen's data-states (%q); declare it on <html> or drop the section", used.value, declared),
		})
	}
	return findings
}

// screenAttrValueRe captures the value of a src= or href= attribute, in
// double-, single-, or unquoted form. Submatches: 1 = attribute name,
// 2 = double-quoted value, 3 = single-quoted value, 4 = unquoted value.
var screenAttrValueRe = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

// screenStyleBlockRe captures inline <style>...</style> contents.
var screenStyleBlockRe = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`)

// screenStyleAttrRe captures inline style="" attribute contents.
var screenStyleAttrRe = regexp.MustCompile(`(?i)\bstyle\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// screenImportRe captures a CSS @import (with or without url()). Submatch 2 is
// the URL (neither quote character nor a closing paren appears in it).
var screenImportRe = regexp.MustCompile(`(?i)@import\s+(?:url\(\s*)?(['"]?)([^'");]+)`)

// screenCSSURLRe captures a CSS url(...) value (quoted or bare). Submatch 2 is
// the URL; an optional closing quote may precede the ')'.
var screenCSSURLRe = regexp.MustCompile(`(?i)url\(\s*(['"]?)([^'")]+)['"]?\s*\)`)

// screenWidthRe captures a px width declaration (width or max-width).
var screenWidthRe = regexp.MustCompile(`(?i)(?:^|[;\s{])(?:max-)?width\s*:\s*(\d+)px`)

// ValidateScreensDir validates every design/screens/*.html under root,
// SP-140-1 §1i/§1g. Device frames are read from the design README (when
// present) to enable the advisory width check. A missing or empty screens
// directory yields no findings, not an error. Findings are sorted by file,
// line, rule, message.
func ValidateScreensDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "screens", "*.html")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	if len(matches) == 0 {
		return findings, nil
	}

	var frames []Frame
	if data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName)); err == nil {
		frames, _ = ParseFrames(string(data))
	}

	// SP-143 §143.5 rule (a): data-nav targets resolve against every screen
	// stem in the tree, not just this file's siblings.
	stems := map[string]bool{}
	for _, stem := range assetStems(root, "screens", ".html") {
		stems[stem] = true
	}

	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to %s: %w", match, root, err)
		}
		findings = append(findings, validateScreen(filepath.ToSlash(rel), data, frames)...)
		findings = append(findings, validateScreenIndexGraph(filepath.ToSlash(rel), data, stems)...)
	}
	sortFindings(findings)
	return findings, nil
}

// validateScreen validates one self-contained screen HTML file per SP-140-1
// §1i. Hard checks: no external/network resource references and the slug name
// rule. Advisory: a container width that does not match any declared device
// frame. The result is never nil.
func validateScreen(relPath string, content []byte, frames []Frame) []Finding {
	findings := []Finding{}
	text := string(content)

	// slug name (hard) — shared with the wireframe rule.
	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".html")
	if !frameNameRe.MatchString(stem) {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleScreenSlugName,
			Severity: SeverityError,
			Message:  fmt.Sprintf("screen stem %q must match the slug rule %s", stem, SlugPattern),
		})
	}

	// external resource references (hard): src/href attributes and CSS
	// @import/url(). Workspace-relative and data: references are allowed.
	for _, m := range screenAttrValueRe.FindAllStringSubmatch(text, -1) {
		val := m[2]
		if val == "" {
			val = m[3]
		}
		if val == "" {
			val = m[4]
		}
		if classifyResourceRef(val) == "external" {
			findings = append(findings, Finding{
				File:     relPath,
				Line:     lineOfNeedle(text, m[1]+"=\""+val+"\""),
				Rule:     ruleScreenExternalRef,
				Severity: SeverityError,
				Message:  fmt.Sprintf("external reference %q on %s; screens must be self-contained (use a workspace-relative or data: reference)", val, m[1]),
			})
		}
	}
	// CSS @import/url() — dedup by value so "@import url(x)" (matched by both
	// the import and url() patterns) yields a single finding.
	cssSeen := map[string]bool{}
	for _, css := range screenCSSContexts(text) {
		for _, re := range []*regexp.Regexp{screenImportRe, screenCSSURLRe} {
			for _, m := range re.FindAllStringSubmatch(css, -1) {
				val := m[2]
				if classifyResourceRef(val) == "external" && !cssSeen[val] {
					cssSeen[val] = true
					findings = append(findings, Finding{
						File:     relPath,
						Rule:     ruleScreenExternalRef,
						Severity: SeverityError,
						Message:  fmt.Sprintf("external CSS reference %q; screens must not pull network resources", val),
					})
				}
			}
		}
	}

	// advisory device-frame width check.
	if len(frames) > 0 {
		widths := screenWidths(text)
		if len(widths) > 0 && !anyWidthMatches(widths, frames) {
			names := make([]string, 0, len(frames))
			for _, f := range frames {
				names = append(names, f.Name)
			}
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleScreenDeviceFrame,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("no container width in this screen matches a declared device frame (%s); size the root container to a declared frame", strings.Join(names, ", ")),
			})
		}
	}

	return finalizeScreenFindings(findings)
}

// screenCSSContexts returns the CSS text to scan: every inline <style> block
// and every inline style="" attribute value.
func screenCSSContexts(text string) []string {
	var out []string
	for _, m := range screenStyleBlockRe.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	for _, m := range screenStyleAttrRe.FindAllStringSubmatch(text, -1) {
		v := m[1]
		if v == "" {
			v = m[2]
		}
		out = append(out, v)
	}
	return out
}

// screenWidths extracts the px values of every width/max-width declaration.
func screenWidths(text string) []int {
	var out []int
	for _, m := range screenWidthRe.FindAllStringSubmatch(text, -1) {
		if v, err := strconv.Atoi(m[1]); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// anyWidthMatches reports whether any of widths equals a declared frame width.
func anyWidthMatches(widths []int, frames []Frame) bool {
	for _, f := range frames {
		for _, w := range widths {
			if w == f.Width {
				return true
			}
		}
	}
	return false
}

// lineOfNeedle returns the 1-based line of the first occurrence of needle in
// text (0 when absent) — best-effort line support for findings.
func lineOfNeedle(text, needle string) int {
	if needle == "" {
		return 0
	}
	idx := strings.Index(text, needle)
	if idx < 0 {
		return 0
	}
	return strings.Count(text[:idx], "\n") + 1
}

// finalizeScreenFindings normalizes a screen finding slice: never nil and
// sorted deterministically (file, line, rule, message).
func finalizeScreenFindings(findings []Finding) []Finding {
	if findings == nil {
		findings = []Finding{}
	}
	sortFindings(findings)
	return findings
}
