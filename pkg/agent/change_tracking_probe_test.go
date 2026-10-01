package agent

// Exploratory harness for list_changes accuracy: drives the primary agent
// through real tool calls with a scripted model and compares what
// list_changes reports against git. Skipped unless
// SPROUT_CHANGE_TRACKING_PROBES is set; scenarios that pass are pinned as
// regression tests in change_tracking_regression_test.go.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/history"
)

type probeFile struct {
	Path        string `json:"path"`
	Op          string `json:"op"`
	Tool        string `json:"tool"`
	Source      string `json:"source"`
	Recoverable bool   `json:"recoverable"`
}

type probeList struct {
	Enabled bool        `json:"enabled"`
	Count   int         `json:"count"`
	Files   []probeFile `json:"files"`
}

func probeRepo(t *testing.T) string {
	t.Helper()
	if os.Getenv("SPROUT_CHANGE_TRACKING_PROBES") == "" {
		t.Skip("exploratory list_changes probes; set SPROUT_CHANGE_TRACKING_PROBES=1 to run")
	}
	return changeTrackingRepo(t)
}

func changeTrackingRepo(t *testing.T) string {
	t.Helper()
	dir := initReviewRepo(t)
	writeFile(t, dir, "a.txt", "alpha\n")
	writeFile(t, dir, "b.txt", "bravo\n")
	writeFile(t, dir, "c.txt", "charlie\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "seed")
	return dir
}

func probeAgent(t *testing.T, dir string, responses ...*ScriptedResponse) (*Agent, *ScriptedClient) {
	t.Helper()
	tmp := t.TempDir()
	oc, or := history.GetPathsForTesting()
	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	t.Cleanup(func() { history.SetPathsForTesting(oc, or) })

	client := NewScriptedClient(responses...)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	_ = mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		cfg.SkipPrompt = true
		cfg.RiskProfile = "permissive"
		return nil
	})
	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ag.Shutdown)
	ag.SetMaxIterations(20)
	ag.SetWorkspaceRoot(dir)
	return ag, client
}

func tc(id, name string, args map[string]any) *ScriptedResponse {
	b, _ := json.Marshal(args)
	return NewScriptedToolCallResponse(id, name, string(b), "")
}

func listChanges(t *testing.T, ag *Agent, args map[string]any) probeList {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	out, err := handleListChanges(context.Background(), ag, args)
	if err != nil {
		t.Fatalf("list_changes: %v", err)
	}
	var l probeList
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	return l
}

func gitChanged(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=all").Output() //nolint:gosec // G204: test temp repo
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		if len(l) < 4 {
			continue
		}
		p := strings.TrimSpace(l[3:])
		if i := strings.Index(p, " -> "); i >= 0 {
			files = append(files, p[:i], p[i+4:])
			continue
		}
		files = append(files, p)
	}
	sort.Strings(files)
	return files
}

// relPaths normalizes reported paths to repo-relative for comparison and
// records the raw forms so path-format inconsistencies are visible.
func relPaths(t *testing.T, dir string, l probeList) (rel []string, raw []string) {
	real, _ := filepath.EvalSymlinks(dir)
	for _, f := range l.Files {
		raw = append(raw, f.Path+" ["+f.Op+"/"+f.Tool+"/"+f.Source+"]")
		p := f.Path
		for _, base := range []string{dir, real} {
			if r, err := filepath.Rel(base, p); err == nil && filepath.IsAbs(p) && !strings.HasPrefix(r, "..") {
				p = r
				break
			}
		}
		rel = append(rel, filepath.ToSlash(p))
	}
	sort.Strings(rel)
	return rel, raw
}

func probeReport(t *testing.T, dir string, ag *Agent) {
	t.Helper()
	l := listChanges(t, ag, nil)
	rel, raw := relPaths(t, dir, l)
	git := gitChanged(t, dir)
	t.Logf("list_changes (%d): %v", l.Count, raw)
	t.Logf("git status   : %v", git)
	seen := map[string]int{}
	for _, p := range rel {
		seen[p]++
	}
	for p, n := range seen {
		if n > 1 {
			t.Errorf("DUPLICATE: %s listed %d times", p, n)
		}
	}
	for _, g := range git {
		if seen[g] == 0 {
			t.Errorf("MISSING from list_changes: %s (changed in git)", g)
		}
	}
	gitSet := map[string]bool{}
	for _, g := range git {
		gitSet[g] = true
	}
	for p := range seen {
		if !gitSet[p] {
			t.Errorf("EXTRA in list_changes (not changed per git): %s", p)
		}
	}
}

