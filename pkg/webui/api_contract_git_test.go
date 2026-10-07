//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// This file holds the git-family contract assertions: the read endpoints
// (TestGitReadRoutesDocumentedAsGET) and the write endpoints
// (TestGitWriteRoutesDocumentedAsPOST). It was split out of
// api_contract_test.go to keep that file focused on the lockstep invariant
// and under the line budget. The shared openapi.yaml / undocumented.txt
// path constants and the repo-root resolver it relies on live in
// api_contract_test.go.
//
// Every JSON route is a Huma operation, so the generated document carries only
// what Huma produces: a 2xx success response, a default error response, the
// family tag, and (for body-bearing routes) a request body. The git family
// uses the thin-handler migration pattern (no-op writtenResponseOutput), so its
// operations document a 200 response plus the default error — no per-route
// request/response schemas. These tests pin that generated shape so a git route
// cannot silently ship in a different (e.g. hand-written) form.

// gitReadRoutes are the git read endpoints the daemon exposes over GET. They
// are the routes this file's OpenAPI paths document under the git tag.
var gitReadRoutes = []string{
	"/api/git/status",
	"/api/git/diff",
	"/api/git/log",
	"/api/git/branches",
	"/api/git/worktrees",
	"/api/git/deep-review/fix/status",
	"/api/git/commit/show",
	"/api/git/commit/show/file",
}

// readOpenAPIDoc parses the full OpenAPI document into a generic tree so a
// test can inspect individual path operations and components.
func readOpenAPIDoc(t *testing.T, specPath string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(specPath) // #nosec G304 -- specPath is the repo-root-joined spec path
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	return doc
}

// asMap is a helper that type-asserts a value to a map, failing the test on a
// mismatch so navigation errors surface with a clear message.
func asMap(t *testing.T, v interface{}, what string) map[string]interface{} {
	t.Helper()
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("expected %s to be a map, got %T", what, v)
	}
	return m
}

// gitOp resolves the operation object for a documented path and HTTP method,
// failing the test if the path or its operation is absent.
func gitOp(t *testing.T, doc map[string]interface{}, route, method string) map[string]interface{} {
	t.Helper()
	paths := asMap(t, doc["paths"], "paths")
	path, ok := paths[route]
	if !ok {
		t.Fatalf("route %q is not a documented path in openapi.yaml", route)
	}
	ops := asMap(t, path, "path "+route)
	op, ok := ops[method]
	if !ok {
		t.Fatalf("route %q has no %s operation in openapi.yaml", route, strings.ToUpper(method))
	}
	return asMap(t, op, strings.ToUpper(method)+" operation for "+route)
}

// gitReadOp resolves the GET operation object for a documented path. It fails
// the test if the path or its GET operation is absent.
func gitReadOp(t *testing.T, doc map[string]interface{}, route string) map[string]interface{} {
	return gitOp(t, doc, route, "get")
}

// gitWriteOp resolves the POST operation object for a documented path. It
// fails the test if the path or its POST operation is absent.
func gitWriteOp(t *testing.T, doc map[string]interface{}, route string) map[string]interface{} {
	return gitOp(t, doc, route, "post")
}

// assertOperationTaggedGit fails the test unless the path operation carries
// the git tag.
func assertOperationTaggedGit(t *testing.T, op map[string]interface{}, route string) {
	t.Helper()
	tags, ok := op["tags"]
	if !ok {
		t.Fatalf("operation for %q has no tags in openapi.yaml", route)
		return
	}
	list, ok := tags.([]interface{})
	if !ok {
		t.Fatalf("operation for %q has a tags that is not a list", route)
		return
	}
	for _, e := range list {
		if s, ok := e.(string); ok && s == "git" {
			return
		}
	}
	t.Fatalf("operation for %q is not tagged git (tags: %v)", route, list)
}

// assertSuccessResponse documents that the operation carries a 2xx success
// response. The Huma thin-handler shape for the git family is a bare 200
// (description only, no schema); this pins that the success response is
// present under its canonical key.
func assertSuccessResponse(t *testing.T, op map[string]interface{}, route string) {
	t.Helper()
	responses := asMap(t, op["responses"], "responses for "+route)
	if _, found := responses["200"]; !found {
		t.Fatalf("operation for %q has no 200 success response (responses: %v)", route, responses)
	}
}

