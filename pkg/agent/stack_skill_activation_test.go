//go:build !js

// stack_skill_activation_test.go — acceptance tests for
// stack-skill auto-activation: when .sprout/starter.json names a starter,
// that starter's skill under pkg/skills/library/<starter>/ activates
// automatically at the start of a turn; when there is no manifest, or the
// starter has no skill, nothing is activated and the turn is never failed.
//
// The tests cover two levels:
//
//   - autoActivateStarterSkill (agent method): a fixture manifest activates
//     the embedded fixture skill; a missing manifest leaves the active
//     skills unchanged; an unknown starter ID activates nothing; a corrupt
//     manifest is a no-op; and activation is idempotent (a second run
//     adds no duplicate).
//   - a scripted agent turn: the auto-activation hook in prepareQueryRun
//     fires and the skill's marker reaches the model's context (the first
//     "system" message).

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// ssFixtureManifestJSON is a minimal valid starter manifest (only
// the starter identity is required). The starter ID "fixture" matches
// the fixture stack skill under pkg/skills/library/fixture/ — the skill the
// embedded library seeds into every config's registry.
const ssFixtureManifestJSON = `{"starter": {"id": "fixture", "version": "1"}}`

// ssUnknownStarterManifestJSON names a starter with no skill under
// pkg/skills/library/ — the "starter with no skill" case: a valid manifest
// whose starter ID is not a registered skill.
const ssUnknownStarterManifestJSON = `{"starter": {"id": "nonexistent", "version": "1"}}`

// ssStaticSiteManifestJSON names the shipped static-site starter, whose
// stack skill lives under pkg/skills/library/static-site/. Unlike the
// fixture (an ID chosen only for the mechanism test), this is a real
// user-facing starter: the manifest ID ("static-site") must resolve to the
// embedded skill of the same ID.
const ssStaticSiteManifestJSON = `{"starter": {"id": "static-site", "version": "1.0.0"}}`

// ssStaticSiteMarker is the marker activateSkillByID folds into the system
// prompt when the static-site skill activates (the skill's frontmatter name).
const ssStaticSiteMarker = "[Skill Activated: Static Site Starter"

// ssWebAppManifestJSON names the shipped web-app starter, whose stack skill
// lives under pkg/skills/library/web-app/. Like static-site it is a real
// user-facing starter: the manifest ID ("web-app") must resolve to the
// embedded skill of the same ID.
const ssWebAppManifestJSON = `{"starter": {"id": "web-app", "version": "1.0.0"}}`

// ssWebAppMarker is the marker activateSkillByID folds into the system prompt
// when the web-app skill activates (the skill's frontmatter name).
const ssWebAppMarker = "[Skill Activated: Web App Starter"

// ssWebAppDataManifestJSON names the shipped web-app-data starter, whose
// stack skill lives under pkg/skills/library/web-app-data/. Like web-app it
// is a real user-facing starter: the manifest ID ("web-app-data") must
// resolve to the embedded skill of the same ID.
const ssWebAppDataManifestJSON = `{"starter": {"id": "web-app-data", "version": "1.0.0"}}`

// ssWebAppDataMarker is the marker activateSkillByID folds into the system
// prompt when the web-app-data skill activates (the skill's frontmatter name).
const ssWebAppDataMarker = "[Skill Activated: Web App with Data Starter"

// ssCorruptManifestJSON is not valid JSON at all. It exercises the
// "unreadable manifest" path (logged, never fails the turn).
const ssCorruptManifestJSON = `{"starter": {`

// ssSkillMarker is the marker activateSkillByID folds into the system prompt
// when the fixture skill activates (the skill's frontmatter name).
const ssSkillMarker = "[Skill Activated: Fixture Starter"

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ssWriteStarterManifest writes content to .sprout/starter.json under root
// (creating the .sprout directory), so the manifest lives exactly where
// starterstore looks.
func ssWriteStarterManifest(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Dir(starterstore.StarterManifestPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create .sprout dir: %v", err)
	}
	if err := os.WriteFile(starterstore.StarterManifestPath(root), []byte(content), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
}

// ssAgent wires client into a fresh agent whose workspace root is a project
// directory (the .sprout/ starter manifest lives under it). It mirrors the
// e2e agent setup from plan_context_test.go: the full context profile keeps
// the tools on the roster, and SkipPrompt keeps the turn from ever blocking
// on an interactive prompt (stdin is closed in tests). The test manager's
// config seeds the embedded skill library (pkg/skills/library), so the
// manifest's starter ID has a skill to activate.
func ssAgent(t *testing.T, client api.ClientInterface, workspaceRoot string) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		cfg.SkipPrompt = true
		return nil
	}); err != nil {
		t.Fatalf("configure test agent: %v", err)
	}

	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)
	ag.SetMaxIterations(10)
	ag.SetWorkspaceRoot(workspaceRoot)
	return ag
}

