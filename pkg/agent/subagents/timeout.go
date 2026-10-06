// Package subagents: subagent execution-timeout resolution (SP-141 phase 4,
// increment 2). ResolveSubagentTimeout picks the effective timeout
// (explicit > SPROUT_TOOL_TIMEOUT floor > persona tier default),
// EnvSubagentTimeout reads the env override, and IsOrchestratorPersona
// identifies the orchestrator persona (the longer tier) via the config
// catalog. The subagent construction that calls this cannot leave
// pkg/agent (it writes unexported *Agent fields), so pkg/agent keeps thin
// *SubagentRunner method wrappers that resolve the shared config manager
// and delegate here.
package subagents

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// DefaultSubagentTimeout bounds a subagent run when the caller didn't set
// an explicit timeout. Generous enough for locally-hosted models (which
// may be slower than cloud APIs) while still bounding the worst case.
const DefaultSubagentTimeout = 30 * time.Minute

// OrchestratorSubagentTimeout bounds the orchestrator persona when it is
// itself spawned as a subagent. It delegates to nested subagents and drives
// multi-phase workflows, so a full hour avoids cutting it short
// mid-delegation.
const OrchestratorSubagentTimeout = time.Hour

// ResolveSubagentTimeout returns the effective execution timeout for a
// subagent run: an explicit caller-set timeout always wins; otherwise the
// orchestrator persona (by canonical ID or alias) gets a full hour, a persona
// with a time budget gets that budget plus the wrap-up grace, and every other
// persona gets the 30-minute default. Alias resolution goes through
// the config catalog so it stays in sync with the persona definitions.
//
// Automation workflows can raise both defaults via
// SPROUT_TOOL_TIMEOUT (seconds) — `sprout automate run` sets it from the
// workflow's subagent_timeout_seconds field. The env value applies as a
// floor-multiple: subagents get the value directly when it exceeds their
// tier default; orchestrators get it when it exceeds the orchestrator
// default. (Implementation: see EnvSubagentTimeout below.)
//
// cm is the shared config manager; nil (no shared state, or no config
// manager) falls back to the persona defaults rather than panicking.
func ResolveSubagentTimeout(cm *configuration.Manager, opts SubagentOptions) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	base := DefaultSubagentTimeout
	if IsOrchestratorPersona(cm, opts.Persona) {
		base = OrchestratorSubagentTimeout
	}
	if b := personaTimeBudget(cm, opts.Persona); b > 0 {
		base = b + wrapUpGrace
	}
	if t := EnvSubagentTimeout(); t > base {
		return t
	}
	return base
}

// wrapUpGrace is added to a persona's time budget when that budget is the
// binding timeout, giving the subagent room to summarize and write its final
// output after the budget expires.
const wrapUpGrace = 3 * time.Minute

// personaTimeBudget returns the configured time budget for a persona, or 0
// when the persona or its budget is unset. A nil config manager (no shared
// state) also returns 0 — the persona defaults apply instead.
func personaTimeBudget(cm *configuration.Manager, persona string) time.Duration {
	if persona == "" || cm == nil {
		return 0
	}
	st := cm.GetConfig().GetSubagentType(persona)
	if st == nil {
		return 0
	}
	return time.Duration(st.TimeBudgetSeconds) * time.Second
}

// EnvSubagentTimeout reads SPROUT_TOOL_TIMEOUT (seconds). Returns 0 when
// unset or unparseable so callers fall back to the persona defaults.
func EnvSubagentTimeout() time.Duration {
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

// IsOrchestratorPersona reports whether the given persona string resolves to
// the canonical orchestrator persona (by ID or alias) via the config catalog.
// A nil manager or config returns false so callers fall back to defaults.
func IsOrchestratorPersona(cm *configuration.Manager, persona string) bool {
	if persona == "" {
		return false
	}
	if cm == nil || cm.GetConfig() == nil {
		return false
	}
	st := cm.GetConfig().GetSubagentType(persona)
	return st != nil && st.ID == personas.IDOrchestrator
}
