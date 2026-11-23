package configuration

// config_personas.go — custom persona hydration (SP-175).
//
// The embedded catalog is the immutable core and stays the base of
// SubagentTypes. Custom personas are read from files — the person's
// `<config dir>/personas/` and a repository's `<project folder>/.sprout/personas/`
// — and merged in after core. Core entries are never replaced, and a custom
// definition that reuses a core id or alias is skipped and reported.
//
// SubagentTypes stays json:"-": custom personas live in their own files, not
// in config.json.

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// PersonasDirName is the directory under the config dir (and under a
// project folder's .sprout) that holds custom persona files.
const PersonasDirName = "personas"

// PersonaIssue statuses. A persona in any of these states is not usable.
const (
	// PersonaStatusPendingAcceptance is a project persona the person has not
	// accepted, or whose file changed since they accepted it.
	PersonaStatusPendingAcceptance = "pending_acceptance"
	// PersonaStatusShadowed is a custom persona another custom scope
	// overrides. Reported rather than silently dropped.
	PersonaStatusShadowed = "shadowed"
	// PersonaStatusInvalid is a persona that failed to load (malformed JSON,
	// a missing required field, or a collision with a core persona).
	PersonaStatusInvalid = "invalid"
)

// PersonaIssue reports a custom persona that is not usable, with enough
// context to act on it. Issues are derived from disk at load time and are
// never persisted.
type PersonaIssue struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
	File   string `json:"file,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// PersonaIssues are the custom personas that did not load, or loaded but are
// shadowed. Surfaced by `/persona list` so nothing is dropped silently.
type PersonaIssues []PersonaIssue

// UserPersonasDir returns `<config dir>/personas`, the user-scope persona
// directory. SPROUT_CONFIG redirects it, so an isolated run reads the
// isolated directory.
func UserPersonasDir() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, PersonasDirName), nil
}

// ProjectPersonasDir returns `<project folder>/.sprout/personas` for a project folder
// config dir (`<project folder>/.sprout`), or "" when there is no project folder layer.
func ProjectPersonasDir(projectFolderConfigDir string) string {
	if strings.TrimSpace(projectFolderConfigDir) == "" {
		return ""
	}
	return filepath.Join(projectFolderConfigDir, PersonasDirName)
}

// ProjectFolderFromConfigDir turns `<project folder>/.sprout` back into
// `<project folder>`. The acceptance record is keyed by the project folder.
func ProjectFolderFromConfigDir(projectFolderConfigDir string) string {
	if strings.TrimSpace(projectFolderConfigDir) == "" {
		return ""
	}
	return filepath.Dir(projectFolderConfigDir)
}

// ProjectPersonaAcceptances returns the accepted content hashes for one
// project folder, keyed by persona id. A nil map accepts nothing.
func (c *Config) ProjectPersonaAcceptances(projectFolder string) map[string]string {
	if c == nil || len(c.AcceptedPersonas) == 0 || projectFolder == "" {
		return nil
	}
	return c.AcceptedPersonas[projectFolder]
}

// AcceptProjectPersona records that the person accepted a project persona at
// the given content hash. The record is per project folder, so accepting a
// repository's persona does not accept the same id in another checkout.
func (c *Config) AcceptProjectPersona(projectFolder, personaID, hash string) {
	if c == nil || projectFolder == "" || personaID == "" {
		return
	}
	if c.AcceptedPersonas == nil {
		c.AcceptedPersonas = make(map[string]map[string]string)
	}
	accepted := c.AcceptedPersonas[projectFolder]
	if accepted == nil {
		accepted = make(map[string]string)
		c.AcceptedPersonas[projectFolder] = accepted
	}
	accepted[personaID] = hash
}

// HydrateCustomPersonas merges the user- and project-scope persona files into
// cfg.SubagentTypes, after core. `projectFolderConfigDir` is `<project folder>/.sprout`
// (empty when there is no project folder layer), which locates the project scope
// and the project folder the acceptance record is keyed by.
//
// A failure here never stops Sprout from starting: personas that load are
// added, and everything that did not is reported in cfg.PersonaIssues.
func HydrateCustomPersonas(cfg *Config, projectFolderConfigDir string) {
	if cfg == nil {
		return
	}
	userDir, err := UserPersonasDir()
	if err != nil {
		log.Printf("[personas] warning: cannot resolve the user persona directory: %v", err)
	}
	projectDir := ProjectPersonasDir(projectFolderConfigDir)

	if userDir == "" && projectDir == "" {
		return
	}

	core, err := personas.DefaultDefinitions()
	if err != nil {
		log.Printf("[personas] warning: cannot load the core catalog: %v", err)
		return
	}

	projectFolder := ProjectFolderFromConfigDir(projectFolderConfigDir)
	result := personas.LoadCustomPersonas(core, personas.LoadOptions{
		UserDir:               userDir,
		ProjectDir:            projectDir,
		AcceptedProjectHashes: cfg.ProjectPersonaAcceptances(projectFolder),
	})

	if cfg.SubagentTypes == nil {
		cfg.SubagentTypes = defaultSubagentTypes()
	}

	// Drop the file-scope personas a previous hydration added, so a reload
	// that finds a persona gone (deleted, or no longer accepted) removes it
	// instead of leaving a stale entry. Core entries are never touched. A
	// hydration with no project folder scope owns only the user entries: it must
	// not delete project entries another layer contributed.
	ownsProjectScope := strings.TrimSpace(projectFolderConfigDir) != ""
	for id, entry := range cfg.SubagentTypes {
		switch personas.Source(entry.Source) {
		case personas.SourceUser:
			delete(cfg.SubagentTypes, id)
		case personas.SourceProject:
			if ownsProjectScope {
				delete(cfg.SubagentTypes, id)
			}
		}
	}

	for _, custom := range result.Personas {
		def := resolvePersonaPromptPath(custom.Definition, custom.File)
		cfg.SubagentTypes[normalizePersonaID(def.ID)] = SubagentTypeFromDefinition(def, custom.Source)
	}

	cfg.PersonaIssues = nil
	for _, pending := range result.PendingProject {
		cfg.PersonaIssues = append(cfg.PersonaIssues, PersonaIssue{
			ID:     pending.ID,
			Name:   pending.Name,
			Source: string(pending.Source),
			File:   pending.File,
			Status: PersonaStatusPendingAcceptance,
			Detail: "not accepted yet; run `/persona accept " + pending.ID + "`",
		})
	}
	for _, problem := range result.Problems {
		status := PersonaStatusInvalid
		if problem.Kind == personas.ProblemWarning {
			status = PersonaStatusShadowed
		}
		cfg.PersonaIssues = append(cfg.PersonaIssues, PersonaIssue{
			ID:     problem.ID,
			Source: string(problem.Source),
			File:   problem.File,
			Status: status,
			Detail: problem.Message,
		})
	}
	sort.SliceStable(cfg.PersonaIssues, func(i, j int) bool {
		if cfg.PersonaIssues[i].Source != cfg.PersonaIssues[j].Source {
			return cfg.PersonaIssues[i].Source < cfg.PersonaIssues[j].Source
		}
		return cfg.PersonaIssues[i].ID < cfg.PersonaIssues[j].ID
	})
}

// resolvePersonaPromptPath makes a file-scoped persona's `system_prompt`
// absolute when it names a file beside the persona. A path that is not beside
// the persona keeps its meaning: the path of a core prompt, which the agent
// resolves against the embedded prompts. This is the one thing a custom
// persona may borrow from core, and it is text only.
func resolvePersonaPromptPath(def personas.Definition, file string) personas.Definition {
	promptPath := strings.TrimSpace(def.SystemPrompt)
	if promptPath == "" || file == "" || filepath.IsAbs(promptPath) {
		return def
	}
	beside := filepath.Join(filepath.Dir(file), promptPath)
	if _, err := os.Stat(beside); err == nil {
		def.SystemPrompt = beside
	}
	return def
}

// SubagentTypeFromDefinition converts a persona definition into the runtime
// SubagentType, tagging where it came from.
func SubagentTypeFromDefinition(definition personas.Definition, source personas.Source) SubagentType {
	return SubagentType{
		ID:                     normalizePersonaID(definition.ID),
		Name:                   definition.Name,
		Description:            definition.Description,
		Provider:               definition.Provider,
		Model:                  definition.Model,
		SystemPrompt:           definition.SystemPrompt,
		SystemPromptText:       definition.SystemPromptText,
		SystemPromptAppend:     definition.SystemPromptAppend,
		AllowedTools:           append([]string{}, definition.AllowedTools...),
		Aliases:                append([]string{}, definition.Aliases...),
		Enabled:                definition.Enabled,
		LocalOnly:              definition.LocalOnly,
		Delegatable:            definition.Delegatable,
		AutoApproveRules:       convertAutoApproveRules(definition.AutoApproveRules),
		IterationBudget:        definition.IterationBudget,
		TimeBudgetSeconds:      definition.TimeBudgetSeconds,
		ReadOnly:               definition.ReadOnly,
		Capabilities:           append([]string{}, definition.Capabilities...),
		WritePaths:             append([]string{}, definition.WritePaths...),
		HostTools:              append([]string{}, definition.HostTools...),
		Skills:                 append([]string{}, definition.Skills...),
		MCP:                    append([]string{}, definition.MCP...),
		CanSpawnNonDelegatable: append([]string{}, definition.CanSpawnNonDelegatable...),
		Source:                 string(source),
	}
}
