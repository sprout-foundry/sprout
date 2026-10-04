// Package benchmark owns the task suite of the SP-154 agent benchmark
// (SP-154 §154a): the on-disk task fixture format and the loader that
// reads and validates it.
//
// A benchmark task is three things pinned together:
//
//   - a plain-language request (what the agent receives);
//   - a frozen SP-148 plan (pkg/plancontract), whose acceptance criteria
//     are the task's acceptance criteria;
//   - a starter reference (SP-153) — the starter the task runs against.
//
// Task files are static JSON fixtures committed to the repository under
// benchmarks/tasks/<starter-id>/<task-id>.json. The freeze is what makes
// benchmark runs comparable across models and over time: the request, the
// plan, and the starter never drift between runs. Pass/fail for a task
// comes only from the SP-149 verification of the plan's acceptance
// criteria — never from the model's own report — and the runner (runner.go)
// enforces that contract.
//
// TODO 154.1 shipped the skeleton (the format, the loader, validation, and
// the committed fixture task); TODO 154.2 adds the runner: headless
// non-interactive runs of a task through the existing agent path, one
// fresh starter copy per run, 3 runs per model by default. Per-task
// metrics (154.3) and the default model list from the provider catalog
// (154.4) have landed; reports (154.5) are still to come.
//
// The package's dependencies (pkg/plancontract, and for the runner
// pkg/agent, pkg/verify, pkg/starters, pkg/planstore, pkg/factory,
// pkg/configuration) are all supported on the js/wasm target, so the
// whole package builds for js/wasm. The runner's tests drive real shell
// commands and real (scripted) agent turns and are !js-only, like the
// SP-149 fixture tests.
package benchmark

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// ErrInvalidTask is returned when a task file parses but is not a valid
// task: a task-field violation (id, request, starter), a plan that fails
// plancontract.Validate, or — under LoadSuite — a starter field that does
// not match the task's directory. Callers detect it with
// errors.Is(err, ErrInvalidTask). File-level failures (unreadable or
// missing file, malformed JSON) are NOT wrapped in ErrInvalidTask: they
// wrap the underlying os or encoding/json error, so a caller can tell
// "the file does not parse" from "the file is not a valid task".
var ErrInvalidTask = errors.New("benchmark: invalid task")

// ValidationError describes the structural problems found in one task. It
// carries one entry per problem so callers can surface them all at once,
// mirroring plancontract.ValidationError for plans.
type ValidationError struct {
	Problems []string
}

// Error implements error. It always lists every problem so the message is
// actionable on its own.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("benchmark: %d problem(s): %s", len(e.Problems), strings.Join(e.Problems, "; "))
}

// Task is one SP-154 benchmark task (SP-154 §154a): a plain-language
// request, a frozen SP-148 plan, and a starter reference. Task files
// are static fixtures committed to the repo — that is what "frozen"
// means: runs are comparable across models and over time because the
// request, the plan, and the starter never drift.
type Task struct {
	// ID is the task identifier: non-empty, a single safe path
	// segment. Convention: the file name without .json.
	ID string `json:"id"`
	// Request is the plain-language request the agent receives.
	Request string `json:"request"`
	// Starter is the starter ID (SP-153) the task runs against.
	Starter string `json:"starter"`
	// Plan is the frozen SP-148 plan: the machine-readable
	// plancontract.Plan. It must validate (plancontract.Validate);
	// its acceptance criteria are the task's acceptance criteria.
	Plan plancontract.Plan `json:"plan"`
}

// validate reports every task-level problem with t. The plan is checked
// with plancontract.Validate — the same validator every plan reader and
// writer runs — so the task format never re-invents plan rules; its
// problems are folded into the returned list. validate is a pure function
// over the struct (no I/O), like plancontract.Validate.
func validate(t *Task) *ValidationError {
	var problems []string

	id := strings.TrimSpace(t.ID)
	if id == "" {
		problems = append(problems, "id is required (convention: the file name without .json)")
	} else if !safePathSegment(id) {
		problems = append(problems,
			fmt.Sprintf("id %q must be a single safe path segment (no path separators, not \".\" or \"..\")", t.ID))
	}

	if strings.TrimSpace(t.Request) == "" {
		problems = append(problems, "request is required (the plain-language request the agent receives)")
	}

	starter := strings.TrimSpace(t.Starter)
	if starter == "" {
		problems = append(problems, "starter is required (the SP-153 starter id the task runs against)")
	} else if !safePathSegment(starter) {
		problems = append(problems,
			fmt.Sprintf("starter %q must be a single safe path segment (no path separators, not \".\" or \"..\")", t.Starter))
	}

	if err := plancontract.Validate(&t.Plan); err != nil {
		if ve, ok := err.(*plancontract.ValidationError); ok {
			problems = append(problems, ve.Problems...)
		} else {
			problems = append(problems, err.Error())
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

// safePathSegment reports whether s is a single safe path segment:
// non-empty, not "." or "..", and containing no path separator (either
// flavour). It mirrors the rule the starter catalogue applies to starter
// ids (pkg/starters): a task id and a starter reference both name a
// directory or a file in the suite layout, so neither may escape it.
func safePathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if r == '/' || r == '\\' {
			return false
		}
	}
	return true
}
