//go:build !js

// Package agent — test helpers exported for use by tests in other
// packages. initSubManagers is unexported but downstream tests (notably
// pkg/webui/api_command_test.go's harness) need a way to bring a bare
// &Agent{} up to the "all sub-managers initialised" state without
// driving a full agent creation. These wrappers exist because
// alternative approaches (e.g. internal test files, testdata
// pre-populated fixtures) would either leak implementation details
// across package boundaries or be more invasive than the wrappers.
package agent

// InitSubManagersForTest forces initialisation of every sub-manager on
// the receiver. Mirrors the production lazy-init path in initSubManagers
// but is exported so test fixtures in other packages can use it without
// poking at internal fields. Does not touch the LLM client, config
// manager, or any of the fields a real NewAgent call would set — the
// goal is "lazy-init done", not "fully production-ready".
func (a *Agent) InitSubManagersForTest() {
	a.initSubManagers()
}