func revertAll(t *testing.T, dir string, ag *Agent) {
	t.Helper()
	out, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	t.Logf("revert_my_changes: err=%v out=%.400s", err, out)
	if left := gitChanged(t, dir); len(left) != 0 {
		t.Errorf("REVERT INCOMPLETE: git still shows %v", left)
		for _, f := range left {
			b, _ := os.ReadFile(filepath.Join(dir, f))
			t.Logf("  %s = %q", f, b)
		}
	}
}

func TestProbe_FileTools(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("w1", "write_file", map[string]any{"path": "new.txt", "content": "new\n"}),
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "ALPHA"}),
		tc("e2", "edit_file", map[string]any{"path": "a.txt", "old_str": "ALPHA", "new_str": "ALPHA2"}),
		tc("w2", "write_file", map[string]any{"path": filepath.Join(dir, "b.txt"), "content": "BRAVO\n"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("make edits"); err != nil {
		t.Fatal(err)
	}
	probeReport(t, dir, ag)
	revertAll(t, dir, ag)
}

func TestProbe_ShellMutations(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("s1", "shell_command", map[string]any{"command": "echo more >> a.txt"}),
		tc("s2", "shell_command", map[string]any{"command": "rm b.txt"}),
		tc("s3", "shell_command", map[string]any{"command": "mv c.txt d.txt"}),
		tc("s4", "shell_command", map[string]any{"command": "mkdir -p sub && printf 'x\\n' > sub/n.txt"}),
		tc("s5", "shell_command", map[string]any{"command": "sed -i.bak 's/alpha/ALPHA/' a.txt && rm a.txt.bak"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("shell edits"); err != nil {
		t.Fatal(err)
	}
	probeReport(t, dir, ag)
	revertAll(t, dir, ag)
}

func TestProbe_ShellAfterCd(t *testing.T) {
	dir := probeRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	ag, _ := probeAgent(t, dir,
		tc("s1", "shell_command", map[string]any{"command": "cd sub"}),
		tc("s2", "shell_command", map[string]any{"command": "printf 'y\\n' > inner.txt"}),
		tc("w1", "write_file", map[string]any{"path": "rel.txt", "content": "relative after cd\n"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("cd then edit"); err != nil {
		t.Fatal(err)
	}
	probeReport(t, dir, ag)
	revertAll(t, dir, ag)
}

func TestProbe_MultiTurn(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "turn1"}),
		NewScriptedTextResponse("turn 1 done"),
		tc("e2", "edit_file", map[string]any{"path": "a.txt", "old_str": "turn1", "new_str": "turn2"}),
		tc("w1", "write_file", map[string]any{"path": "b.txt", "content": "turn2 b\n"}),
		NewScriptedTextResponse("turn 2 done"),
	)
	if _, err := ag.ProcessQuery("turn one"); err != nil {
		t.Fatal(err)
	}
	t.Log("--- after turn 1")
	probeReport(t, dir, ag)
	if _, err := ag.ProcessQuery("turn two"); err != nil {
		t.Fatal(err)
	}
	t.Log("--- after turn 2")
	probeReport(t, dir, ag)
	revertAll(t, dir, ag)
}

func TestProbe_UserEditsBetweenTurns(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "agent"}),
		NewScriptedTextResponse("turn 1 done"),
		tc("s1", "shell_command", map[string]any{"command": "echo hi"}),
		NewScriptedTextResponse("turn 2 done"),
	)
	if _, err := ag.ProcessQuery("turn one"); err != nil {
		t.Fatal(err)
	}
	// The user edits a different file and the agent's file outside the tools.
	writeFile(t, dir, "c.txt", "user edit\n")
	writeFile(t, dir, "a.txt", "agent\nuser appended\n")
	if _, err := ag.ProcessQuery("turn two"); err != nil {
		t.Fatal(err)
	}
	l := listChanges(t, ag, nil)
	_, raw := relPaths(t, dir, l)
	t.Logf("list_changes: %v", raw)
	for _, f := range l.Files {
		if strings.HasSuffix(f.Path, "c.txt") {
			t.Errorf("USER EDIT ATTRIBUTED TO AGENT: %s [%s/%s]", f.Path, f.Op, f.Tool)
		}
	}
	out, err := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	t.Logf("revert: err=%v out=%s", err, out)
	a, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	c, _ := os.ReadFile(filepath.Join(dir, "c.txt"))
	t.Logf("after revert: a.txt=%q c.txt=%q", a, c)
	if string(c) != "user edit\n" {
		t.Errorf("REVERT CLOBBERED USER FILE c.txt: %q", c)
	}
	if strings.Contains(string(a), "agent") && !strings.Contains(string(a), "user appended") {
		t.Errorf("unexpected a.txt after revert: %q", a)
	}
	if !strings.Contains(string(a), "user appended") {
		t.Errorf("REVERT DISCARDED THE USER'S EDIT to a.txt without warning: %q (output: %.300s)", a, out)
	}
}

