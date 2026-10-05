package configuration

// RoleConfig is the per-role model selection (SP-150 §150a). Either
// field is optional: an empty provider falls back to the
// conversation's last-used provider, and an empty model falls back
// to the resolved provider's configured model — the same fallback
// shape the pre-role settings (commit_model & friends) use today.
type RoleConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// Built-in role names (SP-150 §150a). The set is fixed for now; the
// config's map shape still accepts arbitrary names (the open
// question of user-defined roles), but only these are resolved by
// features.
const (
	RolePlanner    = "planner"    // plan mode and plan edits (SP-148)
	RoleCoder      = "coder"      // the main agent loop
	RoleSummarizer = "summarizer" // progress & change summaries (SP-151, SP-157)
	RoleReviewer   = "reviewer"   // code review
	RoleCommit     = "commit"     // commit message generation
)

// BuiltInRoles returns the built-in role names in a stable order.
func BuiltInRoles() []string {
	return []string{RolePlanner, RoleCoder, RoleSummarizer, RoleReviewer, RoleCommit}
}

// GetRole returns the stored selection for a role, or the zero
// RoleConfig when the role is unset (or the receiver is nil). The
// stored selection is returned without fallback; use ResolveRole for
// the fallback-resolved (provider, model) pair.
func (c *Config) GetRole(name string) RoleConfig {
	if c == nil {
		return RoleConfig{}
	}
	return c.Roles[name]
}

// SetRole stores the per-role selection, creating the map as needed.
// The literal "test" provider is rejected at the Config level, the
// same defense-in-depth as SetModelForProvider, so a test provider
// cannot leak into the persisted config through the roles section.
func (c *Config) SetRole(name string, rc RoleConfig) {
	if c == nil {
		return
	}
	if rc.Provider == "test" {
		return
	}
	if c.Roles == nil {
		c.Roles = make(map[string]RoleConfig)
	}
	c.Roles[name] = rc
}

// roleSelection returns the effective stored selection for a role name
// (SP-150 §150a, item 150.2 — "read as aliases"): the role's explicit
// roles-section entry field-wise merged with the legacy setting that
// aliases the role, expressed as a RoleConfig (zero when neither is set).
// The merge is field-wise, mirroring the legacy getters: each role field
// wins, and a field the role left unset falls back to the corresponding
// legacy alias field (never a whole-pair swap, so a provider-only role
// entry still inherits the legacy model it did not override). The alias
// mapping (a binding decision of item 150.2):
//
//   - coder: the subagent settings (subagent_provider/model — subagents
//     do the coding work). The completion settings are the completion
//     path's own alias (read by GetCompletionProvider/Model, which also
//     fall back to the coder role); they are not part of the general
//     coder resolver, so there is no subagent-vs-completion ordering
//     here.
//   - commit: the commit settings (commit_provider/commit_model).
//   - reviewer: the review settings (review_provider/review_model).
//   - planner, summarizer: no legacy alias; only the roles entry.
//
// This is the role-read direction of the alias — reading a role sees the
// legacy settings. The legacy-read direction (the getters consulting the
// roles section) is implemented field-wise inside the getters. A nil
// receiver returns the zero RoleConfig.
func (c *Config) roleSelection(name string) RoleConfig {
	if c == nil {
		return RoleConfig{}
	}
	entry := c.Roles[name]
	provider := entry.Provider
	model := entry.Model
	switch name {
	case RoleCoder:
		if provider == "" {
			provider = c.SubagentProvider
		}
		if model == "" {
			model = c.SubagentModel
		}
	case RoleCommit:
		if provider == "" {
			provider = c.CommitProvider
		}
		if model == "" {
			model = c.CommitModel
		}
	case RoleReviewer:
		if provider == "" {
			provider = c.ReviewProvider
		}
		if model == "" {
			model = c.ReviewModel
		}
	}
	return RoleConfig{Provider: provider, Model: model}
}

// HasExplicitRole reports whether the named role has an explicit user
// selection — a non-empty roles-section entry or a set legacy alias
// setting — as opposed to the resolver's last-used-provider fallback. An
// unset role (which ResolveRole fills from the last-used provider)
// returns false. Callers use it to gate role-specific behavior (the
// reviewer flow) on an actual user selection rather than on the resolver's
// fallback, which always yields a non-empty provider in a live session.
func (c *Config) HasExplicitRole(name string) bool {
	if c == nil {
		return false
	}
	sel := c.roleSelection(name)
	return sel.Provider != "" || sel.Model != ""
}

// ResolveRole resolves a role to a (provider, model) pair with
// field-wise fallback (SP-150 §150a): an empty provider falls back to
// the conversation's last-used provider, and an empty model falls back
// to the resolved provider's configured model. A role that is unset or
// unknown resolves to exactly the conversation's (provider, model)
// unless a legacy setting aliases it (roleSelection, item 150.2). The
// model may be empty when nothing is configured for the resolved
// provider; callers then offer interactive model selection, the same
// contract as the pre-role getters. A nil receiver resolves to ("","").
func (c *Config) ResolveRole(name string) (string, string) {
	if c == nil {
		return "", ""
	}
	role := c.roleSelection(name) // zero when unset, unknown, and unaliased
	provider := role.Provider
	if provider == "" {
		provider = c.LastUsedProvider
	}
	model := role.Model
	if model == "" {
		model = c.GetModelForProvider(provider)
	}
	return provider, model
}
