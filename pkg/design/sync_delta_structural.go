package design

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// routeObjectRe captures an object literal with both a path and an element or
// component, the shape every JS router uses:
//
//	{ path: "/login", element: <Login /> }
//	{ path: '/login', component: Login }
var routeObjectRe = regexp.MustCompile(`\{[^{}]*?(?i:path)\s*:\s*["'](/[a-z0-9][a-z0-9/_-]*)["'][^{}]*\}`)

// componentNameRe captures a JSX component identifier (`<Login />`,
// `element: <Login/>`).
var componentNameRe = regexp.MustCompile(`<\s*([A-Z][A-Za-z0-9]*)\b`)

// screenStemFromRoute derives a wireframe-stem-shaped name from a route path:
// "/check-deposit" → "check-deposit", "/settings/profile" → "settings-profile".
// It returns "" for the root route (a route of "/" names no screen of its own).
func screenStemFromRoute(route string) string {
	trimmed := strings.Trim(route, "/")
	if trimmed == "" {
		return ""
	}
	// A route parameter ("/users/:id") or catch-all ("/files/*") names no
	// screen: there is no single wireframe for it. Check before sanitising,
	// since sanitising strips the ':' that marks the parameter.
	if strings.ContainsAny(trimmed, ":*") {
		return ""
	}
	segments := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '/' })
	for i, s := range segments {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
				return r
			case r == '_' || r == ' ':
				return '-'
			default:
				return -1
			}
		}, s)
		segments[i] = s
	}
	joined := strings.Join(segments, "-")
	if joined == "" {
		return ""
	}
	if !SlugMatches(joined) {
		return ""
	}
	return joined
}

// SlugMatches reports whether a name satisfies the shared slug rule
// (SlugPattern, SP-140-1). It is exported so the sync handler/tests can assert
// a proposed stem is a legal wireframe name without duplicating the regexp.
func SlugMatches(name string) bool {
	ok, err := regexp.MatchString(SlugPattern, name)
	return err == nil && ok
}

// deliveredScreen returns the design/screens/<stem>.html path when a hi-fi
// screen has already been delivered for a stem, "" otherwise. It is the
// structural pass's "is the design tree already ahead here?" check: when a
// screen exists but no wireframe does, the proposal is to backfill the
// wireframe, and the delta names the delivered screen as adoption context.
func deliveredScreen(tree syncTree, stem string) string {
	if stem == "" {
		return ""
	}
	target := path.Join(DirName, "screens", stem+".html")
	for _, f := range tree.screenFiles {
		if f == target {
			return f
		}
	}
	return ""
}

// analyzeStructural detects structural deltas in one touched file: a new
// route/screen in a router file, and nav targets whose wireframe counterpart
// is missing.
func analyzeStructural(f SyncFileInput, tree syncTree) []SyncDelta {
	content := string(f.Content)
	var deltas []SyncDelta

	if isRouterFile(f.Path) {
		for _, m := range routeObjectRe.FindAllStringSubmatchIndex(content, -1) {
			block := content[m[0]:m[1]]
			route := content[m[2]:m[3]]
			stem := screenStemFromRoute(route)
			if stem == "" {
				continue
			}
			component := firstComponentName(block)
			if tree.wireframeStems[stem] {
				// The wireframe exists; the only structural question is the
				// nav edge, which the nav pass below covers. Skip.
				continue
			}
			line := strings.Count(content[:m[0]], "\n") + 1
			wireframePath := path.Join(DirName, "wireframes", stem+".svg")
			designFiles := []string{wireframePath}
			what := fmt.Sprintf("new route %q%s with no wireframe", route, componentSuffix(component))
			if screen := deliveredScreen(tree, stem); screen != "" {
				// The semantic layer is partly ahead: a hi-fi screen is
				// already delivered, so the proposal is to backfill the
				// wireframe the screen implies. The delivered screen is part
				// of the delta's design-file read set.
				designFiles = append(designFiles, screen)
				what = fmt.Sprintf("new route %q%s has a delivered screen %s but no wireframe",
					route, componentSuffix(component), screen)
			}
			d := SyncDelta{
				Delta:         fmt.Sprintf("%s; propose %s", what, wireframePath),
				Kind:          DeltaKindWireframe,
				Basis:         DeltaBasisStructural,
				Confidence:    ConfidenceMedium,
				DesignFiles:   designFiles,
				SafeToApply:   true,
				WireframeStem: stem,
				Status:        FlowDraftStatus,
				CodeFiles:     []string{f.Path},
				Evidence:      fmt.Sprintf("%s (line %d)", strings.TrimSpace(route), line),
			}
			deltas = append(deltas, d)

			// The flow edge that gets the screen into the graph. The source
			// is the route's parent segment when there is one, else the
			// screen is an entry point and the edge is proposed into the
			// first existing flow file (or a new one when none exists).
			edge, flowFile := proposeFlowEdge(stem, tree)
			if edge != "" {
				deltas = append(deltas, SyncDelta{
					Delta: fmt.Sprintf("new screen %q is not in any flow; propose edge %q in %s",
						stem, edge, flowFile),
					Kind:          DeltaKindFlow,
					Basis:         DeltaBasisStructural,
					Confidence:    ConfidenceMedium,
					DesignFiles:   []string{flowFile},
					SafeToApply:   true,
					WireframeStem: stem,
					FlowEdge:      edge,
					FlowFile:      flowFile,
					Status:        FlowDraftStatus,
					CodeFiles:     []string{f.Path},
					Evidence:      fmt.Sprintf("route %q", route),
				})
			}
		}
	}

	// Nav targets: `data-nav="login"` or `to="/login"` values that name a
	// screen whose wireframe is missing.
	for _, target := range navTargets(content) {
		stem := screenStemFromRoute("/" + strings.Trim(target, "/"))
		if stem == "" || tree.wireframeStems[stem] {
			continue
		}
		wireframePath := path.Join(DirName, "wireframes", stem+".svg")
		deltas = append(deltas, SyncDelta{
			Delta: fmt.Sprintf("nav target %q has no wireframe; propose %s",
				target, wireframePath),
			Kind:          DeltaKindWireframe,
			Basis:         DeltaBasisStructural,
			Confidence:    ConfidenceMedium,
			DesignFiles:   []string{wireframePath},
			SafeToApply:   true,
			WireframeStem: stem,
			Status:        FlowDraftStatus,
			CodeFiles:     []string{f.Path},
			Evidence:      target,
		})
	}
	return deltas
}

