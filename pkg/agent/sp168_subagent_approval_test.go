package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ============================================================================
// SP-168 — subagent approval surface
//
// Regression pins for the subagent-approval-surface work:
//   - a subagent's approval dialog is ANSWERABLE through the parent's
//     shared ApprovalManager (the original defect: an unanswerable dialog
//     blocked the subagent until the approval timeout);
//   - a subagent in a workflow/automate run never blocks on a surface —
//     it fails fast so the subagent's model can take a different path;
//   - a subagent may prompt on the parent's terminal only while a single
//     subagent is active (parallel subagents would interleave prompts),
//     and the prompt is labeled so the user knows whose command it is.
// ============================================================================

// makeSubagentForTests builds a second isolated agent wired the way
// createSubagent wires a real subagent (SP-168 increment 1): it shares the
// parent's ApprovalManager, event bus, and live WebUI-client callback, and
// runs at depth 1.
func makeSubagentForTests(t *testing.T, parent *Agent) *Agent {
	t.Helper()
	sub := newIsolatedTestAgent(t)
	t.Cleanup(func() { sub.Shutdown() })
	sub.SetWorkspaceRoot(parent.GetWorkspaceRoot())
	sub.state.SetActivePersona(personas.IDOrchestrator)
	sub.subagentDepth = 1
	// Mirror the createSubagent inheritance (subagent_creation.go).
	if pm := parent.GetSecurityApprovalMgr(); pm != nil {
		sub.security.SetApprovalMgr(pm)
	}
	sub.SetEventBus(parent.GetEventBus())
	if fn := parent.WebUIClientCallback(); fn != nil {
		sub.SetHasActiveWebUIClients(fn)
	}
	return sub
}

// TestSP168_SubagentWebUIDialogAnswerable is the regression test for the
// original hang: a subagent's approval dialog, published on the shared event
// bus, MUST resolve when the answer arrives through the SHARED
// ApprovalManager — the same instance the parent (and any responder) uses.
// Before SP-168 the subagent owned a fresh manager whose pending map no
// response path ever resolved, so the dialog sat unanswered until the
// approval timeout and the subagent stalled its whole run.
func TestSP168_SubagentWebUIDialogAnswerable(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()
	parent.SetWorkspaceRoot(t.TempDir())
	parent.state.SetActivePersona(personas.IDOrchestrator)

	// Browser attached: the WebUI IS the interactive surface.
	parent.SetHasActiveWebUIClients(func() bool { return true })
	eventBus := events.NewEventBus()
	parent.SetEventBus(eventBus)
	mgr := security.NewApprovalManager()
	mgr.SetApprovalTimeout(2 * time.Second)
	parent.security.SetApprovalMgr(mgr)

	sub := makeSubagentForTests(t, parent)
	if err := sub.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.AutoRiskJudge = true
		return nil
	}); err != nil {
		t.Fatalf("config update failed: %v", err)
	}

	const command = "rm somefile.txt"
	// A "review" verdict from the risk judge keeps the human surface — the
	// dialog below is the interactive path, not the judge's auto-approve.
	for _, ag := range []*Agent{parent, sub} {
		ag.GetSecurityAnalysisCache().Set(ChainCacheKey(command), &SecurityAnalysis{
			Summary:        "removes a build artifact",
			Recommendation: "review",
		})
	}

	// A responder stands in for the browser tab: it approves the request
	// through the SHARED manager — the path that was broken before SP-168.
	responder := eventBus.Subscribe("sp168-responder")
	defer eventBus.Unsubscribe("sp168-responder")
	go func() {
		for ev := range responder {
			if ev.Type != events.EventTypeSecurityApprovalRequest {
				continue
			}
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				continue
			}
			reqID, _ := data["request_id"].(string)
			if reqID == "" {
				continue
			}
			for !mgr.RespondToApproval(reqID, true) {
				time.Sleep(time.Millisecond)
			}
		}
	}()

	assessment := sub.ResolveToolRisk("shell_command", map[string]interface{}{"command": command})

	start := time.Now()
	decision, err := sub.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": command})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("subagent approval through the shared manager failed: %v", err)
	}
	if !decision.Approved {
		t.Error("expected the subagent's request to be approved by the shared manager's responder")
	}
	if decision.Surface != "webui" {
		t.Errorf("expected surface 'webui' (the shared dialog), got %q", decision.Surface)
	}
	// The answer arrived promptly — NOT after the manager's 2s timeout.
	if elapsed > time.Second {
		t.Errorf("approval took %s — it should resolve as soon as the shared manager's responder answers, not wait out the timeout", elapsed)
	}
}

