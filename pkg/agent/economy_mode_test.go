package agent

// Economy-mode tests. Pin the per-session behavior: mode on/off, subagent
// routing to the worker model, the main-loop switch for a NEW session only,
// and the rule that a MID-SESSION toggle never switches the main-loop model.

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// economyTestAgent builds an agent with a config manager and a worker role
// pointing at a distinct (cheaper) model, so economy routing is observable.
func economyTestAgent(t *testing.T) *Agent {
	t.Helper()
	a := newRolesTestAgent(t)
	setRoles(t, a, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})
	return a
}

// TestEconomyMode_OnOff pins the basic toggle and its default.
func TestEconomyMode_OnOff(t *testing.T) {
	a := economyTestAgent(t)
	if a.GetEconomyMode() {
		t.Fatal("economy mode must default off")
	}
	a.SetEconomyMode(true)
	if !a.GetEconomyMode() {
		t.Fatal("economy mode must be on after SetEconomyMode(true)")
	}
	a.SetEconomyMode(false)
	if a.GetEconomyMode() {
		t.Fatal("economy mode must be off after SetEconomyMode(false)")
	}
}

// TestEconomyMode_NewSessionStartsMainLoopOnWorker pins the new-session
// effect: with the mode set before the first turn, the main loop starts on
// the worker model.
func TestEconomyMode_NewSessionStartsMainLoopOnWorker(t *testing.T) {
	a := economyTestAgent(t)
	a.SetEconomyMode(true) // before any turn → new session

	if !a.GetMainLoopEconomy() {
		t.Fatal("a new session in economy mode must run the main loop in economy mode")
	}
	if !a.applyEconomyMainLoopModel() {
		t.Fatal("applyEconomyMainLoopModel must switch the main loop on a new-session economy agent")
	}
	if got := a.GetModel(); got != "cheap-model" {
		t.Errorf("main-loop model = %q, want the worker model %q", got, "cheap-model")
	}
}

// TestEconomyMode_MidSessionDoesNotSwitchMainLoop pins the load-bearing rule:
// enabling economy mode AFTER the first turn must not switch the main-loop
// model (the prompt cache is kept); only subagent routing changes.
func TestEconomyMode_MidSessionDoesNotSwitchMainLoop(t *testing.T) {
	a := economyTestAgent(t)

	// The session has started (first turn began), then the mode is enabled.
	a.markSessionStarted()
	a.SetEconomyMode(true)

	if a.GetMainLoopEconomy() {
		t.Fatal("a mid-session economy toggle must not start the main loop in economy mode")
	}
	if a.applyEconomyMainLoopModel() {
		t.Fatal("applyEconomyMainLoopModel must be a no-op for a mid-session toggle")
	}
	// Subagent routing still applies mid-session.
	if _, _, applies := a.economySubagentRoute(); !applies {
		t.Fatal("a mid-session toggle must still re-route subagents to the worker model")
	}
}

// TestEconomyMode_MidSessionDisableKeepsMainLoopEconomy pins the one-way
// rule: a session that STARTED in economy mode keeps its main-loop model even
// if the mode is turned off mid-session (a model switch would invalidate the
// prompt cache).
func TestEconomyMode_MidSessionDisableKeepsMainLoopEconomy(t *testing.T) {
	a := economyTestAgent(t)
	a.SetEconomyMode(true) // new session → main loop economy
	a.markSessionStarted()
	a.SetEconomyMode(false) // mid-session disable

	if a.GetEconomyMode() {
		t.Fatal("economy mode must be off after the mid-session disable")
	}
	if !a.GetMainLoopEconomy() {
		t.Fatal("the main loop must stay in economy mode for the session once started in it")
	}
}

