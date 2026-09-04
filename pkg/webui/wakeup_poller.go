//go:build !js

package webui

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func (ws *ReactWebServer) startWakeupPoller(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ws.checkAndResume()
		}
	}
}

// checkAndResume attempts auto-resume for every agent the server knows:
// the shared-mode agent (when the CLI spawned the WebUI) and every live
// per-chat agent across all client contexts (daemon/service mode).
//
// Polling only ws.agent left daemon sessions without wakeup entirely —
// background completions queued on a per-chat agent sat dormant until the
// user happened to send another message. The query guard inside
// TryAutoResume rejects busy agents, so polling is safe for agents with
// in-flight turns.
func (ws *ReactWebServer) checkAndResume() {
	for _, a := range ws.liveAgents() {
		if a.TryAutoResume() {
			ws.log().Info("automatically resuming wakeup notifications")
		}
	}
}

// liveAgents returns a snapshot of every agent reachable from the server:
// ws.agent (shared mode, may be nil) plus each client context's top-level
// agent and the agents of its chat sessions. Duplicates removed.
func (ws *ReactWebServer) liveAgents() []*agent.Agent {
	seen := make(map[*agent.Agent]struct{})
	var agents []*agent.Agent

	if ws.agent != nil {
		seen[ws.agent] = struct{}{}
		agents = append(agents, ws.agent)
	}

	ws.mutex.RLock()
	for _, ctx := range ws.clientContexts {
		if ctx == nil {
			continue
		}
		if ctx.Agent != nil {
			if _, dup := seen[ctx.Agent]; !dup {
				seen[ctx.Agent] = struct{}{}
				agents = append(agents, ctx.Agent)
			}
		}
		for _, cs := range ctx.ChatSessions {
			if cs == nil {
				continue
			}
			cs.mu.RLock()
			a := cs.Agent
			cs.mu.RUnlock()
			if a != nil {
				if _, dup := seen[a]; !dup {
					seen[a] = struct{}{}
					agents = append(agents, a)
				}
			}
		}
	}
	ws.mutex.RUnlock()

	return agents
}
