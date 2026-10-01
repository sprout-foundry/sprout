//go:build !js

package tools

import (
	"fmt"
	stdsort "sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// staticCritiqueFindings is the non-vision degradation path: it runs the
// existing design validator (the same rule packs design_validate surfaces) over
// the critiqued targets and maps each design.Finding into the critique schema
// {target, area, severity, note, suggestion} with the validator rule id kept
// on the finding (critiqueFinding.Rule) so a consumer can trace it back.
//
// It is deliberately built on the validator that already exists — §4a says
// "degrade to design_validate-style static findings" — so a non-vision primary
// still gets a useful, structured critique (dangling data-nav, missing
// wireframes, literal-vs-token colors, screen inventory, naming) instead of an
// empty result. Items 4.4/4.5 add *new* rule packs to that validator; this
// function picks them up for free, which is why it calls the validator rather
// than re-implementing checks here.
//
// Root is taken from env.WorkspaceRoot exactly as design_validate does. A
// validator I/O error yields no static findings (never an error): the critique
// must not fail the turn, and a target that cannot be statically validated is
// simply reported with no static findings. Findings are filtered to the active
// rubric so a focused pass stays focused, and sorted for determinism.
func staticCritiqueFindings(env ToolEnv, targets []critiqueTarget, rubric string) []critiqueFinding {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	// A whole-tree critique (any "tree" stage) validates the tree once instead
	// of validating each discovered file in turn: the tree validator is the
	// same code design_validate runs with no path, and it resolves
	// cross-file references (data-nav targets, flow node stems, README links)
	// that a single-file validation cannot see.
	treeWide := false
	for _, t := range targets {
		if t.Stage == "tree" {
			treeWide = true
			break
		}
	}

	var designFindings []design.Finding
	if treeWide {
		found, err := design.ValidateTree(root)
		if err != nil {
			// I/O failure inside the validator: degrade to no static findings
			// rather than failing a critique whose render already succeeded.
			return nil
		}
		designFindings = found
	} else {
		for _, t := range targets {
			// The comparison target is a delta input, not a separate critique;
			// its structural problems are not this run's findings.
			if t.Stage == "compare" {
				continue
			}
			rel := t.Label
			if rel == "" {
				rel = t.Source
			}
			if rel == "" {
				continue
			}
			found, err := design.ValidateFile(root, rel)
			if err != nil {
				// A flow (.mmd) is validated as a whole-tree asset by
				// ValidateFile, so it is covered only in the treeWide branch.
				// Any other per-file error is swallowed for the same
				// never-fail reason: advisory findings, not a turn failure.
				continue
			}
			designFindings = append(designFindings, found...)
		}
	}

	// A flow target validates as a tree asset only; when the caller named a
	// single flow, run the tree validator and keep just the flow's findings so
	// a named flow still gets its static checks.
	if !treeWide {
		for _, t := range targets {
			if t.Stage == "compare" || t.Kind != renderKindMermaid {
				continue
			}
			found, err := design.ValidateTree(root)
			if err != nil {
				continue
			}
			if rel := firstNonEmpty(t.Label, t.Source); rel != "" {
				for _, f := range found {
					if f.File == rel {
						designFindings = append(designFindings, f)
					}
				}
			}
			break
		}
	}

	areas := rubricAreas(rubric)
	out := make([]critiqueFinding, 0, len(designFindings))
	seen := make(map[string]bool, len(designFindings))
	for _, f := range designFindings {
		cf := critiqueFindingFromDesign(f)
		if !critiqueAreaInRubric(cf.Area, areas) {
			continue
		}
		// ValidateFile and the flow fallback can both surface a flow finding;
		// dedupe on the tuple a consumer sees.
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", cf.Target, cf.Line, cf.Rule, cf.Note)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, cf)
	}
	stdsort.SliceStable(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Note < out[j].Note
	})
	return out
}

// critiqueFindingFromDesign maps one validator finding
// {file, line?, severity, message, rule} into the critique schema
// {target, area, severity, note, suggestion}. The validator rule id rides on
// the finding so tracing back to the static rule pack is possible.
func critiqueFindingFromDesign(f design.Finding) critiqueFinding {
	note := strings.TrimSpace(f.Message)
	if line := f.Line; line > 0 {
		note = fmt.Sprintf("line %d: %s", line, note)
	}
	return critiqueFinding{
		Target:     f.File,
		Area:       critiqueAreaForRule(f.Rule),
		Severity:   critiqueSeverityForDesign(f.Severity),
		Note:       note,
		Suggestion: critiqueStaticSuggestion(f.Rule),
		Line:       f.Line,
		Rule:       f.Rule,
	}
}

