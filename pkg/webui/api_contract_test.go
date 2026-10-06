//go:build !js

package webui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The backend contract is the OpenAPI description of the routes registered in
// routes.go, versioned and kept in lockstep with an allowlist of routes not yet
// documented. This file checks that lockstep so a new route cannot ship
// undocumented: every registered route must either be a documented path in
// docs/api/openapi.yaml or be listed in docs/api/undocumented.txt, and a route
// that is documented must no longer be listed (the allowlist may only shrink).
//
// The route set is re-extracted here from routes.go with a minimal AST walk
// (the full inventory generator is a main package and is not importable). The
// walk discovers both registration styles that coexist in routes.go: plain
// mux.HandleFunc patterns and Huma operations (huma.Register with a
// huma.Operation Path). The docs live at the repo root, so these tests walk up
// to the directory holding go.mod before reading them.

// contractFiles are the repo-root-relative paths the contract test reads.
const (
	contractRoutesGo    = "pkg/webui/routes.go"
	contractOpenAPIYAML = "docs/api/openapi.yaml"
	contractAllowlist   = "docs/api/undocumented.txt"
)

// openAPIInfo is the subset of the OpenAPI doc the contract test validates.
type openAPIInfo struct {
	Version string
	Paths   []string
	Tags    []string
}

// parseSemver reports whether s is a semantic version string (see semver.org):
// three dot-separated non-negative integer components, an optional hyphenated
// pre-release part (dot-separated identifiers of alphanumerics and hyphens),
// and an optional plus-separated build part. A leading "v" and a leading zero
// in a numeric component are rejected.
func parseSemver(s string) bool {
	if s == "" {
		return false
	}
	// Split off the build metadata (everything after the first '+').
	buildAt := strings.IndexByte(s, '+')
	body := s
	if buildAt >= 0 {
		// Build metadata is dot-separated identifiers like a pre-release.
		for _, id := range strings.Split(s[buildAt+1:], ".") {
			if !semverIdentifierOK(id) {
				return false
			}
		}
		body = s[:buildAt]
	}
	// Split off the pre-release (first hyphen through the end of the body).
	preAt := strings.IndexByte(body, '-')
	core := body
	if preAt >= 0 {
		pre := body[preAt+1:]
		// A pre-release is dot-separated identifiers; each must be non-empty
		// and made of alphanumerics plus hyphens.
		for _, id := range strings.Split(pre, ".") {
			if !semverIdentifierOK(id) {
				return false
			}
		}
		core = body[:preAt]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return false
		}
	}
	return true
}

// semverIdentifierOK reports whether a pre-release or build identifier is
// non-empty and made only of semver characters (alphanumerics and hyphen).
func semverIdentifierOK(part string) bool {
	if part == "" {
		return false
	}
	for _, r := range part {
		if !isSemverIdentifierChar(r) {
			return false
		}
	}
	return true
}

// isSemverIdentifierChar reports whether r is a legal semver identifier
// character (digit, letter, or hyphen).
func isSemverIdentifierChar(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-'
}

// findRepoRoot walks up from dir until it finds the directory holding go.mod
// (the repo root), or returns dir unchanged if none is found.
func findRepoRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil { // #nosec G304 -- dir is a path walked up to the repo root
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// repoRootFromWorkingDir resolves the repo root for the contract test. The webui
// TestMain chdirs every test to a scratch working directory (so handlers that
// resolve paths against the CWD don't touch the real tree), so os.Getwd() does
// not walk up to the repo. Instead we anchor on this test file's own source path
// via runtime.Caller and walk up from there to the directory holding go.mod.
func repoRootFromWorkingDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("could not resolve this test file's path via runtime.Caller")
	}
	root := findRepoRoot(filepath.Dir(file))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil { // #nosec G304 -- root is the walked-up path of this test file
		t.Fatalf("could not locate the repo root from %q (no go.mod found walking up)", filepath.Dir(file))
	}
	return root
}

