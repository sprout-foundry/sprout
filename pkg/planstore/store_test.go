package planstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// Fixed instants so the `updated` stamp (and the markdown metadata) are
// deterministic across the suite.
var (
	fixedCreated = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	fixedNow     = time.Date(2026, 10, 3, 14, 30, 0, 0, time.UTC)
)

// newTestStore returns a Store whose clock is pinned to fixedNow, so every
// Save stamps updated deterministically.
func newTestStore() *Store {
	return &Store{Clock: func() time.Time { return fixedNow }}
}

// testPlan returns a valid plan (New gives revision 1, both timestamps set to
// fixedCreated) with two scope items, each covered by an acceptance item.
func testPlan() *plancontract.Plan {
	p := plancontract.New("Add user authentication to the web app", fixedCreated)
	p.Scope = []plancontract.ScopeItem{
		{ID: "s1", Title: "Auth API", Description: "Login and token endpoints"},
		{ID: "s2", Title: "Session UI", Description: "Login screen"},
	}
	p.Steps = []plancontract.Step{
		{Scope: "s1", Description: "Implement /login and /token endpoints"},
		{Scope: "s2", Description: "Add login screen and wire it to the API"},
	}
	p.Acceptance = []plancontract.Acceptance{
		{ID: "a1", Scope: "s1", Check: "make build", Kind: plancontract.KindBuild},
		{ID: "a2", Scope: "s2", Check: "/login renders", Kind: plancontract.KindPage},
	}
	p.OutOfScope = []plancontract.OutOfScope{
		{Item: "OAuth providers", Reason: "deferred to a follow-up plan"},
	}
	return p
}

// invalidPlan returns a plan that fails validation: scope s2 has no
// acceptance item covering it.
func invalidPlan() *plancontract.Plan {
	p := testPlan()
	p.Acceptance = []plancontract.Acceptance{
		{ID: "a1", Scope: "s1", Check: "make build", Kind: plancontract.KindBuild},
	} // s2 now uncovered
	return p
}

// storedJSON reproduces the exact on-disk bytes Save writes, so the round
// trip test can compare file content against a re-render.
func storedJSON(t *testing.T, p *plancontract.Plan) []byte {
	t.Helper()
	b, err := json.MarshalIndent(p, "", "  ")
	require.NoError(t, err)
	return append(b, '\n')
}

// readPlanFile returns the raw bytes of the project's .sprout/plan.json.
func readPlanFile(t *testing.T, root string) []byte {
	t.Helper()
	b, err := os.ReadFile(PlanJSONPath(root))
	require.NoError(t, err)
	return b
}

func TestLoadMissingPlan(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	plan, err := store.Load(root)
	require.ErrorIs(t, err, ErrNoPlan, "a missing plan must surface the ErrNoPlan sentinel")
	assert.Nil(t, plan)
}

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	stored, err := store.Save(root, testPlan())
	require.NoError(t, err)
	require.NotNil(t, stored)

	loaded, err := store.Load(root)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	// What was read back must render to exactly the bytes on disk, and the
	// in-memory stored plan must match.
	assert.Equal(t, storedJSON(t, loaded), readPlanFile(t, root), "round-tripped plan must render to the on-disk bytes")
	assert.Equal(t, storedJSON(t, stored), storedJSON(t, loaded), "stored and loaded plans must be identical")

	// Spot-check the fields that matter across the boundary.
	assert.Equal(t, stored.Goal, loaded.Goal)
	assert.Equal(t, stored.Revision, loaded.Revision)
	assert.Equal(t, stored.Created, loaded.Created)
	assert.Equal(t, stored.Updated, loaded.Updated)
	assert.Equal(t, stored.Scope, loaded.Scope)
	assert.Equal(t, stored.Acceptance, loaded.Acceptance)
}

func TestSaveBumpsRevisionAndStampsUpdated(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	// First save: New() gives revision 1, so the stored plan is revision 2.
	stored, err := store.Save(root, testPlan())
	require.NoError(t, err)
	assert.Equal(t, 2, stored.Revision, "the first save bumps the plan's revision by one")
	assert.Equal(t, fixedNow, stored.Updated, "updated must be stamped from the clock")
	assert.Equal(t, fixedCreated, stored.Created, "created must stay as set on first creation")

	// The caller's plan is not mutated by Save.
	src := testPlan()
	_, err = store.Save(root, src)
	require.NoError(t, err)
	assert.Equal(t, 1, src.Revision, "Save must not mutate the caller's plan")
	assert.Equal(t, fixedCreated, src.Updated, "Save must not mutate the caller's plan")

	// Editing the stored plan (revision 2) and saving it again keeps the
	// revision climbing, while created stays stable.
	stored.Goal = "Add user authentication and rate limiting"
	stored.Scope = append(stored.Scope, plancontract.ScopeItem{ID: "s3", Title: "Audit log"})
	stored.Acceptance = append(stored.Acceptance, plancontract.Acceptance{ID: "a3", Scope: "s3", Check: "logs writes", Kind: plancontract.KindTest})
	stored2, err := store.Save(root, stored)
	require.NoError(t, err)
	assert.Equal(t, 3, stored2.Revision, "saving the edited plan bumps the revision again")
	assert.Equal(t, fixedCreated, stored2.Created, "created must remain stable across edits")

	loaded, err := store.Load(root)
	require.NoError(t, err)
	assert.Equal(t, 3, loaded.Revision, "the on-disk revision must reflect the latest save")
}

