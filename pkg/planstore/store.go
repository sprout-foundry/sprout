// Package planstore is the file-system-facing half of the structured
// plan: it reads and writes the machine-readable plan document stored at
// .sprout/plan.json and the rendered .sprout/plan.md view.
//
// It builds on pkg/plancontract, which owns the plan schema and its
// validator (the "pure data + validation contract", with no I/O of its own).
// This package owns the I/O:
//
//   - Load reads and validates .sprout/plan.json; a missing file yields the
//     distinguishable ErrNoPlan sentinel, not a failure.
//   - Save validates the plan before touching disk, so an invalid plan is
//     never written. Every save bumps the plan's revision, stamps `updated`,
//     and regenerates the .sprout/plan.md view from the JSON — the markdown
//     is a derived artifact, regenerated on every write, never the source of
//     truth.
//
// The on-disk location is project-relative (.sprout/ under the project root),
// so a plan travels with the code and lands in git history.
package planstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// File names, under the project's .sprout/ directory.
const (
	// PlanJSONName is the authoritative machine-readable plan document.
	PlanJSONName = "plan.json"
	// PlanMarkdownName is the human-readable view rendered from the JSON on
	// every write. It is derived, never hand-edited: a hand-edit is drift and
	// the next Save overwrites it.
	PlanMarkdownName = "plan.md"
)

// planDirName is the per-project state directory that holds the plan files.
// It matches the .sprout/ convention used by the rest of the repo
// (configuration, filediscovery, webcontent).
const planDirName = ".sprout"

// ErrNoPlan is returned by Load when the project has no .sprout/plan.json
// yet. It is an expected, distinguishable not-found (not a failure): callers
// detect it with errors.Is(err, ErrNoPlan) and treat it as "no plan", while
// any other error from Load means the file exists but is unreadable or
// invalid.
var ErrNoPlan = errors.New("no structured plan found")

// Store reads and writes the structured plan for a project. It is safe for
// concurrent use by a single writer; it holds no mutable state beyond the
// (optional) clock. With multiple concurrent writers the revision floor is
// read before the write rather than atomically with it, so monotonicity is
// guaranteed for a single writer only.
type Store struct {
	// Clock supplies the current time used to stamp `updated` on every Save.
	// When nil, time.Now is used. Tests pin it to a fixed instant for
	// deterministic `updated` values.
	Clock func() time.Time
}

// New returns a Store that stamps `updated` with the wall clock.
func New() *Store {
	return &Store{Clock: time.Now}
}

// Load reads and validates the structured plan from the .sprout/plan.json
// under root.
//
//   - When the file does not exist, Load returns (nil, ErrNoPlan).
//   - When the file is corrupt JSON, Load returns a wrapped decode error.
//   - When the file is valid JSON but fails plancontract.Validate, Load
//     returns the *plancontract.ValidationError (recovered with
//     errors.As). In either of those cases the returned plan is nil, so a
//     caller can never observe an invalid plan through Load.
//
// On success Load returns a freshly decoded, valid plan.
func (s *Store) Load(root string) (*plancontract.Plan, error) {
	path := PlanJSONPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoPlan
		}
		return nil, fmt.Errorf("read plan %s: %w", path, err)
	}

	plan, err := plancontract.ValidateJSON(data)
	if err != nil {
		// Discard the decoded-but-invalid plan so callers only ever see a
		// valid plan (or an error) from Load.
		return nil, fmt.Errorf("load plan %s: %w", path, err)
	}
	return plan, nil
}