// firstComponentName returns the first JSX component identifier in a route
// object block, "" when none.
func firstComponentName(block string) string {
	if m := componentNameRe.FindStringSubmatch(block); m != nil {
		return m[1]
	}
	return ""
}

// componentSuffix renders " for component Login" (or "" when unknown) for a
// delta description.
func componentSuffix(component string) string {
	if component == "" {
		return ""
	}
	return " for component " + component
}

// navAttrRe captures a nav target attribute value: data-nav="login" or
// to="/login" (the React-router Link form).
var navAttrRe = regexp.MustCompile(`(?i)\b(?:data-nav|to|href)\s*=\s*["'](/[A-Za-z0-9/_-]*|[a-z0-9][a-z0-9_-]*)["']`)

// navTargets extracts the nav target values in a file, deduped and sorted.
// Route-absolute values ("/login") and bare stems ("login") both qualify;
// external hrefs ("https://…") and anchors ("#…") do not.
func navTargets(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range navAttrRe.FindAllStringSubmatch(content, -1) {
		v := strings.TrimSpace(m[1])
		if v == "" || strings.HasPrefix(v, "//") || strings.Contains(v, "://") {
			continue
		}
		if strings.HasPrefix(v, "#") || strings.HasPrefix(v, ".") {
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// proposeFlowEdge proposes a flow edge for a newly detected screen. The source
// is derived from the screen's name when it has a parent segment
// ("settings-profile" → settings → profile); otherwise the edge is proposed
// from an existing wireframe that links to it (the code's nav targets), and
// failing that from the first existing wireframe stem so the graph stays
// connected. It returns ("", "") when no edge can be proposed (no wireframes
// and no flow file to host it).
func proposeFlowEdge(stem string, tree syncTree) (edge, flowFile string) {
	flowFile = ""
	if len(tree.flowFiles) > 0 {
		flowFile = tree.flowFiles[0]
	} else {
		flowFile = path.Join(DirName, FlowSubdir, stem+".mmd")
	}

	target := stem
	source := ""
	// Prefer an explicit parent segment in a compound stem.
	if i := strings.LastIndex(stem, "-"); i > 0 {
		candidate := stem[:i]
		if tree.wireframeStems[candidate] {
			source = candidate
		}
	}
	if source == "" {
		// Fall back to any existing wireframe (sorted, for determinism).
		stems := make([]string, 0, len(tree.wireframeStems))
		for s := range tree.wireframeStems {
			stems = append(stems, s)
		}
		sort.Strings(stems)
		if len(stems) > 0 {
			source = stems[0]
		}
	}
	if source == "" || source == target {
		return "", ""
	}
	edge = source + " --> " + target
	if tree.flowEdges[edge] {
		return "", ""
	}
	return edge, flowFile
}

// -----------------------------------------------------------------------------
// Inferred deltas — raw hex / magic spacing with no token counterpart
// -----------------------------------------------------------------------------
