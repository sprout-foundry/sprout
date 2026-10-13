package agent

// Worker-role delegation tests. Pin the contract that a worker-persona
// subagent resolves through the worker model role: an explicit roles.worker
// selection wins, and an unset worker falls back to the coder role's resolved
// (provider, model) — so an unset worker behaves exactly like the coder model.
// Also pin that a delegated worker task gets a focused context (its own files
// and goal, never the parent's conversation history) and that subagent
// activity events name the model that actually ran each step.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// TestResolveSubagentProviderModel_WorkerPersona_FallsBackToCoderRole pins
// case (a): with the worker role unset, spawning the worker persona resolves
// to the coder role's (provider, model) and is attributed to the worker role.
func TestResolveSubagentProviderModel_WorkerPersona_FallsBackToCoderRole(t *testing.T) {
	agent := newRolesTestAgent(t)
	setRoles(t, agent, map[string]configuration.RoleConfig{
		configuration.RoleCoder: {Provider: "openrouter", Model: "coder-role-model"},
	})

	provider, model, role, _, err := resolveSubagentProviderModel(agent, "worker", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openrouter" || model != "coder-role-model" {
		t.Errorf("unset worker resolved to %s/%s, want the coder role's (openrouter/coder-role-model)", provider, model)
	}
	if role != configuration.RoleWorker {
		t.Errorf("worker persona role = %q, want %q (usage must attribute to the worker role)", role, configuration.RoleWorker)
	}
}

// TestResolveSubagentProviderModel_WorkerPersona_ExplicitWorkerRoleWins pins
// case (b): an explicit roles.worker selection wins over the coder role, so
// pointing the worker at a cheaper model actually routes the worker persona
// to that model.
func TestResolveSubagentProviderModel_WorkerPersona_ExplicitWorkerRoleWins(t *testing.T) {
	agent := newRolesTestAgent(t)
	setRoles(t, agent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-role-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})

	provider, model, role, _, err := resolveSubagentProviderModel(agent, "worker", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "zai" || model != "cheap-model" {
		t.Errorf("explicit worker resolved to %s/%s, want the worker role's (zai/cheap-model)", provider, model)
	}
	if role != configuration.RoleWorker {
		t.Errorf("worker persona role = %q, want %q", role, configuration.RoleWorker)
	}
}

// TestResolveSubagentProviderModel_NonWorkerPersonaKeepsCoderAttribution pins
// that a non-worker persona (coder) is NOT routed through the worker role: it
// resolves through the coder role and attributes to the coder role. This is the
// "keep on the coder model" side of the delegation guidance.
func TestResolveSubagentProviderModel_NonWorkerPersonaKeepsCoderAttribution(t *testing.T) {
	agent := newRolesTestAgent(t)
	setRoles(t, agent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-role-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})

	_, _, role, _, err := resolveSubagentProviderModel(agent, "coder", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if role != configuration.RoleCoder {
		t.Errorf("coder persona role = %q, want %q (only the worker persona routes through the worker role)", role, configuration.RoleCoder)
	}
}

// TestResolveParallelTaskPersonas_WorkerPersonaResolvesWorkerRole pins the
// parallel path: a worker-persona task in a parallel batch resolves through the
// worker role (provider, model, and role all), while a coder-persona task in
// the same batch stays on the coder role. This is the "parallel delegation
// reuses the existing parallel-subagent path" contract.
func TestResolveParallelTaskPersonas_WorkerPersonaResolvesWorkerRole(t *testing.T) {
	agent := newRolesTestAgent(t)
	setRoles(t, agent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-role-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})

	tasks := []SubagentTask{
		{ID: "w1", Prompt: "summarise the logs", Persona: "worker"},
		{ID: "c1", Prompt: "implement the feature", Persona: "coder"},
	}
	if err := resolveParallelTaskPersonas(context.Background(), agent, tasks); err != nil {
		t.Fatalf("resolveParallelTaskPersonas: %v", err)
	}

	// The worker task resolves through the worker role.
	if tasks[0].Provider != "zai" || tasks[0].Model != "cheap-model" {
		t.Errorf("worker task resolved to %s/%s, want the worker role's (zai/cheap-model)", tasks[0].Provider, tasks[0].Model)
	}
	if tasks[0].Role != configuration.RoleWorker {
		t.Errorf("worker task role = %q, want %q", tasks[0].Role, configuration.RoleWorker)
	}
	if tasks[0].SystemPrompt == "" {
		t.Error("worker task did not get its persona's system prompt")
	}

	// The coder task in the same batch stays on the coder role.
	if tasks[1].Provider != "openrouter" || tasks[1].Model != "coder-role-model" {
		t.Errorf("coder task resolved to %s/%s, want the coder role's (openrouter/coder-role-model)", tasks[1].Provider, tasks[1].Model)
	}
	if tasks[1].Role != configuration.RoleCoder {
		t.Errorf("coder task role = %q, want %q", tasks[1].Role, configuration.RoleCoder)
	}
}