func TestProbe_SubagentEdits(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("sa", "run_subagent", map[string]any{"persona": "coder", "prompt": "edit a.txt"}),
		NewScriptedTextResponse("done"),
	)
	ag.GetSubagentRunner().testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		return NewScriptedClient(
			tc("sw", "write_file", map[string]any{"path": "a.txt", "content": "from subagent\n"}),
			tc("se", "shell_command", map[string]any{"command": "printf 'z\\n' > sub_new.txt"}),
			NewScriptedTextResponse("subagent finished its edits and verified them."),
		), nil
	}
	if _, err := ag.ProcessQuery("delegate"); err != nil {
		t.Fatal(err)
	}
	probeReport(t, dir, ag)
	revertAll(t, dir, ag)
}

func TestProbe_CreateThenDelete(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("w1", "write_file", map[string]any{"path": "tmp.txt", "content": "temp\n"}),
		tc("s1", "shell_command", map[string]any{"command": "rm tmp.txt"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("create and delete"); err != nil {
		t.Fatal(err)
	}
	probeReport(t, dir, ag)
	revertAll(t, dir, ag)
}

func TestProbe_EditThenShellDelete(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "edited"}),
		tc("s1", "shell_command", map[string]any{"command": "rm a.txt"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("edit then delete"); err != nil {
		t.Fatal(err)
	}
	probeReport(t, dir, ag)
	revertAll(t, dir, ag)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "alpha\n" {
		t.Errorf("a.txt not restored to its original: %q", b)
	}
}

func TestProbe_RecoverSessionStart(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "one"}),
		tc("e2", "edit_file", map[string]any{"path": "a.txt", "old_str": "one", "new_str": "two"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("two edits"); err != nil {
		t.Fatal(err)
	}
	out, err := handleRecoverFile(context.Background(), ag, map[string]any{"path": "a.txt", "scope": "session_start"})
	t.Logf("recover_file session_start: err=%v out=%.300s", err, out)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "alpha\n" {
		t.Errorf("recover_file(session_start) did not restore the original: %q", b)
	}
	out, err = handleRecoverFile(context.Background(), ag, map[string]any{"path": "a.txt"})
	t.Logf("recover_file latest: err=%v out=%.300s", err, out)
}

func TestProbe_UserFileCreatedBetweenTurns(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("s1", "shell_command", map[string]any{"command": "printf 'agent\\n' > agent.txt"}),
		NewScriptedTextResponse("turn 1 done"),
		tc("s2", "shell_command", map[string]any{"command": "touch build.marker && rm build.marker"}),
		NewScriptedTextResponse("turn 2 done"),
	)
	if _, err := ag.ProcessQuery("turn one"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "notes.md", "the user's own notes\n") // user creates a file outside the agent
	if _, err := ag.ProcessQuery("turn two"); err != nil {
		t.Fatal(err)
	}
	l := listChanges(t, ag, nil)
	_, raw := relPaths(t, dir, l)
	t.Logf("list_changes: %v", raw)
	for _, f := range l.Files {
		if strings.HasSuffix(f.Path, "notes.md") {
			t.Errorf("USER-CREATED notes.md credited to the agent: [%s/%s]", f.Op, f.Tool)
		}
	}
	out, _ := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
		t.Errorf("DATA LOSS: revert deleted the user's notes.md (%v)\n%s", err, out)
	}
}

func TestProbe_RevertAfterUserCommit(t *testing.T) {
	dir := probeRepo(t)
	ag, _ := probeAgent(t, dir,
		tc("e1", "edit_file", map[string]any{"path": "a.txt", "old_str": "alpha", "new_str": "agent edit"}),
		NewScriptedTextResponse("done"),
	)
	if _, err := ag.ProcessQuery("edit"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-qam", "user commits the agent's edit")
	out, _ := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	t.Logf("revert after commit: %.500s", out)
	b, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	t.Logf("a.txt after revert = %q; git status %v", b, gitChanged(t, dir))
	if string(b) != "agent edit\n" {
		t.Errorf("REVERT UNDID COMMITTED WORK: a.txt = %q", b)
	}
}

func TestProbe_WriteExistingEmptyFile(t *testing.T) {
	dir := probeRepo(t)
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
	l := listChanges(t, ag, nil)
	_, raw := relPaths(t, dir, l)
	t.Logf("list_changes: %v", raw)
	out, _ := handleRevertMyChanges(context.Background(), ag, map[string]any{"scope": "all"})
	if _, err := os.Stat(filepath.Join(dir, ".gitkeep")); err != nil {
		t.Errorf("DATA LOSS: revert deleted the pre-existing empty .gitkeep (%v)\n%.300s", err, out)
	}
}
