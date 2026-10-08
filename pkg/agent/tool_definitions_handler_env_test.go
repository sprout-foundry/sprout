package agent

import (
	"context"
	"path/filepath"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// buildToolEnvFromAgent feeds the seed tool registry — the live executor
// for all agent tool calls (CLI and WebUI alike). SP-127 M4 wired the
// off-workspace classifier/prompter only into ExecuteTool, which turned
// out to be dead code: "prompt" verdicts then fell through with a nil
// prompter and failed with raw errors instead of opening the approval
// dialog. These tests pin both fields on the live path.
func TestBuildToolEnvFromAgentWiresFileAccessInterfaces(t *testing.T) {
	a := newTestAgent(t)

	env := buildToolEnvFromAgent(a)

	if env.FileAccessClassifier == nil {
		t.Fatal("FileAccessClassifier not wired: off-workspace precheck falls through to raw errors (no allow/prompt/deny verdicts)")
	}
	if env.FileAccessPrompter == nil {
		t.Fatal("FileAccessPrompter not wired: off-workspace 'prompt' verdicts never reach the WebUI dialog or CLI prompt")
	}
}

// The wired classifier must actually answer: an in-workspace absolute
// path (the form PrecheckFileAccess passes after SafeResolvePath)
// allows, and a clearly external path does not silently allow.
func TestBuildToolEnvClassifierAnswers(t *testing.T) {
	a := newTestAgent(t)
	env := buildToolEnvFromAgent(a)
	classifier := env.FileAccessClassifier

	inside := filepath.Join(a.GetWorkspaceRoot(), "inside.go")
	if verdict := classifier.ClassifyFileAccess(context.Background(), inside, inside, "read"); verdict != "allow" {
		t.Fatalf("in-workspace absolute path should be 'allow', got %q", verdict)
	}

	outside := filepath.Join(detectHomeDir(), "..", "..", "etc", "passwd")
	if verdict := classifier.ClassifyFileAccess(context.Background(), outside, outside, "read"); verdict == "allow" {
		t.Fatal("external sensitive path must not silently allow")
	}
}

func TestBuildToolEnvNilAgent(t *testing.T) {
	env := buildToolEnvFromAgent(nil)
	if env.FileAccessClassifier != nil || env.FileAccessPrompter != nil {
		t.Fatal("nil agent must not wire file access interfaces")
	}
}

// The read_file repeat guard rides on ToolEnv.ReadCallGuard, so the live
// env builder must wire it. Without this the third identical read_file call
// would return content again (the incident this guards against).
func TestBuildToolEnvWiresReadCallGuard(t *testing.T) {
	a := newTestAgent(t)
	env := buildToolEnvFromAgent(a)
	if env.ReadCallGuard == nil {
		t.Fatal("ReadCallGuard not wired: repeated read_file calls would never be short-circuited")
	}
	if got := env.ReadCallGuard.ObserveRead("x.md", 1, 10); got != 1 {
		t.Fatalf("first observe = %d, want 1", got)
	}
	if got := env.ReadCallGuard.ObserveRead("x.md", 1, 10); got != 2 {
		t.Fatalf("second observe = %d, want 2", got)
	}
	a.ResetFileReadsForNewTurn()
	if got := env.ReadCallGuard.ObserveRead("x.md", 1, 10); got != 1 {
		t.Fatalf("after turn reset observe = %d, want 1", got)
	}
}

// The handler-level Execute path (ExecuteTool) builds its own env, so the
// ReadCallGuard must be wired there too. The agent implements the interface
// directly, so the wiring is a compile-time contract.
func TestExecuteToolEnvWiresReadCallGuard(t *testing.T) {
	a := newTestAgent(t)
	var guard tools.ReadCallGuard = a
	if got := guard.ObserveRead("y.md", 0, 0); got != 1 {
		t.Fatalf("first observe = %d, want 1", got)
	}
}