// ssCountActiveSkills reports how many times skillID appears in the
// agent's active-skill set (a well-formed activation keeps it at 1; a
// duplicate would show up here as 2).
func ssCountActiveSkills(a *Agent, skillID string) int {
	count := 0
	for _, id := range a.state.GetActiveSkills() {
		if id == skillID {
			count++
		}
	}
	return count
}

// ssFindSystemMessageWith reports whether any "system" message sent to the
// model (across all recorded requests) contains marker. The seed core sends
// the composed system prompt as the first "system" message, so the
// auto-activated skill — when folded in — is found here.
func ssFindSystemMessageWith(client *ScriptedClient, marker string) bool {
	for _, msgs := range client.GetSentRequests() {
		for _, m := range msgs {
			if m.Role == "system" && strings.Contains(m.Content, marker) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Agent method: autoActivateStarterSkill
// ---------------------------------------------------------------------------

// TestStackSkillAutoActivation_PresentWhenManifestNamesStarter is the core
// acceptance case: .sprout/starter.json names the "fixture" starter, and the
// fixture skill under pkg/skills/library/fixture/ auto-activates — it lands
// in the active-skill set and its instructions are folded into the system
// prompt.
func TestStackSkillAutoActivation_PresentWhenManifestNamesStarter(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssFixtureManifestJSON)

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	ag.autoActivateStarterSkill()

	if got := ssCountActiveSkills(ag, "fixture"); got != 1 {
		t.Errorf("fixture skill active count = %d, want 1 (active skills: %v)", got, ag.state.GetActiveSkills())
	}
	if !strings.Contains(ag.GetSystemPrompt(), ssSkillMarker) {
		t.Errorf("skill instructions not folded into the system prompt:\n%s", ag.GetSystemPrompt())
	}
}

// TestStackSkillAutoActivation_StaticSiteStarter is the real-starter case:
// a manifest naming the shipped static-site starter activates the
// static-site stack skill under pkg/skills/library/static-site/ — the
// embedded skill whose ID matches the manifest's starter ID. It guards the
// starter→skill mapping for a user-facing starter (not just the fixture).
func TestStackSkillAutoActivation_StaticSiteStarter(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssStaticSiteManifestJSON)

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	ag.autoActivateStarterSkill()

	if got := ssCountActiveSkills(ag, "static-site"); got != 1 {
		t.Errorf("static-site skill active count = %d, want 1 (active skills: %v)", got, ag.state.GetActiveSkills())
	}
	if !strings.Contains(ag.GetSystemPrompt(), ssStaticSiteMarker) {
		t.Errorf("static-site skill instructions not folded into the system prompt:\n%s", ag.GetSystemPrompt())
	}
}

// TestStackSkillAutoActivation_WebAppStarter is the web-app counterpart: a
// manifest naming the shipped web-app starter activates the web-app stack
// skill under pkg/skills/library/web-app/ — the embedded skill whose ID
// matches the manifest's starter ID. It guards the starter→skill mapping for
// the React starter.
func TestStackSkillAutoActivation_WebAppStarter(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssWebAppManifestJSON)

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	ag.autoActivateStarterSkill()

	if got := ssCountActiveSkills(ag, "web-app"); got != 1 {
		t.Errorf("web-app skill active count = %d, want 1 (active skills: %v)", got, ag.state.GetActiveSkills())
	}
	if !strings.Contains(ag.GetSystemPrompt(), ssWebAppMarker) {
		t.Errorf("web-app skill instructions not folded into the system prompt:\n%s", ag.GetSystemPrompt())
	}
}

// TestStackSkillAutoActivation_WebAppDataStarter is the web-app-data
// counterpart: a manifest naming the shipped web-app-data starter activates
// the web-app-data stack skill under pkg/skills/library/web-app-data/ — the
// embedded skill whose ID matches the manifest's starter ID. It guards the
// starter→skill mapping for the React + Hono/D1 starter.
func TestStackSkillAutoActivation_WebAppDataStarter(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssWebAppDataManifestJSON)

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	ag.autoActivateStarterSkill()

	if got := ssCountActiveSkills(ag, "web-app-data"); got != 1 {
		t.Errorf("web-app-data skill active count = %d, want 1 (active skills: %v)", got, ag.state.GetActiveSkills())
	}
	if !strings.Contains(ag.GetSystemPrompt(), ssWebAppDataMarker) {
		t.Errorf("web-app-data skill instructions not folded into the system prompt:\n%s", ag.GetSystemPrompt())
	}
}