// critiqueSeverityForDesign maps the validator's severity vocabulary onto the
// critique's. The validator's `error` is a hard violation, which is a critique
// `blocker`; `warn` maps to `major`, `fix` (a machine-applicable advisory) to
// `minor`, and `info` stays `info`. An unknown severity degrades to `info`
// rather than being dropped.
func critiqueSeverityForDesign(s design.Severity) string {
	switch s {
	case design.SeverityError:
		return "blocker"
	case design.SeverityWarn:
		return "major"
	case design.SeverityFix:
		return "minor"
	case design.SeverityInfo:
		return "info"
	default:
		return "info"
	}
}

// critiqueAreaForRule maps a statically-known validator rule id into the
// critique vocabulary so a static finding carries a meaningful area. Unknown
// rules (including rule packs items 4.4/4.5 add later) fall back to
// "consistency", which is the honest description of "a design convention was
// violated".
func critiqueAreaForRule(rule string) string {
	switch rule {
	// Hierarchy: structure the user must be able to read top-down —
	// wireframe/frame/shape and the flow structure that frames it.
	case "svg_viewbox", "svg_frame_match", "screen_device_frame",
		"manifest_frames", "svg_wellformed", "flowchart_declaration",
		"flowchart_syntax", "flowchart_node_stem":
		return "hierarchy"

	// Affordance: something a user must be able to act on — an external
	// resource that will not load, an icon that is not drawable, a missing
	// wireframe the flow depends on.
	case "screen_external_ref", "icon_wellformed", "icon_self_containment",
		"icon_sprite_symbol", "svg_self_containment":
		return "affordance"

	// Contrast: the token/colour checks — literal colours and raw hex are
	// the static proxy for "this will not meet the palette / may not
	// contrast".
	case "brand_raw_hex", "brand_no_token_refs", "svg_data_uri_size":
		return "contrast"

	// Everything else is consistency: token membership/aliases, naming
	// (slugs), dangling references (data-nav, manifest links, feedback
	// targets), stable ids, text usage, feedback schema, the git contract.
	default:
		return "consistency"
	}
}

// critiqueAreaInRubric reports whether area is in the rubric's focused set.
func critiqueAreaInRubric(area string, areas []string) bool {
	for _, a := range areas {
		if a == area {
			return true
		}
	}
	return false
}

// critiqueStaticSuggestion is the rule-derived fix guidance attached to a
// static finding. §4a's findings carry a concrete fix; for a static finding the
// concrete fix is the validator's own remedy, which is what makes the
// degradation genuinely useful to a non-vision primary. Unknown rules get no
// suggestion rather than invented guidance.
func critiqueStaticSuggestion(rule string) string {
	switch rule {
	case "svg_data_nav_dangling":
		return "Point data-nav at an existing wireframe stem, or add the missing wireframe."
	case "svg_slug_name", "screen_slug_name", "icon_slug_name":
		return "Rename the file to the design slug form (lowercase, hyphen-separated)."
	case "svg_stable_ids":
		return "Give elements stable, semantic ids so flow targets can reference them."
	case "svg_self_containment":
		return "Inline the referenced asset; wireframes and icons must be self-contained."
	case "screen_external_ref":
		return "Inline the resource so the screen renders without network access."
	case "manifest_link_dangling":
		return "Fix the link to an existing design asset, or add the file it names."
	case "manifest_frames":
		return "Add a frames: block naming each device frame as name: WxH."
	case "flowchart_node_stem":
		return "Rename the flow node to match an existing wireframe stem."
	case "consistency_flow_edge_wireframe":
		return "Add the missing wireframe for the flow node, or mark the node terminal (no outgoing edge)."
	case "flowchart_declaration", "flowchart_syntax":
		return "Fix the mermaid declaration/syntax so the flow parses."
	case "token_alias_dangling":
		return "Point the alias at an existing token path, or add the aliased token."
	case "token_alias_cycle":
		return "Break the alias cycle so the token resolves to a literal value."
	case "token_type_membership":
		return "Give the token a declared $type so it satisfies the token contract."
	case "brand_raw_hex", "brand_no_token_refs":
		return "Reference the design token (e.g. a {color.*} token) instead of a literal value."
	case "screen_device_frame":
		return "Match the container width to a frame declared in the design README."
	default:
		return ""
	}
}

// staticCritiqueNotice is the per-run note that accompanies a degraded
// (visual:false) run with static findings.
func staticCritiqueNotice() string {
	return "No vision tier was reachable, so this critique degraded to static " +
		"design-validator findings (visual:false). Areas come from the design " +
		"rule packs, not from pixels: run design_validate for the full rule " +
		"report, and use a vision-capable model — or analyze_image_content — " +
		"to judge the rendered artifact directly."
}

// staticCritiqueCleanNotice is the note for a degraded run whose static pass
// found nothing, so an empty findings list is not mistaken for "no critique
// was attempted".
func staticCritiqueCleanNotice() string {
	return "No vision tier was reachable, so this critique degraded to static " +
		"design-validator findings (visual:false); the static pass found no " +
		"violations. Use a vision-capable model — or analyze_image_content — to " +
		"judge the rendered artifact directly."
}
