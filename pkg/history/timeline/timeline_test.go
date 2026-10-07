package timeline

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// seedHistory redirects pkg/history storage to a temp dir for the duration
// of the test and returns a function that records one revision with the
// given changes.
func seedHistory(t *testing.T) func(revID string, changes ...history.ChangeLog) {
	t.Helper()
	prevC, prevR := history.GetPathsForTesting()
	tmp := t.TempDir()
	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	t.Cleanup(func() { history.SetPathsForTesting(prevC, prevR) })

	return func(revID string, changes ...history.ChangeLog) {
		t.Helper()
		if _, err := history.RecordBaseRevision(revID, "prompt "+revID, "response "+revID, nil); err != nil {
			t.Fatalf("RecordBaseRevision(%q): %v", revID, err)
		}
		for _, c := range changes {
			err := history.RecordChangeWithDetails(
				revID, c.Filename, c.OriginalCode, c.NewCode,
				c.Description, "", "", "", "test-model",
			)
			if err != nil {
				t.Fatalf("RecordChangeWithDetails(%q, %q): %v", revID, c.Filename, err)
			}
		}
	}
}

// TestBuildTimeline_FixtureHistoryChangeSets seeds a fixture history with
// two revisions and asserts the change sets and their template summaries.
func TestBuildTimeline_FixtureHistoryChangeSets(t *testing.T) {
	record := seedHistory(t)

	record("rev-1",
		history.ChangeLog{
			Filename:     "a.go",
			OriginalCode: "package a\n",
			NewCode:      "package a\n\nfunc A() {}\n",
		},
		history.ChangeLog{
			Filename:     "b.go",
			OriginalCode: "package b\n",
			NewCode:      "package b\n\nfunc B() {}\n",
		},
	)

	entries, err := BuildTimeline(nil, map[string][]string{"rev-1": {"scope-1", "scope-2"}}, OldestFirst)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	e := entries[0]
	if e.Kind() != KindChangeSet {
		t.Fatalf("kind = %q, want %q", e.Kind(), KindChangeSet)
	}
	if e.ChangeSet.RevisionID != "rev-1" {
		t.Errorf("revision = %q, want rev-1", e.ChangeSet.RevisionID)
	}
	if got, want := e.ChangeSet.FilesTouched, 2; got != want {
		t.Errorf("files touched = %d, want %d", got, want)
	}
	if got, want := e.ChangeSet.Files, []string{"a.go", "b.go"}; !equalStrings(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if e.ChangeSet.Insertions != 4 {
		t.Errorf("insertions = %d, want 4 (2 per file)", e.ChangeSet.Insertions)
	}
	if e.ChangeSet.Deletions != 0 {
		t.Errorf("deletions = %d, want 0", e.ChangeSet.Deletions)
	}
	if got, want := e.ChangeSet.Summary, "changed 2 files: a.go, b.go (+4/-0)"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if got, want := e.ScopeIDs(), []string{"scope-1", "scope-2"}; !equalStrings(got, want) {
		t.Errorf("scope IDs = %v, want %v", got, want)
	}
}

// TestBuildTimeline_Ordering pins the documented sort orders and the
// tie-break.
func TestBuildTimeline_Ordering(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mk := func(id string, ts time.Time) EntryInput {
		return EntryInput{Revision: &history.RevisionGroup{
			RevisionID: id,
			Timestamp:  ts,
			Changes: []history.ChangeLog{{
				Filename: "f.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "y\n",
			}},
		}}
	}

	inputs := []EntryInput{
		mk("rev-late", t0.Add(2*time.Hour)),
		mk("rev-early", t0),
		mk("rev-mid", t0.Add(time.Hour)),
	}

	oldest := Build(inputs, OldestFirst)
	gotIDs := revisionIDs(oldest)
	if want := []string{"rev-early", "rev-mid", "rev-late"}; !equalStrings(gotIDs, want) {
		t.Errorf("oldest-first = %v, want %v", gotIDs, want)
	}

	newest := Build(inputs, NewestFirst)
	gotIDs = revisionIDs(newest)
	if want := []string{"rev-late", "rev-mid", "rev-early"}; !equalStrings(gotIDs, want) {
		t.Errorf("newest-first = %v, want %v", gotIDs, want)
	}
}

// TestBuild_EqualTimestampTieBreak pins the deterministic tie-break: a
// change set sorts before a deploy at the same timestamp, and equal-kind
// entries sort by their stable identity.
func TestBuild_EqualTimestampTieBreak(t *testing.T) {
	ts := time.Date(2026, 2, 2, 8, 0, 0, 0, time.UTC)
	rev := &history.RevisionGroup{
		RevisionID: "rev-a",
		Timestamp:  ts,
		Changes: []history.ChangeLog{{
			Filename: "f.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "y\n",
		}},
	}
	dep := &deploy.Deployment{ID: "proj-1", Project: "proj", Kind: deploy.KindPreview, CreatedAt: ts}

	entries := Build([]EntryInput{{Deployment: dep}, {Revision: rev}}, OldestFirst)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Kind() != KindChangeSet {
		t.Errorf("first entry kind = %q, want change_set (change sets sort before deploys on a tie)", entries[0].Kind())
	}
	if entries[1].Kind() != KindDeploy {
		t.Errorf("second entry kind = %q, want deploy", entries[1].Kind())
	}
}

// TestBuildTimeline_DeployEntryAlongsideChangeSets is the required
// deploy-in-the-timeline test: a deploy entry appears between change sets
// in timestamp order, carrying the deploy package's value.
func TestBuildTimeline_DeployEntryAlongsideChangeSets(t *testing.T) {
	record := seedHistory(t)
	record("rev-early", history.ChangeLog{
		Filename: "a.go", OriginalCode: "package a\n", NewCode: "package a\n// x\n",
	})

	// The seeded revision is recorded with time.Now(); make the deploy
	// land clearly after it so the order is unambiguous.
	deployment := deploy.Deployment{
		ID:        "proj-1",
		Project:   "proj",
		Kind:      deploy.KindProduction,
		URL:       "https://proj.example",
		Version:   "v3",
		CreatedAt: time.Now().Add(time.Hour),
		Status:    deploy.StatusReady,
	}

	entries, err := BuildTimeline([]deploy.Deployment{deployment}, nil, OldestFirst)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (one change set, one deploy)", len(entries))
	}
	if entries[0].Kind() != KindChangeSet {
		t.Errorf("entry 0 kind = %q, want change_set", entries[0].Kind())
	}
	if entries[1].Kind() != KindDeploy {
		t.Errorf("entry 1 kind = %q, want deploy", entries[1].Kind())
	}
	if entries[1].Deploy.Deployment.ID != "proj-1" {
		t.Errorf("deploy id = %q, want proj-1", entries[1].Deploy.Deployment.ID)
	}
	if got, want := entries[1].Summary(), "deployed v3 to production"; got != want {
		t.Errorf("deploy summary = %q, want %q", got, want)
	}
	if entries[1].ScopeIDs() != nil {
		t.Errorf("deploy scope IDs = %v, want nil", entries[1].ScopeIDs())
	}
}

