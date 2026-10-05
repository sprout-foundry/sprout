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

// ResolveRole resolves a role to a (provider, model) pair with
// field-wise fallback (SP-150 §150a): an empty provider falls back to
// the conversation's last-used provider, and an empty model falls back
// to the resolved provider's configured model. A role that is unset or
// unknown resolves to exactly the conversation's (provider, model).
// The model may be empty when nothing is configured for the resolved
// provider; callers then offer interactive model selection, the same
// contract as the pre-role getters. A nil receiver resolves to ("","").
func (c *Config) ResolveRole(name string) (string, string) {
	if c == nil {
		return "", ""
	}
	role := c.Roles[name] // zero RoleConfig when unset or unknown
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
