//go:build !js

// runner_verify_gate_test.go — the benchmark's "a run always verifies"
// guarantees: a scripted run that changes the copy through the edit tool,
// through a shell command, and through a subagent each produces a
// verification result, and the runner's backstop forces verification when
// the turn's change window stays closed but git shows the copy changed
// application code.
//
// The fixtures and helpers live in runner_test.go and runner_contract_test.go
// (same package): benchTask, benchPlan, shapeManifest, shAvailable. The
// scripted model harness below builds the run's agent the same way
// scriptedFactory does, plus the per-scenario tool wiring.

package benchmark

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// gateJSON marshals tool-call arguments for a scripted response. It is the
// gate test's own helper (the package's other test files carry their own)
// so the file stands alone.
func gateJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// gateFactory builds the run's scripted agent for one scenario: the
// responses closure builds the script (it needs the run-specific copy
// path), and shapeAgent (optional) refines the freshly built agent — e.g.
// to wire a subagent client or to close the change-tracking window.
func gateFactory(
	t *testing.T,
	mgr *configuration.Manager,
	responses func(runDir string) []*agent.ScriptedResponse,
	shapeAgent func(ag *agent.Agent, runDir string),
) AgentFactory {
	t.Helper()
	return func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		client := agent.NewScriptedClient(responses(runDir)...)
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.ContextMode = configuration.ContextModeFull
			cfg.SkipPrompt = true
			cfg.Verification = &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1}
			return nil
		}); err != nil {
			return nil, err
		}
		ag, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
		if err != nil {
			return nil, err
		}
		ag.SetMaxIterations(10)
		ag.SetWorkspaceRoot(runDir)
		if shapeAgent != nil {
			shapeAgent(ag, runDir)
		}
		return ag, nil
	}
}

// gateRunner wires a one-run benchmark runner around the given factory and
// the deterministic echo build command the fixture manifest honors.
func gateRunner(mgr *configuration.Manager, factory AgentFactory) *Runner {
	return &Runner{
		ConfigManager: mgr,
		AgentFactory:  factory,
		ShapeCopy:     shapeManifest("echo gate-ok"),
		RunsPerTask:   1,
	}
}

// TestRunner_VerifyGate_EditTool pins the item's first scenario: a
// scripted run that edits the copy through the edit tool produces a
// verification result.
func TestRunner_VerifyGate_EditTool(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := gateRunner(mgr, gateFactory(t, mgr, func(runDir string) []*agent.ScriptedResponse {
		return []*agent.ScriptedResponse{
			agent.NewToolCallResponse("edit_file", gateJSON(map[string]any{
				"path":    filepath.Join(runDir, "index.html"),
				"old_str": "<h1>Fixture starter</h1>",
				"new_str": "<h1>Fixture starter v2</h1>",
			})),
			agent.NewStopResponse("done"),
		}
	}, nil))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "gate-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Result == nil {
		t.Fatal("Result = nil, want a verification result for an edit-tool turn")
	}
	if runs[0].Err != nil {
		t.Errorf("Err = %v, want nil", runs[0].Err)
	}
}

// TestRunner_VerifyGate_ShellCommand pins the item's second scenario: a
// scripted run that edits the copy through a shell command produces a
// verification result. The command redirects inside the workspace — the
// classifier escalates it to "prompt", so this scenario also pins the
// runner's headless security posture: without it the command is denied
// (no approval surface) and the copy never changes.
func TestRunner_VerifyGate_ShellCommand(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := gateRunner(mgr, gateFactory(t, mgr, func(runDir string) []*agent.ScriptedResponse {
		return []*agent.ScriptedResponse{
			agent.NewToolCallResponse("shell_command", gateJSON(map[string]any{
				"command": "printf 'gate\\n' > src/added.txt",
			})),
			agent.NewStopResponse("done"),
		}
	}, nil))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "gate-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Result == nil {
		t.Fatal("Result = nil, want a verification result for a shell-command turn (the headless posture must let the command run)")
	}
	if runs[0].Err != nil {
		t.Errorf("Err = %v, want nil", runs[0].Err)
	}
}

// TestRunner_VerifyGate_Subagent pins the item's third scenario: a
// scripted run that edits the copy through a subagent produces a
// verification result. The subagent's write reaches the parent's change
// window (MergeChild) after the parent's MarkTurnStart, so the gate opens.
func TestRunner_VerifyGate_Subagent(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := gateRunner(mgr, gateFactory(t, mgr, func(runDir string) []*agent.ScriptedResponse {
		return []*agent.ScriptedResponse{
			agent.NewToolCallResponse("run_subagent", gateJSON(map[string]any{
				"persona": "coder",
				"prompt":  "make the change",
			})),
			agent.NewStopResponse("done"),
		}
	}, func(ag *agent.Agent, runDir string) {
		ag.GetSubagentRunner().SetSubagentClientFactoryForTest(func(api.ClientType, string) (api.ClientInterface, error) {
			return agent.NewScriptedClient(
				agent.NewToolCallResponse("write_file", gateJSON(map[string]any{
					"path":    filepath.Join(runDir, "src", "sub.txt"),
					"content": "sub\n",
				})),
				agent.NewStopResponse("subagent done, file written."),
			), nil
		})
	}))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "gate-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Result == nil {
		t.Fatal("Result = nil, want a verification result for a subagent turn")
	}
	if runs[0].Err != nil {
		t.Errorf("Err = %v, want nil", runs[0].Err)
	}
}