// registeredRoutes extracts every route registration from the routes.go source
// with a minimal AST walk, in source order. It finds both registration styles
// that coexist in routes.go: plain mux.HandleFunc("<pattern>", ...) calls, and
// Huma operations (huma.Register(api, huma.Operation{..., Path: "<path>", ...},
// handler)). The two are combined into a single registered set so the contract
// invariant holds regardless of how a route is mounted: a Huma operation and a
// plain handler both describe one registered route.
func registeredRoutes(t *testing.T, routesPath string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, routesPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", routesPath, err)
	}
	var patterns []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		switch sel.Sel.Name {
		case "HandleFunc":
			// Plain mux.HandleFunc("<pattern>", ...) registration. The receiver
			// variable name is irrelevant to the pattern.
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				// A non-literal pattern cannot be matched against the contract by
				// name; surface it so a refactor that drops the literal is caught.
				t.Errorf("%s: mux.HandleFunc pattern is not a string literal (at %s); the contract test cannot match it",
					filepath.Base(routesPath), fset.Position(lit.Pos()))
				return true
			}
			patterns = append(patterns, lit.Value[1:len(lit.Value)-1])
		case "Register":
			// Huma operation: huma.Register(api, huma.Operation{..., Path: "<path>", ...}, handler).
			// Only the huma package's Register is a route registration.
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "huma" {
				return true
			}
			if len(call.Args) < 2 {
				return true
			}
			op, ok := call.Args[1].(*ast.CompositeLit)
			if !ok {
				return true
			}
			path, ok := humaOperationPath(op)
			if !ok {
				return true
			}
			patterns = append(patterns, path)
		}
		return true
	})
	return patterns
}

// humaOperationPath returns the Path string literal from a huma.Operation
// composite literal, if present. A non-literal Path cannot be matched against
// the contract by name, so it is reported and treated as absent.
func humaOperationPath(op *ast.CompositeLit) (string, bool) {
	for _, elt := range op.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Path" {
			continue
		}
		lit, ok := kv.Value.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		return lit.Value[1 : len(lit.Value)-1], true
	}
	return "", false
}

// readOpenAPIInfo parses the OpenAPI YAML and returns the contract version,
// the documented path keys, and the tag names.
func readOpenAPIInfo(t *testing.T, specPath string) openAPIInfo {
	t.Helper()
	data, err := os.ReadFile(specPath) // #nosec G304 -- specPath is the repo-root-joined spec path
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	info := openAPIInfo{}
	if m, ok := doc["info"].(map[string]interface{}); ok {
		if v, ok := m["version"].(string); ok {
			info.Version = v
		}
	}
	if paths, ok := doc["paths"].(map[string]interface{}); ok {
		info.Paths = make([]string, 0, len(paths))
		for k := range paths {
			info.Paths = append(info.Paths, k)
		}
	}
	if tags, ok := doc["tags"].([]interface{}); ok {
		for _, e := range tags {
			if m, ok := e.(map[string]interface{}); ok {
				if name, ok := m["name"].(string); ok {
					info.Tags = append(info.Tags, name)
				}
			}
		}
	}
	return info
}

// readAllowlist reads the undocumented-routes allowlist: blank lines and lines
// starting with "#" are ignored. The returned list preserves file order so
// callers can check duplicates.
func readAllowlist(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is the repo-root-joined allowlist path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var entries []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries
}

