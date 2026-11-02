//go:build !js

package webui

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// peekClientAgent returns the client's agent when one already exists and
// nil otherwise; unlike getClientAgent it never builds one. Read-only
// endpoints the UI polls on page load (stats, change lists, provider
// listing) use it: they all have a no-agent answer, and an agent build runs
// the provider connection check — a model round trip — so building on a
// read made a fresh page load wait on the model.
func (ws *ReactWebServer) peekClientAgent(clientID string) *agent.Agent {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	exists := false
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		if ctx.Agent != nil {
			exists = true
		} else if ctx.ChatSessions != nil && ctx.DefaultChatID != "" {
			if cs, ok := ctx.ChatSessions[ctx.DefaultChatID]; ok && cs != nil {
				cs.mu.RLock()
				exists = cs.Agent != nil
				cs.mu.RUnlock()
			}
		}
	}
	ws.mutex.RUnlock()
	if !exists {
		return nil
	}

	// The agent exists, so getClientAgent takes its fast path (re-arming
	// the agent for this client's workspace) rather than building one.
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil {
		return nil
	}
	return agentInst
}

// peekChatAgent returns the agent already bound to a chat (the client's
// active chat when chatID is empty), or nil. It never builds one.
func (ws *ReactWebServer) peekChatAgent(clientID, chatID string) *agent.Agent {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		return nil
	}
	if chatID == "" {
		chatID = ctx.getActiveChatID()
	}
	if cs, ok := ctx.ChatSessions[chatID]; ok && cs != nil {
		cs.mu.RLock()
		agentInst := cs.Agent
		cs.mu.RUnlock()
		if agentInst != nil {
			return agentInst
		}
	}
	if chatID == ctx.getActiveChatID() {
		return ctx.Agent
	}
	return nil
}