// TestEconomyMode_SubagentRoutesToWorker pins that a subagent resolving to
// the coder role is routed to the worker model under economy mode, and is
// attributed to the worker role.
func TestEconomyMode_SubagentRoutesToWorker(t *testing.T) {
	a := economyTestAgent(t)
	a.SetEconomyMode(true)

	provider, model, role, _, err := resolveSubagentProviderModel(a, "coder", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "zai" || model != "cheap-model" {
		t.Errorf("economy subagent resolved to %s/%s, want the worker model (zai/cheap-model)", provider, model)
	}
	if role != configuration.RoleWorker {
		t.Errorf("economy subagent role = %q, want %q", role, configuration.RoleWorker)
	}
}

// TestEconomyMode_SubagentOffUsesCoder pins that with the mode off a subagent
// resolves through the coder role as before.
func TestEconomyMode_SubagentOffUsesCoder(t *testing.T) {
	a := economyTestAgent(t)

	provider, model, role, _, err := resolveSubagentProviderModel(a, "coder", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openrouter" || model != "coder-model" {
		t.Errorf("non-economy subagent resolved to %s/%s, want the coder model (openrouter/coder-model)", provider, model)
	}
	if role != configuration.RoleCoder {
		t.Errorf("non-economy subagent role = %q, want %q", role, configuration.RoleCoder)
	}
}

// TestEconomyMode_ExplicitPersonaModelWins pins that an explicit persona
// provider/model is not overridden by economy routing.
func TestEconomyMode_ExplicitPersonaModelWins(t *testing.T) {
	a := economyTestAgent(t)
	a.SetEconomyMode(true)

	// The reviewer persona resolves through the reviewer role when explicitly
	// configured; economy routing must not override it.
	setRoles(t, a, map[string]configuration.RoleConfig{
		configuration.RoleCoder:    {Provider: "openrouter", Model: "coder-model"},
		configuration.RoleWorker:   {Provider: "zai", Model: "cheap-model"},
		configuration.RoleReviewer: {Provider: "openai", Model: "review-model"},
	})
	provider, model, role, _, err := resolveSubagentProviderModel(a, "reviewer", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "review-model" {
		t.Errorf("reviewer resolved to %s/%s, want its explicit role model (openai/review-model)", provider, model)
	}
	if role != configuration.RoleReviewer {
		t.Errorf("reviewer role = %q, want %q (economy must not override an explicit persona)", role, configuration.RoleReviewer)
	}
}

// TestEconomyMode_UnsetWorkerIsNoOp pins that economy mode with no worker
// model configured changes nothing in effect: the worker role falls back to
// the coder role's resolved pair, so a subagent still resolves to the coder
// model (economy routing is a no-op until a cheaper worker model is set).
func TestEconomyMode_UnsetWorkerIsNoOp(t *testing.T) {
	a := newRolesTestAgent(t)
	setRoles(t, a, map[string]configuration.RoleConfig{
		configuration.RoleCoder: {Provider: "openrouter", Model: "coder-model"},
	})
	a.SetEconomyMode(true)

	provider, model, role, _, err := resolveSubagentProviderModel(a, "coder", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openrouter" || model != "coder-model" {
		t.Errorf("with no worker model configured, economy routing resolved to %s/%s, want the coder model (openrouter/coder-model)", provider, model)
	}
	if role != configuration.RoleCoder {
		t.Errorf("with no worker model configured, economy routing must leave the role as coder, got %q", role)
	}
}

// TestEconomyMode_MainLoopSwitchRunsOnce pins the once-guard: the main-loop
// model switch runs at most once per session, so re-running it every turn
// cannot reset the context-token count or re-normalize the conversation.
func TestEconomyMode_MainLoopSwitchRunsOnce(t *testing.T) {
	a := economyTestAgent(t)
	a.SetEconomyMode(true) // new session

	if !a.applyEconomyMainLoopModel() {
		t.Fatal("the first call must switch the main loop")
	}
	if a.applyEconomyMainLoopModel() {
		t.Fatal("the second call must be a no-op (once-guard)")
	}
}

// TestEconomyMode_ExplicitPersonaModelKeepsCoderRole pins that a persona with
// an explicit provider/model keeps its own role under economy mode (it is not
// re-labelled worker, so it is not pulled into worker verification).
func TestEconomyMode_ExplicitPersonaModelKeepsCoderRole(t *testing.T) {
	a := economyTestAgent(t)
	a.SetEconomyMode(true)

	// The reviewer persona with an explicit reviewer role: economy must not
	// re-label it worker.
	setRoles(t, a, map[string]configuration.RoleConfig{
		configuration.RoleCoder:    {Provider: "openrouter", Model: "coder-model"},
		configuration.RoleWorker:   {Provider: "zai", Model: "cheap-model"},
		configuration.RoleReviewer: {Provider: "openai", Model: "review-model"},
	})
	_, _, role, _, err := resolveSubagentProviderModel(a, "reviewer", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if role != configuration.RoleReviewer {
		t.Errorf("explicit reviewer persona role = %q, want %q under economy mode", role, configuration.RoleReviewer)
	}
}

// TestEconomyMode_MainLoopNoOpWithoutExplicitWorker pins that a new session in
// economy mode with no explicit worker role does NOT switch the main loop (the
// worker role falls back to the coder pair, so switching would be a no-op in
// effect and would move a session off an explicitly chosen coder model).
func TestEconomyMode_MainLoopNoOpWithoutExplicitWorker(t *testing.T) {
	a := newRolesTestAgent(t)
	setRoles(t, a, map[string]configuration.RoleConfig{
		configuration.RoleCoder: {Provider: "openrouter", Model: "coder-model"},
	})
	a.SetEconomyMode(true) // new session

	if a.applyEconomyMainLoopModel() {
		t.Fatal("the main-loop switch must be a no-op when no worker role is explicitly configured")
	}
	if got := a.EconomyDescription(); got == "off" || !containsAll(got, "no effect") {
		t.Errorf("description should state the no-op, got %q", got)
	}
}

// TestEconomyMode_NestedSubagentInheritsMode pins that a spawned subagent
// inherits the parent's economy mode, so second-level delegation also routes
// to the worker model.
func TestEconomyMode_NestedSubagentInheritsMode(t *testing.T) {
	parent, runner := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})
	parent.SetEconomyMode(true)

	sub, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()

	if !sub.GetEconomyMode() {
		t.Fatal("a spawned subagent must inherit the parent's economy mode")
	}
	if sub.GetMainLoopEconomy() {
		t.Fatal("a subagent must not start its own main loop in economy mode (only subagent routing propagates)")
	}
}

// TestEconomyMode_ResumedSessionKeepsMainLoopModel pins the resumed-session
// rule: a caller that has loaded a prior conversation marks the session
// started before enabling economy mode, so the main loop keeps the model its
// prompt cache was built on and only subagent routing changes.
func TestEconomyMode_ResumedSessionKeepsMainLoopModel(t *testing.T) {
	a := economyTestAgent(t)
	a.MarkSessionStarted() // the resumed conversation is already under way
	a.SetEconomyMode(true)

	if a.GetMainLoopEconomy() {
		t.Fatal("enabling economy mode on a resumed session must not switch the main loop")
	}
	if _, _, applies := a.economySubagentRoute(); !applies {
		t.Fatal("a resumed session in economy mode must still re-route subagents")
	}
}

// TestEconomyMode_DescriptionNamesEffect pins that the UI-facing description
// states the mode's effect plainly.
func TestEconomyMode_DescriptionNamesEffect(t *testing.T) {
	a := economyTestAgent(t)
	if got := a.EconomyDescription(); got != "off" {
		t.Errorf("off description = %q, want %q", got, "off")
	}
	a.SetEconomyMode(true) // new session
	got := a.EconomyDescription()
	if got == "off" {
		t.Fatal("enabled description must not read as off")
	}
	if !containsAll(got, "zai/cheap-model") {
		t.Errorf("description should name the worker route, got %q", got)
	}
}
