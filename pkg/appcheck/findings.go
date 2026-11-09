//go:build !js

package appcheck

import (
	"fmt"
	"regexp"
	"strings"
)

// failedStatus matches a request line the app answered with a client or
// server error ("500 POST /api/clients").
var failedStatus = regexp.MustCompile(`^[45]\d\d `)

// apiPath matches a request to the app's API, which a build without a server
// cannot answer when hosted.
var apiPath = regexp.MustCompile(` /api/`)

// findingsFrom turns what the probe observed into plain-language findings for
// the person's agent and the review. The wording is the platform check's, so
// a reader sees the same sentences as before.
func findingsFrom(run *probeRun, hasServer bool) []Finding {
	var out []Finding
	problem := func(format string, args ...any) {
		out = append(out, Finding{Severity: SeverityProblem, Text: fmt.Sprintf(format, args...)})
	}
	note := func(format string, args ...any) {
		out = append(out, Finding{Severity: SeverityNote, Text: fmt.Sprintf(format, args...)})
	}

	for _, p := range run.pages {
		switch {
		case p.Status != 200 && p.Status <= 0:
			problem("The page %s answered nothing (it did not load) when opened directly.", p.Route)
		case p.Status != 200:
			problem("The page %s answered %d when opened directly.", p.Route, p.Status)
		case strings.TrimSpace(p.Text) == "":
			problem("The page %s loaded but shows nothing.", p.Route)
		}
	}
	for _, r := range run.failed {
		suffix := ""
		if apiPath.MatchString(r) && !hasServer {
			suffix = " The build has no server (dist/worker.js), so no /api route exists when hosted."
		}
		problem("Loading the pages, a request failed: %s.%s", r, suffix)
	}
	failedInForms := map[string]bool{}
	for _, f := range run.forms {
		for _, s := range f.Sent {
			failedInForms[s] = true
		}
	}
	for _, r := range run.submitErrors {
		if !failedInForms[r] {
			problem("Using the app, a request failed: %s.", r)
		}
	}

	// Each form is judged on its last use: the second round has the data the
	// first round entered elsewhere (an entry form needs a client to pick).
	lastUse := map[string]FormOutcome{}
	var order []string
	for _, f := range run.forms {
		if strings.HasPrefix(f.Form, "button:") {
			continue
		}
		key := f.Route + " " + f.Form
		if _, seen := lastUse[key]; !seen {
			order = append(order, key)
		}
		lastUse[key] = f
	}
	for _, key := range order {
		f := lastUse[key]
		where := fmt.Sprintf("The form %q on %s", f.Form, f.Route)
		switch {
		case !f.SubmitButton:
			note("%s has no submit button the check could find.", where)
		case len(f.Blocked) > 0 && len(f.Sent) == 0:
			problem("%s did not submit with sensible values (%s).", where, strings.Join(f.Blocked, "; "))
		case anyFailed(f.Sent):
			problem("%s was submitted and its request failed (%s).", where, strings.Join(failedOnly(f.Sent), ", "))
		case len(f.Sent) == 0 && hasServer:
			note("%s sent no request when submitted.", where)
		}
	}

	if run.formsUsed > 0 && !run.probeKept {
		note("Values entered in forms (%q and the like) were not shown on any page after a fresh load.", "Probe Alfa")
	}
	for _, e := range run.consoleErrors {
		note("Browser console error: %s", e)
	}
	if run.budgetReached {
		note("The check reached its time limit before using every page.")
	}
	return out
}

// anyFailed reports whether any request line failed.
func anyFailed(lines []string) bool {
	for _, line := range lines {
		if failedStatus.MatchString(line) {
			return true
		}
	}
	return false
}

// failedOnly returns the failing request lines.
func failedOnly(lines []string) []string {
	var out []string
	for _, line := range lines {
		if failedStatus.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}