func TestMarkdownRegeneratedOnEdit(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	stored, err := store.Save(root, testPlan())
	require.NoError(t, err)
	md1, err := os.ReadFile(PlanMarkdownPath(root))
	require.NoError(t, err)
	assert.Contains(t, string(md1), "Add user authentication to the web app")
	assert.Contains(t, string(md1), "**Revision:** 2")

	// Edit the stored plan and save a new revision; the markdown must change.
	stored.Goal = "Add user authentication and rate limiting"
	stored2, err := store.Save(root, stored)
	require.NoError(t, err)
	require.Equal(t, 3, stored2.Revision)

	md2, err := os.ReadFile(PlanMarkdownPath(root))
	require.NoError(t, err)
	assert.NotEqual(t, string(md1), string(md2), "plan.md must be regenerated when the plan changes")
	assert.Contains(t, string(md2), "Add user authentication and rate limiting")
	assert.NotContains(t, string(md2), "Add user authentication to the web app", "the old goal must be gone after the edit")
	assert.Contains(t, string(md2), "**Revision:** 3")
}

func TestInvalidWriteRejected(t *testing.T) {
	store := newTestStore()

	t.Run("not created when no plan exists", func(t *testing.T) {
		root := t.TempDir()
		_, err := store.Save(root, invalidPlan())
		require.Error(t, err)
		_, statErr := os.Stat(PlanJSONPath(root))
		assert.True(t, os.IsNotExist(statErr), "an invalid write must not create plan.json")
		_, statErr = os.Stat(PlanMarkdownPath(root))
		assert.True(t, os.IsNotExist(statErr), "an invalid write must not create plan.md")
	})

	t.Run("existing plan unchanged", func(t *testing.T) {
		root := t.TempDir()
		stored, err := store.Save(root, testPlan())
		require.NoError(t, err)
		before := readPlanFile(t, root)

		_, err = store.Save(root, invalidPlan())
		require.Error(t, err)

		after := readPlanFile(t, root)
		assert.Equal(t, before, after, "an invalid write must leave plan.json byte-identical")

		loaded, err := store.Load(root)
		require.NoError(t, err)
		assert.Equal(t, stored.Revision, loaded.Revision, "the on-disk revision must be unchanged after a rejected write")
	})
}

func TestInvalidWriteErrorNamesTheProblem(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	_, err := store.Save(root, invalidPlan())
	require.Error(t, err)

	var vErr *plancontract.ValidationError
	require.ErrorAs(t, err, &vErr, "the error must wrap the *plancontract.ValidationError")
	assert.Contains(t, err.Error(), `scope item "s2" has no acceptance item`)
}

func TestLoadCorruptJSON(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))
	require.NoError(t, os.WriteFile(PlanJSONPath(root), []byte("{ not valid json "), 0o644))

	store := newTestStore()
	plan, err := store.Load(root)
	require.Error(t, err)
	assert.Nil(t, plan)
	assert.False(t, errors.Is(err, ErrNoPlan), "a corrupt file is a different error than a missing one")
}

func TestLoadInvalidPlan(t *testing.T) {
	root := t.TempDir()
	// Write valid JSON that fails structural validation.
	raw := `{"version":1,"revision":1,"created":"2026-10-03T12:00:00Z","updated":"2026-10-03T12:00:00Z","goal":"","scope":[],"steps":[],"acceptance":[],"out_of_scope":[]}`
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))
	require.NoError(t, os.WriteFile(PlanJSONPath(root), []byte(raw), 0o644))

	store := newTestStore()
	plan, err := store.Load(root)
	require.Error(t, err, "an invalid plan must not load")
	assert.Nil(t, plan)

	var vErr *plancontract.ValidationError
	require.ErrorAs(t, err, &vErr, "Load must surface the ValidationError")
	assert.NotEmpty(t, vErr.Problems)
}

func TestSaveNilPlan(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()
	_, err := store.Save(root, nil)
	require.Error(t, err)
	_, statErr := os.Stat(PlanJSONPath(root))
	assert.True(t, os.IsNotExist(statErr))
}

func TestPlanPaths(t *testing.T) {
	assert.Equal(t, filepath.Join("/proj", ".sprout", "plan.json"), PlanJSONPath("/proj"))
	assert.Equal(t, filepath.Join("/proj", ".sprout", "plan.md"), PlanMarkdownPath("/proj"))
}

// TestSaveMarkdownFailureLeavesJSONWritten pins the write order: the JSON
// (the source of truth) is written before the derived markdown view, so a
// failure regenerating plan.md surfaces an error but leaves a valid, loadable
// plan.json in place rather than a half-written project.
func TestSaveMarkdownFailureLeavesJSONWritten(t *testing.T) {
	root := t.TempDir()
	// Force the markdown write to fail by making the plan.md path a directory.
	require.NoError(t, os.MkdirAll(PlanMarkdownPath(root), 0o755))

	store := newTestStore()
	_, err := store.Save(root, testPlan())
	require.Error(t, err, "a failing markdown write must surface an error")

	loaded, err := store.Load(root)
	require.NoError(t, err, "the JSON source of truth must remain loadable")
	assert.Equal(t, 2, loaded.Revision)
}
