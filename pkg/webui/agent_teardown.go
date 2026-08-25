//go:build !js

package webui

import (
	"log/slog"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// releaseAgents shuts down agents the server is dropping.
//
// Dropping the pointer is not enough. Agent.Shutdown is what closes the
// EmbeddingManager (releasing its HNSW store and stopping an in-flight index
// build), cancels the lifetime context that keeps background watchers running,
// and stops MCP child processes. An agent whose reference is merely cleared
// stays reachable from its own goroutines, so it keeps running — and keeps
// writing to the workspace index — for the life of the daemon.
//
// Callers pass every agent they are releasing; duplicates and nils are
// filtered, because ctx.Agent commonly aliases a chat session's agent and
// double-shutting-down is wasted work. In shared mode ws.agent belongs to the
// CLI, not the server, and is never shut down here.
//
// Shutdown blocks — it waits on the agent's background WaitGroup and gives MCP
// servers up to 5s to stop — so it must run outside ws.mutex and off the
// request path. Each agent is released in its own recovered goroutine.
func (ws *ReactWebServer) releaseAgents(reason string, agents ...*agent.Agent) {
	seen := make(map[*agent.Agent]struct{}, len(agents))

	var shared *agent.Agent
	if ws != nil {
		shared = ws.agent
	}

	for _, a := range agents {
		if a == nil || a == shared {
			continue
		}
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}

		ws.agentTeardownWg.Add(1)
		utils.SafeGo(ws.log(), "agent-shutdown", func() {
			defer ws.agentTeardownWg.Done()
			a.Shutdown()
		}, slog.String("reason", reason))
	}
}

// waitForAgentTeardown blocks until every agent released so far has finished
// shutting down. Shutdown() calls it so the daemon does not exit while an
// agent is still flushing history or its embedding store; tests call it to
// order teardown writes before temp-directory cleanup.
func (ws *ReactWebServer) waitForAgentTeardown() {
	ws.agentTeardownWg.Wait()
}

// chatSessionAgents collects the agents owned by a client context's chat
// sessions. Callers must hold ws.mutex; the returned slice is safe to hand to
// releaseAgents after unlocking.
func chatSessionAgents(ctx *webClientContext) []*agent.Agent {
	if ctx == nil {
		return nil
	}
	agents := make([]*agent.Agent, 0, len(ctx.ChatSessions)+1)
	if ctx.Agent != nil {
		agents = append(agents, ctx.Agent)
	}
	for _, cs := range ctx.ChatSessions {
		if cs == nil {
			continue
		}
		cs.mu.Lock()
		if cs.Agent != nil {
			agents = append(agents, cs.Agent)
		}
		cs.mu.Unlock()
	}
	return agents
}
