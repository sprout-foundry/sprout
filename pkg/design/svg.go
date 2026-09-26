package design

// svg.go — the SVG wireframe / component validation entry points:
// ValidateWireframe / ValidateWireframesDir (wireframes) and
// ValidateComponent / ValidateComponentsDir (components), plus the
// resource-attribute helpers. The low-level SVG parsing helpers (tree
// walking, viewBox parsing, offset / data-URI location) live in
// svg_helpers.go.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Rule ids for the SVG wireframe validator, SP-140-1 §1b/§1g. Constants
// rather than literals so findings and future tooling share one spelling.
const (
	// ruleSVGWellformed fires when the document is not well-formed XML.
	ruleSVGWellformed = "svg_wellformed"

	// ruleSVGViewBox fires when the root is not <svg> or its viewBox is
	// missing or holds non-integer values.
	ruleSVGViewBox = "svg_viewbox"

	// ruleSVGSelfContainment fires on <script> elements and on external or
	// local resource references (href/src).
	ruleSVGSelfContainment = "svg_self_containment"

	// ruleSVGSlugName fires when the wireframe file stem breaks the slug rule.
	ruleSVGSlugName = "svg_slug_name"

	// ruleSVGDataNavDangling fires when a data-nav target is not a wireframe stem.
	ruleSVGDataNavDangling = "svg_data_nav_dangling"

	// ruleSVGFrameMatch is advisory: the root viewBox does not match any
	// declared device frame.
	ruleSVGFrameMatch = "svg_frame_match"

	// ruleSVGTextUsage is advisory: the wireframe has no <text> elements.
	ruleSVGTextUsage = "svg_text_usage"

	// ruleSVGStableIDs is advisory: an interactive element (data-nav) lacks
	// a stable id.
	ruleSVGStableIDs = "svg_stable_ids"

	// ruleSVGDataURISize is advisory (warn): an embedded data: URI exceeds
	// the size threshold, so the SVG diff is no longer reviewable and the
	// raster should move to brand/ (SP-140-1 §1h).
	ruleSVGDataURISize = DataURISizeRule
)

// resourceAttrs are the SVG attribute local names that may reference a
// (potentially external) resource. xlink:href decodes to Local "href".
var resourceAttrs = map[string]struct{}{
	"href": {},
	"src":  {},
}

// isResourceAttr reports whether an XML attribute local name is one that may
// reference a resource.
func isResourceAttr(local string) bool {
	_, ok := resourceAttrs[local]
	return ok
}

// classifyResourceRef classifies an href/src value for the self-containment
// check (SP-140-1 §1b). "" means the reference is allowed (empty, an
// in-document fragment, or a data: URI); "external" is a remote scheme;
// "local" is a bare file path that would break self-containment.
func classifyResourceRef(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return ""
	case strings.HasPrefix(v, "#"):
		return ""
	case strings.HasPrefix(v, "data:"):
		return ""
	case strings.Contains(v, "://"), strings.HasPrefix(v, "//"):
		return "external"
	default:
		return "local"
	}
}

