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

	// The caller's plan is not mutated by Save. The plan used here is a
	// fresh revision-1 plan — a stale caller relative to the revision-2
	// file — so this write also exercises the monotonic floor: the on-disk
	// revision advances to 3 rather than being rewound to 2.
	src := testPlan()
	_, err = store.Save(root, src)
	require.NoError(t, err)
	assert.Equal(t, 1, src.Revision, "Save must not mutate the caller's plan")
	assert.Equal(t, fixedCreated, src.Updated, "Save must not mutate the caller's plan")

	// Editing the stored plan (revision 2) and saving it again keeps the
	// revision climbing past the stale write above (3), while created stays
	// stable.
	stored.Goal = "Add user authentication and rate limiting"
	stored.Scope = append(stored.Scope, plancontract.ScopeItem{ID: "s3", Title: "Audit log"})
	stored.Acceptance = append(stored.Acceptance, plancontract.Acceptance{ID: "a3", Scope: "s3", Check: "logs writes", Kind: plancontract.KindTest})
	stored2, err := store.Save(root, stored)
	require.NoError(t, err)
	assert.Equal(t, 4, stored2.Revision, "saving the edited plan bumps the revision again")
	assert.Equal(t, fixedCreated, stored2.Created, "created must remain stable across edits")

	loaded, err := store.Load(root)
	require.NoError(t, err)
	assert.Equal(t, 4, loaded.Revision, "the on-disk revision must reflect the latest save")
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

// stalePlan returns the shared fixture with an artificially low revision, the
// shape a caller gets from loading a plan before another writer advanced it
// (or from assembling one by hand).
func stalePlan() *plancontract.Plan {
	p := testPlan()
	p.Revision = 1
	return p
}

// TestSaveStaleCallerBumpsFromStored pins monotonicity: a caller arriving
// with a plan whose revision is *lower* than the one on disk must not drag
// the stored revision backwards. The bump comes from the higher of the two
// revisions.
func TestSaveStaleCallerBumpsFromStored(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	// Establish revision 2 on disk.
	stored, err := store.Save(root, testPlan())
	require.NoError(t, err)
	require.Equal(t, 2, stored.Revision)

	// A stale caller (revision 1) saves: the on-disk floor (2) wins over the
	// caller's 1, so the new revision is 3, not 2.
	stored2, err := store.Save(root, stalePlan())
	require.NoError(t, err)
	assert.Equal(t, 3, stored2.Revision, "a stale caller must not decrease the stored revision")

	// Both artifacts on disk reflect the floor-based bump.
	loaded, err := store.Load(root)
	require.NoError(t, err)
	assert.Equal(t, 3, loaded.Revision, "plan.json must reflect the floor-based bump")
	md, err := os.ReadFile(PlanMarkdownPath(root))
	require.NoError(t, err)
	assert.Contains(t, string(md), "**Revision:** 3", "plan.md must reflect the floor-based bump")
}

// TestSaveMonotonicCallerUnchanged pins the unchanged behavior for callers
// that are not behind disk: a caller at the stored revision bumps from it,
// and a caller ahead of disk (e.g. one that assembled the next revision
// itself) keeps its own bump basis.
func TestSaveMonotonicCallerUnchanged(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	stored, err := store.Save(root, testPlan())
	require.NoError(t, err)
	require.Equal(t, 2, stored.Revision)

	// given == stored: a caller at the stored revision bumps as before.
	sameRev := testPlan()
	sameRev.Revision = 2
	stored2, err := store.Save(root, sameRev)
	require.NoError(t, err)
	assert.Equal(t, 3, stored2.Revision)

	// given > stored: the caller's own revision stays the bump basis.
	ahead := testPlan()
	ahead.Revision = 9
	stored3, err := store.Save(root, ahead)
	require.NoError(t, err)
	assert.Equal(t, 10, stored3.Revision, "a caller ahead of disk bumps from its own revision")

	loaded, err := store.Load(root)
	require.NoError(t, err)
	assert.Equal(t, 10, loaded.Revision)
}

// TestSaveFirstWriteNoPriorPlan pins the first-save path: with no plan.json
// on disk (here not even a .sprout/ directory), the floor is just the
// caller's revision, so the stored revision is given + 1 exactly as before.
func TestSaveFirstWriteNoPriorPlan(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	stored, err := store.Save(root, testPlan())
	require.NoError(t, err)
	assert.Equal(t, 2, stored.Revision, "the first save bumps from the given revision alone")

	loaded, err := store.Load(root)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.Revision)
}

// TestSaveCorruptStoredFileFallsBack pins the documented recovery for an
// unreadable stored file: it contributes no floor (the caller's revision is
// the bump basis), the save succeeds, and the corrupt file is replaced by a
// valid plan — a read error from the floor never masks the write.
func TestSaveCorruptStoredFileFallsBack(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))
	require.NoError(t, os.WriteFile(PlanJSONPath(root), []byte("{ not valid json "), 0o644))

	store := newTestStore()
	stored, err := store.Save(root, testPlan())
	require.NoError(t, err, "a corrupt stored file must not block the save")
	assert.Equal(t, 2, stored.Revision, "without a floor the save bumps from the given revision")

	// The write repaired the file: it loads cleanly again.
	loaded, err := store.Load(root)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.Revision)
	md, err := os.ReadFile(PlanMarkdownPath(root))
	require.NoError(t, err)
	assert.Contains(t, string(md), "**Revision:** 2")
}

// TestSaveRevisionNeverDecreasesAcrossStaleSequence proves the invariant end
// to end: interleaving fresh saves with stale-caller saves (the pattern of
// two sessions, or a session plus a hand edit, sharing one project) keeps the
// on-disk revision climbing at every step, never repeating or decreasing.
func TestSaveRevisionNeverDecreasesAcrossStaleSequence(t *testing.T) {
	root := t.TempDir()
	store := newTestStore()

	last := 0
	expect := func(want int, step string) {
		t.Helper()
		loaded, err := store.Load(root)
		require.NoError(t, err, step)
		assert.Equal(t, want, loaded.Revision, "%s: on-disk revision must match", step)
		assert.Greater(t, loaded.Revision, last, "%s: revision must strictly increase", step)
		last = loaded.Revision
	}

	// rev 2: first save.
	_, err := store.Save(root, testPlan())
	require.NoError(t, err)
	expect(2, "first save")

	// rev 3: a stale caller (still holding revision 1) saves.
	_, err = store.Save(root, stalePlan())
	require.NoError(t, err)
	expect(3, "stale caller after first save")

	// rev 4: another stale caller, still holding revision 1.
	_, err = store.Save(root, stalePlan())
	require.NoError(t, err)
	expect(4, "second stale caller in a row")

	// rev 5: a current caller (loaded at rev 4) edits and saves.
	current, err := store.Load(root)
	require.NoError(t, err)
	current.Goal = "Add user authentication and rate limiting"
	_, err = store.Save(root, current)
	require.NoError(t, err)
	expect(5, "current caller after stale saves")

	// rev 6: one more stale caller (holding revision 1) cannot pull it back.
	_, err = store.Save(root, stalePlan())
	require.NoError(t, err)
	expect(6, "stale caller after current caller")

	// The returned stored copies agree with the on-disk sequence.
	assert.Equal(t, last, func() int {
		loaded, err := store.Load(root)
		require.NoError(t, err)
		return loaded.Revision
	}())
}
