package agent

// subagent_task_timeout.go — subagent execution-timeout resolution. The
// policy (explicit > SPROUT_TOOL_TIMEOUT floor > persona tier default) lives
// in pkg/agent/subagents (SP-141 phase 4, increment 2); these thin
// *SubagentRunner wrappers resolve the runner's shared config manager and
// delegate, so the *SubagentRunner-typed call sites and tests are unchanged.
import (
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// defaultSubagentTimeout bounds a subagent run when the caller didn't set
// an explicit timeout.
const defaultSubagentTimeout = subagents.DefaultSubagentTimeout

// orchestratorSubagentTimeout bounds the orchestrator persona when it is
// itself spawned as a subagent.
const orchestratorSubagentTimeout = subagents.OrchestratorSubagentTimeout

// sharedConfigManager returns the runner's shared config manager, or nil
// when there is no shared state (the defensive nil path the policy must
// handle instead of panicking).
func (r *SubagentRunner) sharedConfigManager() *configuration.Manager {
	if r.shared == nil {
		return nil
	}
	return r.shared.ConfigManager
}

// resolveSubagentTimeout returns the effective execution timeout for a
// subagent run; see subagents.ResolveSubagentTimeout for the policy.
func (r *SubagentRunner) resolveSubagentTimeout(opts SubagentOptions) time.Duration {
	return subagents.ResolveSubagentTimeout(r.sharedConfigManager(), opts)
}

// isOrchestratorPersona reports whether the given persona resolves to the
// canonical orchestrator persona via the config catalog; see
// subagents.IsOrchestratorPersona.
func (r *SubagentRunner) isOrchestratorPersona(persona string) bool {
	return subagents.IsOrchestratorPersona(r.sharedConfigManager(), persona)
}
