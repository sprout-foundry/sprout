// Package agent — starter upgrade proposals.
//
// When a project's starter manifest (.sprout/starter.json,
// loaded through pkg/starterstore) names an older version of its starter than
// the embedded starter tree (pkg/starters), the agent is told
// about the upgrade at the start of a turn: an advisory notice is appended to
// the turn's system prompt. The notice names the starter and both versions,
// points at the starter's stack-skill upgrade note (the "## Upgrade note"
// section of the skill auto-activated alongside), and carries
// the load-bearing instruction — the agent may PROPOSE the
// upgrade, but it never applies one silently; the upgrade is applied only
// when the user explicitly approves it.
//
// The turn-start hook (prepareQueryRun, seed_query.go) calls
// starterUpgradeNotice once per turn — after the stack-skill auto-activation
// — and appends the notice to the composed system prompt, exactly
// like the plan summary. A missing or stale-free manifest changes
// nothing: the method returns "" in every case where there is no verifiable
// upgrade to propose.
//
// The mechanism is a pure reader with respect to the project: it reads the
// manifest and the embedded starter catalogue and never writes a file. That
// purity is what guarantees the contract — this mechanism can propose,
// but it cannot apply; applying an upgrade is a deliberate, user-approved
// action.
package agent

import (
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"

	"github.com/sprout-foundry/sprout/pkg/starters"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// starterUpgradeNotice builds the advisory system-prompt notice for a stale
// starter manifest: the project's manifest names starterID at
// projectVersion, and the embedded starter tree is at embeddedVersion.
//
// It returns the notice only when the project is strictly older than the
// embedded tree (projectVersion < embeddedVersion under semver). The notice
// names the starter and both versions, points at the skill's upgrade note,
// and instructs the agent that it may propose the upgrade but must NEVER
// apply it silently — only when the user explicitly approves it.
//
// It returns "" in two other cases:
//
//   - either version does not parse as semver — an ambiguous comparison must
//     never trigger an upgrade proposal (a wrong proposal is worse than a
//     missed one);
//   - the project version equals or is newer than the embedded one — there
//     is no upgrade to propose.
//
// It is pure (no I/O, no state): the same inputs always yield the same
// notice, and it changes no file.
func starterUpgradeNotice(starterID, projectVersion, embeddedVersion string) string {
	project, err := semver.NewVersion(projectVersion)
	if err != nil {
		// Unparseable project version: the comparison is ambiguous — never
		// notify on an ambiguous comparison.
		return ""
	}
	embedded, err := semver.NewVersion(embeddedVersion)
	if err != nil {
		return ""
	}
	if !project.LessThan(embedded) {
		// Same or newer version: nothing to propose.
		return ""
	}
	return fmt.Sprintf(
		"Starter upgrade available: this project's starter manifest (.sprout/starter.json) "+
			"names starter %q at version %s, but the embedded starter tree is at version %s. "+
			"The starter's stack skill carries an upgrade note describing what changed between "+
			"the two versions. You may propose this upgrade to the user, but you must NEVER "+
			"apply it silently: apply the upgrade only when the user explicitly approves it.",
		starterID, projectVersion, embeddedVersion,
	)
}

// starterUpgradeNotice returns the turn-context advisory for the project's
// starter manifest: when .sprout/starter.json names an older
// version of its starter than the embedded starter tree, it returns the
// upgrade notice the agent may act on by proposing (never silently applying)
// the upgrade.
//
// It returns "" when there is nothing to propose:
//
//   - the agent has no workspace root;
//   - the project has no .sprout/starter.json (starterstore.ErrNoManifest);
//   - the manifest is unreadable or invalid (logged; a corrupt manifest is
//     not an upgrade signal, mirroring autoActivateStarterSkill);
//   - the starter id is empty (the validator already rejects this; kept as
//     defense);
//   - the starter has no embedded tree (e.g. a starter that no longer ships)
//     — there is no embedded version to compare against;
//   - the project version equals or is newer than the embedded one, or
//     either version is unparseable (see starterUpgradeNotice, the pure
//     helper).
//
// It is called from prepareQueryRun once per turn. It is a pure reader with
// respect to the project — it reads the manifest and the embedded starter
// catalogue and never writes a file — so the turn-start hook can never apply
// an upgrade by itself ("it never applies them silently").
func (a *Agent) starterUpgradeNotice() string {
	root := a.currentWorkspaceRoot()
	if root == "" {
		return ""
	}

	manifest, err := starterstore.LoadStarterManifest(root)
	if err != nil {
		if !errors.Is(err, starterstore.ErrNoManifest) {
			// An unreadable or invalid manifest is not an upgrade signal — the
			// turn proceeds without the notice, exactly as
			// autoActivateStarterSkill treats it. Log for diagnostics only.
			a.Logger().Debug("starter upgrade check: unreadable starter manifest in %s: %v\n", root, err)
		}
		return ""
	}

	starterID := manifest.Starter.ID
	if starterID == "" {
		return ""
	}

	embedded, err := starters.Version(starterID)
	if err != nil {
		// Unknown starter id (a starter that no longer ships): there is no
		// embedded version to compare against, so there is nothing to
		// propose.
		return ""
	}

	// Unqualified on purpose: this calls the pure helper above (a method is
	// only reachable through its receiver), which performs the version
	// comparison and builds the notice.
	return starterUpgradeNotice(starterID, manifest.Starter.Version, embedded)
}
