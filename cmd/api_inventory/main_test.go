package main

import (
	"os"
	"path/filepath"
	"testing"
)

// testRoot resolves the repo root from the test's working directory (the
// package directory) by walking up to the directory that holds go.mod.
func testRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	root := findRepoRoot(cwd)
	if !isRepoRoot(root) {
		t.Fatalf("could not locate the repo root from %q (no go.mod found walking up)", cwd)
	}
	return root
}

// TestEndpointsDocIsNotStale is the staleness gate. It regenerates the doc in
// memory and byte-compares it against the committed docs/api/endpoints.md.
// A failure means routes, the endpoint registry, the handler method checks, or
// the intercept table changed and the committed doc must be regenerated with
// `go run ./cmd/api_inventory`.
func TestEndpointsDocIsNotStale(t *testing.T) {
	root := testRoot(t)
	generated, err := Generate(root)
	if err != nil {
		t.Fatalf("generate doc in memory: %v", err)
	}
	committedPath := DocPath(root)
	committed, err := os.ReadFile(committedPath)
	if err != nil {
		t.Fatalf("read committed doc %s: %v (run 'go run ./cmd/api_inventory' to create it)", committedPath, err)
	}
	if generated != string(committed) {
		t.Fatalf("docs/api/endpoints.md is stale.\n" +
			"Regenerate it by running `go run ./cmd/api_inventory` from the repo root, then commit the result.\n" +
			"(the in-memory generation and the committed file differ)")
	}
}

