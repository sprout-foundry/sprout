// Package plancontract owns the schema for structured plans: the
// machine-readable plan document stored at .sprout/plan.json (plus a
// rendered .sprout/plan.md view that is regenerated from the JSON on every
// write) and the validator that every reader and writer of that file must run.
//
// A plan is the single source of truth that links planned work to machine-
// checkable acceptance criteria, so that later tooling (verified
// done, progress events, benchmark) can tell whether the work
// a plan describes is actually finished. The package is a pure data +
// validation contract: it holds no I/O. Callers read and write the JSON
// file; this package defines its shape and enforces its invariants.
//
// JSON field names are part of the on-disk contract and must stay in sync
// with the spec (roadmap/SP-148-structured-plans.md). Bumping the
// schema version (SchemaVersion / SupportedVersions) is the only supported
// way to change the contract in a breaking way.
package plancontract

import (
	"fmt"
	"strings"
	"time"
)

// SchemaVersion is the current plan schema version. A plan document carries it
// in its "version" field; readers that do not support it must refuse the plan
// rather than guess.
const SchemaVersion = 1

// SupportedVersions are the schema versions Validate accepts. A plan whose
// version is not in this set fails validation with a clear message instead of
// being silently misread.
var SupportedVersions = []int{SchemaVersion}

// Kind classifies an acceptance item. It is stored as a string
// in the JSON document so unknown values round-trip and can be reported as
// "unknown kind" by the validator instead of failing the JSON decode.
type Kind string

// The acceptance kinds defined by the plan schema.
const (
	// KindBuild: the project builds.
	KindBuild Kind = "build"
	// KindTest: the project's tests pass.
	KindTest Kind = "test"
	// KindPage: a route renders (a page check).
	KindPage Kind = "page"
	// KindInteraction: scripted browser steps (reuse of the
	// browse step format).
	KindInteraction Kind = "interaction"
	// KindManual: reported by a human, never machine-checked.
	KindManual Kind = "manual"
)

// AllKinds returns every defined acceptance kind, in canonical order. It is
// intended for consumers that must enumerate or display the kinds (e.g. the
// planning prompt's schema section).
func AllKinds() []Kind {
	return []Kind{KindBuild, KindTest, KindPage, KindInteraction, KindManual}
}

// kindDescriptions maps each kind to a short human description. It is the
// single source for what a kind means, so prose that explains the schema
// (prompts, docs) and any renderer stay consistent.
func kindDescriptions() map[Kind]string {
	return map[Kind]string{
		KindBuild:       "the project builds",
		KindTest:        "the project's tests pass",
		KindPage:        "a route renders",
		KindInteraction: "scripted browser steps reach an expected outcome",
		KindManual:      "reported by a human, not machine-checked",
	}
}

// KindDescription returns a short human description of k, or "" for an
// unknown kind.
func KindDescription(k Kind) string {
	return kindDescriptions()[k]
}

// ValidKind reports whether k is one of the defined acceptance kinds.
func ValidKind(k Kind) bool {
	switch k {
	case KindBuild, KindTest, KindPage, KindInteraction, KindManual:
		return true
	}
	return false
}

// String renders the kind as its JSON token, for diagnostics.
func (k Kind) String() string { return string(k) }

// Plan is a versioned, machine-readable implementation plan. It is
// the in-memory form of .sprout/plan.json. JSON field names are the on-disk
// contract; see the package doc.
type Plan struct {
	// Version is the schema version of this document.
	Version int `json:"version"`
	// Revision increments on every edit to the plan; the store
	// bumps it on each write. A persisted plan is always >= 1.
	Revision int `json:"revision"`
	// Created and Updated are the plan's lifecycle timestamps (RFC3339 in
	// JSON).
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	// Goal is what the work is for, in one or two sentences.
	Goal string `json:"goal"`

	// Scope is the set of features or changes the plan covers.
	Scope []ScopeItem `json:"scope"`
	// Steps are the ordered implementation steps; each references a scope item.
	Steps []Step `json:"steps"`
	// Design is an optional reference to screens or files under design/
	// (SP-140 directory contract).
	Design string `json:"design,omitempty"`
	// Starter is an optional starter ID when the project uses one.
	Starter string `json:"starter,omitempty"`

	// Acceptance are the machine- or human-checkable criteria. Every scope
	// item must be covered by at least one acceptance item.
	Acceptance []Acceptance `json:"acceptance"`
	// OutOfScope are items discussed and deliberately excluded, with the
	// reason, so scope does not creep silently between sessions.
	OutOfScope []OutOfScope `json:"out_of_scope"`
}

// ScopeItem is one feature or change the plan covers.
type ScopeItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// Step is one ordered implementation step that references a scope item. The
// array order is the step order; a step carries no ID of its own.
type Step struct {
	// Scope is the id of the scope item this step implements.
	Scope string `json:"scope"`
	// Description says what the step does.
	Description string `json:"description"`
}