// TestStackSkillAutoActivation_AbsentWhenNoManifest: a project with no
// .sprout/starter.json has no skill to auto-activate — the active-skill set
// is unchanged and the call never fails.
func TestStackSkillAutoActivation_AbsentWhenNoManifest(t *testing.T) {
	root := t.TempDir() // no .sprout/starter.json

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	before := len(ag.state.GetActiveSkills())

	ag.autoActivateStarterSkill()

	if got := len(ag.state.GetActiveSkills()); got != before {
		t.Errorf("active skills changed without a manifest: before %d, after %v", before, ag.state.GetActiveSkills())
	}
	if ssCountActiveSkills(ag, "fixture") != 0 {
		t.Errorf("fixture skill must not activate without a starter manifest")
	}
}

// TestStackSkillAutoActivation_NoSkillForStarter: a manifest naming a
// starter with no skill under pkg/skills/library/ activates nothing and
// never errors.
func TestStackSkillAutoActivation_NoSkillForStarter(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssUnknownStarterManifestJSON)

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	ag.autoActivateStarterSkill()

	if ssCountActiveSkills(ag, "nonexistent") != 0 {
		t.Errorf("a starter with no skill must not activate anything")
	}
	if ssCountActiveSkills(ag, "fixture") != 0 {
		t.Errorf("the fixture skill must not activate for an unrelated starter ID")
	}
}

// TestStackSkillAutoActivation_CorruptManifestIgnored: an unreadable
// manifest (invalid JSON) is a no-op — it is logged, never a failed turn.
func TestStackSkillAutoActivation_CorruptManifestIgnored(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssCorruptManifestJSON)

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	ag.autoActivateStarterSkill()

	if ssCountActiveSkills(ag, "fixture") != 0 {
		t.Errorf("a corrupt manifest must not activate the fixture skill")
	}
}

// TestStackSkillAutoActivation_Idempotent: running the auto-activation
// twice (two turns of the same session) adds the skill exactly once — no
// duplicate in the active-skill set, no double-folding into the system
// prompt.
func TestStackSkillAutoActivation_Idempotent(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssFixtureManifestJSON)

	ag := ssAgent(t, NewScriptedClient(NewScriptedTextResponse("ok.")), root)
	ag.autoActivateStarterSkill()
	ag.autoActivateStarterSkill()

	if got := ssCountActiveSkills(ag, "fixture"); got != 1 {
		t.Errorf("fixture skill active count after two activations = %d, want 1 (active skills: %v)", got, ag.state.GetActiveSkills())
	}
	if got := strings.Count(ag.GetSystemPrompt(), ssSkillMarker); got != 1 {
		t.Errorf("skill folded into the system prompt %d times, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Scripted agent turn: the hook fires in prepareQueryRun
// ---------------------------------------------------------------------------

// TestStackSkillAutoActivation_InjectedIntoTurnContext drives a real
// (scripted) turn over a fixture manifest and asserts the auto-activation
// hook in prepareQueryRun fired: the fixture skill became active and its
// marker reached the model's context (the first "system" message) in the
// same turn.
func TestStackSkillAutoActivation_InjectedIntoTurnContext(t *testing.T) {
	root := t.TempDir()
	ssWriteStarterManifest(t, root, ssFixtureManifestJSON)

	client := NewScriptedClient(NewScriptedTextResponse("Continuing the fixture work."))
	ag := ssAgent(t, client, root)
	if _, err := ag.ProcessQuery("Continue with the starter."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if ssCountActiveSkills(ag, "fixture") != 1 {
		t.Errorf("fixture skill must be active after the turn (active skills: %v)", ag.state.GetActiveSkills())
	}
	if !ssFindSystemMessageWith(client, ssSkillMarker) {
		t.Errorf("the auto-activated skill must reach the model's context (system message)")
	}
}

// TestStackSkillAutoActivation_NoManifestLeavesContextUnchanged drives a
// real (scripted) turn with NO manifest and asserts the fixture skill's
// marker does not appear in the model's context — nothing was activated.
func TestStackSkillAutoActivation_NoManifestLeavesContextUnchanged(t *testing.T) {
	root := t.TempDir() // no .sprout/starter.json

	client := NewScriptedClient(NewScriptedTextResponse("Done."))
	ag := ssAgent(t, client, root)
	if _, err := ag.ProcessQuery("Do the thing."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if ssCountActiveSkills(ag, "fixture") != 0 {
		t.Errorf("no manifest: the fixture skill must not activate (active skills: %v)", ag.state.GetActiveSkills())
	}
	if ssFindSystemMessageWith(client, ssSkillMarker) {
		t.Errorf("no manifest: the fixture skill marker must NOT appear in the model's context")
	}
}