// TestEveryRegisteredRouteAppearsInDoc is a sanity check: every registered
// route (plain mux plus Huma operations) must show up in the generated doc, so
// a parser regression that silently drops a route cannot slip through.
func TestEveryRegisteredRouteAppearsInDoc(t *testing.T) {
	root := testRoot(t)
	routes, err := GenerateRoutes(root)
	if err != nil {
		t.Fatalf("build route set: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("parsed zero routes; the routes.go parser is broken")
	}
	generated, err := Generate(root)
	if err != nil {
		t.Fatalf("generate doc in memory: %v", err)
	}
	seen := make(map[string]bool, len(routes))
	for _, r := range routes {
		if !containsDocPath(generated, r.Path) {
			t.Errorf("registered route %q is missing from the generated doc", r.Path)
		}
		seen[r.Path] = true
	}
	t.Logf("checked %d registered routes", len(seen))
}

// TestRouteCountIsStable pins the number of registered routes so an accidental
// drop of a registration is caught even if the doc happens to render. The
// count is the full registered set: the plain mux routes parsed from the
// registerXxxRoutes functions plus the Huma operations read from the
// in-process API object (the Huma set is the live registration set, so it
// tracks the handlers even as the huma.Register calls move between files).
func TestRouteCountIsStable(t *testing.T) {
	root := testRoot(t)
	routes, err := GenerateRoutes(root)
	if err != nil {
		t.Fatalf("build route set: %v", err)
	}
	// The inventory must cover every plain mux.HandleFunc registration plus
	// every registered Huma operation.
	const want = 195
	if len(routes) != want {
		t.Errorf("parsed %d routes, want %d (a registration was added or dropped)", len(routes), want)
	}
}

// TestServedByForKnownRoutes asserts the served-by mapping for a few
// representative routes, catching a regression in the registry category
// mapping or the intercept table.
func TestServedByForKnownRoutes(t *testing.T) {
	root := testRoot(t)
	registry, err := parseRegistryFiles(filepath.Join(root, registryRel))
	if err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	cases := []struct {
		path string
		want []string // expected served-by components, in display order
	}{
		{"/api/files", []string{compDaemon, compWASM}},
		{"/api/settings", []string{compDaemon, compHost}},
		{"/api/chat-sessions", []string{compDaemon, compLocal}},
		{"/api/stats", []string{compDaemon, compHost}},
		{"/api/git/commit/show", []string{compDaemon, compLocal}},
		{"/api/query/status", []string{compDaemon, compHost}},
		{"/api/txn/push", []string{compDaemon}},
	}
	for _, c := range cases {
		got := servedByForRoute(c.path, registry)
		if !equalStrings(got, c.want) {
			t.Errorf("servedByForRoute(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestRegistryParse extracts a known entry and checks its fields were parsed
// correctly, guarding the tolerant TS parser.
func TestRegistryParse(t *testing.T) {
	root := testRoot(t)
	registry, err := parseRegistryFiles(filepath.Join(root, registryRel))
	if err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	if len(registry) == 0 {
		t.Fatal("parsed zero registry entries; the registry parser is broken")
	}
	byPath := make(map[string]RegistryEntry, len(registry))
	for _, e := range registry {
		byPath[e.Path] = e
	}
	// An exact entry.
	files, ok := byPath["/api/files"]
	if !ok {
		t.Fatalf("registry entry for /api/files not found")
	}
	if files.Category != "wasm-local" {
		t.Errorf("/api/files category = %q, want wasm-local", files.Category)
	}
	if !equalStrings(files.Methods, []string{"GET"}) {
		t.Errorf("/api/files methods = %v, want [GET]", files.Methods)
	}
	// A prefix entry.
	creds, ok := byPath["/api/settings/credentials/"]
	if !ok {
		t.Fatalf("registry entry for /api/settings/credentials/ not found")
	}
	if !creds.IsPrefix {
		t.Errorf("/api/settings/credentials/ should be a prefix entry")
	}
	if creds.Category != "foundry-backend" {
		t.Errorf("/api/settings/credentials/ category = %q, want foundry-backend", creds.Category)
	}
}

// TestMethodDerivation checks the handler-source method derivation for routes
// not covered by the registry, including multi-method and dispatch handlers.
func TestMethodDerivation(t *testing.T) {
	root := testRoot(t)
	registry, err := parseRegistryFiles(filepath.Join(root, registryRel))
	if err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	idx, err := buildHandlerIndex(filepath.Join(root, webuiRel))
	if err != nil {
		t.Fatalf("build handler index: %v", err)
	}
	cases := []struct {
		path           string
		handler        string
		explicitMethod string
		want           []string
	}{
		// Not in the registry; multi-method via requireMethods(GET, HEAD, POST).
		{"/api/sync", "handleAPISync", "", []string{"GET", "HEAD", "POST"}},
		// Not in the registry; GET+HEAD via requireMethods.
		{"/api/txn/status", "handleAPITxnStatus", "", []string{"GET", "HEAD"}},
		// In the registry (synthetic, isPrefix) — confirms registry methods win
		// over the handler for a prefix-covered route.
		{"/api/settings/mcp/servers/", "handleAPISettingsMCPServers", "", []string{"GET", "POST", "PUT", "DELETE"}},
		// No method check in the handler -> "any".
		{"/health", "", "", []string{"any"}},
		// A Huma operation with an explicit registered method wins over the
		// registry and the handler source (the spec is authoritative).
		{"/api/command/complete", "commandComplete", "POST", []string{"POST"}},
	}
	for _, c := range cases {
		if c.want == nil {
			continue
		}
		got := resolveRouteMethod(c.path, c.handler, c.explicitMethod, registry, idx)
		if !equalStrings(got, c.want) {
			t.Errorf("resolveRouteMethod(%q, %q, %q) = %v, want %v", c.path, c.handler, c.explicitMethod, got, c.want)
		}
	}
}

// TestOutputIsDeterministic pins the guarantee the staleness test relies on:
// regenerating the doc N times yields byte-identical output. Any map-iteration
// order leaking into the rendering (families, method sets, served-by sets)
// would surface here as a mismatch.
func TestOutputIsDeterministic(t *testing.T) {
	root := testRoot(t)
	first, err := Generate(root)
	if err != nil {
		t.Fatalf("first generation: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := Generate(root)
		if err != nil {
			t.Fatalf("regeneration %d: %v", i+1, err)
		}
		if again != first {
			t.Fatalf("generation %d is not byte-identical to the first (non-deterministic output)", i+1)
		}
	}
}

// equalStrings reports whether two string slices are element-wise equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] { // #nosec G602 -- i < len(a) == len(b), both in range
			return false
		}
	}
	return true
}
