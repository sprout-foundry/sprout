package plancontract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidationError describes the structural problems found in a plan. It
// carries one entry per problem so callers can surface them all at once
// (instead of fixing-and-revalidating one at a time). Use errors.As to
// recover the structured list from the error returned by Validate.
type ValidationError struct {
	Problems []string
}

// Error implements error. It always lists every problem so the message is
// actionable on its own.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("plancontract: plan is invalid (%d problem(s)): %s", len(e.Problems), strings.Join(e.Problems, "; "))
}

// Validate reports every structural problem with plan and returns a
// *ValidationError when the plan is invalid, or nil when it is valid.
//
// It is a pure function over the in-memory struct, so it can be used both
// when building a new plan and on every write (SP-148 §148b: "every write
// validates and bumps revision"). The invariants enforced are:
//
//   - version is a supported schema version;
//   - revision >= 1 (a persisted plan has been written at least once);
//   - created and updated are set;
//   - goal is non-empty;
//   - every scope item has a non-empty id and title, and scope ids are unique;
//   - every step references a known scope id and has a description;
//   - every acceptance item has a non-empty id, a known kind, references a
//     known scope id, and acceptance ids are unique;
//   - an interaction acceptance item carries a non-empty steps list whose
//     steps each have a non-empty action, and no other kind carries steps
//     (SP-148 §148d);
//   - every scope item is covered by at least one acceptance item
//     (SP-148 §148b);
//   - every out_of_scope item names an item and records why it was
//     excluded (SP-148 §148a: "with the reason").
func Validate(plan *Plan) error {
	if plan == nil {
		return &ValidationError{Problems: []string{"plan is nil"}}
	}

	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// version.
	if !supportedVersion(plan.Version) {
		add("version %d is not a supported schema version (supported: %s)",
			plan.Version, joinInts(SupportedVersions))
	}

	// revision.
	if plan.Revision < 1 {
		add("revision must be >= 1 (a persisted plan has been written at least once), got %d", plan.Revision)
	}

	// timestamps.
	if plan.Created.IsZero() {
		add("created timestamp is required")
	}
	if plan.Updated.IsZero() {
		add("updated timestamp is required")
	}

	// goal.
	if strings.TrimSpace(plan.Goal) == "" {
		add("goal is required (one or two sentences of what the work is for)")
	}

	// scope: presence, shape, and uniqueness of ids.
	scopeIDs := make(map[string]bool, len(plan.Scope))
	for i, s := range plan.Scope {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			add("scope[%d].id is required", i)
			continue
		}
		if strings.TrimSpace(s.Title) == "" {
			add("scope[%d] (id %q) title is required", i, id)
		}
		if scopeIDs[id] {
			add("duplicate scope id %q (scope ids must be unique)", id)
		}
		scopeIDs[id] = true
	}

	// steps: each must reference a known scope id and carry a description.
	for i, st := range plan.Steps {
		ref := strings.TrimSpace(st.Scope)
		if ref == "" {
			add("steps[%d].scope must reference a scope id", i)
			continue
		}
		if !scopeIDs[ref] {
			add("steps[%d] references unknown scope id %q", i, ref)
		}
		if strings.TrimSpace(st.Description) == "" {
			add("steps[%d] (scope %q) description is required", i, ref)
		}
	}

	// acceptance: shape, kind, scope reference, uniqueness; and the coverage
	// set of scope ids that at least one acceptance item points at.
	acceptanceIDs := make(map[string]bool, len(plan.Acceptance))
	coveredScope := make(map[string]bool, len(plan.Acceptance))
	for i, a := range plan.Acceptance {
		id := strings.TrimSpace(a.ID)
		if id == "" {
			add("acceptance[%d].id is required", i)
		} else if acceptanceIDs[id] {
			add("duplicate acceptance id %q (acceptance ids must be unique)", id)
		} else {
			acceptanceIDs[id] = true
		}

		ref := strings.TrimSpace(a.Scope)
		if ref == "" {
			add("acceptance[%d] (id %q) scope is required (the scope id this check covers)", i, a.ID)
		} else if !scopeIDs[ref] {
			add("acceptance[%d] (id %q) references unknown scope id %q", i, a.ID, ref)
		} else {
			coveredScope[ref] = true
		}

		if !ValidKind(a.Kind) {
			if strings.TrimSpace(string(a.Kind)) == "" {
				add("acceptance[%d] (id %q) is missing kind (required: one of %s)",
					i, a.ID, joinKinds(AllKinds()))
			} else {
				add("acceptance[%d] (id %q) has unknown kind %q (valid kinds: %s)",
					i, a.ID, a.Kind, joinKinds(AllKinds()))
			}
		}

		// steps (SP-148 §148d): an interaction item carries a non-empty
		// list of browse steps, every one of which needs an action; no
		// other kind may carry steps.
		//
		// The action *set* is deliberately not validated here: the executor
		// (webcontent) is the source of truth for which actions exist, and
		// plancontract must stay a pure package that does not drift when the
		// browser gains a new action. Requiring each step's action to be
		// non-empty mirrors parseBrowseSteps' own contract.
		if a.Kind == KindInteraction {
			if len(a.Steps) == 0 {
				add("acceptance[%d] (id %q) is missing steps (kind \"interaction\" requires a non-empty steps list)", i, a.ID)
			} else {
				for si, st := range a.Steps {
					if strings.TrimSpace(st.Action) == "" {
						add("acceptance[%d] (id %q) steps[%d].action is required", i, a.ID, si)
					}
				}
			}
		} else if len(a.Steps) > 0 {
			add("acceptance[%d] (id %q) has steps (only kind \"interaction\" may carry steps; this item's kind is %q)", i, a.ID, a.Kind)
		}
	}

	// every scope item must be covered by at least one acceptance item.
	for _, s := range plan.Scope {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			continue // already reported as a missing id above
		}
		if !coveredScope[id] {
			add("scope item %q has no acceptance item (every scope item needs at least one)", id)
		}
	}

	// out_of_scope: each must name the excluded item and record why it was
	// excluded (SP-148 §148a: "with the reason"), so the exclusion is
	// deliberate and auditable.
	for i, o := range plan.OutOfScope {
		if strings.TrimSpace(o.Item) == "" {
			add("out_of_scope[%d].item is required (the deliberately excluded feature/change)", i)
		}
		if strings.TrimSpace(o.Reason) == "" {
			add("out_of_scope[%d] (item %q) reason is required (why it was excluded)", i, o.Item)
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

// ValidateJSON decodes a plan JSON document and validates the result. It
// returns the decoded plan (non-nil when err is nil) and the first error
// encountered: either a JSON decode error or a *ValidationError. This is the
// convenience entry point for readers/writers that start from the bytes of
// .sprout/plan.json.
func ValidateJSON(data []byte) (*Plan, error) {
	plan := &Plan{}
	if err := json.Unmarshal(data, plan); err != nil {
		return nil, fmt.Errorf("plancontract: invalid plan JSON: %w", err)
	}
	if err := Validate(plan); err != nil {
		return plan, err
	}
	return plan, nil
}

// supportedVersion reports whether v is in SupportedVersions.
func supportedVersion(v int) bool {
	for _, sv := range SupportedVersions {
		if v == sv {
			return true
		}
	}
	return false
}

// joinInts renders a list of ints as "a, b, c".
func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%d", x)
	}
	return strings.Join(parts, ", ")
}

// joinKinds renders a list of kinds as "a, b, c".
func joinKinds(ks []Kind) string {
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = string(k)
	}
	return strings.Join(parts, ", ")
}
