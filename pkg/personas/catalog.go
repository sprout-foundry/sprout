package personas

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed configs/*.json
var embeddedPersonas embed.FS

type Catalog struct {
	Personas []Definition `json:"personas"`
}

type Definition struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Provider           string            `json:"provider,omitempty"`
	Model              string            `json:"model,omitempty"`
	SystemPrompt       string            `json:"system_prompt,omitempty"`
	SystemPromptText   string            `json:"system_prompt_text,omitempty"`
	SystemPromptAppend string            `json:"system_prompt_append,omitempty"`
	AllowedTools       []string          `json:"allowed_tools,omitempty"`
	Enabled            bool              `json:"enabled"`
	Aliases            []string          `json:"aliases,omitempty"`
	LocalOnly          bool              `json:"local_only,omitempty"`
	Delegatable        bool              `json:"delegatable,omitempty"`
	AutoApproveRules   *AutoApproveRules `json:"auto_approve_rules,omitempty"`
	// Capabilities is an explicit list of agency grants this persona holds —
	// e.g. CapabilityGitWrite. Replaces the previous practice of inferring
	// capabilities by sniffing AutoApproveRules. AutoApproveRules now means
	// purely "what auto-approves at runtime"; capabilities mean "what this
	// persona is fundamentally allowed to do".
	Capabilities []string `json:"capabilities,omitempty"`
	// CanSpawnNonDelegatable lists otherwise-undelegatable persona IDs that
	// this persona is explicitly permitted to spawn as a subagent. Replaces
	// the implicit "hasEASpawnAuthority" carve-out: instead of detecting
	// EA-class personas by sniffing AutoApproveRules for "subagent_spawn",
	// the catalog now declares the chain directly. The coordinator carries
	// ["orchestrator"] so the canonical coordinator→orchestrator→specialist
	// chain works without special-case code.
	CanSpawnNonDelegatable []string `json:"can_spawn_non_delegatable,omitempty"`
}

// AutoApproveRules mirrors the configuration package's AutoApproveRules
// for JSON deserialization from persona catalog files.
type AutoApproveRules struct {
	LowRiskOps    []string `json:"low_risk,omitempty"`
	MediumRiskOps []string `json:"medium_risk,omitempty"`
	HighRiskNever []string `json:"high_risk_never,omitempty"`
}

var (
	defaultsOnce sync.Once
	defaultsErr  error
	defaults     map[string]Definition
)

// DefaultDefinitions returns the merged built-in persona definitions from embedded JSON files.
func DefaultDefinitions() (map[string]Definition, error) {
	defaultsOnce.Do(func() {
		defaults, defaultsErr = loadEmbeddedDefinitions()
		if defaultsErr != nil {
			defaults = fallbackDefinitions()
		}
	})

	if defaultsErr != nil {
		return cloneDefinitions(defaults), fmt.Errorf("failed to load default persona definitions: %w", defaultsErr)
	}
	return cloneDefinitions(defaults), nil
}

func loadEmbeddedDefinitions() (map[string]Definition, error) {
	return loadDefinitionsFromFS(embeddedPersonas, "configs")
}

// loadDefinitionsFromFS loads and merges persona Definitions from every .json
// file in the named directory of the given fs.FS. Exposed as a separate
// function so tests can drive the conflict-detection paths with a fake FS.
func loadDefinitionsFromFS(fsys fs.FS, dir string) (map[string]Definition, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read persona configs from %s: %w", dir, err)
	}

	merged := make(map[string]Definition)
	// Track which file declared each ID/alias so conflict messages point
	// the developer at the offending pair rather than silently overwriting.
	idSources := make(map[string]string)    // normalized id → filename that declared it
	aliasSources := make(map[string]string) // normalized alias → "filename:owner-id"

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filename := filepath.Join(dir, entry.Name())
		data, err := fs.ReadFile(fsys, filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read persona config %s: %w", filename, err)
		}

		var catalog Catalog
		if err := json.Unmarshal(data, &catalog); err != nil {
			return nil, fmt.Errorf("failed to parse persona config %s: %w", filename, err)
		}

		for _, persona := range catalog.Personas {
			id := normalizeID(persona.ID)
			if id == "" {
				return nil, fmt.Errorf("persona in %s has empty id", filename)
			}
			if prior, exists := idSources[id]; exists {
				return nil, fmt.Errorf("duplicate persona id %q: declared in both %s and %s", id, prior, filename)
			}
			// An alias from another persona must not shadow this ID.
			if prior, exists := aliasSources[id]; exists {
				return nil, fmt.Errorf("persona id %q in %s is shadowed by alias declared at %s", id, filename, prior)
			}
			persona.ID = id
			merged[id] = persona
			idSources[id] = filename

			for _, alias := range persona.Aliases {
				normalizedAlias := normalizeID(alias)
				if normalizedAlias == "" {
					continue
				}
				if normalizedAlias == id {
					continue // self-alias is harmless
				}
				if prior, exists := idSources[normalizedAlias]; exists {
					return nil, fmt.Errorf("alias %q on persona %q (%s) shadows persona id declared at %s",
						normalizedAlias, id, filename, prior)
				}
				if prior, exists := aliasSources[normalizedAlias]; exists {
					return nil, fmt.Errorf("alias %q is declared twice: %s and %s:%s",
						normalizedAlias, prior, filename, id)
				}
				aliasSources[normalizedAlias] = filename + ":" + id
			}
		}
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("no personas found in embedded persona configs")
	}

	return merged, nil
}

func normalizeID(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func cloneDefinitions(src map[string]Definition) map[string]Definition {
	out := make(map[string]Definition, len(src))
	for id, def := range src {
		defCopy := def
		defCopy.AllowedTools = append([]string{}, def.AllowedTools...)
		defCopy.Aliases = append([]string{}, def.Aliases...)
		out[id] = defCopy
	}
	return out
}

// fallbackDefinitions returns a minimal hardcoded persona set used only when
// the embedded catalog JSON fails to load. Keep this in sync with the
// canonical catalog (capabilities, spawn permissions) so that a fallback boot
// doesn't silently lose authority — e.g., without CapabilityGitWrite here,
// the fallback orchestrator can't commit even if the user opts in.
func fallbackDefinitions() map[string]Definition {
	return map[string]Definition{
		IDOrchestrator: {
			ID:           IDOrchestrator,
			Name:         "Orchestrator",
			Description:  "Primary orchestration persona",
			AllowedTools: []string{"shell_command", "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file", "search_files", "web_search", "fetch_url", "run_subagent", "run_parallel_subagents", "view_history", "rollback_changes", "list_skills", "activate_skill", "TodoWrite", "TodoRead", "manage_memory"},
			Enabled:      true,
			Capabilities: []string{CapabilityGitWrite},
		},
		IDGeneral: {
			ID:           IDGeneral,
			Name:         "General",
			Description:  "General-purpose persona",
			SystemPrompt: "pkg/agent/prompts/subagent_prompts/general.md",
			AllowedTools: []string{"shell_command", "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file", "search_files", "list_skills", "activate_skill", "TodoWrite", "TodoRead"},
			Enabled:      true,
		},
	}
}
