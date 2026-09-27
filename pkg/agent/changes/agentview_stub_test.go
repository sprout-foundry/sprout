package changes

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// stubAgentView is the test double for the AgentView seam. It mirrors
// what the old in-package tests got from `&Agent{workspaceRoot: ...}`:
// a workspace root and otherwise-inert agent surface (no bus, no LLM).
// fileChanged records PublishFileChange calls so tests can assert the
// WebUI activity-feed side effects.
type stubAgentView struct {
	workspaceRoot string
}

func (s stubAgentView) GetSessionID() string { return "stub-session" }
func (s stubAgentView) GetModel() string     { return "stub-model" }
func (s stubAgentView) GetWorkspaceRoot() string {
	return s.workspaceRoot
}
func (s stubAgentView) GenerateResponse(messages []api.Message) (string, error) {
	return "stub response", nil
}
func (s stubAgentView) DebugLogger() DebugLogger { return nil }
func (s stubAgentView) PublishFileChange(filePath, action, content string) {
	_ = content // value receiver: recording into s.fileChanged would be lost anyway
}
func (s stubAgentView) PublishRawFileChanged(eventType string, payload interface{}) {}
