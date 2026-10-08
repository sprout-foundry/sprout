package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// isolationTestHome points the user home (where isolated worktrees live) at a
// temp dir so tests never touch ~/.sprout.
func isolationTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(home, "state"))
	return home
}

func writeFileToolCall(path, content string) api.ToolCall {
	tc := api.ToolCall{ID: "call_write", Type: "function"}
	tc.Function.Name = "write_file"
	args, _ := json.Marshal(map[string]string{"path": path, "content": content})
	tc.Function.Arguments = string(args)
	return tc
}

// scriptWriter makes each subagent write path=content, then finish. hold, if
// non-nil, is closed by the test to let the write proceed.
func scriptWriter(parent *Agent, path, content string, finalErr error, hold chan struct{}) {
	parent.GetSubagentRunner().testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		write := NewScriptedResponseBuilder().ToolCall(writeFileToolCall(path, content))
		if hold != nil {
			<-hold
		}
		final := NewScriptedResponseBuilder().Content("Wrote the change; it is complete and verified for this test.")
		if finalErr != nil {
			final = NewScriptedResponseBuilder().Error(finalErr)
		}
		return NewScriptedClient(write.Build(), final.Build()), nil
	}
}

func isolatedSpec(t *testing.T, parent *Agent, dir string) *subagentLaunchSpec {
	t.Helper()
	spec, err := prepareSubagentLaunch(context.Background(), parent, map[string]any{"persona": "coder", "prompt": "update main.go"})
	if err != nil {
		t.Fatalf("prepareSubagentLaunch: %v", err)
	}
	spec.subagentWorkspaceRoot = dir
	return spec
}

// waitSubagentWorktree polls for the isolated worktree's SEED to complete:
// createIsolatedWorkspace creates the worktree (at HEAD) first and seeds it
// afterwards — the seed's `git diff HEAD` runs in the PARENT workspace, so
// anything that mutates the workspace between `worktree add` and the seed's
// baseline commit gets baked into the baseline (that raced the old fixed
// sleep and turned the conflict test into a clean-apply). The deterministic
// "seed done" signal is the baseline commit itself.
func waitSubagentWorktree(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(home, "state", "worktrees")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				wt := filepath.Join(root, e.Name())
				out, err := runGitIn(context.Background(), wt, "log", "-1", "--format=%s")
				if err == nil && strings.TrimSpace(out) == "sprout isolation baseline" {
					return wt
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("seeded isolated worktree (baseline commit) never appeared under %s", root)
	return ""
}

func worktreesLeft(t *testing.T, home string) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(home, "state", "worktrees"))
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}

func TestCreateIsolatedWorkspace_SeedsUncommittedState(t *testing.T) {
	isolationTestHome(t)
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\n// uncommitted edit\nfunc main() {}\n")
	writeFile(t, dir, "new.go", "package main\n")

	ws, err := createIsolatedWorkspace(context.Background(), dir, "t1")
	if err != nil {
		t.Fatalf("createIsolatedWorkspace: %v", err)
	}
	defer ws.remove(context.Background())

	got, _ := os.ReadFile(filepath.Join(ws.dir(), "main.go"))
	if !strings.Contains(string(got), "uncommitted edit") {
		t.Error("uncommitted edit not seeded")
	}
	if _, err := os.Stat(filepath.Join(ws.dir(), "new.go")); err != nil {
		t.Error("untracked file not seeded")
	}
	if patch, err := ws.collectPatch(context.Background()); err != nil || strings.TrimSpace(patch) != "" {
		t.Errorf("fresh worktree should have no changes vs baseline: %v %q", err, patch)
	}

	if _, err := createIsolatedWorkspace(context.Background(), t.TempDir(), "t2"); err == nil {
		t.Error("non-git directory accepted")
	}
}

func TestRunIsolatedSubagent_AppliesChangesBack(t *testing.T) {
	home := isolationTestHome(t)
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	writeFile(t, dir, "notes.txt", "keep me\n")
	parent.workspaceRoot = dir
	scriptWriter(parent, "main.go", "package main\n\nfunc main() { println(\"from subagent\") }\n", nil, nil)

	result := runIsolatedSubagent(context.Background(), parent, isolatedSpec(t, parent, dir), "iso-1", true)

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(got), "from subagent") {
		t.Fatalf("change not applied to the workspace:\n%s\n%s", got, result.Output)
	}
	if !strings.HasPrefix(result.Output, "[isolated run] Changes applied") {
		t.Errorf("outcome not reported: %q", result.Output)
	}
	if len(result.FileChanges) == 0 {
		t.Fatal("subagent's change not tracked")
	}
	for _, c := range result.FileChanges {
		if c.FilePath != filepath.Join(dir, "main.go") {
			t.Errorf("tracked change not remapped to the workspace in its own path form: %s (want %s)", c.FilePath, filepath.Join(dir, "main.go"))
		}
	}
	if left := worktreesLeft(t, home); len(left) != 0 {
		t.Errorf("worktree not removed after a clean apply: %v", left)
	}
}

