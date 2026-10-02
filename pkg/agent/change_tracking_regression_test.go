package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// These run the primary agent from a process CWD (the package dir) that is
// not the workspace root — the daemon/WebUI situation that exposed the
// tracking bugs below.

func TestChangeTracking_RelativePathsResolveAgainstWorkspace(t *testing.T) {
	dir := changeTrackingRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "ALPHA"}),
		tc("w1", "write_file", map[string]any{"path": "b.txt", "content": "BRAVO\n"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("edit"); err != nil {
		t.Fatal(err)
	}

	ops := map[string]string{}
	for _, f := range listChanges(t, ag, nil).Files {
		ops[filepath.Base(f.Path)] = f.Op
	}
	if ops["a.txt"] != "edit" {
		t.Errorf("edit_file of a relative path not tracked: %v", ops)
	}
	if ops["b.txt"] != "write" {
		t.Errorf("overwrite of an existing file recorded as %q, want write (a create would make revert delete it)", ops["b.txt"])
	}

	if _, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"}); err != nil {
		t.Fatal(err)
	}
	if left := gitChanged(t, dir); len(left) != 0 {
		t.Errorf("revert left changes: %v", left)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "bravo\n" {
		t.Errorf("b.txt not restored: %q", b)
	}
}

func TestChangeTracking_SubagentEditsTrackedAndRevertible(t *testing.T) {
	dir := changeTrackingRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("sa", "run_subagent", map[string]any{"persona": "coder", "prompt": "edit"}),
		NewScriptedTextResponse("done"),
	)
	ag.GetSubagentRunner().testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		return NewScriptedClient(
			// A shell mutation as the subagent's FIRST mutating command: it
			// used to become the lazily-primed baseline and vanish.
			tc("se", "shell_command", map[string]any{"command": "printf 'z\\n' > sub_new.txt"}),
			tc("sw", "write_file", map[string]any{"path": "a.txt", "content": "from subagent\n"}),
			NewScriptedTextResponse("subagent finished its edits and verified them."),
		), nil
	}
	if _, err := ag.ProcessQuery("delegate"); err != nil {
		t.Fatal(err)
	}

	sources := map[string]string{}
	for _, f := range listChanges(t, ag, nil).Files {
		sources[filepath.Base(f.Path)] = f.Source
	}
	for _, name := range []string{"a.txt", "sub_new.txt"} {
		if !strings.HasPrefix(sources[name], "subagent:coder") {
			t.Errorf("%s not tracked as a subagent change: %v", name, sources)
		}
	}

	if _, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"}); err != nil {
		t.Fatal(err)
	}
	if left := gitChanged(t, dir); len(left) != 0 {
		t.Errorf("subagent changes not revertible: %v", left)
	}
}

func TestBuildAgentToolFuncs_SubagentHasOwnDispatch(t *testing.T) {
	_, runner := newReviewTestRunner(t)
	sub, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()
	if sub.toolFuncs == nil || sub.toolFuncs.TrackFileWrite == nil || sub.toolFuncs.RevertMyChanges == nil {
		t.Fatal("subagent has no per-agent tool dispatch; its tools fall back to another agent's")
	}
}

func TestChangeTracking_UserFileBetweenTurnsNotCreditedOrDeleted(t *testing.T) {
	dir := changeTrackingRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("s1", "shell_command", map[string]any{"command": "printf 'agent\\n' > agent.txt"}),
		NewScriptedTextResponse("turn 1 done"),
		tc("s2", "shell_command", map[string]any{"command": "touch build.marker && rm build.marker"}),
		NewScriptedTextResponse("turn 2 done"),
	)
	if _, err := ag.ProcessQuery("turn one"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "notes.md", "the user's own notes\n")
	if _, err := ag.ProcessQuery("turn two"); err != nil {
		t.Fatal(err)
	}
	for _, f := range listChanges(t, ag, nil).Files {
		if strings.HasSuffix(f.Path, "notes.md") {
			t.Errorf("user-created notes.md credited to the agent: %+v", f)
		}
	}
	if _, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
		t.Errorf("revert deleted the user's file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.txt")); !os.IsNotExist(err) {
		t.Errorf("revert did not remove the agent's own file: %v", err)
	}
}

