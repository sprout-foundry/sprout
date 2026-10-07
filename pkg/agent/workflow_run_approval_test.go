package agent

import (
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// TestWorkflowRunNeverPromptsOnCaution pins fix (a): a workflow/automate run is
// non-interactive for approval purposes even when launched from a terminal (so
// the console reports itself interactive). A Caution command must proceed per
// the configured risk profile instead of blocking on an approval prompt nobody
// answers.
func TestWorkflowRunNeverPromptsOnCaution(t *testing.T) {
	caution := map[string]interface{}{"command": "rm test.txt"}

	// Precondition: this command is Caution with a prompt and NOT a hard block.
	if r := tools.ClassifyToolCall("shell_command", caution); r.Risk.String() != "CAUTION" || !r.ShouldPrompt || r.IsHardBlock {
		t.Fatalf("precondition: rm test.txt classify = %+v; want CAUTION+prompt, non-hard-block", r)
	}

	t.Run("workflow run with interactive console does not prompt", func(t *testing.T) {
		// Simulate a terminal: force-interactive env + an interactive logger.
		t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
		utils.GetLogger(false)
		defer utils.GetLogger(true)

		a := newIsolatedTestAgent(t)
		defer a.Shutdown()
		// Enable the interactive CLI surface: SkipPrompt must be off for the
		// hook's utils.GetLogger(0) to report interactive, so canPrompt would
		// be true but for the workflow flag. Without this the subtest would be
		// vacuous (canPrompt already false via SkipPrompt).
		if err := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.SkipPrompt = false
			return nil
		}); err != nil {
			t.Fatalf("set SkipPrompt=false: %v", err)
		}
		// A workflow run is being driven from a config file, not a keyboard.
		a.SetWorkflowRun(true)

		// If the fix is absent, the hook would take the CLI prompt path, the
		// closed test stdin would fail closed, and the tool call would come
		// back as "user rejected". Proceeding with no error proves no prompt.
		if err := newPreExecuteHook(a)("shell_command", caution); err != nil {
			t.Fatalf("workflow run must not wait on/deny a Caution approval; got: %v", err)
		}
	})

	t.Run("interactive non-workflow run still consults the prompt", func(t *testing.T) {
		// Same interactive console, but NOT a workflow run: the prompt path is
		// live, so a closed stdin fails closed — demonstrating that the
		// workflow flag, not the console, is what suppresses the prompt.
		t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
		utils.GetLogger(false)
		defer utils.GetLogger(true)

		a := newIsolatedTestAgent(t)
		defer a.Shutdown()
		// Enable the interactive CLI surface: SkipPrompt must be off for
		// utils.GetLogger(0) to report interactive in the hook.
		if err := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.SkipPrompt = false
			return nil
		}); err != nil {
			t.Fatalf("set SkipPrompt=false: %v", err)
		}
		if a.IsWorkflowRun() {
			t.Fatal("agent must not be a workflow run by default")
		}

		err := newPreExecuteHook(a)("shell_command", caution)
		if err == nil {
			t.Fatal("an interactive non-workflow run with no answerable stdin must fail closed on the prompt")
		}
		if !strings.Contains(err.Error(), "rejected") {
			t.Fatalf("expected a prompt-rejection error, got: %v", err)
		}
	})
}

// TestWorkflowRunStillBlocksHardBlocks pins the boundary of fix (a): making a
// workflow run non-interactive for approvals must not weaken the unconditional
// critical tier. A hard block stays a hard block.
func TestWorkflowRunStillBlocksHardBlocks(t *testing.T) {
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
	utils.GetLogger(false)
	defer utils.GetLogger(true)

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	a.SetWorkflowRun(true)

	err := newPreExecuteHook(a)("shell_command", map[string]interface{}{"command": "rm -rf /"})
	if err == nil {
		t.Fatal("hard block must still be rejected in a workflow run")
	}
}

// TestWorkflowRunIsNonInteractive pins the accessor contract every approval
// surface relies on: a workflow run reports non-interactive regardless of the
// console's TTY status.
func TestWorkflowRunIsNonInteractive(t *testing.T) {
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if a.IsNonInteractive() {
		t.Fatal("precondition: force-interactive agent should be interactive")
	}
	a.SetWorkflowRun(true)
	if !a.IsNonInteractive() {
		t.Fatal("a workflow run must report non-interactive even with a forced-interactive console")
	}
	if !a.IsWorkflowRun() {
		t.Fatal("IsWorkflowRun must report true after SetWorkflowRun(true)")
	}
}

// TestNonInteractiveHardBlockRejectsOneCommand pins fix (b): a hard block in a
// non-interactive run rejects that one command with a clear, actionable tool
// error instead of the run-ending "The run will exit" path — and the next tool
// call still runs.
func TestNonInteractiveHardBlockRejectsOneCommand(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	hook := newPreExecuteHook(a)

	// The hard-blocked command is rejected with an actionable error.
	err := hook("shell_command", map[string]interface{}{"command": "rm -rf /"})
	if err == nil {
		t.Fatal("hard-blocked command must return an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "The run will exit") {
		t.Fatalf("hard block must not announce that the run exits; got: %s", msg)
	}
	if !strings.Contains(msg, "rejected") {
		t.Fatalf("hard block error must tell the agent the command was rejected; got: %s", msg)
	}

	// The NEXT tool call still runs: no latched run-terminating state.
	if err := hook("shell_command", map[string]interface{}{"command": "ls -la"}); err != nil {
		t.Fatalf("the next tool call must still run after a hard block; got: %v", err)
	}
	// And another hard block is still a hard block — the tier stays absolute.
	if err := hook("shell_command", map[string]interface{}{"command": "rm -rf /"}); err == nil {
		t.Fatal("hard block must remain absolute on repeat")
	}
}
