package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// computerUseOnce guards one-time registration of the computer-use tools into
// the global registries. Registration is global (the registries are
// process-wide singletons), so it must happen at most once even across multiple
// agent creations.
var computerUseOnce sync.Once

// computerUseToolNames is the set of tool names that are restricted to the
// computer_user persona. Populated when the tools are registered; empty (and
// therefore inert) when computer use is disabled.
var computerUseToolNames = map[string]bool{}

// RegisterComputerUseTools wires the computer_user persona's desktop-control
// tools (SP-063) into the agent's registries — but only when cfg explicitly
// enables them. Idempotent and safe to call on every agent creation.
//
// Gating layers (defense in depth):
//  1. cfg.ComputerUse.Enabled must be true — off by default.
//  2. A real platform backend must be constructable (macOS+cliclick or
//     linux/X11+xdotool); otherwise nothing is registered and the reason is
//     returned for the caller to surface.
//  3. Exposure is limited to the computer_user persona's allowed_tools, and a
//     dispatch-layer guard (isComputerUseToolBlocked) rejects the tools for any
//     other active persona.
//  4. Every action is rate-limited and audited (see the wrapped backend).
func RegisterComputerUseTools(cfg *configuration.Config) error {
	if cfg == nil || cfg.ComputerUse == nil || !cfg.ComputerUse.Enabled {
		return nil
	}
	cu := cfg.ComputerUse.Resolve()

	real, err := computer_use.NewPlatformBackend()
	if err != nil {
		return fmt.Errorf("computer use unavailable: %w", err)
	}

	// Compose decorators: handler → audit → rate-limit → real backend.
	var backend computer_use.ComputerBackend = computer_use.NewRateLimitedBackend(real, cu.MaxActionsPerMinute)
	auditDir := cu.AuditLogDir
	if auditDir == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			auditDir = filepath.Join(home, ".config", "sprout", "computer_use_log")
		}
	}
	if auditDir != "" {
		if ab, aerr := computer_use.NewAuditingBackend(backend, auditDir, "session"); aerr == nil {
			backend = ab
		}
	}
	computer_use.SetBackend(backend)

	computerUseOnce.Do(func() {
		newReg := tools.GetNewToolRegistry()
		canon := GetToolRegistry()
		for _, h := range computer_use.Handlers() {
			if regErr := newReg.Register(h); regErr != nil {
				// Already registered (e.g. a prior call) — definitions are
				// global too, so skip re-adding.
				continue
			}
			canon.RegisterTool(toolConfigFromHandler(h))
		}
		for _, name := range computer_use.ToolNames() {
			computerUseToolNames[name] = true
		}
	})
	return nil
}

// toolConfigFromHandler derives a canonical ToolConfig (used for LLM tool
// definitions + per-persona allowlist filtering) from a new-interface handler's
// self-described Definition.
func toolConfigFromHandler(h tools.ToolHandler) ToolConfig {
	def := h.Definition()
	params := make([]ParameterConfig, 0, len(def.Parameters))
	for _, p := range def.Parameters {
		params = append(params, ParameterConfig{
			Name:        p.Name,
			Type:        p.Type,
			Required:    p.Required,
			Description: p.Description,
		})
	}
	return ToolConfig{
		Name:        def.Name,
		Description: def.Description,
		Parameters:  params,
	}
}

// isComputerUseToolBlocked reports whether the named tool is a computer-use
// tool being invoked by a persona other than computer_user. This is the
// dispatch-layer enforcement of SP-063 Phase 6 (tools allowlisted only for the
// computer_user persona). It is inert when computer use is disabled (the name
// set is empty).
func isComputerUseToolBlocked(toolName string, agent *Agent) bool {
	if !computerUseToolNames[toolName] {
		return false
	}
	if agent == nil {
		return true
	}
	active := normalizeAgentPersonaID(agent.state.GetActivePersona())
	return active != personas.IDComputerUser
}
