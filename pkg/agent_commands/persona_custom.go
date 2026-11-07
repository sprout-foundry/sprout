package commands

// persona_custom.go — the custom-persona half of /persona (SP-175):
// creating a user-scope persona, accepting a project-scope one, and showing
// where a persona came from.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// personaNew creates a user-scope persona file. With `--from <core-id>` the
// core persona's definition is copied once; the file is the creator's from
// then on, and no Sprout release changes what it does.
func (p *PersonaCommand) personaNew(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /persona new <id> [--from <core-id>]")
	}
	id := personas.NormalizeID(args[0])
	if id == "" {
		return fmt.Errorf("/persona new: an id is required")
	}
	if !personas.SafePersonaID(id) {
		return fmt.Errorf("/persona new: %q is not a valid persona id: use letters, digits, and underscores", id)
	}

	from := ""
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--from" && i+1 < len(args):
			from = personas.NormalizeID(args[i+1])
			i++
		case strings.HasPrefix(args[i], "--from="):
			from = personas.NormalizeID(strings.TrimPrefix(args[i], "--from="))
		default:
			return fmt.Errorf("/persona new: unknown argument %q", args[i])
		}
	}

	core, err := personas.DefaultDefinitions()
	if err != nil {
		return fmt.Errorf("load core persona catalog: %w", err)
	}
	if _, taken := core[id]; taken {
		return fmt.Errorf("/persona new: %q is a core persona id; core personas are immutable", id)
	}

	def := personas.Definition{
		ID:          id,
		Name:        strings.ReplaceAll(id, "_", " "),
		Description: "Custom persona",
		Enabled:     true,
	}
	if from != "" {
		source, ok := core[from]
		if !ok {
			return fmt.Errorf("/persona new: --from %q is not a core persona", from)
		}
		def = copyCoreForCustom(source, id)
	}

	dir, err := configuration.UserPersonasDir()
	if err != nil {
		return fmt.Errorf("/persona new: resolve the user persona directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("/persona new: create %s: %w", dir, err)
	}
	path := filepath.Join(dir, id+".json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("/persona new: %s already exists", path)
	}

	data, err := json.MarshalIndent(personas.Catalog{Personas: []personas.Definition{def}}, "", "  ")
	if err != nil {
		return fmt.Errorf("/persona new: encode persona: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("/persona new: write %s: %w", path, err)
	}

	console.GlyphSuccess.Fprintf(p.out(), "Created persona %s", id)
	p.printf("   File: %s\n", path)
	if from != "" {
		p.printf("   Copied from core persona %s. It is yours from now on: no Sprout release changes it.\n", from)
	}
	p.println("   Edit the file, then `/persona list` to see it.")
	return nil
}

// copyCoreForCustom turns a core definition into the starting point for a
// custom persona. The id is the creator's; aliases are dropped because they
// belong to the core persona and would collide. A core prompt file becomes
// the custom persona's `system_prompt` — text only, carrying none of the core
// persona's tools or rules.
func copyCoreForCustom(source personas.Definition, id string) personas.Definition {
	def := source
	def.ID = id
	def.Name = strings.ReplaceAll(id, "_", " ")
	def.Description = "Based on the " + source.Name + " persona"
	def.Aliases = nil
	def.CanSpawnNonDelegatable = nil
	def.Enabled = true
	if strings.TrimSpace(def.SystemPromptText) == "" && strings.TrimSpace(def.SystemPrompt) == "" {
		def.SystemPrompt = source.SystemPrompt
	}
	return def
}

// personaAccept accepts a project-scope persona, recording the file's current
// content hash. Editing the file afterwards withdraws the acceptance.
func (p *PersonaCommand) personaAccept(id string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()
	if configManager == nil {
		return fmt.Errorf("configuration manager is not available")
	}
	config := configManager.GetConfig()
	if config == nil {
		return fmt.Errorf("configuration is not available")
	}

	issue, ok := findPendingPersona(config.PersonaIssues, id)
	if !ok {
		return fmt.Errorf("/persona accept: no project persona %q is waiting to be accepted (see /persona list)", id)
	}
	data, err := os.ReadFile(issue.File)
	if err != nil {
		return fmt.Errorf("/persona accept: read %s: %w", issue.File, err)
	}
	hash := personas.ContentHash(data)

	workspaceRoot := chatAgent.GetWorkspaceRoot()
	if strings.TrimSpace(workspaceRoot) == "" {
		return fmt.Errorf("/persona accept: no workspace root; project personas are unavailable here")
	}
	if err := configManager.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.AcceptProjectPersona(workspaceRoot, issue.ID, hash)
		return nil
	}); err != nil {
		return fmt.Errorf("/persona accept: save config: %w", err)
	}
	if err := configManager.Reload(); err != nil {
		return fmt.Errorf("/persona accept: reload config: %w", err)
	}

	console.GlyphSuccess.Fprintf(p.out(), "Accepted project persona %s", issue.ID)
	p.printf("   Recorded against %s in %s.\n", issue.File, workspaceRoot)
	return nil
}

// findPendingPersona looks up a pending-acceptance issue by id or file.
func findPendingPersona(issues configuration.PersonaIssues, raw string) (configuration.PersonaIssue, bool) {
	needle := personas.NormalizeID(raw)
	for _, issue := range issues {
		if issue.Status != configuration.PersonaStatusPendingAcceptance {
			continue
		}
		if personas.NormalizeID(issue.ID) == needle {
			return issue, true
		}
	}
	return configuration.PersonaIssue{}, false
}

// printPersonaSources renders the source of each persona and the custom
// personas that are not usable, so nothing is dropped silently.
func (p *PersonaCommand) printPersonaSources(config *configuration.Config, chatAgent *agent.Agent) {
	if len(config.PersonaIssues) > 0 {
		p.println("\nCustom personas not in use:")
		for _, issue := range config.PersonaIssues {
			name := issue.ID
			if name == "" {
				name = filepath.Base(issue.File)
			}
			p.printf("- %s (%s, %s): %s\n", name, issue.Source, issue.Status, issue.Detail)
		}
		p.println("  Accept a project persona with: /persona accept <id>")
	}
	if ids := chatAgent.HostPersonaIDs(); len(ids) > 0 {
		p.println("\nHost personas:")
		for _, id := range ids {
			p.printf("- %s (host)\n", id)
		}
	}
}

// personaSourceLabel names where a persona came from, reading an untagged
// entry as core.
func personaSourceLabel(persona configuration.SubagentType) string {
	if strings.TrimSpace(persona.Source) == "" {
		return string(personas.SourceCore)
	}
	return persona.Source
}

// effectiveToolSet returns the tool names the persona is offered, including
// the tools its selectors add. Used by /persona show.
func effectiveToolSet(persona configuration.SubagentType) []string {
	seen := make(map[string]struct{}, len(persona.AllowedTools)+len(persona.HostTools))
	for _, name := range persona.AllowedTools {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	if personas.Source(personaSourceLabel(persona)).IsCustom() {
		for _, name := range persona.HostTools {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				seen[trimmed] = struct{}{}
			}
		}
		if len(persona.Skills) > 0 {
			seen["activate_skill"] = struct{}{}
			seen["list_skills"] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
