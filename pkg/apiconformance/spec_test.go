package apiconformance

import (
	"net/http"
	"path/filepath"
	"testing"
)

// TestNewValidatorRealSpec compiles the committed contract's schemas (including
// the shared ErrorModel and the inline 200 schemas) and checks the operation
// lookup.
func TestNewValidatorRealSpec(t *testing.T) {
	root := findRepoRootFromTestFile(t)
	spec, err := LoadSpec(filepath.Join(root, "docs/api/openapi.yaml"))
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	v, err := NewValidator(spec)
	if err != nil {
		t.Fatalf("NewValidator on the real spec: %v", err)
	}
	for _, opID := range []string{"get-config", "get-stats"} {
		if v.SchemaFor(opID) == nil {
			t.Errorf("SchemaFor(%s) = nil, want the inline 200 schema", opID)
		}
	}
	if v.SchemaFor("gitStatus") != nil {
		t.Error("SchemaFor(gitStatus) != nil, want nil (no concrete 200 schema)")
	}
	if v.ErrModel() == nil {
		t.Error("ErrModel() = nil, want the shared ErrorModel")
	}
}

// TestLoadSpecRealContract locks the loader against the committed contract: it
// must parse 164 operations and 13 families, the contract version, the shared
// ErrorModel, and the two schema-backed operations with their inline object
// schemas.
func TestLoadSpecRealContract(t *testing.T) {
	root := findRepoRootFromTestFile(t)
	spec, err := LoadSpec(filepath.Join(root, "docs/api/openapi.yaml"))
	if err != nil {
		t.Fatalf("LoadSpec on the committed contract: %v", err)
	}

	if n := len(spec.Operations); n != 164 {
		t.Errorf("operations = %d, want 164", n)
	}
	if got := len(spec.Families()); got != 13 {
		t.Errorf("families = %d, want 13: %v", got, spec.Families())
	}
	if spec.ContractVersion() != "1.0.0" {
		t.Errorf("ContractVersion = %q, want 1.0.0", spec.ContractVersion())
	}
	if _, ok := spec.ComponentsSchemas["ErrorModel"]; !ok {
		t.Error("ComponentsSchemas missing ErrorModel")
	}

	byID := map[string]Operation{}
	for _, op := range spec.Operations {
		byID[op.OperationID] = op
	}
	for _, opID := range []string{"get-config", "get-stats"} {
		op, ok := byID[opID]
		if !ok {
			t.Errorf("operation %s not found", opID)
			continue
		}
		if !op.HasOKSchema {
			t.Errorf("%s: HasOKSchema=false, want true", opID)
		}
		if op.OKSchemaRef != "" {
			t.Errorf("%s: OKSchemaRef=%q, want an inline schema", opID, op.OKSchemaRef)
		}
		if typ, ok := op.OKSchemaSchema["type"]; !ok || typ != "object" {
			t.Errorf("%s: inline 200 schema type = %v, want object", opID, typ)
		}
	}
	// The two known schemaless GETs must not be flagged as schema-backed.
	for _, opID := range []string{"gitStatus", "filesList"} {
		if op, ok := byID[opID]; ok && op.HasOKSchema {
			t.Errorf("%s: HasOKSchema=true, want false (it documents no 200 schema)", opID)
		}
	}
}

// TestErrModelValidation exercises the shared error model end-to-end: a body is
// validated against the compiled ErrorModel (which resolves
// #/components/schemas/ErrorDetail) and a non-object is rejected. This proves the
// $ref-into-components path the validator relies on for the default error
// responses.
func TestErrModelValidation(t *testing.T) {
	root := findRepoRootFromTestFile(t)
	spec, err := LoadSpec(filepath.Join(root, "docs/api/openapi.yaml"))
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	v, err := NewValidator(spec)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	em := v.ErrModel()
	if em == nil {
		t.Fatal("ErrModel() = nil, want the shared ErrorModel")
	}
	good := map[string]any{
		"type":   "https://example.com/errors",
		"title":  "Bad Request",
		"status": 400,
		"detail": "query is required",
		"errors": []any{map[string]any{"location": "body.query", "message": "required"}},
	}
	if err := em.Validate(good); err != nil {
		t.Errorf("ErrorModel rejected a valid problem body: %v", err)
	}
	if err := em.Validate("a string is not an error object"); err == nil {
		t.Error("ErrorModel accepted a non-object body, want a failure")
	}
}

// TestDefaultProbesMatchContract cross-checks the default probe table against
// the committed contract: every probe is a GET, references a real operation, and
// belongs to a real family (so a renamed family or removed operation cannot ship
// silently in the probe table).
func TestDefaultProbesMatchContract(t *testing.T) {
	root := findRepoRootFromTestFile(t)
	spec, err := LoadSpec(filepath.Join(root, "docs/api/openapi.yaml"))
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	famSet := map[string]bool{}
	for _, f := range spec.Families() {
		famSet[f] = true
	}
	opSet := map[string]bool{}
	for _, op := range spec.Operations {
		opSet[op.OperationID] = true
	}

	probes := DefaultProbes()
	if len(probes) == 0 {
		t.Fatal("DefaultProbes() is empty")
	}
	seen := map[string]bool{}
	for _, p := range probes {
		if p.Method != http.MethodGet {
			t.Errorf("probe %s: method %q, want GET", p.OperationID, p.Method)
		}
		if !famSet[p.Family] {
			t.Errorf("probe %s: family %q is not a contract family", p.OperationID, p.Family)
		}
		if !opSet[p.OperationID] {
			t.Errorf("probe %s: not a contract operation", p.OperationID)
		}
		if seen[p.OperationID] {
			t.Errorf("duplicate probe %s", p.OperationID)
		}
		seen[p.OperationID] = true
	}
}

// TestFamiliesUnion checks Families() is the stable union of declared and
// operation tags (a declared-but-unused family still appears).
func TestFamiliesUnion(t *testing.T) {
	spec := &Spec{
		Tags: []string{"zzz", "aaa"},
		Operations: []Operation{
			{Tags: []string{"mmm", "aaa"}},
		},
	}
	got := spec.Families()
	want := []string{"aaa", "mmm", "zzz"}
	if !slicesEqual(got, want) {
		t.Fatalf("Families() = %v, want %v (sorted union of declared and operation tags)", got, want)
	}
}

// slicesEqual reports whether two string slices are equal element-for-element.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
