package agent

import (
	"context"
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
