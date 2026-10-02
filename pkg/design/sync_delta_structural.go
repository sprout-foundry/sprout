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
// a proposed stem is a legal screen name without duplicating the regexp.
func SlugMatches(name string) bool {
	ok, err := regexp.MatchString(SlugPattern, name)
	return err == nil && ok
}

// deliveredScreen returns the design/screens/<stem>.html path when a hi-fi
// screen has already been delivered for a stem, "" otherwise. It is the
// structural pass's "is the design tree already ahead here?" check: when the
// screen exists, a route delta is a no-op (the screen is the artifact), and
// only the flow edge may still be proposed.
func deliveredScreen(tree syncTree, stem string) string {
	if stem == "" {
		return ""
	}
	target := path.Join(DirName, ScreenSubdir, stem+".html")
	for _, f := range tree.screenFiles {
		if f == target {
			return f
		}
	}
	return ""
}

// analyzeStructural detects structural deltas in one touched file: a new
// route/screen in a router file, and nav targets whose screen counterpart
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
			if tree.screenStems[stem] || tree.wireframeStems[stem] {
				// The screen exists (or its pre-migration wireframe does —
				// the deprecation validator owns that nag); the only
				// structural question is the flow edge, covered below. Skip.
				continue
			}
			line := strings.Count(content[:m[0]], "\n") + 1
			screenPath := ScreenRelPath(stem)
			deltas = append(deltas, SyncDelta{
				Delta: fmt.Sprintf("new route %q%s with no screen; propose %s",
					route, componentSuffix(component), screenPath),
				Kind:          DeltaKindScreen,
				Basis:         DeltaBasisStructural,
				Confidence:    ConfidenceMedium,
				DesignFiles:   []string{screenPath},
				SafeToApply:   true,
				ScreenStem:    stem,
				Status:        FlowDraftStatus,
				CodeFiles:     []string{f.Path},
				Evidence:      fmt.Sprintf("%s (line %d)", strings.TrimSpace(route), line),
			})

			// The flow step that gets the screen into the graph. The source
			// is the route's parent segment when there is one, else the
			// screen is an entry point and the step is proposed into the
			// first existing flow source (or a new one when none exists).
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
					ScreenStem:    stem,
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
	// screen whose primary artifact is missing. A legacy wireframe with the
	// stem also suppresses the proposal — the screen concept exists and the
	// deprecation validator owns the conversion nag.
	for _, target := range navTargets(content) {
		stem := screenStemFromRoute("/" + strings.Trim(target, "/"))
		if stem == "" || tree.screenStems[stem] || tree.wireframeStems[stem] {
			continue
		}
		screenPath := ScreenRelPath(stem)
		deltas = append(deltas, SyncDelta{
			Delta: fmt.Sprintf("nav target %q has no screen; propose %s",
				target, screenPath),
			Kind:          DeltaKindScreen,
			Basis:         DeltaBasisStructural,
			Confidence:    ConfidenceMedium,
			DesignFiles:   []string{screenPath},
			SafeToApply:   true,
			ScreenStem:    stem,
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
// from an existing screen that links to it (the code's nav targets), and
// failing that from the first existing screen stem so the graph stays
// connected. The proposal targets the flow SOURCE document
// (design/flows/<name>.json) — the derived .mmd is regenerated from it, never
// hand-written. It returns ("", "") when no edge can be proposed (no screens
// and no flow source to host it).
func proposeFlowEdge(stem string, tree syncTree) (edge, flowFile string) {
	flowFile = ""
	if len(tree.flowFiles) > 0 {
		flowFile = tree.flowFiles[0]
	} else {
		flowFile = FlowSourceRelPath(stem)
	}

	target := stem
	source := ""
	// Prefer an explicit parent segment in a compound stem.
	if i := strings.LastIndex(stem, "-"); i > 0 {
		candidate := stem[:i]
		if tree.screenStems[candidate] {
			source = candidate
		}
	}
	if source == "" {
		// Fall back to any existing screen (sorted, for determinism).
		stems := make([]string, 0, len(tree.screenStems))
		for s := range tree.screenStems {
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