// TestWorkerSpawn_FocusedContextNoParentHistory pins the focused-context
// contract: a delegated worker task receives only the provided files and goal
// (plus the expected output format spelled out in the prompt), never the
// parent's conversation history. The parent's messages live in its state and
// are not forwarded to the subagent's enhanced prompt.
func TestWorkerSpawn_FocusedContextNoParentHistory(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	t.Cleanup(agent.Shutdown)

	dir := t.TempDir()
	writeFile(t, dir, "note.txt", "NOTE_MARKER: the file content the worker must summarise")
	agent.workspaceRoot = dir

	// Distinct parent conversation history that must NOT leak into the
	// subagent's prompt.
	agent.AddMessage(api.Message{Role: "user", Content: "HISTORY_MARKER: earlier we discussed the database schema and the migration plan"})
	agent.AddMessage(api.Message{Role: "assistant", Content: "HISTORY_MARKER: noted, I will keep the schema changes in mind"})

	spec, err := prepareSubagentLaunch(filesystem.WithWorkspaceRoot(context.Background(), dir), agent, map[string]interface{}{
		"persona": "worker",
		"prompt":  "TASK_MARKER: summarise the provided file; reply with a one-line summary",
		"files":   "note.txt",
	})
	if err != nil {
		t.Fatalf("prepareSubagentLaunch: %v", err)
	}

	// The delegated task gets the file content and the goal...
	if !containsAll(spec.enhancedPrompt, "NOTE_MARKER: the file content the worker must summarise", "TASK_MARKER: summarise the provided file") {
		t.Errorf("enhanced prompt missing the focused context (file + task):\n%s", spec.enhancedPrompt)
	}
	// ...but not the parent's conversation history.
	if containsAll(spec.enhancedPrompt, "HISTORY_MARKER") {
		t.Errorf("parent conversation history leaked into the worker's prompt:\n%s", spec.enhancedPrompt)
	}
	// And it is attributed to the worker role.
	if spec.role != configuration.RoleWorker {
		t.Errorf("worker spawn role = %q, want %q", spec.role, configuration.RoleWorker)
	}
}

// newWorkerEventAgent builds an agent with an event bus and an isolated config
// manager, for asserting on published subagent activity events.
func newWorkerEventAgent(t *testing.T) (*Agent, *events.EventBus, *configuration.Manager) {
	t.Helper()
	agent, bus := newTestAgentWithEventBus(t)
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}
	agent.configManager = mgr
	return agent, bus, mgr
}

// expectSubagentActivityEvent blocks until the named phase's spawn event (or a
// timeout) and returns its data map. Skips events of other types (e.g. the
// agent_message events the resolution warnings emit) and other phases.
func expectSubagentActivityEvent(t *testing.T, ch <-chan events.UIEvent, phase string) map[string]interface{} {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != events.EventTypeSubagentActivity {
				continue // skip agent_message and other event types
			}
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				t.Fatalf("event data is not a map: %T", ev.Data)
			}
			if data["phase"] != phase {
				continue
			}
			return data
		case <-timeout:
			t.Fatalf("timed out waiting for a %q subagent activity event", phase)
			return nil
		}
	}
}

