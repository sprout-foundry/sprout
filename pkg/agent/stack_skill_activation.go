// Package agent — stack-skill auto-activation.
//
// When a project's starter manifest (.sprout/starter.json,
// loaded through pkg/starterstore) names a starter, the starter's own stack
// skill — the skill under pkg/skills/library/<starter>/ that carries the
// stack's conventions (formatter/linter, test conventions, the commands
// declared in the manifest) — activates automatically at the start of a
// turn, so the model works with the starter's defaults from the first turn.
//
// Auto-activation is best-effort context enrichment: a missing or invalid
// manifest, or a starter with no skill, is a no-op and never fails a turn.
// Activation is idempotent — the skill is activated once and stays active
// for the rest of the session — so the per-turn cost is one manifest read
// and one registry lookup.
//
// The upgrade-note half (the agent may propose an upgrade,
// never apply it silently) arrives later; this file is the
// auto-activation mechanism only.
package agent

import (
	"errors"

	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// autoActivateStarterSkill activates the project's stack skill
// at the start of a turn. It reads the starter manifest from the
// project root; when the manifest names a starter and a skill with that ID
// is registered, the skill is activated through the same core as the
// activate_skill tool (activateSkillByID): load, add to the active-skill
// set, and fold into the system prompt.
//
// It is a no-op (no error, no state change) when:
//
//   - the project has no .sprout/starter.json (starterstore.ErrNoManifest);
//   - the manifest is unreadable or invalid (logged; a corrupt manifest is
//     not a reason to fail a turn);
//   - the starter has no skill in the registry (or the skill is disabled or
//     unreadable) — the turn proceeds without it;
//   - the skill is already active (idempotent across turns).
//
// It is called once per turn from prepareQueryRun, before the system prompt
// is handed to the seed agent, so an activation made this turn reaches the
// model's context in the same turn.
func (a *Agent) autoActivateStarterSkill() {
	root := a.currentWorkspaceRoot()
	if root == "" {
		return
	}

	manifest, err := starterstore.LoadStarterManifest(root)
	if err != nil {
		if !errors.Is(err, starterstore.ErrNoManifest) {
			// An unreadable or invalid manifest is not an activation failure —
			// the turn proceeds without the skill. Log for diagnostics only.
			a.Logger().Debug("stack skill auto-activation: unreadable starter manifest in %s: %v\n", root, err)
		}
		return
	}

	starterID := manifest.Starter.ID
	if starterID == "" {
		return
	}

	// Best-effort: a starter with no skill (or an unreadable one) simply has
	// nothing to activate. The error is logged, never surfaced to the turn.
	if _, err := a.activateSkillByID(starterID); err != nil {
		a.Logger().Debug("stack skill auto-activation: no usable skill for starter %q: %v\n", starterID, err)
	}
}
