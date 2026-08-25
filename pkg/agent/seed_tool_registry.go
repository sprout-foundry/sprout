// Package agent: seed ToolRegistry construction and registration of all sprout tools.
package agent

import (
	"fmt"
	"time"

	core "github.com/sprout-foundry/seed/core"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// NewSeedToolRegistry creates a seed core.ToolRegistry with all sprout tools registered.
func NewSeedToolRegistry(agent *Agent) *core.ToolRegistry {
	var ep core.EventPublisher
	if agent != nil && agent.GetEventBus() != nil {
		ep = newRichEventPublisher(agent.GetEventBus(), agent)
	}

	return newSeedToolRegistryWithPublisher(agent, ep)
}

// newSeedToolRegistryWithPublisher creates a seed ToolRegistry using the provided EventPublisher.
func newSeedToolRegistryWithPublisher(agent *Agent, ep core.EventPublisher) *core.ToolRegistry {
	registry := core.NewToolRegistry(core.ToolRegistryOptions{
		DefaultTimeout: 5 * time.Minute,
		MaxResultSize:  50 * 1024,
		EventPublisher: ep,
		PreExecuteHook: newPreExecuteHook(agent),
	})

	// Build the LCM tool allowlist set (if applicable).
	var allowSet map[string]struct{}
	if agent != nil && len(agent.contextProfile.ToolAllowlist) > 0 {
		allowSet = make(map[string]struct{}, len(agent.contextProfile.ToolAllowlist))
		for _, name := range agent.contextProfile.ToolAllowlist {
			allowSet[name] = struct{}{}
		}
	}

	for _, h := range tools.GetNewToolRegistry().All() {
		if h.Definition().RequiresEmbeddings && (agent == nil || agent.GetEmbeddingManager() == nil) {
			continue
		}
		if agent != nil {
			// LCM tool allowlist: skip tools not in the curated set.
			if allowSet != nil {
				if _, ok := allowSet[h.Name()]; !ok {
					continue
				}
			}
			if h.Name() == "run_parallel_subagents" {
				if agent.contextProfile.Mode == configuration.ContextModeLowContext || !agent.CanSpawnSubagents() {
					continue
				}
			}
			if h.Name() == "run_subagent" && !agent.CanSpawnSubagents() {
				continue
			}
		}
		if err := registry.Register(convertHandlerToSeedToolConfig(h, agent)); err != nil {
			panic(fmt.Sprintf("seed registry: failed to register %q: %v", h.Name(), err))
		}
	}

	return registry
}
