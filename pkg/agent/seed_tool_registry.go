package agent

import (
	"time"

	core "github.com/sprout-foundry/seed/core"
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
func newSeedToolRegistryWithPublisher(agent *Agent, ep core.EventPublisher) *core.ToolRegistry {
	registry := core.NewToolRegistry(core.ToolRegistryOptions{
		DefaultTimeout:  5 * time.Minute,
		MaxResultSize:   50 * 1024,
		EventPublisher:  ep,
		PreExecuteHook:  newPreExecuteHook(agent),
	})

	registerTools(registry, agent)

	return registry
}
