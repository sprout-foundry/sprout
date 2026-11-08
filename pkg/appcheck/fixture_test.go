//go:build !js

package appcheck

// fixture_test.go runs the probe against a small fixture app and checks the
// findings a person would get: a page error, a blank page, a form that sends
// nothing and a failed request — the four things the check exists to catch.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fixtureApp is the app the tests use: a working home page with an entry
// form, a page whose script throws (it still renders), a blank page, a form
// that sends nothing, and a page whose request fails.
func fixtureApp() *fixtureSite {
	return &fixtureSite{
		formsUsed: map[string]int{},
		pages: map[string]*fixturePage{
			"/": {
				text:  "Clients\nAdd client\nList is empty",
				links: []string{"/clients/1"},
				forms: []fixtureForm{{label: "Add client", submit: func() fixtureSubmit {
					return fixtureSubmit{sent: "201 POST /api/clients"}
				}}},
			},
			"/clients/1": {
				text:          "Client Probe Alfa\nName\nNotes",
				consoleErrors: []string{"Uncaught TypeError: cannot read properties of undefined (reading 'name')"},
				forms: []fixtureForm{{label: "Contact", submit: func() fixtureSubmit {
					return fixtureSubmit{sent: "200 POST /api/contacts"}
				}}},
			},
			"/settings": {
				text: "Settings\nSave",
				forms: []fixtureForm{{label: "Save settings", submit: func() fixtureSubmit {
					return fixtureSubmit{}
				}}},
			},
			"/broken": {
				text: "",
			},
			"/reports": {
				// Its data request fails, so the page itself answers 500.
				status: 500,
				text:   "Reports\nTotals",
			},
		},
	}
}

func TestProbeFindingsOnFixtureApp(t *testing.T) {
	site := fixtureApp()
	// The blank page and a page whose request fails are linked from home.
	site.pages["/"].links = []string{"/clients/1", "/settings", "/broken", "/reports"}

	run, err := runProbe(newFixturePageOps(site), "", "", 30*time.Second)
	if err != nil {
		t.Fatalf("runProbe: %v", err)
	}
	hasServer := true
	findings := findingsFrom(run, hasServer)

	texts := make([]string, 0, len(findings))
	for _, f := range findings {
		texts = append(texts, f.Severity+": "+f.Text)
	}
	joined := strings.Join(texts, "\n")

	want := []struct {
		what string
		text string
	}{
		{"blank page", `The page /broken loaded but shows nothing.`},
		{"page error", `Browser console error: Uncaught TypeError: cannot read properties of undefined (reading 'name')`},
		{"form sends nothing", `The form "Save settings" on /settings sent no request when submitted.`},
	}
	for _, w := range want {
		if !strings.Contains(joined, w.text) {
			t.Errorf("missing %s finding %q in:\n%s", w.what, w.text, joined)
		}
	}

	// The failed request is recorded on the fresh-load pass from the page's
	// own failing request.
	if !containsText(run.failed, "500 GET /reports") {
		t.Errorf("failed requests = %v, want 500 GET /reports", run.failed)
	}

	// The failed request the probe recorded becomes a problem finding.
	t.Run("failed request finding", func(t *testing.T) {
		want := "Loading the pages, a request failed: 500 GET /reports."
		if !strings.Contains(joined, want) {
			t.Errorf("missing failed-request finding %q in:\n%s", want, joined)
		}
	})

	// The working form is not a problem, and the probe's entries are not
	// reported as kept when the fixture never shows them back.
	for _, f := range findings {
		if strings.Contains(f.Text, `"Add client"`) && f.Severity == SeverityProblem {
			t.Errorf("working form reported as a problem: %s", f.Text)
		}
	}
	if run.probeKept {
		t.Error("probeKept = true, want false on the fixture")
	}
	if len(run.pages) == 0 {
		t.Error("no pages recorded")
	}
}

func TestProbeReportsCouldNotRunWithoutBrowser(t *testing.T) {
	root := t.TempDir()
	writeFixtureProject(t, root)

	report, err := Run(context.Background(), Options{
		Root:    root,
		NoBuild: true,
		Budget:  time.Second,
		Browser: unavailableBrowser{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusSkipped {
		t.Fatalf("status = %q, want %q", report.Status, StatusSkipped)
	}
	if !strings.Contains(report.Reason, "browser not available") {
		t.Errorf("reason = %q, want it to explain the missing browser", report.Reason)
	}
	if report.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0 (the check never stands in the way of work)", report.ExitCode())
	}
}
