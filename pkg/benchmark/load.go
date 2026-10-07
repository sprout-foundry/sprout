package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadTask reads one task file, unmarshals it, and
// validates it against the task rules and plancontract.Validate. On any
// error the returned task is nil, so a caller never observes an invalid
// task (the pkg/planstore and pkg/starterstore loader contract).
//
// Error contract:
//
//   - Unreadable file (missing file, permission): a wrapped os error; a
//     missing file is detectable with errors.Is(err, fs.ErrNotExist).
//   - Malformed JSON (including an empty file or a non-object document):
//     a wrapped encoding/json error.
//   - Parsed but invalid (a task-field violation, or a plan that fails
//     plancontract.Validate): ErrInvalidTask wrapping the problems; the
//     message names the file and lists every problem.
func LoadTask(path string) (*Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("benchmark: read task file %s: %w", path, err)
	}
	return decodeTask(path, data)
}

// LoadSuite loads every task fixture under dir from the layout
// <dir>/<starter-id>/<task-id>.json, and returns the tasks in
// deterministic order — sorted by starter directory name, then by file
// name — so a suite loads identically on every machine and every run.
//
// Layout rules:
//
//   - A starter directory is any subdirectory of dir; files directly
//     under dir are ignored.
//   - A task file is a .json file directly under a starter directory.
//     Non-.json files and subdirectories inside a starter directory are
//     ignored: a task is exactly one level deep.
//   - The task's starter field MUST equal its starter directory name
//     (the mirror of pkg/starters' embedded-tree/descriptor check): the
//     suite can never point at another starter's tasks. The mismatch is an
//     error naming the file.
//   - The file name conventionally equals the task id plus .json, but a
//     mismatch is NOT an error: the format stays lenient where the spec is
//     silent, and the loader never guesses an id from the file name.
//
// A missing dir is an error (a loader precondition, detectable with
// errors.Is(err, fs.ErrNotExist)), not a not-found sentinel. A dir that
// exists but holds no task files returns an empty (non-nil) slice and a
// nil error: an empty suite is a valid result, and a caller that needs
// tasks can check len.
func LoadSuite(dir string) ([]*Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("benchmark: read suite dir %s: %w", dir, err)
	}
	tasks := make([]*Task, 0)
	// os.ReadDir returns entries sorted by name, so both levels of the
	// walk are deterministic without further sorting.
	for _, dirEntry := range entries {
		if !dirEntry.IsDir() {
			continue
		}
		starterDir := filepath.Join(dir, dirEntry.Name())
		files, err := os.ReadDir(starterDir)
		if err != nil {
			return nil, fmt.Errorf("benchmark: read starter dir %s: %w", dirEntry.Name(), err)
		}
		for _, file := range files {
			if file.IsDir() || !isTaskFileName(file.Name()) {
				continue
			}
			task, err := loadTaskFile(filepath.Join(starterDir, file.Name()), dirEntry.Name())
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// loadTaskFile reads one task file from the suite layout and, in addition
// to the LoadTask rules, enforces the layout invariant: the task's starter
// field must equal the starter directory it was found in.
func loadTaskFile(path, starterDir string) (*Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("benchmark: read task file %s: %w", path, err)
	}
	task, err := decodeTask(path, data)
	if err != nil {
		return nil, err
	}
	if got, want := strings.TrimSpace(task.Starter), starterDir; got != want {
		return nil, fmt.Errorf("%w: %s: starter field %q does not match its directory %q",
			ErrInvalidTask, path, task.Starter, want)
	}
	return task, nil
}

// decodeTask unmarshals one task file and validates it. LoadTask and the
// suite layout share it so every task file is held to the same rules
// whether it is loaded alone or through LoadSuite.
func decodeTask(path string, data []byte) (*Task, error) {
	task := &Task{}
	if err := json.Unmarshal(data, task); err != nil {
		return nil, fmt.Errorf("benchmark: invalid task JSON %s: %w", path, err)
	}
	if verr := validate(task); verr != nil {
		return nil, fmt.Errorf("%w: %s: %s", ErrInvalidTask, path, strings.Join(verr.Problems, "; "))
	}
	return task, nil
}

// isTaskFileName reports whether name is a task file name in the suite
// layout: it ends in .json and has a non-empty stem. A file named exactly
// ".json" is not a task file.
func isTaskFileName(name string) bool {
	stem := strings.TrimSuffix(name, ".json")
	return stem != "" && stem != name
}
