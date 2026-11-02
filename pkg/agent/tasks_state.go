package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// Task records are persisted so a restart can report what happened to a task
// instead of losing it. The worktree and branch outlive the process, so the
// record's job after a restart is to say "this was running when sprout
// stopped" (and where its work is), never to claim it is still running.
//
// The file is per workspace: two checkouts of the same repo must not share a
// task list.

// taskStateFile is the on-disk shape. Versioned so a future change can
// migrate rather than guess.
type taskStateFile struct {
	Version int             `json:"version"`
	Tasks   []persistedTask `json:"tasks"`
}

const taskStateVersion = 1

// persistedTask is a task record as written to disk. It deliberately omits
// the live agent and worktree handles: neither survives a restart.
type persistedTask struct {
	ID        string    `json:"id"`
	Branch    string    `json:"branch"`
	Path      string    `json:"path"`
	Base      string    `json:"base,omitempty"`
	Prompt    string    `json:"prompt"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished,omitempty"`
	Status    string    `json:"status"`
	Commit    string    `json:"commit,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Workspace string    `json:"workspace"`
}

// taskStatePath returns the task-state file for a workspace.
func taskStatePath(workspaceRoot string) (string, error) {
	state, err := envutil.StateDir()
	if err != nil {
		return "", err
	}
	// One file per workspace, keyed by a stable digest of its path so a
	// workspace under a path with separators in its name cannot escape the
	// directory.
	return filepath.Join(state, "tasks", workspaceKey(workspaceRoot)+".json"), nil
}

// workspaceKey derives a filesystem-safe key from a workspace path.
func workspaceKey(workspaceRoot string) string {
	clean := filepath.Clean(workspaceRoot)
	var b strings.Builder
	for _, r := range clean {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	key := b.String()
	if key == "" {
		key = "workspace"
	}
	// A long path would exceed the filename limit; keep a stable tail.
	if len(key) > 120 {
		key = key[len(key)-120:]
	}
	return key
}

// save writes every record for one workspace. Best-effort: a failed save
// must never fail the task that triggered it.
func (r *taskRegistry) save(workspaceRoot string) {
	path, err := taskStatePath(workspaceRoot)
	if err != nil {
		return
	}
	records := r.recordsFor(workspaceRoot)
	if len(records) == 0 {
		_ = os.Remove(path)
		return
	}
	data, err := json.MarshalIndent(taskStateFile{Version: taskStateVersion, Tasks: records}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// recordsFor snapshots one workspace's records under the lock.
func (r *taskRegistry) recordsFor(workspaceRoot string) []persistedTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]persistedTask, 0, len(r.order))
	for _, id := range r.order {
		rec := r.byID[id]
		if rec == nil || rec.Workspace != workspaceRoot {
			continue
		}
		out = append(out, persistedTask{
			ID:        rec.ID,
			Branch:    rec.Branch,
			Path:      rec.Path,
			Base:      rec.Base,
			Prompt:    rec.Prompt,
			Started:   rec.Started,
			Finished:  rec.Finished,
			Status:    rec.Status,
			Commit:    rec.Commit,
			Summary:   rec.Summary,
			Workspace: rec.Workspace,
		})
	}
	return out
}

// loadTasks restores a workspace's records from disk, marking anything that
// was running when the process stopped as "interrupted". A task cannot
// outlive its process, so claiming it is still running would be a lie the
// concurrency limit and /task merge would act on.
func (r *taskRegistry) loadTasks(workspaceRoot string) {
	path, err := taskStatePath(workspaceRoot)
	if err != nil {
		return
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed filename under the state dir
	if err != nil {
		return
	}
	var file taskStateFile
	if err := json.Unmarshal(data, &file); err != nil {
		return // a corrupt file is dropped, not fatal
	}
	if file.Version != taskStateVersion {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range file.Tasks {
		if p.ID == "" || r.byID[p.ID] != nil {
			continue // already live in this process
		}
		status := p.Status
		finished := p.Finished
		if status == "running" {
			status = "interrupted"
			if finished.IsZero() {
				finished = p.Started
			}
		}
		rec := &taskRecord{
			ID:        p.ID,
			Branch:    p.Branch,
			Path:      p.Path,
			Base:      p.Base,
			Prompt:    p.Prompt,
			Started:   p.Started,
			Finished:  finished,
			Status:    status,
			Commit:    p.Commit,
			Summary:   p.Summary,
			Workspace: p.Workspace,
			orderIdx:  -1,
		}
		r.order = append(r.order, rec.ID)
		r.byID[rec.ID] = rec
	}
	sort.SliceStable(r.order, func(i, j int) bool {
		a, b := r.byID[r.order[i]], r.byID[r.order[j]]
		if a == nil || b == nil {
			return false
		}
		return a.Started.Before(b.Started)
	})
	r.pruneLocked()
}

// loadTasksOnce restores a workspace's tasks the first time this process
// looks at them, so /tasks after a restart reports what happened.
func (a *Agent) loadTasksOnce() {
	if a == nil {
		return
	}
	workspace := a.currentWorkspaceRoot()
	a.taskLoadMu.Lock()
	defer a.taskLoadMu.Unlock()
	if a.taskLoaded[workspace] {
		return
	}
	if a.taskLoaded == nil {
		a.taskLoaded = map[string]bool{}
	}
	a.taskLoaded[workspace] = true
	processTasks.loadTasks(workspace)
}

// taskInterruptedNote tells the user what a restarted task's record means.
func taskInterruptedNote(t TaskInfo) string {
	if t.Status != "interrupted" {
		return ""
	}
	return fmt.Sprintf("Task %s was running when sprout stopped; its worktree is at %s (branch %s). "+
		"Any uncommitted work there is still on disk.", t.ID, t.Worktree, t.Branch)
}
