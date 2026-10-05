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
// roles-section entry when set, otherwise the legacy setting that aliases
// the role, expressed as a RoleConfig (zero when neither is set). The
// alias mapping (a binding decision of item 150.2):
//
//   - coder: the subagent settings (subagent_provider/model — subagents
//     do the coding work), then the completion settings (inline
//     completion is code generation and the built-in set has no
//     completion-specific role). Subagent precedes completion: it is the
//     more general setting.
//   - commit: the commit settings (commit_provider/commit_model).
//   - reviewer: the review settings (review_provider/review_model).
//   - planner, summarizer: no legacy alias; only the roles entry.
//
// An explicit roles-section entry (any non-empty field) always wins over
// the legacy alias, so the roles section stays the authoritative source.
// This is the role-read direction of the alias — reading a role sees the
// legacy settings. The legacy-read direction (the getters consulting the
// roles section) is implemented field-wise inside the getters. A nil
// receiver returns the zero RoleConfig.
func (c *Config) roleSelection(name string) RoleConfig {
	if c == nil {
		return RoleConfig{}
	}
	if entry := c.Roles[name]; entry.Provider != "" || entry.Model != "" {
		return entry
	}
	switch name {
	case RoleCoder:
		if c.SubagentProvider != "" || c.SubagentModel != "" {
			return RoleConfig{Provider: c.SubagentProvider, Model: c.SubagentModel}
		}
		if c.CompletionProvider != "" || c.CompletionModel != "" {
			return RoleConfig{Provider: c.CompletionProvider, Model: c.CompletionModel}
		}
	case RoleCommit:
		if c.CommitProvider != "" || c.CommitModel != "" {
			return RoleConfig{Provider: c.CommitProvider, Model: c.CommitModel}
		}
	case RoleReviewer:
		if c.ReviewProvider != "" || c.ReviewModel != "" {
			return RoleConfig{Provider: c.ReviewProvider, Model: c.ReviewModel}
		}
	}
	return RoleConfig{}
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
