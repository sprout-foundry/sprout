package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// testRoot resolves the repo root from the test's working directory (the
// package directory) by walking up to the directory that holds go.mod.
func testRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		t.Fatalf("could not locate the repo root from %q: %v", cwd, err)
	}
	return root
}

// TestOpenAPIDocIsNotStale is the staleness gate. It regenerates the merged
// OpenAPI document in memory (seed + registered Huma operations) and
// byte-compares it against the committed docs/api/openapi.yaml. A failure
// means the seed or a registered Huma operation changed and the committed doc
// must be regenerated with `go run ./cmd/genapi`.
func TestOpenAPIDocIsNotStale(t *testing.T) {
	root := testRoot(t)
	generated, err := Generate(root)
	if err != nil {
		t.Fatalf("generate doc in memory: %v", err)
	}
	committedPath := filepath.Join(root, outRel)
	committed, err := os.ReadFile(committedPath) // #nosec G304 -- outRel is a fixed repo-relative path
	if err != nil {
		t.Fatalf("read committed doc %s: %v (run 'go run ./cmd/genapi' to create it)", committedPath, err)
	}
	if string(generated) != string(committed) {
		t.Fatalf("docs/api/openapi.yaml is stale.\n" +
			"Regenerate it by running `go run ./cmd/genapi` from the repo root, then commit the result.\n" +
			"(the in-memory generation and the committed file differ)")
	}
}

// TestHumaOperationsArePresentInGeneratedDoc is a sanity check: the
// Huma-migrated paths (/api/stats, /api/config) must appear in the generated
// document, so a regression that silently drops a registration cannot slip
// through.
func TestHumaOperationsArePresentInGeneratedDoc(t *testing.T) {
	root := testRoot(t)
	generated, err := Generate(root)
	if err != nil {
		t.Fatalf("generate doc: %v", err)
	}
	paths := pathsOfYAML(t, generated)
	for _, path := range []string{"/api/stats", "/api/config"} {
		if !paths[path] {
			t.Errorf("Huma path %q missing from generated doc", path)
		}
	}
}

// TestMergePreservesSeedFamilies guards the merge: every path the hand-written
// seed documents must survive into the merged output (the generated Huma
// sections are unioned on top, never replacing the seed's families).
func TestMergePreservesSeedFamilies(t *testing.T) {
	root := testRoot(t)
	baseBytes, err := os.ReadFile(filepath.Join(root, baseRel)) // #nosec G304 -- baseRel is a fixed repo-relative path
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	base, err := unmarshalDoc(baseBytes)
	if err != nil {
		t.Fatalf("parse seed: %v", err)
	}
	generated, err := Generate(root)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	merged, err := unmarshalDoc(generated)
	if err != nil {
		t.Fatalf("parse generated: %v", err)
	}
	for p := range pathsOf(base) {
		if !pathsOf(merged)[p] {
			t.Errorf("seed path %q dropped from merged output", p)
		}
	}
}

// pathsOf returns the set of path keys in an OpenAPI document map.
func pathsOf(doc map[string]any) map[string]bool {
	paths, _ := doc["paths"].(map[string]any)
	out := make(map[string]bool, len(paths))
	for p := range paths {
		out[p] = true
	}
	return out
}

// pathsOfYAML parses a generated document (with its generated header) and
// returns its path-key set. The header is a comment block, so a plain
// yaml.Unmarshal of the whole body still yields the document map.
func pathsOfYAML(t *testing.T, doc []byte) map[string]bool {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(doc, &m); err != nil {
		t.Fatalf("parse generated doc: %v", err)
	}
	return pathsOf(m)
}
