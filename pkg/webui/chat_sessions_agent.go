//go:build !js

// Package webui provides React web server with embedded assets
package webui

// chat_sessions_agent.go — the per-session agent-wiring path
// (getOrCreateAgent), split out of chat_sessions.go.

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// getOrCreateAgent returns the agent for this chat session, creating one
// lazily if needed. The agent is created outside the chatSession mutex to
// avoid holding it during potentially slow I/O (JSON deserialization, state
// import). If two goroutines race to create the agent, only one wins and the
// other's agent becomes unreferenced.
//
// When the session has a Provider/Model set, those are applied to the agent
// after creation, providing per-session provider/model scoping.
//
// The agent's workspace root is set to the chat's worktree path if set,
// otherwise it falls back to the provided workspaceRoot parameter.
//
// The optional workspaceChdir function, when non-nil, wraps the
// agent.NewAgentWithLayers call so that initialization-time os.Getwd() and
// relative-path resolution observe the correct workspace directory (critical
// in daemon mode where the process CWD may differ from the client workspace).
func (cs *chatSession) getOrCreateAgent(workspaceRoot string, configBase string, workspaceDir string, eventBus *events.EventBus, clientID, userID string, workspaceChdir func(string, func() error) error) (*agent.Agent, error) {
	cs.mu.Lock()
	if cs.Agent != nil {
		agentWorkspace := cs.WorktreePath
		if agentWorkspace == "" {
			agentWorkspace = workspaceRoot
		}
		agentInst := cs.Agent
		cs.mu.Unlock()
		rearmWebUIAgent(agentInst, nil, agentSetupConfig{
			WorkspaceRoot: agentWorkspace,
			ClientID:      clientID,
			ChatID:        cs.ID,
			UserID:        userID,
		})
		return agentInst, nil
	}
	// Capture session-scoped provider/model before releasing the lock
	sessionProvider := cs.Provider
	sessionModel := cs.Model
	sessionWorktree := cs.WorktreePath
	sessionHandoff := cs.HandoffContext
	sessionSnapshot := append([]byte(nil), cs.AgentState...)
	cs.mu.Unlock()

	// Use chat's worktree path if set, otherwise use provided workspaceRoot
	agentWorkspace := sessionWorktree
	if agentWorkspace == "" {
		agentWorkspace = workspaceRoot
	}

	// Fast check: if no provider is configured, return immediately with a
	// sentinel error instead of attempting expensive agent creation.
	// NOTE: A narrow TOCTOU race exists between this config read and the
	// config read inside agent.NewAgentWithLayers. Acceptable since the worst
	// case is a single unnecessary retry after the user configures a provider.
	if !isProviderAvailableInWorkspace(agentWorkspace) {
		return nil, ErrNoProviderConfigured
	}

	// Create agent outside the lock.
	snapshot := sessionSnapshot
	var created *agent.Agent
	var createErr error
	created, createErr = agent.NewAgentWithLayersInWorkspace(configBase, workspaceDir, agentWorkspace, "")
	if createErr != nil {
		if errors.Is(createErr, agent.ErrModelNotAvailable) || errors.Is(createErr, agent.ErrProviderNotConfigured) {
			return nil, createErr
		}
		return nil, fmt.Errorf("create chat agent: %w", createErr)
	}

	setupWebUIAgent(created, agentSetupConfig{
		EventBus:      eventBus,
		WorkspaceRoot: agentWorkspace,
		ClientID:      clientID,
		ChatID:        cs.ID,
		UserID:        userID,
	})

	// Inject handoff context into system prompt if present (one-time injection)
	if sessionHandoff != "" {
		currentPrompt := created.GetSystemPrompt()
		handoffSection := formatHandoffSystemPrompt(sessionHandoff)
		created.SetSystemPrompt(currentPrompt + handoffSection)
		cs.mu.Lock()
		cs.HandoffContext = "" // Clear after injection
		cs.mu.Unlock()
	}

	if len(snapshot) > 0 {
		if err := created.ImportState(snapshot); err != nil {
			slog.Default().Warn("failed to import chat session state", slog.Any("err", err))
		}
	}

	// Apply session-scoped provider/model if set on the session.
	// This provides per-session provider/model scoping without affecting
	// other sessions or the global config.
	//
	// Provider/model ordering is deliberate: we only apply the session
	// model AFTER the provider switch succeeds. If SetProvider fails
	// (provider not available, factory couldn't build a client, etc.)
	// the agent retains its previous/default provider — and a model
	// name that belonged to the failed provider would either error out
	// on SetModel or, worse, silently shadow an unrelated model's name
	// on a different provider (e.g. "gpt-4o" leaking onto Ollama).
	providerApplied := sessionProvider == ""
	if sessionProvider != "" {
		providerType, err := created.GetConfigManager().MapStringToClientType(sessionProvider)
		if err != nil {
			slog.Default().Warn("invalid chat session provider", slog.String("provider", sessionProvider), slog.Any("err", err))
		} else if err := created.SetProvider(providerType); err != nil {
			slog.Default().Warn("failed to set chat session provider", slog.String("provider", sessionProvider), slog.Any("err", err))
		} else {
			providerApplied = true
		}
	}
	if providerApplied && sessionModel != "" {
		if err := created.SetModel(sessionModel); err != nil {
			slog.Default().Warn("failed to set chat session model", slog.String("model", sessionModel), slog.Any("err", err))
		}
	}

	// Apply any additional config overrides from the session (e.g., subagent_provider,
	// reasoning_effort, etc.) directly to the config manager in-memory.
	cs.mu.Lock()
	sessionOverrides := cs.ConfigOverrides
	cs.mu.Unlock()
	if len(sessionOverrides) > 0 {
		// Store overrides on the agent so they get saved with the session state
		created.SetConfigOverrides(sessionOverrides)
		cm := created.GetConfigManager()
		if cm != nil {
			if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
				_, err := applyPartialSettings(cfg, sessionOverrides)
				return err
			}); err != nil {
				slog.Default().Warn("failed to apply chat session config overrides", slog.Any("err", err))
			}
		}
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.Agent == nil {
		// We won the race — store our agent.
		cs.Agent = created
		cs.CurrentSessionID = strings.TrimSpace(created.GetSessionID())
	} else {
		// Lost the creation race. Shut our agent down rather than dropping the
		// reference — it already spawned an embedding-index build and MCP
		// servers, which would otherwise outlive the daemon's knowledge of it.
		orphan := created
		created = cs.Agent
		utils.SafeGo(slog.Default(), "agent-shutdown", func() {
			orphan.Shutdown()
		}, slog.String("reason", "chat_agent_creation_race"))
		agentWorkspace := cs.WorktreePath
		if agentWorkspace == "" {
			agentWorkspace = workspaceRoot
		}
		rearmWebUIAgent(created, nil, agentSetupConfig{
			WorkspaceRoot: agentWorkspace,
			ClientID:      clientID,
			ChatID:        cs.ID,
			UserID:        userID,
		})
	}
	return created, nil
}
