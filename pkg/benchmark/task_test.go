package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/starters"
)

// repoRoot is the repository root, resolved from this package's directory
// the way other repo tests locate root-level fixtures.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

const committedFixturePath = "benchmarks/tasks/fixture/add-version-badge.json"

func committedFixtureFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), filepath.FromSlash(committedFixturePath))
}

func loadCommittedFixture(t *testing.T) *Task {
	t.Helper()
	task, err := LoadTask(committedFixtureFile(t))
	if err != nil {
		t.Fatalf("LoadTask(%s): %v", committedFixturePath, err)
	}
	return task
}

func TestLoadTaskCommittedFixture(t *testing.T) {
	task := loadCommittedFixture(t)

	if want := "add-version-badge"; task.ID != want {
		t.Errorf("task ID = %q, want %q (the file name without .json)", task.ID, want)
	}
	if strings.TrimSpace(task.Request) == "" {
		t.Error("request is empty")
	}
	if task.Starter != "fixture" {
		t.Errorf("starter = %q, want %q (the 153.3 fixture starter)", task.Starter, "fixture")
	}
	if err := plancontract.Validate(&task.Plan); err != nil {
		t.Errorf("frozen plan does not validate: %v", err)
	}
	if task.Plan.Version != plancontract.SchemaVersion {
		t.Errorf("plan version = %d, want %d", task.Plan.Version, plancontract.SchemaVersion)
	}
	if task.Plan.Starter != task.Starter {
		t.Errorf("plan starter %q disagrees with task starter %q", task.Plan.Starter, task.Starter)
	}
}

// TestCommittedFixturePinsToStarterCatalogue is the "on the 153.3 fixture
// starter" pin: the fixture task's starter id resolves in the embedded
// catalogue, and the starter's build command and route appear in the
// frozen plan's acceptance — the fixture is coherent end to end.
func TestCommittedFixturePinsToStarterCatalogue(t *testing.T) {
	task := loadCommittedFixture(t)

	m, err := starters.Manifest(task.Starter)
	if err != nil {
		t.Fatalf("starters.Manifest(%q): %v", task.Starter, err)
	}
	if m.Starter.ID != task.Starter {
		t.Errorf("catalogue starter id = %q, task names %q", m.Starter.ID, task.Starter)
	}
	var sawBuild, sawPage bool
	for _, a := range task.Plan.Acceptance {
		if a.Kind == plancontract.KindBuild && a.Check == m.Build {
			sawBuild = true
		}
		if a.Kind == plancontract.KindPage && slices.Contains(m.Routes, a.Check) {
			sawPage = true
		}
	}
	if !sawBuild {
		t.Errorf("no acceptance item checks the starter's build command %q", m.Build)
	}
	if !sawPage {
		t.Errorf("no acceptance item checks one of the starter's routes %v", m.Routes)
	}
}

func TestLoadSuiteCommitted(t *testing.T) {
	suiteDir := filepath.Join(repoRoot(t), "benchmarks", "tasks")
	tasks, err := LoadSuite(suiteDir)
	if err != nil {
		t.Fatalf("LoadSuite(%s): %v", suiteDir, err)
	}
	if len(tasks) < 1 {
		t.Fatalf("suite has no tasks; want at least the fixture task")
	}
	var saw bool
	for _, task := range tasks {
		if task.Starter == "fixture" && task.ID == "add-version-badge" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("suite of %d task(s) does not include fixture/add-version-badge", len(tasks))
	}
	// Deterministic order: by starter directory, then file name. The
	// committed tasks follow the file-name == task-id convention, so the
	// documented (dir, file) order is also (starter, id) order.
	for i := 1; i < len(tasks); i++ {
		prev, cur := tasks[i-1], tasks[i]
		if prev.Starter > cur.Starter || (prev.Starter == cur.Starter && prev.ID > cur.ID) {
			t.Errorf("suite order not sorted at index %d: %q/%q before %q/%q",
				i, prev.Starter, prev.ID, cur.Starter, cur.ID)
		}
	}
}