// Save validates plan and writes it to .sprout/plan.json under root, then
// regenerates the .sprout/plan.md view from it. It returns the stored plan —
// a copy with the bumped revision and stamped `updated` — on success.
//
// Every save bumps the plan's revision by one, from the higher of the
// revision the plan carries and the revision currently stored on disk, and
// stamps `updated` with s.now(), so each write is observable as a new
// revision and the revision never decreases. The floor matters because a
// caller can legitimately arrive with a stale plan — one loaded before
// another process wrote, or assembled by hand — and bumping only from the
// caller's value would move the revision backwards. A stored file that
// cannot be read or parsed contributes no floor: the save proceeds from the
// caller's revision alone. Overwriting a mangled stored file with a valid
// plan is the better recovery here — refusing to save would break the normal
// flow for an unrelated defect the write is about to repair anyway — and it
// means a failed floor read never masks the outcome of the write itself.
// `created` stays as set on the caller's plan.
//
// The caller's plan is left unmodified: Save works on a copy.
//
// An invalid plan is rejected before any file is touched: neither
// plan.json nor plan.md is written and the error wraps the
// *plancontract.ValidationError naming every problem.
func (s *Store) Save(root string, plan *plancontract.Plan) (*plancontract.Plan, error) {
	if plan == nil {
		return nil, fmt.Errorf("save plan: plan is nil")
	}

	// Validate before touching disk or mutating anything, so a rejected
	// write never leaves a partial file behind.
	if err := plancontract.Validate(plan); err != nil {
		return nil, fmt.Errorf("save plan %s (rejected, nothing written): %w", PlanJSONName, err)
	}

	// Work on a copy: Save never mutates the caller's plan. Only the scalar
	// fields (Revision, Updated) change, so a shallow copy is sufficient.
	stored := *plan
	stored.Revision = s.revisionFloor(root, plan.Revision) + 1
	stored.Updated = s.now()

	jsonBytes, err := planJSONBytes(&stored)
	if err != nil {
		return nil, err
	}

	// JSON is the source of truth; write it first, then the derived view.
	if err := writeFile(root, PlanJSONName, jsonBytes); err != nil {
		return nil, err
	}
	if err := writeFile(root, PlanMarkdownName, []byte(RenderMarkdown(&stored))); err != nil {
		// A failure here leaves a newer JSON than a stale markdown. JSON
		// remains authoritative; the next Save re-syncs the view.
		return nil, err
	}
	return &stored, nil
}

// revisionFloor returns the lowest revision the next write may bump from:
// the higher of given and the revision currently stored at the project's
// plan.json. When no plan is stored yet (no file, or no .sprout/ directory),
// or the stored file cannot be read or parsed into a valid plan, the floor
// is just given — the caller's revision is the only input. A floor read
// failure is deliberately swallowed: the stored file contributes an
// optimization-grade floor, not a precondition, and Save overwrites whatever
// is there on its next write anyway.
func (s *Store) revisionFloor(root string, given int) int {
	stored, err := s.Load(root)
	if err != nil {
		return given
	}
	if stored.Revision > given {
		return stored.Revision
	}
	return given
}

// now returns the current time per the store's clock, falling back to
// time.Now when no clock is set.
func (s *Store) now() time.Time {
	if s == nil || s.Clock == nil {
		return time.Now()
	}
	return s.Clock()
}

// PlanJSONPath returns the path to the project's .sprout/plan.json for the
// given project root.
func PlanJSONPath(root string) string {
	return filepath.Join(root, planDirName, PlanJSONName)
}

// PlanMarkdownPath returns the path to the project's .sprout/plan.md for the
// given project root.
func PlanMarkdownPath(root string) string {
	return filepath.Join(root, planDirName, PlanMarkdownName)
}

// planJSONBytes renders plan to its on-disk JSON form: 2-space indented with
// a trailing newline. It is deterministic for a given plan — the schema has
// no maps and struct fields marshal in declaration order — so identical
// stored plans render identical bytes on every platform.
func planJSONBytes(plan *plancontract.Plan) ([]byte, error) {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal plan: %w", err)
	}
	return append(b, '\n'), nil
}

// writeFile creates the .sprout/ directory under root (if needed) and writes
// name with data, returning a wrapped error on failure.
func writeFile(root, name string, data []byte) error {
	dir := filepath.Join(root, planDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s dir: %w", planDirName, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
