//go:build !js

package appcheck

// report_shape_test.go pins the report's JSON shape: the platform parses
// .sprout/app-check.json, so field names, types and the enumerated values of
// status and severity are a contract. A change here is a change to a
// published interface.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestReportJSONShape pins the exact key set, nesting and value vocabulary.
func TestReportJSONShape(t *testing.T) {
	r := &Report{
		Status:         StatusRan,
		HasServer:      true,
		Findings:       []Finding{{Severity: SeverityProblem, Text: "p"}, {Severity: SeverityNote, Text: "n"}},
		Pages:          []Page{{Route: "/", Status: 200, Text: "hello", Screenshot: "app-check/page-0.png"}},
		Forms:          []FormOutcome{{Route: "/", Form: "Signup", Steps: 1, SubmitButton: true, Sent: []string{"201 POST /api/x"}, Blocked: []string{}}},
		FailedRequests: []string{"500 GET /api/reports"},
		ConsoleErrors:  []string{"boom"},
		ProbeKept:      true,
		BudgetReached:  false,
		DurationMS:     1234,
		Screenshot:     "app-check/page-0.png",
	}
	path := filepath.Join(t.TempDir(), "app-check.json")
	if err := r.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: temp dir path built above
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}

	wantKeys := []string{
		"checked_at", "status", "reason", "has_server", "findings", "pages", "forms",
		"failed_requests", "console_errors", "probe_kept", "budget_reached",
		"duration_ms", "screenshot",
	}
	if keys := sortedKeys(got); !reflect.DeepEqual(keys, sorted(wantKeys)) {
		t.Fatalf("report keys = %v, want %v", keys, wantKeys)
	}

	// Values the platform switches on.
	if got["status"] != StatusRan {
		t.Errorf("status = %v, want %q", got["status"], StatusRan)
	}
	assertKeys(t, "finding", got["findings"].([]any)[0].(map[string]any), []string{"severity", "text"})
	if sev := got["findings"].([]any)[0].(map[string]any)["severity"]; sev != SeverityProblem {
		t.Errorf("severity = %v, want %q", sev, SeverityProblem)
	}
	assertKeys(t, "page", got["pages"].([]any)[0].(map[string]any), []string{"route", "status", "text", "screenshot"})
	assertKeys(t, "form", got["forms"].([]any)[0].(map[string]any), []string{"route", "form", "steps", "submit_button", "sent", "blocked"})

	if _, ok := got["pages"].([]any)[0].(map[string]any)["status"].(float64); !ok {
		t.Errorf("page status is not a number")
	}
	if _, ok := got["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms is not a number")
	}
	if _, ok := got["has_server"].(bool); !ok {
		t.Errorf("has_server is not a boolean")
	}
}

// TestNotRunReportShape pins the not-run shape: every list is present and
// empty (never null), so a reader can index it without a nil check.
func TestNotRunReportShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "app-check.json")
	if err := NotRun("browser not available (none found).").WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: temp dir path built above
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if got["status"] != StatusSkipped {
		t.Errorf("status = %v, want %q", got["status"], StatusSkipped)
	}
	if got["reason"] == "" {
		t.Errorf("reason is empty")
	}
	for _, key := range []string{"findings", "pages", "forms", "failed_requests", "console_errors"} {
		list, ok := got[key].([]any)
		if !ok {
			t.Errorf("%s = %#v, want an empty list", key, got[key])
			continue
		}
		if len(list) != 0 {
			t.Errorf("%s has %d entries, want 0", key, len(list))
		}
	}
}

// TestExitCode pins the exit contract: 1 only when the check ran and found
// problems, 0 when it found none or could not run.
func TestExitCode(t *testing.T) {
	cases := []struct {
		name   string
		report *Report
		want   int
	}{
		{"not run", NotRun("no browser"), 0},
		{"ran clean", &Report{Status: StatusRan, Findings: []Finding{{Severity: SeverityNote, Text: "n"}}}, 0},
		{"ran with problem", &Report{Status: StatusRan, Findings: []Finding{{Severity: SeverityProblem, Text: "p"}}}, 1},
		{"build failed", Failed("npm run build failed"), 1},
	}
	for _, c := range cases {
		if got := c.report.ExitCode(); got != c.want {
			t.Errorf("%s: ExitCode() = %d, want %d", c.name, got, c.want)
		}
	}
}

func assertKeys(t *testing.T, what string, obj map[string]any, want []string) {
	t.Helper()
	if keys := sortedKeys(obj); !reflect.DeepEqual(keys, sorted(want)) {
		t.Errorf("%s keys = %v, want %v", what, keys, want)
	}
}

func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return sorted(keys)
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
