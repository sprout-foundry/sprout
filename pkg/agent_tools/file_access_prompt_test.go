package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type fakeFSPrompter struct {
	called    int
	approve   bool
	lastTool  string
	lastPath  string
	lastMode  string
	returnCtx context.Context
}

func (f *fakeFSPrompter) PromptFileAccess(ctx context.Context, toolName, filePath, resolvedPath, mode string) (context.Context, bool) {
	f.called++
	f.lastTool = toolName
	f.lastPath = filePath
	f.lastMode = mode
	if f.approve {
		return filesystem.WithSecurityBypass(ctx), true
	}
	return ctx, false
}

type fakeClassifier struct {
	verdict string
	calls   int
}

func (f *fakeClassifier) ClassifyFileAccess(ctx context.Context, filePath, resolvedPath, mode string) string {
	f.calls++
	return f.verdict
}

func (f *fakeClassifier) IsFolderSessionAllowed(absPath string) bool { return false }

// outsideWorkspaceTarget creates a file guaranteed outside both the
// process workspace (repo cwd) and /tmp: under the user's HOME, in a
// uniquely named dir the classifier does not allowlist.
func outsideWorkspaceTarget(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	dir := filepath.Join(home, ".sprout-test-outside")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skipf("cannot create outside fixture dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	outside := filepath.Join(dir, "marker.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Skipf("cannot create outside fixture: %v", err)
	}
	return outside
}

func TestPromptForOffWorkspacePath(t *testing.T) {
	ctx := context.Background()
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{FileAccessPrompter: approved}

	got, ok := promptForOffWorkspacePath(ctx, env, "read_file", "/etc/hosts", "/etc/hosts", "read")
	if !ok || got == nil {
		t.Fatalf("approved prompt: ok=%v ctx=%v", ok, got)
	}
	if approved.lastTool != "read_file" || approved.lastMode != "read" {
		t.Fatalf("prompter received tool=%s mode=%s", approved.lastTool, approved.lastMode)
	}

	denied := &fakeFSPrompter{}
	env2 := ToolEnv{FileAccessPrompter: denied}
	if _, ok := promptForOffWorkspacePath(ctx, env2, "write_file", "/x", "/x", "write"); ok {
		t.Fatal("denied prompt must return false")
	}

	env3 := ToolEnv{}
	if _, ok := promptForOffWorkspacePath(ctx, env3, "read_file", "/x", "/x", "read"); ok {
		t.Fatal("nil prompter must return false without prompting")
	}
}

func TestReadFileHandlerPromptFlow(t *testing.T) {
	outside := outsideWorkspaceTarget(t)

	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
		WorkspaceRoot:        t.TempDir(),
	}
	var h ToolHandler = &readFileHandler{}
	args := map[string]interface{}{"path": outside}
	res, err := h.Execute(context.Background(), env, args)
	if err != nil || res.IsError {
		t.Fatalf("approved off-workspace read failed: err=%v output=%.120s", err, res.Output)
	}
	if approved.called != 1 {
		t.Fatalf("expected prompter called once, got %d", approved.called)
	}
	if !strings.Contains(res.Output, "outside") {
		t.Fatalf("unexpected output: %.120s", res.Output)
	}

	denied := &fakeFSPrompter{}
	env2 := ToolEnv{
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   denied,
		WorkspaceRoot:        t.TempDir(),
	}
	res2, err2 := h.Execute(context.Background(), env2, args)
	if err2 == nil && !res2.IsError {
		t.Fatal("denied off-workspace read must error")
	}
	if !strings.Contains(err2.Error(), "outside working directory") && !strings.Contains(res2.Output, "outside working directory") {
		t.Fatalf("expected off-workspace error, got err=%v out=%.120s", err2, res2.Output)
	}

	env3 := ToolEnv{
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		WorkspaceRoot:        t.TempDir(),
	}
	res3, err3 := h.Execute(context.Background(), env3, args)
	if err3 == nil && !res3.IsError {
		t.Fatal("nil-prompter off-workspace read must keep raw error behavior")
	}
}

func TestWriteFileHandlerPromptFlow(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
		WorkspaceRoot:        t.TempDir(),
	}
	var h ToolHandler = &writeFileHandler{}
	target := strings.TrimSuffix(outside, ".txt") + "-w.txt"
	args := map[string]interface{}{"path": target, "content": "written-by-test"}
	res, err := h.Execute(context.Background(), env, args)
	if err != nil || res.IsError {
		t.Fatalf("approved off-workspace write failed: err=%v out=%.120s", err, res.Output)
	}
	if approved.lastMode != "write" {
		t.Fatalf("expected write mode, got %s", approved.lastMode)
	}
	if b, rerr := os.ReadFile(target); rerr != nil || string(b) != "written-by-test" {
		t.Fatalf("write did not land: read err=%v content=%q", rerr, string(b))
	}
	os.Remove(target)
}