func TestChangeTracking_RevertKeepsCommittedWork(t *testing.T) {
	dir := changeTrackingRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "agent edit"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("edit"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-qam", "user commits the agent's edit")
	out, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "agent edit\n" {
		t.Errorf("revert undid committed work: a.txt = %q\n%s", b, out)
	}
}

func TestChangeTracking_ExistingEmptyFileIsNotACreate(t *testing.T) {
	dir := changeTrackingRepo(t)
	writeFile(t, dir, ".gitkeep", "")
	gitIn(t, dir, "add", ".gitkeep")
	gitIn(t, dir, "commit", "-qm", "gitkeep")
	ag, _ := probeAgent(t, dir,
		tc("w1", "write_file", map[string]any{"path": ".gitkeep", "content": "x\n"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("write"); err != nil {
		t.Fatal(err)
	}
	for _, f := range listChanges(t, ag, nil).Files {
		if f.Op == "create" {
			t.Errorf("write to an existing empty file recorded as a create: %+v", f)
		}
	}
	if _, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".gitkeep"))
	if err != nil || len(b) != 0 {
		t.Errorf("revert should restore the empty .gitkeep, got %q, %v", b, err)
	}
}

func TestChangeTracking_RecoverFileRelativePathSessionStart(t *testing.T) {
	dir := changeTrackingRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "one"}),
		tc("e2", "edit_file", map[string]any{"path": "a.txt", "old_str": "one", "new_str": "two"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("two edits"); err != nil {
		t.Fatal(err)
	}
	if _, err := handleRecoverFile(context.Background(), ag, map[string]any{"path": "a.txt", "scope": "session_start"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "alpha\n" {
		t.Errorf("recover_file(session_start) on a relative path did not restore the original: %q", b)
	}
}

func TestListChanges_GroupsPerFileAndDropsNetZero(t *testing.T) {
	dir := changeTrackingRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "one"}),
		tc("e2", "edit_file", map[string]any{"path": "a.txt", "old_str": "one", "new_str": "two"}),
		tc("w1", "write_file", map[string]any{"path": "tmp.txt", "content": "temp\n"}),
		tc("s1", "shell_command", map[string]any{"command": "rm tmp.txt"}),
		tc("e3", "edit_file", map[string]any{"path": "b.txt", "old_str": "bravo", "new_str": "B"}),
		tc("e4", "edit_file", map[string]any{"path": "b.txt", "old_str": "B", "new_str": "bravo"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("edits"); err != nil {
		t.Fatal(err)
	}

	l := listChanges(t, ag, nil)
	if len(l.Files) != 1 || filepath.Base(l.Files[0].Path) != "a.txt" || l.Files[0].Op != "edit" {
		t.Fatalf("want only a.txt (net edit); created-then-deleted and edited-back files have no net change: %+v", l.Files)
	}
	var raw struct {
		Files []struct {
			Changes int `json:"changes"`
		} `json:"files"`
	}
	out, _ := handleListChanges(context.Background(), ag, map[string]any{})
	_ = json.Unmarshal([]byte(out), &raw)
	if raw.Files[0].Changes != 2 {
		t.Errorf("a.txt should fold its 2 changes, got %d", raw.Files[0].Changes)
	}

	perChange := listChanges(t, ag, map[string]any{"group_by": "change"})
	if len(perChange.Files) < 6 {
		t.Errorf("group_by=change should list every recorded change, got %d", len(perChange.Files))
	}
}

func TestChangeTracking_PatchStructuredFileTracked(t *testing.T) {
	dir := changeTrackingRepo(t)
	writeFile(t, dir, "pkg.json", "{\"name\":\"x\"}\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "json")
	ag, _ := probeAgent(t, dir,
		tc("p1", "patch_structured_file", map[string]any{"path": "pkg.json", "patch_ops": []any{map[string]any{"op": "replace", "path": "/name", "value": "y"}}}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("patch"); err != nil {
		t.Fatal(err)
	}
	if l := listChanges(t, ag, nil); len(l.Files) != 1 || filepath.Base(l.Files[0].Path) != "pkg.json" {
		t.Fatalf("patch_structured_file edit not tracked: %+v", l.Files)
	}
	if _, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"}); err != nil {
		t.Fatal(err)
	}
	if left := gitChanged(t, dir); len(left) != 0 {
		t.Errorf("patch_structured_file edit not revertible: %v", left)
	}
}

func TestListChanges_FiltersUseToolNamesAndRelativePaths(t *testing.T) {
	dir := changeTrackingRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	ag, _ := probeAgent(t, dir,
		tc("w1", "write_file", map[string]any{"path": "pkg/auth/x.go", "content": "package auth\n"}),
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "A"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("edits"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".") // a second turn commits the first to history
	for args, want := range map[string]string{
		`{"path_pattern":"pkg/auth/*.go"}`: "x.go",
		`{"path_pattern":"*.go"}`:          "x.go",
		`{"path_pattern":"pkg/"}`:          "x.go",
		`{"tool":"edit_file"}`:             "a.txt",
		`{"tool":"write_file"}`:            "x.go",
	} {
		var m map[string]any
		_ = json.Unmarshal([]byte(args), &m)
		l := listChanges(t, ag, m)
		if len(l.Files) != 1 || filepath.Base(l.Files[0].Path) != want {
			t.Errorf("filter %s: want only %s, got %+v", args, want, l.Files)
		}
	}
}

func TestRevert_RecordsItselfAndIsIdempotent(t *testing.T) {
	dir := changeTrackingRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "edited"}),
		tc("w1", "write_file", map[string]any{"path": "new.txt", "content": "new\n"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("edits"); err != nil {
		t.Fatal(err)
	}
	if _, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"}); err != nil {
		t.Fatal(err)
	}
	if l := listChanges(t, ag, nil); len(l.Files) != 0 {
		t.Errorf("list_changes still reports reverted changes: %+v", l.Files)
	}
	out, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	if err != nil || strings.Contains(out, "stale") || !strings.Contains(out, `"failed": 0`) {
		t.Errorf("a second revert should be a clean no-op: %v\n%s", err, out)
	}
}

func TestRevert_AfterGitCheckoutDiscardsAgentWork(t *testing.T) {
	dir := changeTrackingRepo(t)
	var calls []*ScriptedResponse
	for i := 0; i < 12; i++ {
		name := "d" + string(rune('a'+i)) + ".txt"
		writeFile(t, dir, name, "orig\n")
		calls = append(calls, tc("w"+name, "write_file", map[string]any{"path": name, "content": "agent\n"}))
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "d files")
	calls = append(calls, tc("gc", "shell_command", map[string]any{"command": "git checkout -- ."}), NewScriptedTextResponse("done"))
	ag, _ := probeAgent(t, dir, calls...)
	if _, err := ag.ProcessQuery("write then checkout"); err != nil {
		t.Fatal(err)
	}
	for _, f := range listChanges(t, ag, map[string]any{"group_by": "change"}).Files {
		if f.Op == "bulk" && f.Path == "shell_command" {
			t.Errorf("bulk rollup named with the constant label instead of its command: %+v", f)
		}
	}
	out, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	if err != nil || !strings.Contains(out, `"failed": 0`) {
		t.Errorf("files already back at their original state should not fail the revert: %v\n%.500s", err, out)
	}
	if left := gitChanged(t, dir); len(left) != 0 {
		t.Errorf("workspace not at its pre-session state: %v", left)
	}
}
