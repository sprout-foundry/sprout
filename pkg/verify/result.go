package verify

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// DefaultMaxExcerptBytes bounds Check.Excerpt by default: an excerpt
// keeps the head and the tail of a command's output (failures surface
// at the tail) plus an explicit truncation marker, so a result stays
// small even for verbose builds.
const DefaultMaxExcerptBytes = 8 * 1024

// Commands are the trusted build and test commands for one project's
// verification run: resolved only from the starter
// manifest and the explicit project configuration. An empty field means
// "no command for that check" — the runner never guesses one.
type Commands struct {
	// Build is the trusted build command, or "" when none is configured.
	Build string `json:"build,omitempty"`
	// Test is the trusted test command, or "" when none is configured.
	Test string `json:"test,omitempty"`
}

// Check is the outcome of one verification check. It carries the
// evidence the contract names: which check ran, its pass/fail state,
// and a bounded excerpt of its output.
type Check struct {
	// Kind is the check's plancontract kind (build or test).
	Kind plancontract.Kind `json:"kind"`
	// Items are the ids of the plan acceptance items this check covers
	// (empty for baseline checks and when the plan declares none).
	Items []string `json:"items,omitempty"`
	// Command is the trusted command that ran, or "" when the check was
	// skipped because no command was configured.
	Command string `json:"command,omitempty"`
	// Skipped marks a check that did not run: no trusted command was
	// available, or the run was cancelled. Skipped checks do not gate
	// the result (see Result.Passed) but are listed in it, so the
	// result always says what could not be verified.
	Skipped bool `json:"skipped,omitempty"`
	// Passed reports whether the check passed. For a skipped check it is
	// false.
	Passed bool `json:"passed,omitempty"`
	// Reason explains a skipped or abnormal check (timeout, executor
	// error, cancellation). It is empty for a clean pass or a normal
	// non-zero exit (the output excerpt carries the evidence there).
	Reason string `json:"reason,omitempty"`
	// Excerpt is a bounded excerpt of the check's combined output
	// (the evidence).
	Excerpt string `json:"excerpt,omitempty"`
	// Routes are the routes a page check attempted, in manifest order
	// (page checks only; empty for every other kind).
	Routes []string `json:"routes,omitempty"`
	// Screenshots are the per-route screenshot file paths, parallel to
	// Routes ("" where a capture failed or the route never reached a
	// browser capture; page checks only).
	Screenshots []string `json:"screenshots,omitempty"`
	// Steps are the scripted browser steps an interaction check ran, in
	// execution order (interaction checks only; empty for every other kind).
	// They come from the plan's interaction acceptance item.
	Steps []plancontract.BrowseStep `json:"steps,omitempty"`
	// Duration is the wall time the check took to execute.
	Duration time.Duration `json:"duration,omitempty"`
}

// Result is the structured outcome of one verification run. It is plain
// data: the turn-end hook consumes it, the final-reply contract reports
// from it, and the progress events record it as a structured event with
// evidence.
type Result struct {
	// Baseline is true when the run happened without an active
	// plan: the build and test commands ran as a baseline.
	Baseline bool `json:"baseline,omitempty"`
	// PlanRevision is the revision of the plan whose acceptance items
	// the checks cover (0 in baseline mode).
	PlanRevision int `json:"plan_revision,omitempty"`
	// Checks are the individual check outcomes, in run order.
	Checks []Check `json:"checks"`
	// Errors are run-level findings (an unreadable plan or manifest, a
	// configuration source failure) that prevented some commands from
	// resolving. They are reported, never swallowed.
	Errors []string `json:"errors,omitempty"`
}

// Failed reports whether the run failed: an executed check failed, or a
// run-level error was recorded. A run whose checks were all skipped did
// not fail — it verified nothing (see Passed).
func (r *Result) Failed() bool {
	if r == nil {
		return false
	}
	if len(r.Errors) > 0 {
		return true
	}
	for _, c := range r.Checks {
		if !c.Skipped && !c.Passed {
			return true
		}
	}
	return false
}

// Passed reports whether the verification run passed: nothing failed and
// at least one check actually ran. A run whose checks were all skipped
// (no trusted command anywhere) passed nothing — partial or vacuous
// success is never reported as success.
func (r *Result) Passed() bool {
	if r == nil {
		return false
	}
	if r.Failed() {
		return false
	}
	for _, c := range r.Checks {
		if !c.Skipped {
			return true
		}
	}
	return false
}

// Summary renders a compact, deterministic one-line summary of the run
// for display and for the final reply.
func (r *Result) Summary() string {
	if r == nil {
		return "no verification result"
	}
	var b strings.Builder
	if r.Baseline {
		b.WriteString("baseline: ")
	} else {
		fmt.Fprintf(&b, "plan rev %d: ", r.PlanRevision)
	}
	parts := make([]string, 0, len(r.Checks)+len(r.Errors))
	for _, c := range r.Checks {
		var status string
		switch {
		case c.Skipped:
			status = "skipped"
		case !c.Passed:
			status = "failed"
		default:
			status = "passed"
		}
		part := c.Kind.String() + ": " + status
		if c.Command != "" {
			part += " (" + c.Command + ")"
		}
		// A page check that ran reports how many routes it covered, so the
		// summary says plainly what was verified.
		if c.Kind == plancontract.KindPage && !c.Skipped && len(c.Routes) > 0 {
			if len(c.Routes) == 1 {
				part += " [1 route]"
			} else {
				part += fmt.Sprintf(" [%d routes]", len(c.Routes))
			}
		}
		// An interaction check that ran reports how many scripted steps it
		// executed (the confirmed outcome).
		if c.Kind == plancontract.KindInteraction && !c.Skipped && len(c.Steps) > 0 {
			if len(c.Steps) == 1 {
				part += " [1 step]"
			} else {
				part += fmt.Sprintf(" [%d steps]", len(c.Steps))
			}
		}
		// A manual check reports how many manual items it lists.
		if c.Kind == plancontract.KindManual && c.Skipped && len(c.Items) > 0 {
			if len(c.Items) == 1 {
				part += " [1 manual item]"
			} else {
				part += fmt.Sprintf(" [%d manual items]", len(c.Items))
			}
		}
		if c.Reason != "" {
			part += " — " + c.Reason
		}
		parts = append(parts, part)
	}
	parts = append(parts, r.Errors...)
	b.WriteString(strings.Join(parts, "; "))
	return b.String()
}

// boundedExcerpt keeps output within maxBytes by preserving its head and
// tail (where failures surface) around an explicit truncation marker.
// Output shorter than the bound is returned unchanged; a bound below 32
// bytes is too small to be useful and falls back to the default.
func boundedExcerpt(output string, maxBytes int) string {
	if maxBytes < 32 {
		maxBytes = DefaultMaxExcerptBytes
	}
	if len(output) <= maxBytes {
		return output
	}
	half := maxBytes / 2
	truncated := len(output) - maxBytes
	return output[:half] +
		"\n...[truncated " + strconv.Itoa(truncated) + " bytes]...\n" +
		output[len(output)-half:]
}