// TestLoadSuiteStaticSite loads the committed task suite and pins the
// static-site tasks: the suite root loads cleanly, the committed tasks are
// exactly the ones below (falsifiable — a renamed, added, or removed file
// fails the test), every one names static-site as its starter, and each
// frozen plan validates. A second load returns the same tasks in the same
// order, pinning the loader's determinism.
func TestLoadSuiteStaticSite(t *testing.T) {
	suiteDir := filepath.Join(repoRoot(t), "benchmarks", "tasks")
	tasks, err := LoadSuite(suiteDir)
	if err != nil {
		t.Fatalf("LoadSuite(%s): %v", suiteDir, err)
	}

	wantIDs := []string{
		"add-about-section",
		"add-footer-link",
		"add-form-field",
		"add-page",
		"add-robots-txt",
	}

	var gotIDs []string
	for _, task := range tasks {
		if task.Starter != "static-site" {
			continue
		}
		gotIDs = append(gotIDs, task.ID)
		if err := plancontract.Validate(&task.Plan); err != nil {
			t.Errorf("task %q frozen plan does not validate: %v", task.ID, err)
		}
		if task.Plan.Starter != task.Starter {
			t.Errorf("task %q plan starter %q disagrees with task starter %q",
				task.ID, task.Plan.Starter, task.Starter)
		}
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("static-site task ids = %v, want %v (the committed tasks, in suite order)", gotIDs, wantIDs)
	}

	again, err := LoadSuite(suiteDir)
	if err != nil {
		t.Fatalf("LoadSuite(%s) (second load): %v", suiteDir, err)
	}
	if len(again) != len(tasks) {
		t.Fatalf("second load returned %d task(s), first returned %d (loader must be deterministic)",
			len(again), len(tasks))
	}
	for i := range tasks {
		if tasks[i].Starter != again[i].Starter || tasks[i].ID != again[i].ID {
			t.Errorf("load order differs at index %d: %q/%q then %q/%q",
				i, tasks[i].Starter, tasks[i].ID, again[i].Starter, again[i].ID)
		}
	}
}

// TestStaticSiteTasksPinToStarterCatalogue is the static-site coherence
// pin: the committed static-site tasks resolve against the embedded starter
// catalogue, and every task's acceptance uses checks the verification
// runner can run from that starter's manifest — the build command, one of
// its routes, or its test command.
func TestStaticSiteTasksPinToStarterCatalogue(t *testing.T) {
	tasks, err := LoadSuite(filepath.Join(repoRoot(t), "benchmarks", "tasks"))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	m, err := starters.Manifest("static-site")
	if err != nil {
		t.Fatalf("starters.Manifest(%q): %v", "static-site", err)
	}
	if m.Starter.ID != "static-site" {
		t.Errorf("catalogue starter id = %q, want static-site", m.Starter.ID)
	}

	routes := make(map[string]bool, len(m.Routes))
	for _, r := range m.Routes {
		routes[r] = true
	}

	matched := 0
	for _, task := range tasks {
		if task.Starter != "static-site" {
			continue
		}
		matched++
		for _, a := range task.Plan.Acceptance {
			switch a.Kind {
			case plancontract.KindBuild:
				if a.Check != m.Build {
					t.Errorf("task %q acceptance %q build check = %q, want the manifest build command %q",
						task.ID, a.ID, a.Check, m.Build)
				}
			case plancontract.KindTest:
				if a.Check != m.Test {
					t.Errorf("task %q acceptance %q test check = %q, want the manifest test command %q",
						task.ID, a.ID, a.Check, m.Test)
				}
			case plancontract.KindPage:
				if !routes[a.Check] {
					t.Errorf("task %q acceptance %q page check = %q, want one of the manifest routes %v",
						task.ID, a.ID, a.Check, m.Routes)
				}
			}
		}
	}
	if matched == 0 {
		t.Fatal("no static-site tasks loaded; the catalogue pin asserted nothing")
	}
}

// TestTaskRoundTripStability proves the frozen format is stable: a marshal
// cycle of the committed fixture reproduces the same task, byte for byte.
// The fixture uses second-granularity UTC timestamps so the time.Time JSON
// round-trip is byte-stable as well.
func TestTaskRoundTripStability(t *testing.T) {
	original := loadCommittedFixture(t)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal fixture task: %v", err)
	}
	path := filepath.Join(t.TempDir(), "add-version-badge.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write round-trip file: %v", err)
	}
	reloaded, err := LoadTask(path)
	if err != nil {
		t.Fatalf("LoadTask(round-trip): %v", err)
	}
	if !reflect.DeepEqual(original, reloaded) {
		t.Errorf("round-trip task differs from original:\n original: %+v\n reloaded: %+v", original, reloaded)
	}
	again, err := json.Marshal(reloaded)
	if err != nil {
		t.Fatalf("marshal reloaded task: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Errorf("marshalled bytes are not stable across a marshal cycle:\n first: %s\n second: %s", data, again)
	}
}

// validTask returns a task that passes every rule, as the base for the
// negative table.
func validTask() *Task {
	ts := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	return &Task{
		ID:      "add-version-badge",
		Request: "Add a version badge to the '/' page of the fixture starter.",
		Starter: "fixture",
		Plan: plancontract.Plan{
			Version:  plancontract.SchemaVersion,
			Revision: 1,
			Created:  ts,
			Updated:  ts,
			Goal:     "Show the starter id and version on the fixture page.",
			Scope: []plancontract.ScopeItem{
				{ID: "version-badge", Title: "Version badge on the '/' page"},
			},
			Steps: []plancontract.Step{
				{Scope: "version-badge", Description: "Add and fill the badge element."},
			},
			Starter: "fixture",
			Acceptance: []plancontract.Acceptance{
				{ID: "build-passes", Scope: "version-badge", Check: "npm run build", Kind: plancontract.KindBuild},
				{ID: "page-renders", Scope: "version-badge", Check: "/", Kind: plancontract.KindPage},
			},
			OutOfScope: []plancontract.OutOfScope{
				{Item: "More routes on the fixture starter", Reason: "Single-page test-only starter."},
			},
		},
	}
}

