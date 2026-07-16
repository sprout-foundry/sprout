// Package agent: seed ToolRegistry construction and registration of all sprout
// tools. (split from seed_tool_registry.go)
package agent

import (
	"fmt"
	"time"

	core "github.com/sprout-foundry/seed/core"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// NewSeedToolRegistry creates a seed core.ToolRegistry with all 30 sprout tools
// registered. The registry implements core.ToolExecutor directly, so it can be
// used as the Executor in core.Options.
//
// Seed's ToolRegistry handles: channel suffix stripping, alias resolution,
// argument parsing/repair, type coercion, required parameter validation,
// per-tool timeouts, result truncation, circuit breakers, parallel execution
// for SafeForParallel tools, and event publishing.
//
// Sprout-specific concerns are wired through:
//   - PreExecuteHook: security classification + subagent nesting prevention
//   - Handler closures: capture agent for sprout's (ctx, agent, args) signature
//     and apply all post-processing (constraints, truncation, secret redaction,
//     duplicate embedding check, TodoWrite events, error sanitization).
func NewSeedToolRegistry(agent *Agent) *core.ToolRegistry {
	var ep core.EventPublisher
	if agent != nil && agent.GetEventBus() != nil {
		ep = newRichEventPublisher(agent.GetEventBus(), agent)
	}

	return newSeedToolRegistryWithPublisher(agent, ep)
}

// newSeedToolRegistryWithPublisher creates a seed ToolRegistry using the
// provided EventPublisher. This is used by processQueryWithSeed which creates
// one shared publisher for both the registry and the seed core agent so that
// all events carry the same client_id/chat_id/user_id metadata.
//
// The registry is built from the handler-based tool registry in
// pkg/agent_tools/ — the single source of truth for tool definitions.
// Each handler is converted to a seed core.ToolConfig via
// convertHandlerToSeedToolConfig, which wires up the handler closures
// and post-processing pipeline.
func newSeedToolRegistryWithPublisher(agent *Agent, ep core.EventPublisher) *core.ToolRegistry {
	registry := core.NewToolRegistry(core.ToolRegistryOptions{
		DefaultTimeout: 5 * time.Minute,
		MaxResultSize:  50 * 1024,
		EventPublisher: ep,
		PreExecuteHook: newPreExecuteHook(agent),
	})

	// Register all tools from the handler-based tool registry.
	for _, h := range tools.GetNewToolRegistry().All() {
		if agent != nil && !agent.CanSpawnSubagents() && (h.Name() == "run_subagent" || h.Name() == "run_parallel_subagents") {
			continue
		}
		if err := registry.Register(convertHandlerToSeedToolConfig(h, agent)); err != nil {
			panic(fmt.Sprintf("seed registry: failed to register %q: %v", h.Name(), err))
		}
	}

	return registry
}