// TestBuildTimeline_DeterministicRepeatedBuilds asserts the builder is
// deterministic: same inputs, same output, byte-for-byte on summaries.
func TestBuildTimeline_DeterministicRepeatedBuilds(t *testing.T) {
	record := seedHistory(t)
	record("rev-1",
		history.ChangeLog{Filename: "b.go", OriginalCode: "p\n", NewCode: "p\nq\n"},
		history.ChangeLog{Filename: "a.go", OriginalCode: "p\n", NewCode: "p\nq\n"},
	)

	first, err := BuildTimeline(nil, nil, NewestFirst)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := BuildTimeline(nil, nil, NewestFirst)
		if err != nil {
			t.Fatalf("BuildTimeline: %v", err)
		}
		if len(again) != len(first) {
			t.Fatalf("length changed: %d vs %d", len(again), len(first))
		}
		for j := range first {
			if first[j].Summary() != again[j].Summary() {
				t.Fatalf("summary diverged at %d: %q vs %q", j, first[j].Summary(), again[j].Summary())
			}
		}
	}
}

// TestBuildTimeline_NoHistoryIsEmpty asserts an empty history yields no
// entries and no error.
func TestBuildTimeline_NoHistoryIsEmpty(t *testing.T) {
	_ = seedHistory(t)
	entries, err := BuildTimeline(nil, nil, OldestFirst)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func revisionIDs(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ChangeSet.RevisionID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