// BrowseStep is one scripted browser step of an interaction acceptance item.
// It mirrors the JSON wire format of
// webcontent.BrowseStep — the browse tool's step language — field for field
// (same fields, same JSON tags, same omitempty rules), so a plan's steps can
// be handed to the browse step parser (parseBrowseSteps in
// pkg/agent/tool_handlers_browse.go) without conversion: there is one step
// language for browser automation.
//
// plancontract must stay a pure package (it is used by every reader and
// writer of the plan, including WASM builds) and therefore cannot import
// pkg/webcontent (which drags in a headless browser). Compatibility is a
// wire-format contract, not a Go type dependency: plancontract fits
// webcontent's wire format, never the reverse. A cross-package pin test in
// pkg/agent (plan_browse_compat_test.go) keeps the two formats together:
// marshalling a plan's steps and running them through parseBrowseSteps must
// round-trip field for field. When webcontent.BrowseStep gains a field or a
// tag changes, this struct and that pin test must be updated in the same
// change.
type BrowseStep struct {
	// Action is the step verb (e.g. "click", "fill", "assert_text"). It is
	// required and non-empty (mirroring parseBrowseSteps); the action set
	// itself is validated by the executor at runtime, not by the plan
	// validator.
	Action string `json:"action"`
	// Selector is the CSS selector the step targets, where applicable.
	Selector string `json:"selector,omitempty"`
	// Value is the value the step types or fills, where applicable.
	Value string `json:"value,omitempty"`
	// Key is the keyboard key for press-style steps, where applicable.
	Key string `json:"key,omitempty"`
	// Millis is a wait time in milliseconds for sleep-style steps, where
	// applicable.
	Millis int `json:"millis,omitempty"`
	// Script is a JavaScript snippet for eval-style steps, where applicable.
	Script string `json:"script,omitempty"`
	// Expect is the expected value or text for assert-style steps, where
	// applicable.
	Expect string `json:"expect,omitempty"`
	// ScreenshotPath is the file path for a screenshot_selector step's
	// cropped element screenshot, where applicable.
	ScreenshotPath string `json:"screenshot_path,omitempty"`
}

// Acceptance is one check that proves a scope item is done.
// Kind selects which verifier applies; an empty or unknown kind is
// a validation error.
type Acceptance struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	// Check is the concrete check (command, route, or steps reference). It is
	// optional for KindManual, which is reported rather than run.
	Check string `json:"check,omitempty"`
	Kind  Kind   `json:"kind"`
	// Steps are the scripted browser steps for KindInteraction items
	// in execution order. They are empty for every other
	// kind; the validator enforces both rules (required non-empty for
	// interaction, forbidden otherwise).
	Steps []BrowseStep `json:"steps,omitempty"`
}

// OutOfScope is an item that was discussed and deliberately left out of the
// plan. Item is the excluded feature/change; Reason records why it was
// excluded, so the exclusion is deliberate and auditable
// "with the reason"). Both are required for a valid plan — an exclusion
// without a reason is exactly the silent scope creep the list exists to
// prevent.
type OutOfScope struct {
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// New returns a plan ready to be filled in: the current schema version,
// revision 1, both timestamps set to now, and every collection initialized to
// an empty (non-nil) slice so the marshalled JSON is complete and stable.
// Callers then populate Goal, Scope, Steps, Acceptance, and OutOfScope and
// run Validate before writing.
func New(goal string, now time.Time) *Plan {
	return &Plan{
		Version:    SchemaVersion,
		Revision:   1,
		Created:    now,
		Updated:    now,
		Goal:       goal,
		Scope:      []ScopeItem{},
		Steps:      []Step{},
		Acceptance: []Acceptance{},
		OutOfScope: []OutOfScope{},
	}
}

// ScopeByID returns the scope item with the given id and whether it was found.
func (p *Plan) ScopeByID(id string) (ScopeItem, bool) {
	if p == nil {
		return ScopeItem{}, false
	}
	for _, s := range p.Scope {
		if s.ID == id {
			return s, true
		}
	}
	return ScopeItem{}, false
}

// AcceptanceForScope returns the acceptance items that cover the given scope
// id, in document order.
func (p *Plan) AcceptanceForScope(scopeID string) []Acceptance {
	if p == nil {
		return nil
	}
	var out []Acceptance
	for _, a := range p.Acceptance {
		if a.Scope == scopeID {
			out = append(out, a)
		}
	}
	return out
}

// BumpRevision records an edit: it advances the revision by one and sets
// Updated to now. The store calls this before writing so that
// every write is observable as a new revision.
func (p *Plan) BumpRevision(now time.Time) {
	if p == nil {
		return
	}
	p.Revision++
	p.Updated = now
}

// Summary is a compact, deterministic rendering of the plan for context
// injection and display. It never includes raw free-form
// fields beyond goal/scope, so it is safe to place in a model context.
func (p *Plan) Summary() string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Plan (v%d, rev %d): %s\n", p.Version, p.Revision, strings.TrimSpace(p.Goal))
	for _, s := range p.Scope {
		fmt.Fprintf(&b, "  scope %s: %s\n", s.ID, s.Title)
	}
	return b.String()
}
