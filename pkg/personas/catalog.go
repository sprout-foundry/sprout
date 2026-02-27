package personas

import (
	"embed"
	"encoding/json"
	"fmt"
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
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Provider         string   `json:"provider,omitempty"`
	Model            string   `json:"model,omitempty"`
	SystemPrompt     string   `json:"system_prompt,omitempty"`
	SystemPromptText string   `json:"system_prompt_text,omitempty"`
	AllowedTools     []string `json:"allowed_tools,omitempty"`
	Enabled          bool     `json:"enabled"`
	Aliases          []string `json:"aliases,omitempty"`
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

	return cloneDefinitions(defaults), defaultsErr
}

func loadEmbeddedDefinitions() (map[string]Definition, error) {
	entries, err := embeddedPersonas.ReadDir("configs")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded persona configs: %w", err)
	}

	merged := make(map[string]Definition)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filename := filepath.Join("configs", entry.Name())
		data, err := embeddedPersonas.ReadFile(filename)
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
			persona.ID = id
			merged[id] = persona
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

func fallbackDefinitions() map[string]Definition {
	return map[string]Definition{
		"orchestrator": {
			ID:           "orchestrator",
			Name:         "Orchestrator",
			Description:  "Primary orchestration persona",
			AllowedTools: []string{"shell_command", "read_file", "write_file", "edit_file", "search_files", "web_search", "fetch_url", "run_subagent", "run_parallel_subagents", "TodoWrite", "TodoRead"},
			Enabled:      true,
		},
		"general": {
			ID:           "general",
			Name:         "General",
			Description:  "General-purpose persona",
			SystemPrompt: "pkg/agent/prompts/subagent_prompts/general.md",
			AllowedTools: []string{"shell_command", "read_file", "write_file", "edit_file", "search_files", "TodoWrite", "TodoRead"},
			Enabled:      true,
		},
	}
}
