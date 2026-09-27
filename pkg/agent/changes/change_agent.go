package changes

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ChangeAgent is the narrow surface ChangeTracker needs from its owning
// agent (SP-141 phase 2). *agent.Agent satisfies it structurally, so
// production call sites and the agent-side test suite pass a live *Agent
// unchanged. This package deliberately does not import pkg/agent; the
// DebugLogger sub-interface is what keeps Logger() off the interface
// (a concrete *AgentLogger return type would force an import cycle).
type ChangeAgent interface {
	GetSessionID() string
	GetModel() string
	GetWorkspaceRoot() string
	GenerateResponse([]api.Message) (string, error)
	PublishFileChange(filePath, action, content string)
	PublishRawEvent(eventType string, data interface{})
	DebugLogger() DebugLogger
}

// DebugLogger is the debug-log surface the shell-snapshot path routes
// through. *agent.AgentLogger satisfies it. Nil means "log nowhere"
// (the caller's fallback).
type DebugLogger interface {
	Debug(format string, args ...any)
}
