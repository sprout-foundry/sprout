//go:build !js

package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sprout-foundry/sprout/pkg/apiconformance"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// setConformanceFlags configures the api conformance flag variables for a test
// and restores the originals afterwards.
func setConformanceFlags(t *testing.T, base string, families []string, spec string, asJSON bool) {
	t.Helper()
	origBase, origFamilies, origSpec, origJSON := apiConcBaseURL, apiConcFamilies, apiConcSpec, apiConcJSON
	t.Cleanup(func() {
		apiConcBaseURL, apiConcFamilies, apiConcSpec, apiConcJSON = origBase, origFamilies, origSpec, origJSON
	})
	apiConcBaseURL, apiConcFamilies, apiConcSpec, apiConcJSON = base, families, spec, asJSON
}

// writeConformanceSpec writes a minimal OpenAPI document whose operations are
// exactly the default probes' paths (so every probe maps to a documented
// operation) and returns its path. The document has no concrete 200 schemas,
// so a probe's body only needs to be valid JSON to pass.
func writeConformanceSpec(t *testing.T) string {
	t.Helper()
	probes := apiconformance.DefaultProbes()
	famSet := map[string]struct{}{}
	paths := map[string]any{}
	for _, p := range probes {
		famSet[p.Family] = struct{}{}
		method := map[string]any{
			"operationId": p.OperationID,
			"summary":     "probe " + p.OperationID,
			"tags":        []string{p.Family},
		}
		if paths[p.Path] == nil {
			paths[p.Path] = map[string]any{p.Method: method}
		} else {
			paths[p.Path].(map[string]any)[p.Method] = method
		}
	}
	fams := make([]string, 0, len(famSet))
	for f := range famSet {
		fams = append(fams, f)
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Test contract", "version": "9.9.9"},
		"tags":    tagsYAML(fams),
		"paths":   paths,
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

// tagsYAML renders a family-name list as the OpenAPI tags structure.
func tagsYAML(names []string) []any {
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n})
	}
	return out
}

