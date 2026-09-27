package agent

// subagent_task_timeout.go — subagent execution-timeout resolution, split
// out of subagent_task.go. resolveSubagentTimeout picks the effective timeout
// (explicit > SPROUT_TOOL_TIMEOUT floor > persona tier default), envSubagentTimeout
// reads the env override, and isOrchestratorPersona identifies the orchestrator
// persona (a longer tier) via the config catalog.
import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// defaultSubagentTimeout bounds a subagent run when the caller didn't set
// an explicit timeout. Generous enough for locally-hosted models (which may
// be slower than cloud APIs) while still bounding the worst case.
const defaultSubagentTimeout = 30 * time.Minute

// orchestratorSubagentTimeout bounds the orchestrator persona when it is
// itself spawned as a subagent. It delegates to nested subagents and drives
// multi-phase workflows, so a full hour avoids cutting it short
// mid-delegation.
const orchestratorSubagentTimeout = time.Hour

// resolveSubagentTimeout returns the effective execution timeout for a
// subagent run: an explicit caller-set timeout always wins; otherwise the
// orchestrator persona (by canonical ID or alias) gets a full hour and every
// other persona gets the 30-minute default. Alias resolution goes through
// the config catalog so it stays in sync with the persona definitions.
//
// Automation workflows can raise both defaults via
// SPROUT_TOOL_TIMEOUT (seconds) — `sprout automate run` sets it from the
// workflow's subagent_timeout_seconds field. The env value applies as a
// floor-multiple: subagents get the value directly when it exceeds their
// tier default; orchestrators get it when it exceeds the orchestrator
// default. (Implementation: see envSubagentTimeout below.)
func (r *SubagentRunner) resolveSubagentTimeout(opts SubagentOptions) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	if t := envSubagentTimeout(); t > 0 {
		if r.isOrchestratorPersona(opts.Persona) {
			if t > orchestratorSubagentTimeout {
				return t
			}
		} else if t > defaultSubagentTimeout {
			return t
		}
	}
	if r.isOrchestratorPersona(opts.Persona) {
		return orchestratorSubagentTimeout
	}
	return defaultSubagentTimeout
}

// envSubagentTimeout reads SPROUT_TOOL_TIMEOUT (seconds). Returns 0 when
// unset or unparseable so callers fall back to the persona defaults.
func envSubagentTimeout() time.Duration {
	raw := os.Getenv("SPROUT_TOOL_TIMEOUT")
	if raw == "" {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// isOrchestratorPersona reports whether the given persona string resolves to
// the canonical orchestrator persona (by ID or alias) via the config catalog.
func (r *SubagentRunner) isOrchestratorPersona(persona string) bool {
	if persona == "" {
		return false
	}
	if r.shared == nil || r.shared.ConfigManager == nil {
		return false
	}
	st := r.shared.ConfigManager.GetConfig().GetSubagentType(persona)
	return st != nil && st.ID == personas.IDOrchestrator
}
