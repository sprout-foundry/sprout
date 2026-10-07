package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	webuiRel    = "pkg/webui"
	registryRel = "webui/src/services/cloudEndpointRegistry/endpoints"
	docRel      = "docs/api/endpoints.md"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if !isRepoRoot(root) {
		fmt.Fprintf(os.Stderr, "api_inventory: %s does not look like the repo root (missing go.mod)\n", root)
		os.Exit(1)
	}
	doc, err := Generate(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "api_inventory: %v\n", err)
		os.Exit(1)
	}
	outPath := filepath.Join(root, docRel)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil { // #nosec G703 -- root is the developer-supplied repo root, not network input
		fmt.Fprintf(os.Stderr, "api_inventory: create doc dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, []byte(doc), 0o644); err != nil { // #nosec G703 -- resolves within the developer-supplied repo root
		fmt.Fprintf(os.Stderr, "api_inventory: write doc: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", outPath)
}

// buildRouteSet assembles the full registered route table for root: the plain
// mux routes parsed from the registerXxxRoutes functions plus the Huma
// operations read straight from the in-process API object. The two are merged
// (de-duplicated) so the result is the complete route set and cannot drift
// from the handlers as the Huma registration moves between files.
func buildRouteSet(root string) ([]Route, error) {
	plain, err := parsePlainRoutes(root)
	if err != nil {
		return nil, err
	}
	huma, err := humaOpsInProcess()
	if err != nil {
		return nil, err
	}
	return combineRoutes(plain, huma), nil
}

// Generate builds the full endpoints.md content in memory from the repo root.
// It is deterministic (stable family order, sorted rows, no timestamps) so a
// staleness test can byte-compare against the committed doc.
func Generate(root string) (string, error) {
	routes, err := buildRouteSet(root)
	if err != nil {
		return "", fmt.Errorf("build route set: %w", err)
	}
	registry, err := parseRegistryFiles(filepath.Join(root, registryRel))
	if err != nil {
		return "", fmt.Errorf("parse registry: %w", err)
	}
	idx, err := buildHandlerIndex(filepath.Join(root, webuiRel))
	if err != nil {
		return "", fmt.Errorf("build handler index: %w", err)
	}
	rows := buildInventory(routes, registry, idx)
	return renderDocument(rows), nil
}

// GenerateRoutes returns just the combined route table, exposed for tests that
// want to assert on the registration set without rendering the doc.
func GenerateRoutes(root string) ([]Route, error) {
	return buildRouteSet(root)
}

// DocPath returns the repo-relative path of the generated doc.
func DocPath(root string) string {
	return filepath.Join(root, docRel)
}

// containsDocPath reports whether a generated doc lists path as a route. The
// check matches the backtick-quoted path cell so that a short path (e.g.
// /api/settings) is not satisfied by a longer path's cell
// (/api/settings/credentials).
func containsDocPath(doc, path string) bool {
	return strings.Contains(doc, "`"+path+"`")
}

// isRepoRoot reports whether dir contains a go.mod (the repo root marker).
func isRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod")) // #nosec G703 -- dir is the developer-supplied repo root
	return err == nil
}

// findRepoRoot walks up from start until it finds a directory containing a
// go.mod, and returns it (or start if it is already the root).
func findRepoRoot(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	for {
		if isRepoRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}
