package main

import (
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/webui"
)

// humaMethodKeys is the fixed set of OpenAPI method keys, in canonical order,
// used to read the per-method operation out of a path item.
var humaMethodKeys = []string{
	"get", "post", "put", "patch", "delete", "head", "options", "trace",
}

// humaOpsInProcess returns the Huma operation set straight from the registered
// API object, without parsing source. It calls the webui package's exported
// HumaOpenAPIDoc, which registers every Huma operation on a throwaway mux and
// serializes the resulting OpenAPI document; we then read each path's
// per-method operationId back out of that document. The set is the live
// registration set (the same registerHumaOperations call the server uses), so
// it cannot drift from the handlers even when the huma.Register calls move to
// other files. The Handler column for a Huma route is its operationId (the
// stable identifier huma assigns to the operation); HandlerBase is empty, so
// method derivation falls back to the registry or "any".
//
// Routes are returned in deterministic order (path, then operationId) so the
// inventory is byte-stable across runs.
func humaOpsInProcess() ([]Route, error) {
	doc, err := webui.HumaOpenAPIDoc()
	if err != nil {
		return nil, err
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		// No paths in the document means no Huma operations were registered.
		// That is a real state (not an error), so return the empty set.
		return nil, nil
	}
	var routes []Route
	for path, item := range paths {
		pm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, mk := range humaMethodKeys {
			opAny, present := pm[mk]
			if !present || opAny == nil {
				continue
			}
			op, ok := opAny.(map[string]any)
			if !ok {
				continue
			}
			opID, _ := op["operationId"].(string)
			if opID == "" {
				continue
			}
			routes = append(routes, Route{
				Path:           path,
				Handler:        opID,
				HandlerBase:    "",
				RegisterFn:     "huma",
				ExplicitMethod: strings.ToUpper(mk),
			})
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Handler < routes[j].Handler
	})
	return routes, nil
}

// routeKey is the identity used to de-duplicate the combined plain + Huma
// route set, so a path registered in both sources does not render twice.
func routeKey(r Route) string {
	return r.Path + " " + r.Handler
}

// combineRoutes merges the plain and Huma route sets, dropping exact
// duplicates (same path and handler) so a path registered in both does not
// render twice. Output order is stable: plain routes first (source order),
// then Huma routes (sorted), which keeps the doc deterministic.
func combineRoutes(plain, huma []Route) []Route {
	seen := make(map[string]bool, len(plain)+len(huma))
	out := make([]Route, 0, len(plain)+len(huma))
	for _, r := range plain {
		k := routeKey(r)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	for _, r := range huma {
		k := routeKey(r)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}
