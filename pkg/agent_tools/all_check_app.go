//go:build !js

package tools

// registerCheckAppTool registers check_app, which builds and serves the
// project and drives the host's headless browser — available in native
// (desktop/daemon) builds only.
func registerCheckAppTool() []ToolHandler {
	return []ToolHandler{&checkAppHandler{}}
}