// assertDefaultErrorResponse documents that the operation carries a default
// error response $ref-ing Huma's ErrorModel — the generated minimum every
// Huma operation must have.
func assertDefaultErrorResponse(t *testing.T, op map[string]interface{}, route string) {
	t.Helper()
	responses := asMap(t, op["responses"], "responses for "+route)
	def, found := responses["default"]
	if !found {
		t.Fatalf("operation for %q has no default error response (responses: %v)", route, responses)
	}
	d, ok := def.(map[string]interface{})
	if !ok {
		t.Fatalf("default response for %q is not a map", route)
	}
	content, ok := d["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("default response for %q has no content", route)
	}
	media, ok := content["application/problem+json"].(map[string]interface{})
	if !ok {
		t.Fatalf("default response for %q is not application/problem+json", route)
	}
	schema, ok := media["schema"].(map[string]interface{})
	if !ok {
		t.Fatalf("default response for %q has no schema", route)
	}
	ref, ok := schema["$ref"].(string)
	if !ok || ref != "#/components/schemas/ErrorModel" {
		t.Fatalf("default response for %q does not $ref the ErrorModel (got %v)", route, schema["$ref"])
	}
}

// TestGitReadRoutesDocumentedAsGET pins the git read endpoints: each is a
// documented GET operation tagged git with a 200 success response and a
// default error response, and each has been removed from the undocumented
// allowlist. The general lockstep test already proves the routes exist
// somewhere in the contract; this one proves they are documented in the
// generated Huma git-read shape this family requires.
func TestGitReadRoutesDocumentedAsGET(t *testing.T) {
	root := repoRootFromWorkingDir(t)
	specPath := filepath.Join(root, contractOpenAPIYAML)
	allowPath := filepath.Join(root, contractAllowlist)

	doc := readOpenAPIDoc(t, specPath)
	allowlist := readAllowlist(t, allowPath)
	allowSet := make(map[string]bool, len(allowlist))
	for _, p := range allowlist {
		allowSet[p] = true
	}

	for _, route := range gitReadRoutes {
		if allowSet[route] {
			t.Errorf("route %q is documented but still listed in undocumented.txt (the allowlist must shrink)", route)
		}
		op := gitReadOp(t, doc, route)
		assertOperationTaggedGit(t, op, route)
		assertSuccessResponse(t, op, route)
		assertDefaultErrorResponse(t, op, route)
	}
}

// gitWriteRoutes are the git write (mutating) endpoints the daemon exposes
// over POST. They are the routes this file's OpenAPI paths document under the
// git tag alongside the read endpoints above.
var gitWriteRoutes = []string{
	"/api/git/branch/create",
	"/api/git/checkout",
	"/api/git/commit",
	"/api/git/commit-message",
	"/api/git/confirm",
	"/api/git/deep-review",
	"/api/git/deep-review/fix",
	"/api/git/deep-review/fix/start",
	"/api/git/discard",
	"/api/git/pull",
	"/api/git/pull-request",
	"/api/git/push",
	"/api/git/revert",
	"/api/git/stage",
	"/api/git/stage-all",
	"/api/git/unstage",
	"/api/git/unstage-all",
	"/api/git/worktree/checkout",
	"/api/git/worktree/create",
	"/api/git/worktree/remove",
}

// assertGitRouteRegistered fails the test unless the route is registered as a
// Huma operation (its pattern appears in the AST-walked route files). The
// lockstep invariant (every registered route is documented) is proven by
// TestOpenAPISpecCoversAllRegisteredRoutes; this checks the converse for the
// git family so a path documented in openapi.yaml but never registered is
// caught rather than silently shipped.
func assertGitRouteRegistered(t *testing.T, registered map[string]bool, route string) {
	t.Helper()
	if !registered[route] {
		t.Errorf("documented git route %q is not registered as a Huma operation in the route files", route)
	}
}

// TestGitWriteRoutesDocumentedAsPOST pins the git write endpoints: each is
// registered, a documented POST operation tagged git, removed from the
// undocumented allowlist, and documented with a 200 success response plus a
// default error response. The general lockstep test already proves the routes
// exist somewhere in the contract; this one proves they are documented in the
// generated Huma git-write shape this family requires.
func TestGitWriteRoutesDocumentedAsPOST(t *testing.T) {
	root := repoRootFromWorkingDir(t)
	specPath := filepath.Join(root, contractOpenAPIYAML)
	allowPath := filepath.Join(root, contractAllowlist)

	doc := readOpenAPIDoc(t, specPath)
	allowlist := readAllowlist(t, allowPath)
	allowSet := make(map[string]bool, len(allowlist))
	for _, p := range allowlist {
		allowSet[p] = true
	}
	registeredPatterns := registeredRoutes(t, contractRouteFiles(root)...)
	registered := make(map[string]bool, len(registeredPatterns))
	for _, p := range registeredPatterns {
		registered[p] = true
	}

	for _, route := range gitWriteRoutes {
		if allowSet[route] {
			t.Errorf("route %q is documented but still listed in undocumented.txt (the allowlist must shrink)", route)
		}
		assertGitRouteRegistered(t, registered, route)
		op := gitWriteOp(t, doc, route)
		assertOperationTaggedGit(t, op, route)
		assertSuccessResponse(t, op, route)
		assertDefaultErrorResponse(t, op, route)
	}
}
