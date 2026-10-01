//go:build !js

package workflow

// loader_normalize.go — workflow field-normalization + subagent-override
// helpers, split from loader.go. These normalize individual config fields
// (reasoning effort, when, paths, persona IDs), validate small leaf fields,
// and apply subagent-type overrides; they are the helpers around the big
// AgentWorkflowConfig.Validate().
import (
	"log"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func NormalizeReasoningEffort(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return ""
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	default:
		return ""
	}
}

func NormalizeWorkflowWhen(v string) string {
	trimmed := strings.TrimSpace(strings.ToLower(v))
	if trimmed == "" {
		return WorkflowWhenAlways
	}
	return trimmed
}

func NormalizeWorkflowPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

// NormalizeWorkflowPersonaID normalizes a persona ID the same way config.go does.
func NormalizeWorkflowPersonaID(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// FindSubagentTypeMapKey finds the original map key in SubagentTypes matching the
// given normalized persona ID. It mirrors the lookup logic in config.go GetSubagentType.
func FindSubagentTypeMapKey(subagentTypes map[string]configuration.SubagentType, normalizedID string) (string, bool) {
	for key, st := range subagentTypes {
		if NormalizeWorkflowPersonaID(key) == normalizedID {
			return key, true
		}
		for _, alias := range st.Aliases {
			if NormalizeWorkflowPersonaID(alias) == normalizedID {
				return key, true
			}
		}
	}
	return "", false
}

// ApplyWorkflowSubagentOverrides patches the SubagentTypes map entries matching
// the given overrides. No error is returned for unknown personas — they are skipped.
// Log lines are emitted for every skip and every successful apply so that silent
// divergence between the workflow JSON and the actual SubagentTypes is visible.
func ApplyWorkflowSubagentOverrides(subagentTypes map[string]configuration.SubagentType, overrides WorkflowSubagentOverrides) {
	for personaID, override := range overrides {
		if override.Provider == "" && override.Model == "" {
			log.Printf("[workflow] subagent_overrides: empty override for %q — both provider and model are empty; nothing to apply", personaID)
			continue
		}
		normalizedID := NormalizeWorkflowPersonaID(personaID)
		if normalizedID == "" {
			continue
		}
		mapKey, found := FindSubagentTypeMapKey(subagentTypes, normalizedID)
		if !found {
			log.Printf("[workflow] subagent_overrides: unknown persona %q — no matching SubagentTypes entry or alias; override ignored (provider=%s model=%s)", personaID, override.Provider, override.Model)
			continue
		}
		st := subagentTypes[mapKey]
		if !st.Enabled {
			log.Printf("[workflow] subagent_overrides: disabled persona %q — enabled=false; override ignored (provider=%s model=%s)", personaID, override.Provider, override.Model)
			continue
		}
		if override.Provider != "" {
			st.Provider = override.Provider
		}
		if override.Model != "" {
			st.Model = override.Model
		}
		subagentTypes[mapKey] = st
		log.Printf("[workflow] subagent_overrides applied: persona %q → provider=%s model=%s", personaID, st.Provider, st.Model)
	}
}

func IsValidWorkflowWhen(v string) bool {
	switch v {
	case WorkflowWhenAlways, WorkflowWhenOnSuccess, WorkflowWhenOnError:
		return true
	default:
		return false
	}
}