// TestRunner_VerifyGate_BackstopForcesVerification pins the backstop: when
// the turn's change window stays closed (change tracking disabled, so the
// turn-end hook's gate never opens) but the copy nonetheless changed
// application code, the runner's git baseline detects the change and
// forces verification, so the run still records a result.
func TestRunner_VerifyGate_BackstopForcesVerification(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := gateRunner(mgr, gateFactory(t, mgr, func(runDir string) []*agent.ScriptedResponse {
		return []*agent.ScriptedResponse{
			agent.NewToolCallResponse("edit_file", gateJSON(map[string]any{
				"path":    filepath.Join(runDir, "index.html"),
				"old_str": "<h1>Fixture starter</h1>",
				"new_str": "<h1>Fixture starter v2</h1>",
			})),
			agent.NewStopResponse("done"),
		}
	}, func(ag *agent.Agent, runDir string) {
		// Close the window the hook gates on, while the edit still lands
		// on disk: this is the failure mode the backstop exists for.
		ag.DisableChangeTracking()
	}))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "gate-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Result == nil {
		t.Fatal("Result = nil, want the backstop to force verification on a copy that changed")
	}
	if len(runs[0].Result.Checks) == 0 {
		t.Error("Result.Checks is empty, want the forced verification run's checks")
	}
}

// TestRunner_VerifyGate_BackstopInertWithoutChange pins the backstop's
// guard: a turn that changed nothing (the window is empty and git shows
// no application-code change) is not verified — the backstop must not
// invent a verification run for an unchanged copy.
func TestRunner_VerifyGate_BackstopInertWithoutChange(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := gateRunner(mgr, gateFactory(t, mgr, func(runDir string) []*agent.ScriptedResponse {
		return []*agent.ScriptedResponse{
			agent.NewStopResponse("I did nothing."),
		}
	}, nil))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "gate-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Result != nil {
		t.Errorf("Result = %+v, want nil: an unchanged turn must not be verified", runs[0].Result)
	}
	if runs[0].Passed {
		t.Error("Passed = true, want false: no verification result, no pass")
	}
}

// TestRunner_ConfigureRun_ForcesChangeTracking pins the config half of the
// gate guarantee: configureRun forces change tracking on (the subsystem
// switch and the shell walk), so a user configuration that disabled it
// cannot silently close the verification gate for every run.
func TestRunner_ConfigureRun_ForcesChangeTracking(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	// Seed a configuration that explicitly disables change tracking.
	disabled := false
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ChangeTracking = &configuration.ChangeTrackingConfig{
			Enabled:          &disabled,
			ShellWalkEnabled: &disabled,
		}
		return nil
	}); err != nil {
		t.Fatalf("seed manager config: %v", err)
	}

	runner := &Runner{ConfigManager: mgr}
	if err := runner.configureRun(ModelSpec{}); err != nil {
		t.Fatalf("configureRun: %v", err)
	}

	cfg := mgr.GetConfig()
	if cfg.ChangeTracking == nil {
		t.Fatal("ChangeTracking = nil after configureRun")
	}
	if cfg.ChangeTracking.Enabled == nil || !*cfg.ChangeTracking.Enabled {
		t.Error("change tracking not forced on by configureRun")
	}
	if cfg.ChangeTracking.ShellWalkEnabled == nil || !*cfg.ChangeTracking.ShellWalkEnabled {
		t.Error("shell walk not forced on by configureRun")
	}
}

// TestRunner_VerifyGate_ShellWalkDisabledStillVerifies pins the shell
// dimension of the gate guarantee end to end: a shell-command edit is
// tracked and verified even though the user's configuration disabled the
// shell walk, because the runner forces it on for the run.
func TestRunner_VerifyGate_ShellWalkDisabledStillVerifies(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	disabled := false
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ChangeTracking = &configuration.ChangeTrackingConfig{ShellWalkEnabled: &disabled}
		return nil
	}); err != nil {
		t.Fatalf("seed manager config: %v", err)
	}

	runner := gateRunner(mgr, gateFactory(t, mgr, func(runDir string) []*agent.ScriptedResponse {
		return []*agent.ScriptedResponse{
			agent.NewToolCallResponse("shell_command", gateJSON(map[string]any{
				"command": "printf 'gate\\n' > src/added.txt",
			})),
			agent.NewStopResponse("done"),
		}
	}, nil))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "gate-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if runs[0].Result == nil {
		t.Fatal("Result = nil, want a verification result with the shell walk forced on for the run")
	}
}
