package configuration

// GetRefreshSystemPromptOnModelChange (Spec B) reports whether the agent
// should re-derive its system prompt on every provider/model swap
// instead of freezing it at agent-creation time.
//
// When false (the default), the prompt is set once in
// initAgentFromResolvedProvider and never touched on subsequent
// SetProvider/SetModel calls. This preserves bit-identical behavior for
// existing sessions that may have observed and relied on a particular
// prompt composition.
//
// When true, the agent's refreshSystemPrompt() (called from setClient)
// re-runs GetEmbeddedSystemPromptForProfile against the active provider
// and context window. The configured SystemPromptText override still
// wins — only the embedded portion is re-derived.
//
// This getter exists alongside the field so call sites read through a
// stable accessor (and so future migrations can change the storage
// shape without touching every read site). A nil-safe return of false
// matches the field's zero-value default and keeps bare Config literals
// in tests working without explicit initialization.
func (c *Config) GetRefreshSystemPromptOnModelChange() bool {
	if c == nil {
		return false
	}
	return c.RefreshSystemPromptOnModelChange
}