// checkContract validates the contract invariants given the three parsed inputs
// and returns a list of human-readable issues (empty when the contract holds).
func checkContract(registered, documented, allowlist []string) []string {
	documentedSet := make(map[string]bool, len(documented))
	for _, p := range documented {
		documentedSet[p] = true
	}
	allowSet := make(map[string]bool, len(allowlist))
	allowCount := make(map[string]int, len(allowlist))
	for _, p := range allowlist {
		allowSet[p] = true
		allowCount[p]++
	}

	var issues []string
	seen := make(map[string]bool, len(registered))
	for _, r := range registered {
		if seen[r] {
			// Duplicate registrations in routes.go are legal for the mux; they
			// only need one contract entry, so dedup before checking.
			continue
		}
		seen[r] = true
		switch {
		case documentedSet[r] && allowSet[r]:
			issues = append(issues, fmt.Sprintf(
				"route %q is documented in openapi.yaml but also still listed in undocumented.txt; "+
					"remove the now-stale allowlist entry (the allowlist may only shrink)", r))
		case !documentedSet[r] && !allowSet[r]:
			issues = append(issues, fmt.Sprintf(
				"registered route %q is neither documented in openapi.yaml nor listed in undocumented.txt; "+
					"document the route in openapi.yaml or add it to the allowlist", r))
		}
	}
	for p, n := range allowCount {
		if n > 1 {
			issues = append(issues, fmt.Sprintf(
				"route %q is listed %d times in undocumented.txt; list it once", p, n))
		}
	}
	return issues
}

// contractIssues runs the whole invariant (version check plus the
// registered/document/allowlist lockstep) and returns human-readable issues.
func contractIssues(registered []string, info openAPIInfo, allowlist []string) []string {
	var issues []string
	if !parseSemver(info.Version) {
		issues = append(issues, fmt.Sprintf(
			"openapi.yaml info.version is missing or not a semantic version string (got %q); it is the contract version and must match semver",
			info.Version))
	}
	return append(issues, checkContract(registered, info.Paths, allowlist)...)
}

// TestOpenAPISpecCoversAllRegisteredRoutes is the contract invariant: every
// route registered in pkg/webui/routes.go must be documented in
// docs/api/openapi.yaml or listed in docs/api/undocumented.txt, the two must
// not overlap (a documented route must be removed from the allowlist so it can
// only shrink), and the OpenAPI info.version must be a semantic version.
func TestOpenAPISpecCoversAllRegisteredRoutes(t *testing.T) {
	root := repoRootFromWorkingDir(t)
	routesPath := filepath.Join(root, contractRoutesGo)
	specPath := filepath.Join(root, contractOpenAPIYAML)
	allowPath := filepath.Join(root, contractAllowlist)

	registered := registeredRoutes(t, routesPath)
	if len(registered) == 0 {
		t.Fatalf("parsed zero registered routes from %s; the routes.go parser is broken", routesPath)
	}
	info := readOpenAPIInfo(t, specPath)
	allowlist := readAllowlist(t, allowPath)

	issues := contractIssues(registered, info, allowlist)
	if len(issues) > 0 {
		t.Fatalf("backend contract out of lockstep (%d issue(s)):\n  %s",
			len(issues), strings.Join(issues, "\n  "))
	}
	t.Logf("contract holds: %d registered routes, %d documented paths, %d allowlist entries, version %s",
		len(registered), len(info.Paths), len(allowlist), info.Version)
}

// TestOpenAPIContractTagsMirrorInventoryFamilies guards the tag set that later
// items attach paths to: one tag per family, named exactly as the route
// inventory groups them.
func TestOpenAPIContractTagsMirrorInventoryFamilies(t *testing.T) {
	want := []string{
		"conversation/query", "files", "git", "settings", "terminal",
		"workspace/instances", "sync/txn", "sessions", "search",
		"diagnostics", "design", "starters", "misc/static",
	}
	root := repoRootFromWorkingDir(t)
	info := readOpenAPIInfo(t, filepath.Join(root, contractOpenAPIYAML))

	got := append([]string(nil), info.Tags...)
	sort.Strings(got)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if len(got) != len(wantSorted) {
		t.Fatalf("openapi.yaml has %d tags, want %d (one per family)\n got: %v\nwant: %v",
			len(got), len(wantSorted), got, wantSorted)
	}
	for i := range wantSorted {
		if got[i] != wantSorted[i] {
			t.Errorf("tag %d = %q, want %q", i, got[i], wantSorted[i])
		}
	}
}

