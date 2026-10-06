package main

import (
	"fmt"
	"sort"
	"strings"
)

// inventoryRow is a single rendered row of the route inventory.
type inventoryRow struct {
	Path       string
	Method     string
	Handler    string
	ServedBy   string
	Family     string
	RegisterFn string
	Notes      []string
}

// buildInventory computes the rows for every route: family, method, and
// served-by. Rows are returned in deterministic order (family, then path).
func buildInventory(routes []Route, registry []RegistryEntry, idx *handlerIndex) []inventoryRow {
	rows := make([]inventoryRow, 0, len(routes))
	for _, r := range routes {
		family := familyForPath(r.Path)
		methods := resolveRouteMethod(r.Path, r.HandlerBase, registry, idx)
		served := servedByForRoute(r.Path, registry)
		row := inventoryRow{
			Path:       r.Path,
			Method:     formatMethods(methods),
			Handler:    r.Handler,
			ServedBy:   strings.Join(served, ", "),
			Family:     family,
			RegisterFn: r.RegisterFn,
		}
		// An API route that the web UI's routing does not explicitly classify
		// (no registry entry, no intercept) is a gap: daemon-only, and in
		// cloud mode it falls through to the host catch-all proxy.
		if isAPIRoute(r.Path) && !isCovered(r.Path, registry) {
			row.Notes = append(row.Notes, "not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)")
		}
		// Surface a registry/handler method divergence so the OpenAPI seed
		// author is aware the in-browser and daemon implementations disagree.
		if note := methodProvenanceNote(r.Path, r.HandlerBase, registry, idx); note != "" {
			row.Notes = append(row.Notes, note)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		fi, fj := familyOrderIndex(rows[i].Family), familyOrderIndex(rows[j].Family)
		if fi != fj {
			return fi < fj
		}
		if rows[i].Path != rows[j].Path {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].Method < rows[j].Method
	})
	return rows
}

// renderDocument produces the full endpoints.md content from the inventory
// rows. Output is deterministic (stable family order, sorted rows).
func renderDocument(rows []inventoryRow) string {
	var b strings.Builder
	b.WriteString(header)

	// Group rows by family in the fixed family order.
	famIndex := make(map[string][]inventoryRow)
	for _, r := range rows {
		famIndex[r.Family] = append(famIndex[r.Family], r)
	}
	for _, fam := range familyOrder {
		group := famIndex[fam]
		if len(group) == 0 {
			continue
		}
		b.WriteString("\n## " + fam + "\n\n")
		b.WriteString("| Method | Path | Handler | Served by |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, r := range group {
			row := fmt.Sprintf("| %s | `%s` | %s | %s |", r.Method, r.Path, r.Handler, r.ServedBy)
			b.WriteString(row + "\n")
		}
		// Append per-row notes (routing gaps and registry/handler method
		// divergences) for this family.
		var notes []string
		for _, r := range group {
			for _, n := range r.Notes {
				notes = append(notes, fmt.Sprintf("- `%s` — %s", r.Path, n))
			}
		}
		if len(notes) > 0 {
			b.WriteString("\nNotes:\n")
			for _, n := range notes {
				b.WriteString(n + "\n")
			}
		}
	}
	return b.String()
}

// header is the fixed document preamble. It explains the columns and how the
// file is generated, so the committed doc stays self-describing.
const header = `# API endpoint inventory

> **Generated file — do not edit by hand.** Regenerate with
> ` + "`go run ./cmd/api_inventory`" + ` from the repo root. A Go test in
> ` + "`cmd/api_inventory`" + ` fails when this file is stale.

Every route registered in ` + "`pkg/webui/routes.go`" + ` is listed below, grouped by
family.

**Served-by semantics.** In the local product every route is served by the
**daemon** (the Go web server). In a hosted/cloud deployment the same route is
served by whichever of the **in-browser WASM agent**, **browser-local**
storage, or the **host** backend the web UI's routing sends it to (the cloud
endpoint registry and the dynamic intercepts in
` + "`webui/src/services/cloudAdapter.ts`" + `). The Served-by column lists the applicable
components. In cloud mode, a route the web UI does not explicitly classify
falls through to the standard host proxy as a safety net; the column records
only explicit classifications, so a daemon-only row means the route is proxied
to the host backend by that catch-all.

| Column | Meaning |
|---|---|
| Method | HTTP methods the route accepts. For a route listed in the endpoint registry (or a registry prefix entry covering it), the registry's method list; otherwise derived from the handler source in ` + "`pkg/webui/*.go`" + `. ` + "`any`" + ` means the handler performs no method check and accepts any method. |
| Path | The mux pattern as registered in ` + "`pkg/webui/routes.go`" + `. A trailing ` + "`/`" + ` marks a prefix route; ` + "`{name}`" + ` is a Go 1.22 wildcard segment. |
| Handler | The handler registered for the route (a ` + "`ReactWebServer`" + ` method, an inline closure, or a package-qualified handler). |
| Served by | The components that serve the route, in the order: daemon, in-browser WASM agent, browser-local, host. |
`
