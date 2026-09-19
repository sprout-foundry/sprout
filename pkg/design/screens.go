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
)

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
