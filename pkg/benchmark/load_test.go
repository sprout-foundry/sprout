package benchmark

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// suiteTask returns a minimal valid task for the given starter and id, for
// the LoadSuite layout tests.
func suiteTask(starter, id string) *Task {
	task := validTask()
	task.ID = id
	task.Starter = starter
	task.Plan.Starter = starter
	return task
}

func writeSuiteTask(t *testing.T, suiteDir, starter, id string) {
	t.Helper()
	path := filepath.Join(suiteDir, starter, id+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir starter dir %s: %v", starter, err)
	}
	data, err := json.Marshal(suiteTask(starter, id))
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadSuiteOrderAndIgnoredEntries(t *testing.T) {
	dir := t.TempDir()
	writeSuiteTask(t, dir, "b-starter", "second")
	writeSuiteTask(t, dir, "a-starter", "second")
	writeSuiteTask(t, dir, "a-starter", "first")
	writeStrayFiles(t, dir)

	tasks, err := LoadSuite(dir)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	var got []string
	for _, task := range tasks {
		got = append(got, task.Starter+"/"+task.ID)
	}
	want := []string{"a-starter/first", "a-starter/second", "b-starter/second"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("suite order = %v, want %v (sorted by starter dir, then file name; stray files ignored)", got, want)
	}
}

// writeStrayFiles lays down entries the loader must ignore: a non-.json
// file and a .json file directly under the suite dir (not under a starter
// dir), a non-.json file inside a starter dir, and a subdirectory inside a
// starter dir holding a .json file (tasks are exactly one level deep).
func writeStrayFiles(t *testing.T, dir string) {
	t.Helper()
	strays := map[string]string{
		filepath.Join(dir, "notes.md"):                          "not a starter",
		filepath.Join(dir, "stray.json"):                        `{}`,
		filepath.Join(dir, "a-starter", "README.md"):            "read me",
		filepath.Join(dir, "a-starter", "nested", "inner.json"): `{}`,
	}
	for path, content := range strays {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func TestLoadSuiteStarterMismatch(t *testing.T) {
	dir := t.TempDir()
	// The task's starter field names a different starter than the
	// directory it lives in.
	path := filepath.Join(dir, "a-starter", "task.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(suiteTask("b-starter", "task"))
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	_, err = LoadSuite(dir)
	if err == nil {
		t.Fatal("LoadSuite succeeded; want a starter mismatch error")
	}
	if !errors.Is(err, ErrInvalidTask) {
		t.Errorf("mismatch error does not wrap ErrInvalidTask: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("mismatch error does not name the offending file %s: %v", path, err)
	}
}

func TestLoadSuiteInvalidTaskFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture", "broken.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"id": "", "request": "", "starter": "fixture", "plan": {}}`), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	_, err := LoadSuite(dir)
	if err == nil {
		t.Fatal("LoadSuite succeeded; want a validation error")
	}
	if !errors.Is(err, ErrInvalidTask) {
		t.Errorf("error does not wrap ErrInvalidTask: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the offending file %s: %v", path, err)
	}
}

func TestLoadSuiteMissingDir(t *testing.T) {
	_, err := LoadSuite(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("LoadSuite(missing dir) succeeded; want an error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing dir error should be detectable with errors.Is(err, fs.ErrNotExist): %v", err)
	}
	if errors.Is(err, ErrInvalidTask) {
		t.Errorf("a missing dir is a precondition failure, not an invalid task: %v", err)
	}
}

func TestLoadSuiteEmpty(t *testing.T) {
	dir := t.TempDir()
	// An empty suite dir plus a starter dir with no task files.
	if err := os.MkdirAll(filepath.Join(dir, "empty-starter"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tasks, err := LoadSuite(dir)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if tasks == nil {
		t.Fatal("LoadSuite returned a nil slice; want an empty non-nil slice")
	}
	if len(tasks) != 0 {
		t.Errorf("suite has %d task(s); want none", len(tasks))
	}
}