// TestSP168_SubagentWorkflowRunFailsFast pins the fail-fast contract for
// autonomous runs: a subagent inside a workflow/automate run must never
// block on an approval surface. With a "review" risk-judge verdict and no
// human to answer, the request is DENIED immediately with actionable
// guidance instead of stalling until the approval timeout.
func TestSP168_SubagentWorkflowRunFailsFast(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()
	parent.SetWorkspaceRoot(t.TempDir())
	parent.state.SetActivePersona(personas.IDOrchestrator)

	parent.SetHasActiveWebUIClients(func() bool { return true })
	eventBus := events.NewEventBus()
	parent.SetEventBus(eventBus)
	mgr := security.NewApprovalManager()
	mgr.SetApprovalTimeout(2 * time.Second)
	parent.security.SetApprovalMgr(mgr)

	sub := makeSubagentForTests(t, parent)
	sub.SetWorkflowRun(true)
	if err := sub.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.AutoRiskJudge = true
		return nil
	}); err != nil {
		t.Fatalf("config update failed: %v", err)
	}

	const command = "rm somefile.txt"
	// A "review" verdict is the interesting case: it WOULD prompt a human,
	// but a workflow run has no human — it must fail fast.
	sub.GetSecurityAnalysisCache().Set(ChainCacheKey(command), &SecurityAnalysis{
		Summary:        "removes a project file",
		Recommendation: "review",
	})

	assessment := sub.ResolveToolRisk("shell_command", map[string]interface{}{"command": command})
	args := map[string]interface{}{"command": command}

	start := time.Now()
	decision, err := sub.RequestApproval(assessment, "shell_command", args)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a workflow-run subagent with an unanswerable 'review' verdict must be denied, not approved")
	}
	if decision.Approved {
		t.Error("expected a denial, got an approval")
	}
	if decision.Surface != "llm-judge" {
		t.Errorf("expected fail-fast surface 'llm-judge', got %q", decision.Surface)
	}
	if elapsed >= time.Second {
		t.Errorf("fail-fast denial took %s — it must deny immediately, not wait for the approval timeout", elapsed)
	}
	// No dialog may have been published at all: a workflow run must not
	// block a browser either.
	if n := countSecurityApprovalEvents(t, parent, func() {
		_, _ = sub.RequestApproval(assessment, "shell_command", args)
	}); n != 0 {
		t.Errorf("expected 0 approval events for a workflow-run subagent, got %d", n)
	}
}

// TestSP168_SubagentCLI_LabeledAndSingleOnly pins the terminal-prompt rules
// for subagents: with exactly one active subagent the parent's terminal
// prompts (labeled, so the user knows whose command it is), and with
// several active subagents the prompt is skipped — interleaved prompts on
// one TTY would corrupt each other — and a "review" verdict fails fast.
func TestSP168_SubagentCLI_LabeledAndSingleOnly(t *testing.T) {
	cases := []struct {
		name         string
		active       int
		wantApproved bool
	}{
		{"single subagent prompts", 1, true},
		{"parallel subagents skip", 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := newIsolatedTestAgent(t)
			defer parent.Shutdown()
			parent.SetWorkspaceRoot(t.TempDir())
			parent.state.SetActivePersona(personas.IDOrchestrator)
			// An interactive terminal, no browser: the TTY is the surface.
			if err := parent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
				cfg.SkipPrompt = false
				return nil
			}); err != nil {
				t.Fatalf("config update failed: %v", err)
			}
			parent.SetHasActiveWebUIClients(func() bool { return false })
			parent.SetEventBus(events.NewEventBus())
			mgr := security.NewApprovalManager()
			parent.security.SetApprovalMgr(mgr)

			sub := makeSubagentForTests(t, parent)
			if err := sub.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
				cfg.SkipPrompt = false
				cfg.AutoRiskJudge = true
				return nil
			}); err != nil {
				t.Fatalf("config update failed: %v", err)
			}

			const command = "rm somefile.txt"
			// "review" keeps the human surface, so the outcome below is
			// decided by whether a surface is available to this subagent.
			sub.GetSecurityAnalysisCache().Set(ChainCacheKey(command), &SecurityAnalysis{
				Summary:        "removes a build artifact",
				Recommendation: "review",
			})

			// Simulate the process-wide active-subagent count.
			for i := 0; i < tc.active; i++ {
				subagents.IncrementActiveSubagents()
			}
			t.Cleanup(func() {
				for i := 0; i < tc.active; i++ {
					subagents.DecrementActiveSubagents()
				}
			})

			// The logger's terminal prompt is a process-global stdin read;
			// the SecurityPromptHook seam stands in for it and captures the
			// text the user would see.
			var capturedPrompt string
			oldHook := utils.SecurityPromptHook
			utils.SecurityPromptHook = func(prompt, _ string, _ *utils.SecurityAnalysisView) utils.ApprovalChoice {
				capturedPrompt = prompt
				return utils.ApprovalChoiceApproveOnce
			}
			defer func() { utils.SecurityPromptHook = oldHook }()
			// GetLogger(false) marks the shared logger interactive; restore
			// the skip-prompt state so other tests are unaffected.
			utils.GetLogger(false)
			defer utils.GetLogger(true)

			assessment := sub.ResolveToolRisk("shell_command", map[string]interface{}{"command": command})
			decision, err := sub.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": command})

			if tc.wantApproved {
				if err != nil {
					t.Fatalf("expected the terminal prompt to approve the command, got: %v", err)
				}
				if decision.Surface != "cli" {
					t.Errorf("expected surface 'cli' (the parent's terminal), got %q", decision.Surface)
				}
				if capturedPrompt == "" {
					t.Fatal("the terminal prompt was never rendered")
				}
				if want := "Subagent (depth 1) is running:"; !strings.Contains(capturedPrompt, want) {
					t.Errorf("prompt missing the subagent label %q — got %q", want, capturedPrompt)
				}
			} else {
				if err == nil {
					t.Fatal("expected a fail-fast denial with several active subagents, got approval")
				}
				if decision.Surface != "llm-judge" {
					t.Errorf("expected the fail-fast surface 'llm-judge', got %q", decision.Surface)
				}
				if capturedPrompt != "" {
					t.Error("the terminal prompt must be skipped when several subagents are active")
				}
			}
		})
	}
}