// TestContractParserDetectsStaleAndMissingRoutes is the negative case: the
// invariant actually fires. It builds a tiny synthetic tree (a two-route
// routes.go, a one-path openapi.yaml, and an allowlist) and checks both
// directions — a documented route left in the allowlist, and a registered
// route that is neither documented nor allowed.
func TestContractParserDetectsStaleAndMissingRoutes(t *testing.T) {
	root := t.TempDir()
	routesPath := filepath.Join(root, contractRoutesGo)
	specPath := filepath.Join(root, contractOpenAPIYAML)
	allowPath := filepath.Join(root, contractAllowlist)
	if err := os.MkdirAll(filepath.Dir(routesPath), 0o755); err != nil { // #nosec G301 -- test fixture
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil { // #nosec G301 -- test fixture
		t.Fatalf("mkdir: %v", err)
	}

	// /api/foo is registered and documented, but the allowlist keeps a stale
	// entry for it AND omits the registered /api/bar route. /api/huma is a
	// Huma operation (registered via huma.Register) that is neither documented
	// nor allowed, so the invariant must flag it too — this proves the parser
	// discovers Huma routes, not just plain mux patterns.
	routesSrc := "package webui\n" +
		"import \"net/http\"\n" +
		"func register(m *http.ServeMux) {\n" +
		"\tm.HandleFunc(\"/api/foo\", h)\n" +
		"\tm.HandleFunc(\"/api/bar\", h)\n" +
		"\thuma.Register(api, huma.Operation{Path: \"/api/huma\"}, h)\n" +
		"}\n"
	if err := os.WriteFile(routesPath, []byte(routesSrc), 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write routes: %v", err)
	}
	specSrc := "openapi: 3.1.0\ninfo:\n  title: t\n  version: 1.0.0\npaths:\n  /api/foo:\n    get: {}\n"
	if err := os.WriteFile(specPath, []byte(specSrc), 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write spec: %v", err)
	}
	allowSrc := "# header\n/api/foo\n\n"                                     // stale entry for /api/foo; /api/bar and /api/huma missing
	if err := os.WriteFile(allowPath, []byte(allowSrc), 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write allowlist: %v", err)
	}

	registered := registeredRoutes(t, routesPath)
	info := readOpenAPIInfo(t, specPath)
	allowlist := readAllowlist(t, allowPath)

	issues := contractIssues(registered, info, allowlist)
	joined := strings.Join(issues, "\n")

	for _, r := range []string{"/api/foo", "/api/bar", "/api/huma"} {
		if !containsRoute(registered, r) {
			t.Fatalf("parser dropped registered route %q: %v", r, registered)
		}
	}
	if !strings.Contains(joined, "remove the now-stale allowlist entry") {
		t.Errorf("did not flag stale allowlist entry for documented /api/foo:\n%s", joined)
	}
	// Both the plain /api/bar and the Huma /api/huma routes are missing from
	// the contract; the invariant must flag each by name. This is what proves
	// the parser discovers Huma operations (the plain route alone could not
	// drive these two assertions).
	for _, r := range []string{"/api/bar", "/api/huma"} {
		if !strings.Contains(joined, fmt.Sprintf("registered route %q is neither documented", r)) {
			t.Errorf("did not flag missing route %q:\n%s", r, joined)
		}
	}
}

// containsRoute reports whether the slice of registered route patterns contains
// the given pattern.
func containsRoute(paths []string, p string) bool {
	for _, x := range paths {
		if x == p {
			return true
		}
	}
	return false
}

// TestParseSemver checks the semantic-version matcher the contract version
// relies on.
func TestParseSemver(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"1.0.0", true},
		{"0.0.0", true},
		{"1.2.3", true},
		{"10.20.30", true},
		{"1.0.0-rc.1", true},
		{"1.0.0+build.5", true},
		{"1.0.0-rc.1+build.5", true},
		{"1.0.0-beta", true},
		{"1.0", false},
		{"1.0.0.0", false},
		{"01.2.3", false}, // leading zero in a component
		{"v1.0.0", false}, // leading v is not semver
		{"1..0", false},   // empty component
		{"1.0.x", false},
		{"", false},
		{"abc", false},
	}
	for _, c := range cases {
		if got := parseSemver(c.in); got != c.want {
			t.Errorf("parseSemver(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