func TestRunIsolatedSubagent_ConflictLeavesWorkspaceUntouched(t *testing.T) {
	home := isolationTestHome(t)
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	parent.workspaceRoot = dir
	hold := make(chan struct{})
	scriptWriter(parent, "main.go", "package main\n\nfunc main() { println(\"subagent\") }\n", nil, hold)

	done := make(chan *SubagentResult, 1)
	go func() {
		done <- runIsolatedSubagent(context.Background(), parent, isolatedSpec(t, parent, dir), "iso-2", true)
	}()

	// The primary edits the same lines while the subagent works. Wait until
	// the subagent's isolated worktree EXISTS (poll — a fixed sleep raced on
	// slower macOS runners: when the primary's write landed before the
	// worktree was seeded, the seed carried the primary's version, the
	// subagent patch applied cleanly on top of it, and the apply-back
	// overwrote the workspace — the exact "conflict modified the workspace"
	// failure). The scripted factory blocks on `hold` until after the write,
	// so existence of the worktree (seeded from the pre-write workspace) is
	// the correct sequencing point.
	waitSubagentWorktree(t, home)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"primary\") }\n")
	if got, _ := os.ReadFile(filepath.Join(dir, "main.go")); !strings.Contains(string(got), "primary") {
		t.Fatalf("primary write did not land before release: %s", got)
	}
	close(hold)

	result := <-done
	got, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(got), "primary") || strings.Contains(string(got), "subagent") {
		t.Fatalf("conflicting patch modified the workspace:\n%s\n--- result.Output:\n%s", got, result.Output)
	}
	if !strings.Contains(result.Output, "no longer apply cleanly") || len(result.FileChanges) != 0 {
		t.Errorf("conflict not reported: %q", result.Output)
	}
	if left := worktreesLeft(t, home); len(left) != 1 {
		t.Errorf("worktree should be kept for manual merge, got %v", left)
	}
	patches, _ := filepath.Glob(filepath.Join(home, "state", "worktrees", "*.patch"))
	if len(patches) != 1 {
		t.Errorf("patch not kept: %v", patches)
	}
}

func TestFinishIsolatedRun_UncleanRunsAreNotApplied(t *testing.T) {
	for name, result := range map[string]*SubagentResult{
		"error":           {Error: errors.New("max iterations reached")},
		"budget exceeded": {BudgetExceeded: true},
		"truncated":       {Truncated: true},
		"cancelled":       {Cancelled: true},
	} {
		t.Run(name, func(t *testing.T) {
			home := isolationTestHome(t)
			dir := initReviewRepo(t)
			ws, err := createIsolatedWorkspace(context.Background(), dir, "u")
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, ws.dir(), "main.go", "package main\n// half done\n")

			note := finishIsolatedRun(context.Background(), ws, result)

			got, _ := os.ReadFile(filepath.Join(dir, "main.go"))
			if strings.Contains(string(got), "half done") {
				t.Fatalf("partial changes applied: %s", note)
			}
			if result.Cancelled {
				if len(worktreesLeft(t, home)) != 0 || !strings.Contains(note, "discarded") {
					t.Errorf("cancelled work should be discarded: %s", note)
				}
				return
			}
			if !strings.Contains(note, "NOT applied") || len(worktreesLeft(t, home)) != 1 {
				t.Errorf("unclean run should keep its work for review: %s", note)
			}
		})
	}
}

func TestBackgroundWriter_RunsIsolated(t *testing.T) {
	isolationTestHome(t)
	a, _ := newBackgroundTestAgent(t, "", 0)
	dir := initReviewRepo(t)
	a.workspaceRoot = dir
	scriptWriter(a, "main.go", "package main\n\nfunc main() { println(\"background coder\") }\n", nil, nil)

	bg, isolate, err := resolveSubagentBackground(a, "coder", map[string]any{"background": true})
	if err != nil || !bg || !isolate {
		t.Fatalf("background coder should run isolated: bg=%v isolate=%v err=%v", bg, isolate, err)
	}

	out, err := handleRunSubagent(context.Background(), a, map[string]any{"persona": "coder", "prompt": "update main.go", "background": true})
	if err != nil {
		t.Fatal(err)
	}
	task := waitBackgroundDone(t, a, startedTaskID(t, out))
	if task.status != "completed" || !strings.Contains(task.result, "Changes applied") {
		t.Fatalf("background isolated run: %s\n%s", task.status, task.result)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(got), "background coder") {
		t.Error("background coder's change not applied")
	}

	if _, _, err := resolveSubagentBackground(a, "coder", map[string]any{"isolation": "container"}); err == nil {
		t.Error("unknown isolation accepted")
	}
	a.contextProfile = configuration.ContextProfile{Mode: configuration.ContextModeLowContext}
	if bg, iso, _ := resolveSubagentBackground(a, "coder", map[string]any{"background": true}); bg || iso {
		t.Error("without background support a coder must run normally, not isolated")
	}
}
