// Package appcheck runs the app check: it serves a project's build output
// the way the platform hosts it, uses the app in a headless browser the way
// a visitor would, and reports what a person would run into — pages that do
// not load or are blank, forms that refuse sensible values or send nothing,
// requests that fail, errors in the browser, and entries that are gone after
// a fresh load.
//
// The report is written as JSON (.sprout/app-check.json) and read by the
// platform, so the field names and meanings in Report and its parts are a
// contract: change them only together with every reader.
package appcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Status values a Report can carry.
const (
	// StatusRan means the check built/served the app and used it.
	StatusRan = "ran"
	// StatusSkipped means the check could not run (no project, no build
	// output, no browser); the check never stands in the way of work.
	StatusSkipped = "skipped"
)

// Finding severities. Only "problem" changes the exit code.
const (
	SeverityProblem = "problem"
	SeverityNote    = "note"
)

// Report is the app check's result. It is written verbatim as JSON, so the
// tags below are the contract the platform parses.
type Report struct {
	// CheckedAt is when the report was written (RFC 3339). It is not read by
	// the platform; it makes a kept report readable on its own.
	CheckedAt string `json:"checked_at"`
	// Status is "ran" or "skipped".
	Status string `json:"status"`
	// Reason explains a skipped status.
	Reason string `json:"reason"`
	// HasServer reports whether the build ships a server (dist/worker.js
	// with a main entry in wrangler.toml); a build without one has no /api
	// routes when hosted.
	HasServer bool `json:"has_server"`
	// Findings are the plain-language problems and notes.
	Findings []Finding `json:"findings"`
	// Pages are the routes loaded fresh in the browser.
	Pages []Page `json:"pages"`
	// Forms are the forms and action buttons the probe used.
	Forms []FormOutcome `json:"forms"`
	// FailedRequests are failing requests with method and route
	// ("500 POST /api/clients"), recorded on the fresh-load pass.
	FailedRequests []string `json:"failed_requests"`
	// ConsoleErrors are the browser's console errors and page errors.
	ConsoleErrors []string `json:"console_errors"`
	// ProbeKept reports whether values entered in forms were still shown on
	// a page after a fresh load.
	ProbeKept bool `json:"probe_kept"`
	// BudgetReached reports whether the time budget ran out before every
	// page was used.
	BudgetReached bool `json:"budget_reached"`
	// DurationMS is how long the whole check took.
	DurationMS int64 `json:"duration_ms"`
	// Screenshot is the screenshot of the first page ("" when none).
	Screenshot string `json:"screenshot"`
}

// Finding is one plain-language observation for the person's agent and the
// review.
type Finding struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
}

// Page is a route loaded fresh in the browser.
type Page struct {
	Route      string `json:"route"`
	Status     int    `json:"status"`
	Text       string `json:"text"`
	Screenshot string `json:"screenshot,omitempty"`
}

// FormOutcome is what happened when the probe used one form (or pressed one
// action button) on a route.
type FormOutcome struct {
	// Route is the route the form lives on.
	Route string `json:"route"`
	// Form is a short, stable label for the form ("Signup", "button: Mark
	// done").
	Form string `json:"form"`
	// Steps is how many steps a multi-step form took.
	Steps int `json:"steps"`
	// SubmitButton reports whether the probe found something to submit with.
	SubmitButton bool `json:"submit_button"`
	// Sent are the write requests ("<status> <method> <path>") the submit
	// made.
	Sent []string `json:"sent"`
	// Blocked are the browser's refusals ("field: message").
	Blocked []string `json:"blocked"`
}

// NotRun returns a report for a check that could not run.
func NotRun(reason string) *Report {
	return &Report{
		Status:         StatusSkipped,
		Reason:         reason,
		Findings:       []Finding{},
		Pages:          []Page{},
		Forms:          []FormOutcome{},
		FailedRequests: []string{},
		ConsoleErrors:  []string{},
	}
}

// Failed returns a report for a check that ran but hit a setup failure (a
// failed build, a server that never came up): one problem finding and no
// pages, so the exit code is 1.
func Failed(problem string) *Report {
	r := NotRun("")
	r.Status = StatusRan
	r.Findings = []Finding{{Severity: SeverityProblem, Text: problem}}
	return r
}

// Problems returns the findings with severity "problem".
func (r *Report) Problems() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == SeverityProblem {
			out = append(out, f)
		}
	}
	return out
}

// Notes returns the findings with severity "note".
func (r *Report) Notes() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == SeverityNote {
			out = append(out, f)
		}
	}
	return out
}

// ExitCode is the process exit code the check wants: 1 when it ran and
// found problems, 0 when it found none or could not run.
func (r *Report) ExitCode() int {
	if r.Status == StatusRan && len(r.Problems()) > 0 {
		return 1
	}
	return 0
}

// WriteJSON writes the report to path (creating parent directories). Fields
// that were never set are written as empty lists / zero values, never null:
// the platform parses the shape, and a missing list must read as "none".
func (r *Report) WriteJSON(path string) error {
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	if r.Pages == nil {
		r.Pages = []Page{}
	}
	if r.Forms == nil {
		r.Forms = []FormOutcome{}
	}
	if r.FailedRequests == nil {
		r.FailedRequests = []string{}
	}
	if r.ConsoleErrors == nil {
		r.ConsoleErrors = []string{}
	}
	data, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return fmt.Errorf("encode app check report: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	return nil
}