// ValidateWireframe validates one SVG wireframe per SP-140-1 §1b. relPath is
// the design-relative path (e.g. "design/wireframes/login.svg"); knownStems
// is the set of all wireframe file stems (without .svg) used to resolve
// data-nav targets; frames (optional) enables the advisory frame-match check
// and is nil for runs without declared device frames.
//
// Hard checks (SeverityError): XML well-formedness, a root <svg> with an
// integer viewBox, self-containment (no <script>, no external/local
// resource refs), the slug name rule, and data-nav targets resolving to a
// wireframe stem. Advisory checks (SeverityInfo): frame match, <text>
// usage, and stable ids on interactive elements. The result is never nil.
func ValidateWireframe(relPath string, content []byte, knownStems []string, frames []Frame) []Finding {
	stemSet := make(map[string]struct{}, len(knownStems))
	for _, s := range knownStems {
		stemSet[s] = struct{}{}
	}

	var findings []Finding

	// slug name rule (hard) — the file stem must match the shared slug rule.
	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".svg")
	if !frameNameRe.MatchString(stem) {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGSlugName,
			Severity: SeverityError,
			Message:  fmt.Sprintf("wireframe stem %q must match the slug rule %s", stem, SlugPattern),
		})
	}

	walk := walkSVG(content)

	if walk.error != nil {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     walk.errorLine,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  fmt.Sprintf("not well-formed XML: %v", walk.error),
		})
		return finalizeWireframeFindings(findings)
	}

	if !walk.sawRoot {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  "missing root element; a wireframe must be a single <svg> document",
		})
		return finalizeWireframeFindings(findings)
	}

	// root <svg> + integer viewBox (hard).
	switch {
	case !walk.rootIsSVG:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("root element is <%s>, expected <svg>", walk.rootName),
		})
	case walk.viewBoxMissing:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  "root <svg> is missing the viewBox attribute",
		})
	case !walk.viewBoxAllInt:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("viewBox %q must hold four integer values (minX minY width height)", walk.viewBoxRaw),
		})
	}

	// self-containment (hard): no <script>, no external/local resource refs.
	scriptOff := 0
	for range walk.scripts {
		off := findTagOffset(content, "script", scriptOff)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  "wireframes must be self-contained: remove <script>",
		})
		if off >= 0 {
			scriptOff = off + 1
		}
	}
	for _, r := range walk.resourceRefs {
		verdict := classifyResourceRef(r.value)
		if verdict == "" {
			continue
		}
		off := findAttrValueOffset(content, r.attr, r.value, 0)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  fmt.Sprintf("self-containment: %s reference %q in <%s> breaks self-containment (embed a data: URI or reference from brand/)", verdict, r.value, r.tag),
		})
	}

	// data-nav targets must resolve to a wireframe stem (hard); interactive
	// elements without a stable id are advisory.
	for _, d := range walk.dataNavs {
		off := findAttrValueOffset(content, "data-nav", d.value, 0)
		line := lineOfOffset(content, off)
		if _, ok := stemSet[d.value]; !ok {
			findings = append(findings, Finding{
				File:     relPath,
				Line:     line,
				Rule:     ruleSVGDataNavDangling,
				Severity: SeverityError,
				Message:  fmt.Sprintf("data-nav target %q does not match any wireframe stem", d.value),
			})
		}
		if d.value != "" && d.id == "" {
			findings = append(findings, Finding{
				File:     relPath,
				Line:     line,
				Rule:     ruleSVGStableIDs,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("interactive element with data-nav=%q has no stable id attribute", d.value),
			})
		}
	}

	// <text> usage (advisory): a screen wireframe should keep text as <text>.
	if !walk.hasText {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGTextUsage,
			Severity: SeverityInfo,
			Message:  "wireframe contains no <text> elements; keep text as <text> so it stays greppable and accessible",
		})
	}

	// frame match (advisory): the root viewBox W/H should match a declared
	// device frame. Skipped when no frames are declared or the viewBox is
	// absent / non-integer.
	if len(frames) > 0 && walk.rootIsSVG && !walk.viewBoxMissing && walk.viewBoxAllInt {
		if !frameMatches(walk.viewBoxW, walk.viewBoxH, frames) {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleSVGFrameMatch,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("viewBox %dx%d does not match any declared device frame", walk.viewBoxW, walk.viewBoxH),
			})
		}
	}

	// embedded data URI size (advisory warn, SP-140-1 §1h binary hygiene).
	findings = append(findings, ValidateDataURISizes(relPath, content)...)

	// literal fill/stroke/font-family values not backed by a {token.path}
	// comment (advisory info, SP-140-4 §4b "Token usage"): wireframes are
	// low-fidelity drafts so literals are allowed, but they are tracked for sync.
	findings = append(findings, validateTokenUsage(relPath, content)...)

	return finalizeWireframeFindings(findings)
}

// ValidateWireframesDir validates every design/wireframes/*.svg under root,
// SP-140-1 §1b/§1g. All wireframe stems are gathered first so data-nav
// targets resolve across files; device frames are read from the design
// README (when present) to enable the advisory frame-match check. A missing
// or empty wireframes directory yields no findings, not an error — a
// whole-tree validator run must not fail on workspaces without wireframes.
//
// Files are validated in glob order; the combined findings are sorted by
// file, line, rule, message. Errors are I/O failures only — per-file rule
// violations are findings, not errors.
func ValidateWireframesDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "wireframes", "*.svg")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	if len(matches) == 0 {
		return findings, nil
	}

	// Gather the full stem set so data-nav targets resolve across files.
	stems := make([]string, 0, len(matches))
	for _, m := range matches {
		stems = append(stems, strings.TrimSuffix(path.Base(filepath.ToSlash(m)), ".svg"))
	}

	// Device frames from the design README (advisory frame-match only).
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
		findings = append(findings, ValidateWireframe(filepath.ToSlash(rel), data, stems, frames)...)
	}
	sortFindings(findings)
	return findings, nil
}

