package apiconformance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// findRepoRootFromTestFile resolves the repo root (the directory holding go.mod)
// by walking up from this test file's own path. It mirrors the webui contract
// test's helper so the loader test reads the committed contract regardless of
// the working directory.
func findRepoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("could not resolve this test file's path via runtime.Caller")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil { // #nosec G304 -- walking up to the repo root
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate the repo root from %q (no go.mod walking up)", file)
		}
		dir = parent
	}
}

// objectSchema is an inline JSON-Schema requiring a JSON object; it stands in
// for the generated document's "type: object, additionalProperties: {}" 200
// schema.
func objectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": map[string]any{}}
}

// cannedSpec builds a small in-memory spec for the runner test: a couple of
// schema-backed GETs (get-config, schemaOp), a schemaless GET (get-stats,
// gitStatus), and one operation (brokenOp) whose canned response will be a 500.
// Families are chosen so the per-family roll-up is non-trivial: settings and
// diagnostics each mix a pass and a failure, git is all-pass.
func cannedSpec() *Spec {
	return &Spec{
		Info:              Info{Version: "9.9.9"},
		Tags:              []string{"settings", "diagnostics", "git"},
		ComponentsSchemas: map[string]any{},
		Operations: []Operation{
			{
				OperationID: "get-config", Method: "get", Path: "/api/config",
				Tags: []string{"settings"}, OKStatusCode: 200,
				HasOKSchema: true, OKSchemaSchema: objectSchema(),
			},
			{
				OperationID: "get-stats", Method: "get", Path: "/api/stats",
				Tags: []string{"diagnostics"}, OKStatusCode: 200,
			},
			{
				OperationID: "gitStatus", Method: "get", Path: "/api/git/status",
				Tags: []string{"git"}, OKStatusCode: 200,
			},
			{
				OperationID: "brokenOp", Method: "get", Path: "/api/broken",
				Tags: []string{"settings"}, OKStatusCode: 200,
			},
			{
				OperationID: "schemaOp", Method: "get", Path: "/api/schema",
				Tags: []string{"diagnostics"}, OKStatusCode: 200,
				HasOKSchema: true, OKSchemaSchema: map[string]any{"type": "object"},
			},
		},
	}
}

// cannedProbes point at the canned server's paths, in the same order as the
// canned spec's families: settings (pass + 500), diagnostics (pass + shape
// failure), git (pass).
func cannedProbes() []Probe {
	get := http.MethodGet
	return []Probe{
		{OperationID: "get-config", Method: get, Path: "/api/config", ExpectedStatus: 200, Family: "settings"},
		{OperationID: "brokenOp", Method: get, Path: "/api/broken", ExpectedStatus: 200, Family: "settings"},
		{OperationID: "get-stats", Method: get, Path: "/api/stats", ExpectedStatus: 200, Family: "diagnostics"},
		{OperationID: "schemaOp", Method: get, Path: "/api/schema", ExpectedStatus: 200, Family: "diagnostics"},
		{OperationID: "gitStatus", Method: get, Path: "/api/git/status", ExpectedStatus: 200, Family: "git"},
	}
}