// TestSubagentActivityEvent_NamesTheModelThatRan pins the events acceptance
// criterion: the spawn event's payload names the provider/model that actually
// ran the step. A worker-model step names the worker model; a coder-model step
// names the coder model.
func TestSubagentActivityEvent_NamesTheModelThatRan(t *testing.T) {
	agent, bus, mgr := newWorkerEventAgent(t)
	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-model"},
			configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ch := bus.Subscribe("worker_event_test")
	defer bus.Unsubscribe("worker_event_test")

	// A worker-model step: resolve the worker persona and publish its spawn
	// event the way handleRunSubagent does.
	workerProvider, workerModel, workerRole, _, err := resolveSubagentProviderModel(agent, "worker", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publishSubagentActivity(context.Background(), agent, "spawn", "Starting worker", map[string]interface{}{
		"persona":     "worker",
		"provider":    workerProvider,
		"model":       workerModel,
		"is_parallel": false,
	})
	workerData := expectSubagentActivityEvent(t, ch, "spawn")
	if workerData["model"] != "cheap-model" {
		t.Errorf("worker step event model = %v, want the worker model %q", workerData["model"], "cheap-model")
	}
	if workerData["provider"] != "zai" {
		t.Errorf("worker step event provider = %v, want %q", workerData["provider"], "zai")
	}
	if workerRole != configuration.RoleWorker {
		t.Errorf("worker step role = %q, want %q", workerRole, configuration.RoleWorker)
	}

	// A coder-model step (non-worker) names the coder model, not the worker's.
	coderProvider, coderModel, coderRole, _, err := resolveSubagentProviderModel(agent, "coder", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publishSubagentActivity(context.Background(), agent, "spawn", "Starting coder", map[string]interface{}{
		"persona":     "coder",
		"provider":    coderProvider,
		"model":       coderModel,
		"is_parallel": false,
	})
	coderData := expectSubagentActivityEvent(t, ch, "spawn")
	if coderData["model"] != "coder-model" {
		t.Errorf("coder step event model = %v, want the coder model %q", coderData["model"], "coder-model")
	}
	if coderData["provider"] != "openrouter" {
		t.Errorf("coder step event provider = %v, want %q", coderData["provider"], "openrouter")
	}
	if coderRole != configuration.RoleCoder {
		t.Errorf("coder step role = %q, want %q", coderRole, configuration.RoleCoder)
	}
}

// TestRollupSubagentUsage_WorkerRoleAttributesToWorker pins the metering
// contract: a completed worker subagent's usage rolls up under the worker role
// (not the parent's coder role), with its real token split.
func TestRollupSubagentUsage_WorkerRoleAttributesToWorker(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// The parent's own coder turn.
	addRoleTurn(t, a, configuration.RoleCoder, 100, 10, 0.01)

	// A worker subagent completes, attributed to the worker role.
	a.RollupSubagentUsage(&subagents.SubagentResult{
		Role:             configuration.RoleWorker,
		TokensUsed:       30,
		Cost:             0.02,
		PromptTokens:     20,
		CompletionTokens: 10,
	})

	byRole := map[string]RoleUsage{}
	for _, e := range a.GetRoleUsage() {
		byRole[e.Role] = e
	}
	if got := byRole[configuration.RoleWorker]; got.PromptTokens != 20 || got.CompletionTokens != 10 {
		t.Errorf("worker role = %+v, want 20 prompt / 10 completion", got)
	}
	// The parent's coder bucket is untouched by the worker rollup.
	if got := byRole[configuration.RoleCoder]; got.PromptTokens != 100 || got.CompletionTokens != 10 {
		t.Errorf("coder role = %+v, want 100 prompt / 10 completion (parent's own turn only)", got)
	}
}

// TestRunParallel_WorkerTaskResultRoleIsWorker pins the end-to-end role
// attribution the parallel path needs: a worker-persona task run through
// RunParallel carries RoleWorker on its SubagentResult, so the parent's
// RollupSubagentUsage attributes the worker's tokens to the worker role rather
// than the coder role. (The resolveParallelTaskPersonas test pins the task's
// role; this pins that the runner propagates it onto the result.)
func TestRunParallel_WorkerTaskResultRoleIsWorker(t *testing.T) {
	parent, runner := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-role-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})

	runner.testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		return NewScriptedClient(NewScriptedResponseBuilder().Content("summarised the file; nothing notable.").Build()), nil
	}

	tasks := []SubagentTask{
		{ID: "w1", Prompt: "summarise the file", Persona: "worker"},
		{ID: "c1", Prompt: "implement it", Persona: "coder"},
	}
	if err := resolveParallelTaskPersonas(context.Background(), parent, tasks); err != nil {
		t.Fatalf("resolveParallelTaskPersonas: %v", err)
	}

	results := runner.RunParallel(context.Background(), tasks, SubagentOptions{})
	roleByID := map[string]string{}
	for _, r := range results {
		if r.Error != nil {
			t.Fatalf("task %s failed: %v", r.ID, r.Error)
		}
		roleByID[r.ID] = r.Role
	}
	if roleByID["w1"] != configuration.RoleWorker {
		t.Errorf("worker task result role = %q, want %q (usage must roll up under the worker role)", roleByID["w1"], configuration.RoleWorker)
	}
	if roleByID["c1"] != configuration.RoleCoder {
		t.Errorf("coder task result role = %q, want %q", roleByID["c1"], configuration.RoleCoder)
	}
}

// containsAll reports whether s contains every substring in subs.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
