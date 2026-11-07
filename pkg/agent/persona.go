package agent

import (
	"fmt"
	"sort"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/injection"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

func (a *Agent) GetActivePersona() string {
	return normalizeAgentPersonaID(a.state.GetActivePersona())
}

func (a *Agent) ClearActivePersona() {
	a.state.SetActivePersona("")
	if strings.TrimSpace(a.baseSystemPrompt) != "" {
		a.systemPrompt = a.baseSystemPrompt
	}
}

func (a *Agent) ApplyPersona(personaID string) error {
	personaID = normalizeAgentPersonaID(personaID)
	if a.configManager == nil {
		return agenterrors.NewConfig(fmt.Sprintf("agent configuration manager is not available for persona %q", personaID), nil)
	}

	config := a.configManager.GetConfig()
	if config == nil {
		return agenterrors.NewConfig(fmt.Sprintf("agent configuration is not available for persona %q", personaID), nil)
	}

	persona := a.resolvePersona(personaID)
	if persona == nil {
		available := a.GetAvailablePersonaIDs()
		if len(available) == 0 {
			return agenterrors.NewNotFound(fmt.Sprintf("persona %s (no enabled personas configured)", personaID))
		}
		return agenterrors.NewNotFound(fmt.Sprintf("persona %s (available personas: %s)", personaID, strings.Join(available, ", ")))
	}
	if canonical := normalizeAgentPersonaID(persona.ID); canonical != "" {
		personaID = canonical
	}

	// Computer use is gated for every persona that holds the capability, not
	// only the core computer_user: a custom persona cannot grant itself past
	// the activation gates (SP-175 decision 4).
	wantsComputerUse := personaID == personas.IDComputerUser ||
		persona.HasCapability(personas.CapabilityComputerUse)
	if wantsComputerUse {
		if err := a.checkComputerUseActivation(); err != nil {
			return err
		}
	}

	if strings.TrimSpace(persona.Provider) != "" {
		providerType, err := a.configManager.MapStringToClientType(strings.TrimSpace(persona.Provider))
		if err != nil {
			return agenterrors.NewConfig(fmt.Sprintf("invalid persona provider %q", persona.Provider), err)
		}
		if providerType != a.getClientType() {
			if err := a.SetProvider(providerType); err != nil {
				return agenterrors.NewConfig(fmt.Sprintf("failed switching to persona provider %q", persona.Provider), err)
			}
		}
	}

	if model := strings.TrimSpace(persona.Model); model != "" {
		if err := a.SetModel(model); err != nil {
			return agenterrors.NewConfig(fmt.Sprintf("failed setting persona model %q", model), err)
		}
	}

	if promptText := strings.TrimSpace(persona.SystemPromptText); promptText != "" {
		a.SetSystemPrompt(promptText)
	} else if promptPath := strings.TrimSpace(persona.SystemPrompt); promptPath != "" {
		if err := a.SetSystemPromptFromFile(promptPath); err != nil {
			return agenterrors.NewConfig(fmt.Sprintf("failed loading persona system prompt %q", promptPath), err)
		}
	}

	if appendText := strings.TrimSpace(persona.SystemPromptAppend); appendText != "" {
		current := a.GetSystemPrompt()
		if strings.TrimSpace(current) != "" {
			a.SetSystemPrompt(current + "\n\n---\n\n" + appendText)
		} else {
			a.SetSystemPrompt(appendText)
		}
	}

	if personaID == personas.IDOrchestrator {
		if policy := strings.TrimSpace(orchestratorGitPolicyAppend); policy != "" {
			current := a.GetSystemPrompt()
			if strings.TrimSpace(current) != "" {
				a.SetSystemPrompt(current + "\n\n---\n\n" + policy)
			} else {
				a.SetSystemPrompt(policy)
			}
		}
	}

	// Applying a persona that does not hold the computer-use capability
	// releases the global pointer. Read the id only after the swap, so the
	// check is against the persona actually being applied.
	a.state.SetActivePersona(personaID)

	if wantsComputerUse {
		SetActiveComputerUseAgent(a)

		a.PublishAgentMessage("warning", computerUseWarning(), nil)
	} else {
		SetActiveComputerUseAgent(nil)
	}

	if a.subagentDepth == 0 {
		a.rootPersonaID = personaID
	}

	a.MergeEventMetadata(map[string]interface{}{
		"subagent_depth": a.subagentDepth,
		"active_persona": personaID,
		// persona_source says where the definition came from, so a host can
		// tell its own personas from Sprout's and from a repository's
		// (SP-175). An untagged entry is a core persona.
		"persona_source": personaSourceOf(persona),
	})

	return nil
}

// personaSourceOf reads a persona's source, treating an untagged entry as
// core.
func personaSourceOf(persona *configuration.SubagentType) string {
	if persona == nil || strings.TrimSpace(persona.Source) == "" {
		return string(personas.SourceCore)
	}
	return persona.Source
}

// getActivePersonaToolAllowlist returns the concrete tool names the active
// persona declares, or nil when it declares none. For a custom persona this
// is `allowed_tools` plus the host tools its `host_tools` selects plus the
// skill tools when `skills` is set (SP-175); a core persona's list is
// returned exactly as declared.
func (a *Agent) getActivePersonaToolAllowlist() []string {
	roster := a.activePersonaRoster()
	if roster == nil {
		return nil
	}
	return roster.names()
}

// activePersonaRoster is the tool set the active persona declares, with its
// selectors resolved. Nil means the persona places no restriction on the
// roster (no active persona, or one that names no tools).
//
// `allowed_tools` is the base set. For a custom persona it is extended by
// host tools matched through `host_tools` and, when `skills` is set, the two
// skill tools — a name in either is offered without also appearing in
// `allowed_tools`. MCP tools are not part of this set: they are filtered
// separately by personaMCPAllows, because they never enter the registry.
func (a *Agent) activePersonaRoster() *personaRoster {
	persona := a.activePersonaConfig()
	if persona == nil {
		return nil
	}

	base := trimmedNonEmpty(persona.AllowedTools)
	custom := isCustomPersonaSource(persona.Source)
	if !custom {
		// Core personas: exactly as declared, and never extended.
		if len(base) == 0 {
			return nil
		}
		return &personaRoster{tools: toNameSet(base)}
	}

	extra := make([]string, 0, 4)
	for _, name := range persona.HostTools {
		trimmed := strings.TrimSpace(name)
		switch trimmed {
		case "":
			continue
		case "*":
			extra = append(extra, tools.RegisteredHostToolNames()...)
		default:
			extra = append(extra, trimmed)
		}
	}
	if len(persona.Skills) > 0 {
		extra = append(extra, "activate_skill", "list_skills")
	}

	if len(base) == 0 {
		// Empty allowed_tools means the default agent's tools. The additive
		// selectors cannot narrow that, so there is no restriction to apply.
		return nil
	}

	set := toNameSet(base)
	for _, name := range extra {
		set[name] = struct{}{}
	}
	return &personaRoster{tools: set}
}

// personaRoster is a resolved tool-name set.
type personaRoster struct {
	tools map[string]struct{}
}

func (r *personaRoster) names() []string {
	if r == nil || len(r.tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// personaMCPAllows reports whether the active persona may use the named MCP
// tool. Core personas keep today's behaviour — the tool is offered when
// allowed_tools names it — so this returns true for them. A custom persona is
// limited to its `mcp` selectors, and "absent: none" means no MCP tool at
// all (SP-175).
func (a *Agent) personaMCPAllows(server, tool string) bool {
	persona := a.activePersonaConfig()
	if persona == nil || !isCustomPersonaSource(persona.Source) {
		return true
	}
	return mcpSelectorAllows(persona.MCP, server, tool)
}

// mcpSelectorAllows matches an MCP tool against a persona's `mcp` selectors:
// "<server>" covers every tool on that server, "<server>__<tool>" one tool,
// and "*" everything. Matching is the SP-170 whole-segment rule, so the same
// entry means the same thing in a persona and in approved_mcp_tools.
func mcpSelectorAllows(selectors []string, server, tool string) bool {
	if server == "" || tool == "" {
		return false
	}
	for _, raw := range selectors {
		selector := strings.TrimSpace(raw)
		if selector == "" {
			continue
		}
		if selector == "*" {
			return true
		}
		if injection.AllowlistedMCPTool([]string{selector}, server, tool) {
			return true
		}
	}
	return false
}

// isCustomPersonaSource reports whether a persona came from a custom scope.
// An empty source is a core entry built before sources were tracked.
func isCustomPersonaSource(source string) bool {
	switch personas.Source(strings.TrimSpace(source)) {
	case personas.SourceHost, personas.SourceUser, personas.SourceProject:
		return true
	default:
		return false
	}
}

func trimmedNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func toNameSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func normalizeAgentPersonaID(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// activePersonaConfig resolves the active persona, host scope first. Nil when
// no persona is active.
func (a *Agent) activePersonaConfig() *configuration.SubagentType {
	if a == nil || a.state == nil {
		return nil
	}
	personaID := normalizeAgentPersonaID(a.state.GetActivePersona())
	if personaID == "" {
		return nil
	}
	return a.resolvePersona(personaID)
}

// resolvePersona resolves a persona id or alias: host-scope definitions
// first, then the configuration (core, then the custom scopes hydrated into
// it). Returns a copy, so callers cannot mutate the definition.
func (a *Agent) resolvePersona(personaID string) *configuration.SubagentType {
	personaID = normalizeAgentPersonaID(personaID)
	if personaID == "" {
		return nil
	}
	if persona := a.hostPersona(personaID); persona != nil {
		return persona
	}
	if a.configManager == nil {
		return nil
	}
	config := a.configManager.GetConfig()
	if config == nil {
		return nil
	}
	return config.GetSubagentType(personaID)
}

// hostPersona resolves a host-scope definition by id or alias.
func (a *Agent) hostPersona(personaID string) *configuration.SubagentType {
	a.hostPersonasMu.RLock()
	defer a.hostPersonasMu.RUnlock()
	if len(a.hostPersonas) == 0 {
		return nil
	}
	personaID = normalizeAgentPersonaID(personaID)
	if persona, ok := a.hostPersonas[personaID]; ok {
		copied := persona
		return &copied
	}
	for _, persona := range a.hostPersonas {
		for _, alias := range persona.Aliases {
			if normalizeAgentPersonaID(alias) == personaID {
				copied := persona
				return &copied
			}
		}
	}
	return nil
}

// DefinePersona registers a host-scope persona definition, replacing any
// earlier definition with the same id. The host is the creator, so the
// definition is trusted; it still may not reuse a core persona's id or alias,
// because core personas are immutable (SP-175). An invalid definition is
// rejected with every reason, and nothing is registered.
func (a *Agent) DefinePersona(def personas.Definition) error {
	if a == nil {
		return agenterrors.NewInvalidInputError("agent is nil", nil)
	}
	core, err := personas.DefaultDefinitions()
	if err != nil {
		return agenterrors.NewConfig("load core persona catalog", err)
	}
	if problems, _ := personas.ValidateDefinition(core, def); len(problems) > 0 {
		return agenterrors.NewValidation("invalid persona definition: "+strings.Join(problems, "; "), nil)
	}

	def.ID = personas.NormalizeID(def.ID)
	def.Name = strings.TrimSpace(def.Name)
	def.Description = strings.TrimSpace(def.Description)

	st := configuration.SubagentTypeFromDefinition(def, personas.SourceHost)
	st.Enabled = true

	a.hostPersonasMu.Lock()
	defer a.hostPersonasMu.Unlock()
	if a.hostPersonas == nil {
		a.hostPersonas = make(map[string]configuration.SubagentType)
	}
	a.hostPersonas[st.ID] = st
	return nil
}

// ApplyPersonaDefinition registers a host-scope definition and applies it to
// this agent, the headless API's "create an agent as this persona" path.
func (a *Agent) ApplyPersonaDefinition(def personas.Definition) error {
	if err := a.DefinePersona(def); err != nil {
		return err
	}
	return a.ApplyPersona(def.ID)
}

// HostPersonaIDs returns the ids of the host-scope personas defined on this
// agent, sorted.
func (a *Agent) HostPersonaIDs() []string {
	a.hostPersonasMu.RLock()
	defer a.hostPersonasMu.RUnlock()
	ids := make([]string, 0, len(a.hostPersonas))
	for id := range a.hostPersonas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a *Agent) GetAvailablePersonaIDs() []string {
	if a.configManager == nil {
		return nil
	}
	config := a.configManager.GetConfig()
	if config == nil || config.SubagentTypes == nil {
		return nil
	}

	isLocal := a.IsLocalMode()

	personaIDs := make([]string, 0, len(config.SubagentTypes)+len(a.HostPersonaIDs()))
	for id, persona := range config.SubagentTypes {
		if !persona.Enabled {
			continue
		}
		// Filter out user-disabled personas (DisabledPersonas takes precedence
		// over the catalog's Enabled flag).
		if config.IsPersonaDisabled(id) {
			continue
		}
		// Filter out LocalOnly personas in cloud mode
		if persona.LocalOnly && !isLocal {
			continue
		}
		personaIDs = append(personaIDs, id)
	}
	// Host-scope personas are the host's own definitions, so the user's
	// DisabledPersonas does not apply to them (SP-175). A host persona that
	// shares an id with a user or project persona shadows it, so the id is
	// listed once.
	seen := make(map[string]struct{}, len(personaIDs))
	for _, id := range personaIDs {
		seen[id] = struct{}{}
	}
	for _, id := range a.HostPersonaIDs() {
		if _, dup := seen[id]; dup {
			continue
		}
		if persona := a.hostPersona(id); persona != nil && persona.LocalOnly && !isLocal {
			continue
		}
		personaIDs = append(personaIDs, id)
	}
	sort.Strings(personaIDs)
	return personaIDs
}

func (a *Agent) GetPersonaProviderModel(personaID string) (string, string, error) {
	personaID = normalizeAgentPersonaID(personaID)
	if a.configManager == nil {
		return "", "", agenterrors.NewPermanentError("configuration manager is not available", nil)
	}
	config := a.configManager.GetConfig()
	if config == nil {
		return "", "", agenterrors.NewPermanentError("configuration is not available", nil)
	}
	persona := a.resolvePersona(personaID)
	if persona == nil {
		return "", "", agenterrors.NewNotFound(fmt.Sprintf("persona %s", personaID))
	}

	// Resolve provider: persona → config.SubagentProvider → parent runtime provider.
	provider := strings.TrimSpace(persona.Provider)
	if provider == "" {
		provider = strings.TrimSpace(config.SubagentProvider)
	}
	if provider == "" {
		provider = a.parentRuntimeProvider()
	}

	// Resolve model: persona → config.SubagentModel → provider default → current model.
	model := strings.TrimSpace(persona.Model)
	if model == "" {
		model = strings.TrimSpace(config.SubagentModel)
	}
	if model == "" {
		if providerType, err := a.configManager.MapStringToClientType(provider); err == nil {
			model = a.configManager.GetModelForProvider(providerType)
		}
	}
	if model == "" {
		model = a.GetModel()
	}

	return provider, model, nil
}

// parentRuntimeProvider returns the parent agent's effective provider key.
func (a *Agent) parentRuntimeProvider() string {
	if p := strings.TrimSpace(string(a.getClientType())); p != "" {
		return p
	}
	return strings.TrimSpace(a.GetProvider())
}

func (a *Agent) GetAvailableToolNames() []string {
	tools := a.getOptimizedToolDefinitions(nil)
	if len(tools) == 0 {
		tools = BuildToolDefinitionsForAgent(a)
	}

	names := make([]string, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (a *Agent) isGitWriteAllowed() bool {
	persona := a.activePersonaConfig()
	if persona == nil {
		return false
	}
	return persona.HasCapability(personas.CapabilityGitWrite)
}

func (a *Agent) canSpawnNonDelegatable(target string) bool {
	spawner := a.activePersonaConfig()
	if spawner == nil {
		return false
	}
	normalizedTarget := normalizeAgentPersonaID(target)
	for _, allowed := range spawner.CanSpawnNonDelegatable {
		if normalizeAgentPersonaID(allowed) == normalizedTarget {
			return true
		}
	}
	return false
}

func computerUseWarning() string {
	panicKey := "The panic key is off or unavailable (see the startup log)."
	if chord := computer_use.ArmedPanicChord(); chord != "" {
		panicKey = fmt.Sprintf("Press %s to halt computer-use actions.", strings.ToUpper(chord))
	}
	return "⚠  COMPUTER USE ACTIVE — The agent can now control your mouse, keyboard, and screen. Watch the screen. Stop the agent (Ctrl+C) if it does something unexpected. " +
		panicKey + " Per-session opt-in and destructive-app blocking are NOT yet implemented."
}