func writeTaskFile(t *testing.T, task *Task) string {
	t.Helper()
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	path := filepath.Join(t.TempDir(), "case.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write task file: %v", err)
	}
	return path
}

func TestLoadTaskValid(t *testing.T) {
	path := writeTaskFile(t, validTask())
	task, err := LoadTask(path)
	if err != nil {
		t.Fatalf("LoadTask(valid): %v", err)
	}
	if !reflect.DeepEqual(task, validTask()) {
		t.Errorf("loaded task differs from the written task:\n got: %+v", task)
	}
}

func TestLoadTaskRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		// mutate transforms the valid base task (nil: none).
		mutate func(t *Task)
		// raw, when set, is written verbatim instead of marshalling a task.
		raw *string
		// wantIs is the sentinel the error must wrap. Nil means a
		// file-level error: non-nil, and NOT ErrInvalidTask.
		wantIs error
	}{
		{name: "missing id", mutate: func(t *Task) { t.ID = "" }, wantIs: ErrInvalidTask},
		{name: "blank id", mutate: func(t *Task) { t.ID = "   " }, wantIs: ErrInvalidTask},
		{name: "id with path separator", mutate: func(t *Task) { t.ID = "a/b" }, wantIs: ErrInvalidTask},
		{name: "id is ..", mutate: func(t *Task) { t.ID = ".." }, wantIs: ErrInvalidTask},
		{name: "missing request", mutate: func(t *Task) { t.Request = "" }, wantIs: ErrInvalidTask},
		{name: "missing starter", mutate: func(t *Task) { t.Starter = "" }, wantIs: ErrInvalidTask},
		{name: "starter with path separator", mutate: func(t *Task) { t.Starter = "web/app" }, wantIs: ErrInvalidTask},
		{
			name:   "unknown acceptance kind",
			mutate: func(t *Task) { t.Plan.Acceptance[0].Kind = "bogus" },
			wantIs: ErrInvalidTask,
		},
		{
			name: "scope item without acceptance coverage",
			mutate: func(t *Task) {
				t.Plan.Scope = append(t.Plan.Scope,
					plancontract.ScopeItem{ID: "orphan", Title: "Uncovered"})
			},
			wantIs: ErrInvalidTask,
		},
		{
			name:   "out-of-scope entry without a reason",
			mutate: func(t *Task) { t.Plan.OutOfScope[0].Reason = "" },
			wantIs: ErrInvalidTask,
		},
		{
			// The frozen check: a drifted (unsupported) plan schema version
			// is rejected, so the loader never silently accepts a plan the
			// validator does not support.
			name:   "unsupported plan schema version",
			mutate: func(t *Task) { t.Plan.Version = 99 },
			wantIs: ErrInvalidTask,
		},
		{
			name:   "plan revision zero",
			mutate: func(t *Task) { t.Plan.Revision = 0 },
			wantIs: ErrInvalidTask,
		},
		{
			name:   "malformed JSON",
			raw:    strPtr("{ not json"),
			wantIs: nil,
		},
		{
			name:   "empty file",
			raw:    strPtr(""),
			wantIs: nil,
		},
		{
			name:   "non-object JSON",
			raw:    strPtr(`[1, 2]`),
			wantIs: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "case.json")
			var data []byte
			if tc.raw != nil {
				data = []byte(*tc.raw)
			} else {
				task := validTask()
				if tc.mutate != nil {
					tc.mutate(task)
				}
				var err error
				data, err = json.Marshal(task)
				if err != nil {
					t.Fatalf("marshal base task: %v", err)
				}
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("write task file: %v", err)
			}

			task, err := LoadTask(path)
			if err == nil {
				t.Fatalf("LoadTask succeeded; want an error")
			}
			if task != nil {
				t.Fatalf("LoadTask returned a task alongside an error: %+v", task)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error does not name the file %s: %v", path, err)
			}
			if tc.wantIs != nil {
				if !errors.Is(err, tc.wantIs) {
					t.Errorf("error does not wrap %v: %v", tc.wantIs, err)
				}
			} else if errors.Is(err, ErrInvalidTask) {
				t.Errorf("file-level error unexpectedly wraps ErrInvalidTask: %v", err)
			}
		})
	}
}

func TestLoadTaskMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	task, err := LoadTask(path)
	if task != nil || err == nil {
		t.Fatalf("task = %+v, err = %v; want nil task and an error", task, err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file error should be detectable with errors.Is(err, fs.ErrNotExist): %v", err)
	}
}

func strPtr(s string) *string { return &s }