// newCatchAllAPIServer stands up a server answering 200 + a valid JSON object
// for every /api/ path (a conformance-green implementation).
func newCatchAllAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	writeOK := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeOK(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// countProbesForFamily counts the default probes in the given family.
func countProbesForFamily(family string) int {
	n := 0
	for _, p := range apiconformance.DefaultProbes() {
		if p.Family == family {
			n++
		}
	}
	return n
}

// TestAPIConformanceAllPass runs the command's full path against a green
// implementation: a nil error and a report (JSON or human) on stdout.
func TestAPIConformanceAllPass(t *testing.T) {
	srv := newCatchAllAPIServer(t)
	spec := writeConformanceSpec(t)

	t.Run("json", func(t *testing.T) {
		setConformanceFlags(t, srv.URL, nil, spec, true)
		out := testutil.CaptureStdout(t, func() {
			if err := runAPIConformance(apiConformanceCmd, nil); err != nil {
				t.Fatalf("runAPIConformance: %v", err)
			}
		})
		var doc struct {
			AllPassed bool `json:"allPassed"`
			Results   []struct {
				OperationID string `json:"operationId"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
			t.Fatalf("stdout is not the report JSON: %v\n%s", err, out)
		}
		if !doc.AllPassed {
			t.Error("allPassed=false, want true")
		}
		if len(doc.Results) != len(apiconformance.DefaultProbes()) {
			t.Errorf("results = %d, want %d (every default probe)", len(doc.Results), len(apiconformance.DefaultProbes()))
		}
	})

	t.Run("human", func(t *testing.T) {
		setConformanceFlags(t, srv.URL, nil, spec, false)
		out := testutil.CaptureStdout(t, func() {
			if err := runAPIConformance(apiConformanceCmd, nil); err != nil {
				t.Fatalf("runAPIConformance: %v", err)
			}
		})
		if !strings.Contains(out, "0 failed") {
			t.Errorf("human report does not report zero failures:\n%s", out)
		}
	})
}

// TestAPIConformanceFailureProbesNonZeroError proves the CI contract: when a
// probe fails the command returns an error (which the CLI maps to a non-zero
// exit) while the report is still printed to stdout.
func TestAPIConformanceFailureProbesNonZeroError(t *testing.T) {
	// /api/config answers 500; every other probe path answers 200.
	writeJSON := func(w http.ResponseWriter, status int, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 500, `{"type":"about:blank","title":"boom"}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, 200, `{}`)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	spec := writeConformanceSpec(t)
	setConformanceFlags(t, srv.URL, nil, spec, false)

	var err error
	out := testutil.CaptureStdout(t, func() {
		err = runAPIConformance(apiConformanceCmd, nil)
	})
	if err == nil {
		t.Fatal("a failing probe must yield a non-nil error (non-zero exit)")
	}
	if !strings.Contains(err.Error(), "1 of") {
		t.Errorf("error = %q, want it to name the failing probe count", err)
	}
	if !strings.Contains(out, "get-config") {
		t.Errorf("the printed report should name the failed probe:\n%s", out)
	}
}

// TestAPIConformanceFamilyFilter restricts the run to one family and rejects an
// unknown family name as a usage error.
func TestAPIConformanceFamilyFilter(t *testing.T) {
	srv := newCatchAllAPIServer(t)
	spec := writeConformanceSpec(t)

	setConformanceFlags(t, srv.URL, []string{"settings"}, spec, true)
	var doc struct {
		Results []struct {
			Family string `json:"family"`
		} `json:"results"`
	}
	out := testutil.CaptureStdout(t, func() {
		if err := runAPIConformance(apiConformanceCmd, nil); err != nil {
			t.Fatalf("runAPIConformance: %v", err)
		}
	})
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
		t.Fatalf("stdout is not the report JSON: %v", err)
	}
	if len(doc.Results) != countProbesForFamily("settings") {
		t.Fatalf("results = %d, want the %d settings probes", len(doc.Results), countProbesForFamily("settings"))
	}
	for _, r := range doc.Results {
		if r.Family != "settings" {
			t.Errorf("a non-settings probe leaked into the filtered run: %s", r.Family)
		}
	}

	// An unknown family is a usage error (exit 2 via the CLI).
	setConformanceFlags(t, srv.URL, []string{"no-such-family"}, spec, false)
	err := runAPIConformance(apiConformanceCmd, nil)
	if err == nil {
		t.Fatal("an unknown family must be rejected")
	}
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Errorf("unknown-family error = %v, want a usage error (non-usage would exit 1)", err)
	}
}

// TestAPIConformanceMissingBaseURL is a usage error.
func TestAPIConformanceMissingBaseURL(t *testing.T) {
	setConformanceFlags(t, "", nil, "", false)
	err := runAPIConformance(apiConformanceCmd, nil)
	if err == nil {
		t.Fatal("missing --base-url must be rejected")
	}
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Errorf("missing --base-url error = %v, want a usage error", err)
	}
}

// TestLocateOpenAPIContract verifies the spec-locator: it finds the committed
// document by walking up from a nested directory, and fails cleanly when the
// document is absent.
func TestLocateOpenAPIContract(t *testing.T) {
	// From the cmd package's own directory the walk-up lands on the repo root's
	// committed contract.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	found, err := locateOpenAPIContract(cwd)
	if err != nil {
		t.Fatalf("locateOpenAPIContract from the cmd package: %v", err)
	}
	if !strings.HasSuffix(found, filepath.Join("docs", "api", "openapi.yaml")) {
		t.Errorf("located %q, want the repo's docs/api/openapi.yaml", found)
	}

	// A nested temp tree with its own document.
	nested := filepath.Join(t.TempDir(), "a", "b")
	if err := os.MkdirAll(filepath.Join(nested, "docs", "api"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	local := filepath.Join(nested, "docs", "api", "openapi.yaml")
	if err := os.WriteFile(local, []byte("openapi: 3.1.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := locateOpenAPIContract(nested)
	if err != nil {
		t.Fatalf("locateOpenAPIContract in a nested tree: %v", err)
	}
	if got != filepath.Join(nested, "docs", "api", "openapi.yaml") {
		t.Errorf("located %q, want %q", got, filepath.Join(nested, "docs", "api", "openapi.yaml"))
	}

	// No document anywhere up the tree.
	empty := t.TempDir()
	if _, err := locateOpenAPIContract(empty); err == nil {
		t.Error("expected an error when the contract document is absent")
	}
}

// TestAPIConformanceFindsCommittedContract runs the loader against the real
// committed contract (found by walking up from the cmd package) and asserts the
// document the command actually parses is the one the repository ships.
func TestAPIConformanceFindsCommittedContract(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	specPath, err := locateOpenAPIContract(cwd)
	if err != nil {
		t.Fatalf("locate contract: %v", err)
	}
	spec, err := apiconformance.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("LoadSpec on the committed contract: %v", err)
	}
	if spec.ContractVersion() != "1.0.0" {
		t.Errorf("contract version = %q, want 1.0.0", spec.ContractVersion())
	}
	if len(spec.Families()) == 0 {
		t.Error("the committed contract declares no families")
	}
	if len(spec.Operations) == 0 {
		t.Error("the committed contract has no operations")
	}
}
