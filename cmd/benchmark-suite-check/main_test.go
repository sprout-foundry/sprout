package main

import (
	"os"
	"path/filepath"
	"testing"
)

// validTaskJSON returns a minimal task fixture that passes the harness's
// loader (valid task fields + a plan that passes plancontract.Validate), for
// the given starter and task id. It is the shape every suite fixture has.
func validTaskJSON(starter, id string) string {
	return `
{
  "id": "` + id + `",
  "request": "Add a contact page the header can reach so visitors have somewhere to find directions.",
  "starter": "` + starter + `",
  "plan": {
    "version": 1,
    "revision": 1,
    "created": "2026-10-02T09:00:00Z",
    "updated": "2026-10-02T09:00:00Z",
    "goal": "Give the site a contact page the header can reach.",
    "scope": [
      { "id": "contact-page", "title": "A /contact/ page" }
    ],
    "steps": [
      { "scope": "contact-page", "description": "Create src/pages/contact.astro using the shared layout." }
    ],
    "starter": "` + starter + `",
    "acceptance": [
      { "id": "build-passes", "scope": "contact-page", "check": "npm run build", "kind": "build" }
    ],
    "out_of_scope": []
  }
}`
}

// writeSuite creates a <dir>/<starter>/<task>.json suite with the given
// starter->ids map (each file using validTaskJSON unless the caller wants to
// override a file's content).
func writeSuite(t *testing.T, dir string, starterIDs map[string][]string) {
	t.Helper()
	for starter, ids := range starterIDs {
		wd := filepath.Join(dir, starter)
		if err := os.MkdirAll(wd, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", wd, err)
		}
		for _, id := range ids {
			p := filepath.Join(wd, id+".json")
			if err := os.WriteFile(p, []byte(validTaskJSON(starter, id)), 0o644); err != nil {
				t.Fatalf("write %s: %v", p, err)
			}
		}
	}
}

func TestCheckSuiteAllValid(t *testing.T) {
	dir := t.TempDir()
	writeSuite(t, dir, map[string][]string{
		"static-site": {"add-page", "add-robots"},
		"web-app":     {"add-theme"},
	})
	failed, perStarter, err := checkSuite(dir)
	if err != nil {
		t.Fatalf("checkSuite: %v", err)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}
	if perStarter["static-site"] != 2 || perStarter["web-app"] != 1 {
		t.Fatalf("perStarter = %+v, want static-site=2 web-app=1", perStarter)
	}
}

// TestCheckSuiteReportsBadPlan writes a suite where one task's plan leaves a
// scope item uncovered by acceptance, and checks checkSuite flags exactly
// that file (the others still pass).
func TestCheckSuiteReportsBadPlan(t *testing.T) {
	dir := t.TempDir()
	writeSuite(t, dir, map[string][]string{"static-site": {"good", "bad-plan"}})
	// Overwrite the bad-plan fixture with a plan whose scope is uncovered.
	bad := filepath.Join(dir, "static-site", "bad-plan.json")
	badJSON := `{
  "id": "bad-plan",
  "request": "Add a footer.",
  "starter": "static-site",
  "plan": {
    "version": 1, "revision": 1,
    "created": "2026-10-02T09:00:00Z", "updated": "2026-10-02T09:00:00Z",
    "goal": "Add a footer.",
    "scope": [ { "id": "footer", "title": "A footer" } ],
    "steps": [ { "scope": "footer", "description": "Add a footer to the layout." } ],
    "starter": "static-site",
    "acceptance": [],
    "out_of_scope": []
  }
}`
	if err := os.WriteFile(bad, []byte(badJSON), 0o644); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	failed, perStarter, err := checkSuite(dir)
	if err != nil {
		t.Fatalf("checkSuite: %v", err)
	}
	if failed != 1 {
		t.Fatalf("failed = %d, want 1 (only bad-plan)", failed)
	}
	if perStarter["static-site"] != 1 {
		t.Fatalf("static-site valid count = %d, want 1 (good only)", perStarter["static-site"])
	}
}

// TestCheckSuiteReportsStarterMismatch writes a task whose starter field does
// not match its directory and checks checkSuite flags it.
func TestCheckSuiteReportsStarterMismatch(t *testing.T) {
	dir := t.TempDir()
	writeSuite(t, dir, map[string][]string{"static-site": {"orphan"}})
	// A task living under static-site/ but declaring itself a web-app task.
	mismatch := filepath.Join(dir, "static-site", "orphan.json")
	mismatchJSON := `{
  "id": "orphan",
  "request": "Add a theme toggle.",
  "starter": "web-app",
  "plan": {
    "version": 1, "revision": 1,
    "created": "2026-10-02T09:00:00Z", "updated": "2026-10-02T09:00:00Z",
    "goal": "Add a theme toggle.",
    "scope": [ { "id": "theme", "title": "A theme toggle" } ],
    "steps": [ { "scope": "theme", "description": "Add a toggle." } ],
    "starter": "web-app",
    "acceptance": [ { "id": "build-passes", "scope": "theme", "check": "npm run build", "kind": "build" } ],
    "out_of_scope": []
  }
}`
	if err := os.WriteFile(mismatch, []byte(mismatchJSON), 0o644); err != nil {
		t.Fatalf("write mismatch fixture: %v", err)
	}
	failed, _, err := checkSuite(dir)
	if err != nil {
		t.Fatalf("checkSuite: %v", err)
	}
	if failed != 1 {
		t.Fatalf("failed = %d, want 1 (starter mismatch)", failed)
	}
}

func TestCheckSuiteMissingDirIsAnError(t *testing.T) {
	if _, _, err := checkSuite(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatalf("checkSuite on a missing dir = nil error, want an error (usage problem)")
	}
}
