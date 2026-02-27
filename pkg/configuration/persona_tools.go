package configuration

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

var (
	personaToolNamesOnce sync.Once
	personaToolNames     map[string]struct{}
)

func getKnownPersonaToolNames() map[string]struct{} {
	personaToolNamesOnce.Do(func() {
		known := make(map[string]struct{})
		for _, tool := range api.GetToolDefinitions() {
			name := strings.TrimSpace(tool.Function.Name)
			if name != "" {
				known[name] = struct{}{}
			}
		}

		// Runtime registry tools not included in api.GetToolDefinitions().
		known["git"] = struct{}{}
		known["run_parallel_subagents"] = struct{}{}
		known["self_review"] = struct{}{}
		known["list_skills"] = struct{}{}
		known["activate_skill"] = struct{}{}

		personaToolNames = known
	})

	return personaToolNames
}

// UnknownPersonaTools returns unknown tool names from a configured persona allowlist.
func UnknownPersonaTools(toolNames []string) []string {
	known := getKnownPersonaToolNames()
	unknownSet := make(map[string]struct{})

	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		// Allow direct MCP tool names (mcp_<server>_<tool>) in addition to mcp_tools.
		if strings.HasPrefix(trimmed, "mcp_") {
			continue
		}
		if _, ok := known[trimmed]; !ok {
			unknownSet[trimmed] = struct{}{}
		}
	}

	if len(unknownSet) == 0 {
		return nil
	}

	unknown := make([]string, 0, len(unknownSet))
	for name := range unknownSet {
		unknown = append(unknown, name)
	}
	sort.Strings(unknown)
	return unknown
}

func warnUnknownPersonaTools(subagentTypes map[string]SubagentType) {
	for id, persona := range subagentTypes {
		unknown := UnknownPersonaTools(persona.AllowedTools)
		if len(unknown) == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr,
			"WARNING: persona %q has unknown allowed_tools entries: %s\n",
			id,
			strings.Join(unknown, ", "),
		)
	}
}
