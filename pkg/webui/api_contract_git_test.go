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
// (contract.5, TestGitReadRoutesDocumentedAsGET) and the write endpoints
// (contract.6, TestGitWriteRoutesDocumentedAsPOST). It was split out of
// api_contract_test.go to keep that file focused on the lockstep invariant
// and under the line budget. The shared openapi.yaml / undocumented.txt
// path constants and the repo-root resolver it relies on live in
// api_contract_test.go.

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

// get200Ref returns the $ref string of a path operation's 200 JSON response,
// failing the test if that response or its schema $ref is absent.
func get200Ref(t *testing.T, op map[string]interface{}, route string) string {
	return getSuccessRef(t, op, route, "200")
}

// getSuccessRef returns the $ref string of a path operation's success JSON
// response, failing the test if that response or its schema $ref is absent.
// The code is the literal HTTP status key in the responses map.
func getSuccessRef(t *testing.T, op map[string]interface{}, route, code string) string {
	t.Helper()
	responses := asMap(t, op["responses"], "responses for "+strings.ToUpper(code))
	r, ok := responses[code]
	if !ok {
		t.Fatalf("operation for %q has no %s response", route, code)
	}
	content := asMap(t, asMap(t, r, code+" response for "+route)["content"], code+" content for "+route)
	jsonMedia, found := content["application/json"]
	if !found {
		t.Fatalf("operation for %q %s response has no application/json content", route, code)
	}
	schema := asMap(t, jsonMedia, "application/json media for "+route)["schema"]
	schemaMap := asMap(t, schema, code+" schema for "+route)
	ref, found := schemaMap["$ref"]
	if !found {
		t.Fatalf("operation for %q %s schema is not a $ref", route, code)
	}
	refStr, ok := ref.(string)
	if !ok {
		t.Fatalf("operation for %q %s schema $ref is not a string", route, code)
	}
	return refStr
}

// assertRefResolvesToComponentSchema fails the test unless a $ref of the form
// #/components/schemas/<name> names a schema defined in the document.
func assertRefResolvesToComponentSchema(t *testing.T, doc map[string]interface{}, ref string, route string) {
	t.Helper()
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		t.Fatalf("success schema $ref for %q does not target components.schemas (got %q)", route, ref)
	}
	name := strings.TrimPrefix(ref, prefix)
	components := asMap(t, doc["components"], "components")
	schemas := asMap(t, components["schemas"], "components.schemas")
	if _, found := schemas[name]; !found {
		t.Fatalf("success schema $ref %q for %q does not resolve to a defined component schema", ref, route)
	}
}

// TestGitReadRoutesDocumentedAsGET pins the git read endpoints: each is a
// documented GET operation tagged git with a 200 response that $refs a defined
// component schema, and each has been removed from the undocumented allowlist.
// The general lockstep test already proves the routes exist somewhere in the
// contract; this one proves they are documented in the git-read shape this
// family requires.
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
		ref := get200Ref(t, op, route)
		assertRefResolvesToComponentSchema(t, doc, ref, route)
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

// gitWriteRoutesWithBody are the git write routes whose handlers decode a
// JSON request body; the rest of gitWriteRoutes (commit-message, deep-review,
// pull, push, stage-all, unstage-all) take no body. Each route here must be
// documented with a requestBody whose application/json schema $refs a defined
// component schema — the request half of "schemas derive from the handlers'
// actual types" (the success-response half is asserted inline in the test).
var gitWriteRoutesWithBody = map[string]bool{
	"/api/git/branch/create":         true,
	"/api/git/checkout":              true,
	"/api/git/commit":                true,
	"/api/git/confirm":               true,
	"/api/git/deep-review/fix":       true,
	"/api/git/deep-review/fix/start": true,
	"/api/git/discard":               true,
	"/api/git/pull-request":          true,
	"/api/git/revert":                true,
	"/api/git/stage":                 true,
	"/api/git/unstage":               true,
	"/api/git/worktree/checkout":     true,
	"/api/git/worktree/create":       true,
	"/api/git/worktree/remove":       true,
}

// assertGitRouteRegistered fails the test unless the route is registered as a
// HandleFunc pattern in routes.go. The lockstep invariant (every registered
// route is documented) is proven by TestOpenAPISpecCoversAllRegisteredRoutes;
// this checks the converse for the git family so a path documented in
// openapi.yaml but never registered is caught rather than silently shipped.
func assertGitRouteRegistered(t *testing.T, registered map[string]bool, route string) {
	t.Helper()
	if !registered[route] {
		t.Errorf("documented git route %q is not registered as a HandleFunc pattern in routes.go", route)
	}
}

// getRequestBodyRef returns the $ref string of a path operation's request
// body's application/json schema, failing the test if that body, its JSON
// content, or its schema $ref is absent.
func getRequestBodyRef(t *testing.T, op map[string]interface{}, route string) string {
	t.Helper()
	body := asMap(t, op["requestBody"], "requestBody for "+route)
	content := asMap(t, body["content"], "requestBody content for "+route)
	jsonMedia, found := content["application/json"]
	if !found {
		t.Fatalf("requestBody for %q has no application/json content", route)
	}
	schema := asMap(t, jsonMedia, "application/json media for "+route)["schema"]
	schemaMap := asMap(t, schema, "requestBody schema for "+route)
	ref, found := schemaMap["$ref"]
	if !found {
		t.Fatalf("requestBody schema for %q is not a $ref", route)
	}
	refStr, ok := ref.(string)
	if !ok {
		t.Fatalf("requestBody schema $ref for %q is not a string", route)
	}
	return refStr
}

// TestGitWriteRoutesDocumentedAsPOST pins the git write endpoints: each is
// registered in routes.go, a documented POST operation tagged git, removed
// from the undocumented allowlist, and documented with a 2xx success response
// that $refs a defined component schema; every body-bearing route also
// documents a request body whose schema $refs a defined component schema. The
// general lockstep test already proves the routes exist somewhere in the
// contract; this one proves they are documented in the git-write shape this
// family requires.
func TestGitWriteRoutesDocumentedAsPOST(t *testing.T) {
	root := repoRootFromWorkingDir(t)
	routesPath := filepath.Join(root, contractRoutesGo)
	specPath := filepath.Join(root, contractOpenAPIYAML)
	allowPath := filepath.Join(root, contractAllowlist)

	doc := readOpenAPIDoc(t, specPath)
	allowlist := readAllowlist(t, allowPath)
	allowSet := make(map[string]bool, len(allowlist))
	for _, p := range allowlist {
		allowSet[p] = true
	}
	registeredPatterns := registeredRoutes(t, routesPath)
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

		// Body-bearing routes must document a request body whose schema $refs a
		// defined component schema.
		if gitWriteRoutesWithBody[route] {
			reqRef := getRequestBodyRef(t, op, route)
			assertRefResolvesToComponentSchema(t, doc, reqRef, route)
		}

		// A 2xx success response must $ref a defined component schema. The
		// git write family is 200-only, but resolve the smallest 2xx key so
		// the assertion still holds if a route ever gains a 201.
		responses := asMap(t, op["responses"], "responses for POST "+route)
		var successCode string
		for code := range responses {
			if len(code) == 3 && code[0] == '2' && (successCode == "" || code < successCode) {
				successCode = code
			}
		}
		if successCode == "" {
			t.Fatalf("POST operation for %q has no 2xx response", route)
			continue
		}
		ref := getSuccessRef(t, op, route, successCode)
		assertRefResolvesToComponentSchema(t, doc, ref, route)
	}
}