// ValidateComponent validates one design/components/*.svg component spec,
// SP-140-1 §1b applied to the component tier: a self-contained, low-fidelity
// SVG that shows one reusable UI component in its key variants and states.
// relPath is the design-relative path (e.g. "design/components/button.svg").
//
// Components are the composable layer beneath screens: a screen wireframe
// is a composition of components, so a component spec carries no navigation
// semantics (no data-nav check) and its viewBox is the component's bounding
// box, not a device frame (no frame-match check). Everything else the
// wireframe contract requires applies unchanged: well-formed XML, a root
// <svg> with an integer viewBox, self-containment, the slug name rule,
// <text> labels (advisory), data-URI size (advisory), and token-usage
// comments (advisory).
//
// Hard checks (SeverityError): well-formedness, root/viewBox, self-
// containment, slug name. Advisory (info/warn): text usage, data-URI size,
// token usage. The result is never nil.
func ValidateComponent(relPath string, content []byte) []Finding {
	findings := []Finding{}

	// slug name rule (hard) — the file stem must match the shared slug rule.
	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".svg")
	if !frameNameRe.MatchString(stem) {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGSlugName,
			Severity: SeverityError,
			Message:  fmt.Sprintf("component stem %q must match the slug rule %s", stem, SlugPattern),
		})
	}

	walk := walkSVG(content)
	if walk.error != nil {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     walk.errorLine,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  fmt.Sprintf("not well-formed XML: %v", walk.error),
		})
		return finalizeWireframeFindings(findings)
	}
	if !walk.sawRoot {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  "missing root element; a component spec must be a single <svg> document",
		})
		return finalizeWireframeFindings(findings)
	}

	// root <svg> + integer viewBox (hard).
	switch {
	case !walk.rootIsSVG:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("root element is <%s>, expected <svg>", walk.rootName),
		})
	case walk.viewBoxMissing:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  "root <svg> is missing the viewBox attribute",
		})
	case !walk.viewBoxAllInt:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("viewBox %q must hold four integer values (minX minY width height)", walk.viewBoxRaw),
		})
	}

	// self-containment (hard): no <script>, no external/local resource refs.
	scriptOff := 0
	for range walk.scripts {
		off := findTagOffset(content, "script", scriptOff)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  "component specs must be self-contained: remove <script>",
		})
		if off >= 0 {
			scriptOff = off + 1
		}
	}
	for _, r := range walk.resourceRefs {
		verdict := classifyResourceRef(r.value)
		if verdict == "" {
			continue
		}
		off := findAttrValueOffset(content, r.attr, r.value, 0)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  fmt.Sprintf("self-containment: %s reference %q in <%s> breaks self-containment (embed a data: URI or reference from brand/)", verdict, r.value, r.tag),
		})
	}

	// <text> usage (advisory): variant and state labels should stay greppable.
	if !walk.hasText {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGTextUsage,
			Severity: SeverityInfo,
			Message:  "component spec contains no <text> elements; label variants and states with <text> so they stay greppable and accessible",
		})
	}

	// embedded data URI size (advisory warn, SP-140-1 §1h binary hygiene).
	findings = append(findings, ValidateDataURISizes(relPath, content)...)

	// literal fill/stroke/font-family values not backed by a {token.path}
	// comment (advisory info, SP-140-4 §4b "Token usage").
	findings = append(findings, validateTokenUsage(relPath, content)...)

	return finalizeWireframeFindings(findings)
}

// ValidateComponentsDir validates every design/components/*.svg under root.
// A missing or empty components directory yields no findings, not an error —
// a whole-tree validator run must not fail on workspaces without a component
// tier. Findings are sorted by file, line, rule, message; errors are I/O
// failures only.
func ValidateComponentsDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "components", "*.svg")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	if len(matches) == 0 {
		return findings, nil
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
		findings = append(findings, ValidateComponent(filepath.ToSlash(rel), data)...)
	}
	sortFindings(findings)
	return findings, nil
}
