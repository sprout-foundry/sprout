package changes

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// fakeAgent is the minimal ChangeAgent double the in-package tests use in
// place of a real *agent.Agent (which cannot be referenced here without an
// import cycle). Only GetWorkspaceRoot is exercised by the pure-tracker
// tests; the rest are no-ops.
type fakeAgent struct {
	workspaceRoot string
}

func (f *fakeAgent) GetSessionID() string                           { return "" }
func (f *fakeAgent) GetModel() string                               { return "" }
func (f *fakeAgent) GetWorkspaceRoot() string                       { return f.workspaceRoot }
func (f *fakeAgent) GenerateResponse([]api.Message) (string, error) { return "", nil }
func (f *fakeAgent) PublishFileChange(string, string, string)       {}
func (f *fakeAgent) PublishRawEvent(string, interface{})            {}
func (f *fakeAgent) DebugLogger() DebugLogger                       { return nil }