// newCannedServer stands up an httptest server: 200 + a valid object for
// /api/config, 200 + {} for the schemaless paths, a 500 for /api/broken, and
// 200 + a JSON array for /api/schema (which violates the object schema).
func newCannedServer(t *testing.T) *httptest.Server {
	t.Helper()
	writeJSON := func(w http.ResponseWriter, status int, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"port":1700,"workspaceRoot":"/tmp/ws"}`)
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{}`)
	})
	mux.HandleFunc("/api/git/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{}`)
	})
	mux.HandleFunc("/api/broken", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 500, `{"type":"internal","title":"boom"}`)
	})
	mux.HandleFunc("/api/schema", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `[1,2,3]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// resultFor finds the result for the given operation id.
func resultFor(t *testing.T, rep *Report, opID string) Result {
	t.Helper()
	for _, r := range rep.Results {
		if r.OperationID == opID {
			return r
		}
	}
	t.Fatalf("no result for %q in %d results", opID, len(rep.Results))
	return Result{}
}

// TestRunSafeAgainstCannedServer is the runner self-test: it drives RunSafe
// against a canned httptest server and asserts status classification, the shape
// check, per-family aggregation, AllPassed/Failed, and Render.
func TestRunSafeAgainstCannedServer(t *testing.T) {
	srv := newCannedServer(t)
	spec := cannedSpec()
	suite := New(srv.URL, spec, WithProbes(cannedProbes()))

	rep, err := suite.RunSafe(context.Background())
	if err != nil {
		t.Fatalf("RunSafe returned error: %v", err)
	}

	if len(rep.Results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(rep.Results))
	}
	if rep.AllPassed() {
		t.Fatal("AllPassed() = true, want false (there are two failures)")
	}

	// Passes: a valid object, a schemaless {}, and the git {} path.
	for _, opID := range []string{"get-config", "get-stats", "gitStatus"} {
		r := resultFor(t, rep, opID)
		if !r.Passed {
			t.Errorf("%s: passed=false, want true (reason=%q status=%d)", opID, r.Reason, r.Status)
		}
		if r.Status != 200 {
			t.Errorf("%s: status=%d, want 200", opID, r.Status)
		}
	}

	// Failure 1: the 500 is classified as a status mismatch.
	broken := resultFor(t, rep, "brokenOp")
	if broken.Passed {
		t.Error("brokenOp: passed=true, want false")
	}
	if broken.Status != 500 {
		t.Errorf("brokenOp: status=%d, want 500", broken.Status)
	}
	if !strings.Contains(broken.Reason, "500") {
		t.Errorf("brokenOp: reason %q does not mention the 500 status", broken.Reason)
	}

	// Failure 2: a 200 whose body (an array) violates the object schema.
	schema := resultFor(t, rep, "schemaOp")
	if schema.Passed {
		t.Error("schemaOp: passed=true, want false (shape violation must be caught)")
	}
	if schema.Status != 200 {
		t.Errorf("schemaOp: status=%d, want 200 (the shape check, not the status, fails)", schema.Status)
	}
	if !strings.Contains(schema.Reason, "body shape") {
		t.Errorf("schemaOp: reason %q does not mark a body-shape failure", schema.Reason)
	}

	// Failed() lists exactly the two failing ops.
	failed := rep.Failed()
	if len(failed) != 2 {
		t.Fatalf("Failed() returned %d results, want 2", len(failed))
	}
	got := map[string]bool{}
	for _, r := range failed {
		got[r.OperationID] = true
	}
	if !got["brokenOp"] || !got["schemaOp"] {
		t.Errorf("Failed() = %v, want exactly {brokenOp, schemaOp}", got)
	}

	// Per-family aggregation: settings {2,1,1}, diagnostics {2,1,1}, git {1,1,0}.
	want := map[string]FamilyResult{
		"settings":    {Family: "settings", Probed: 2, Passed: 1, Failed: 1},
		"diagnostics": {Family: "diagnostics", Probed: 2, Passed: 1, Failed: 1},
		"git":         {Family: "git", Probed: 1, Passed: 1, Failed: 0},
	}
	if len(rep.PerFamily) != len(want) {
		t.Fatalf("PerFamily has %d families, want %d: %v", len(rep.PerFamily), len(want), rep.PerFamily)
	}
	for fam, wr := range want {
		gotF, ok := rep.PerFamily[fam]
		if !ok {
			t.Errorf("PerFamily missing family %q", fam)
			continue
		}
		if gotF != wr {
			t.Errorf("PerFamily[%q] = %+v, want %+v", fam, gotF, wr)
		}
	}

	// Render includes each family section and the failure lines.
	rendered := rep.Render()
	for _, wantSub := range []string{"settings", "diagnostics", "git", "brokenOp", "schemaOp", "status=500"} {
		if !strings.Contains(rendered, wantSub) {
			t.Errorf("Render() missing %q:\n%s", wantSub, rendered)
		}
	}
}

// TestRunSafeAllPass confirms a fully green run reports AllPassed and an empty
// failure list.
func TestRunSafeAllPass(t *testing.T) {
	srv := newCannedServer(t)
	spec := cannedSpec()
	probes := []Probe{
		{OperationID: "get-config", Method: http.MethodGet, Path: "/api/config", ExpectedStatus: 200, Family: "settings"},
		{OperationID: "get-stats", Method: http.MethodGet, Path: "/api/stats", ExpectedStatus: 200, Family: "diagnostics"},
		{OperationID: "gitStatus", Method: http.MethodGet, Path: "/api/git/status", ExpectedStatus: 200, Family: "git"},
	}
	suite := New(srv.URL, spec, WithProbes(probes))
	rep, err := suite.RunSafe(context.Background())
	if err != nil {
		t.Fatalf("RunSafe: %v", err)
	}
	if !rep.AllPassed() {
		t.Errorf("AllPassed() = false, want true; failed: %v", rep.Failed())
	}
	if n := len(rep.Failed()); n != 0 {
		t.Errorf("Failed() = %d results, want 0", n)
	}
	if n := len(rep.PerFamily); n != 3 {
		t.Errorf("PerFamily = %d families, want 3", n)
	}
}

// TestReportJSONStable checks JSON() is deterministic (byte-identical across
// calls) and round-trips the key fields, with families in sorted order.
func TestReportJSONStable(t *testing.T) {
	rep := NewReport([]Result{
		{Probe: Probe{OperationID: "b", Family: "beta"}, Status: 200, Passed: true},
		{Probe: Probe{OperationID: "a", Family: "alpha"}, Status: 500, Passed: false, Reason: "status 500, want 2xx"},
	})
	b1, err := rep.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	b2, err := rep.JSON()
	if err != nil {
		t.Fatalf("JSON (second): %v", err)
	}
	if string(b1) != string(b2) {
		t.Errorf("JSON() is not deterministic:\n%s\n---\n%s", b1, b2)
	}

	var doc reportDoc
	if err := json.Unmarshal(b1, &doc); err != nil {
		t.Fatalf("JSON() did not round-trip: %v", err)
	}
	if doc.AllPassed {
		t.Errorf("AllPassed = true, want false")
	}
	if len(doc.Families) != 2 {
		t.Fatalf("families = %d, want 2", len(doc.Families))
	}
	if doc.Families[0].Family != "alpha" || doc.Families[1].Family != "beta" {
		t.Errorf("families not sorted: %v", doc.Families)
	}
	if len(doc.Results) != 2 || doc.Results[1].Reason == "" {
		t.Errorf("results missing failure reason: %+v", doc.Results)
	}
}

// TestRunMutatingGate is the disposable-workspace gate: RunMutating without an
// explicitly set disposable workspace returns the gate error; with one set it
// runs.
func TestRunMutatingGate(t *testing.T) {
	spec := cannedSpec()

	noWorkspace := New("http://127.0.0.1:1", spec, WithProbes(cannedProbes()))
	if _, err := noWorkspace.RunMutating(context.Background()); err == nil {
		t.Fatal("RunMutating without a disposable workspace returned nil error; want the gate error")
	} else if !strings.Contains(err.Error(), "disposable") {
		t.Fatalf("RunMutating gate error = %q, want it to mention the disposable workspace", err)
	}

	withWorkspace := New("http://127.0.0.1:1", spec,
		WithProbes(cannedProbes()), WithDisposableWorkspace(t.TempDir()))
	rep, err := withWorkspace.RunMutating(context.Background())
	if err != nil {
		t.Fatalf("RunMutating with a disposable workspace: %v", err)
	}
	// No mutating probes are registered yet: an empty, all-passed report.
	if !rep.AllPassed() {
		t.Errorf("RunMutating (no mutating probes) AllPassed=false, want true")
	}
	if len(rep.Results) != 0 {
		t.Errorf("RunMutating (no mutating probes) returned %d results, want 0", len(rep.Results))
	}
}

// TestValidateBody covers the body shape check directly: valid JSON against a
// schema, a shape violation, invalid JSON, and the no-schema "any JSON" case.
func TestValidateBody(t *testing.T) {
	v, err := NewValidator(cannedSpec())
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	schema := v.SchemaFor("get-config")
	if schema == nil {
		t.Fatal("SchemaFor(get-config) = nil, want the object schema")
	}

	if ok, reason := ValidateBody(schema, []byte(`{"a":1}`)); !ok {
		t.Errorf("object against object schema: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := ValidateBody(schema, []byte(`[1,2]`)); ok {
		t.Errorf("array against object schema: ok=true, want a shape violation")
	} else if !strings.Contains(reason, "got array, want object") {
		t.Errorf("array shape violation reason = %q, want the type mismatch", reason)
	}
	if ok, reason := ValidateBody(schema, []byte(`{not json`)); ok {
		t.Errorf("invalid JSON: ok=true, want a failure")
	} else if !strings.Contains(reason, "not valid JSON") {
		t.Errorf("invalid JSON reason = %q, want a JSON parse failure", reason)
	}
	// No schema: any valid JSON passes.
	if ok, reason := ValidateBody(nil, []byte(`{"anything":"goes"}`)); !ok {
		t.Errorf("no-schema any JSON: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := ValidateBody(nil, []byte(`oops`)); ok {
		t.Errorf("no-schema invalid JSON: ok=true, want a failure")
	} else if !strings.Contains(reason, "not valid JSON") {
		t.Errorf("no-schema invalid JSON reason = %q", reason)
	}
}
