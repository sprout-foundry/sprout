package agent

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/skills"
)

// modeSkills maps a workspace mode to the skills the agent works with while
// the user is in that mode — the mode shapes the agent per request, not the
// chat (SP-147 §3). Modes register client-side (SP-155 §155b); a mode with
// no entry here, or an id this build does not know, adds nothing.
var modeSkills = map[string][]string{
	"design": {skills.SkillIDDesignSystem},
}

// SetWorkspaceMode records the workspace mode the next query was sent from.
// Hosts (the web UI server, the WASM shell) call it before each query; the
// mode's skills activate at turn start.
func (a *Agent) SetWorkspaceMode(mode string) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	a.workspaceMode.Store(&normalized)
}

// WorkspaceMode returns the mode recorded by SetWorkspaceMode, or "".
func (a *Agent) WorkspaceMode() string {
	if mode := a.workspaceMode.Load(); mode != nil {
		return *mode
	}
	return ""
}

// autoActivateModeSkills folds the current mode's skills into the system
// prompt so the agent starts the turn knowing the mode's workflow instead
// of discovering how to load it. Best-effort: a missing or disabled skill is
// logged and the turn proceeds. Skills stay folded after the user switches
// modes — one conversation spans modes, and that history is the point.
func (a *Agent) autoActivateModeSkills() {
	for _, skillID := range modeSkills[a.WorkspaceMode()] {
		if _, err := a.activateSkillByID(skillID); err != nil {
			a.Logger().Debug("workspace mode %q: skill %q not activated: %v\n", a.WorkspaceMode(), skillID, err)
		}
	}
}
