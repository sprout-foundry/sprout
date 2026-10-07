//go:build !js

// huma_workspace.go holds the Huma operations and their thin handlers for the
// workspace/instances family: the /api/workspace* and /api/instances* routes
// (registered via registerWorkspaceHumaOperations from registerHumaOperations
// in huma_routes.go).
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are
// unchanged and no behavior changes. /api/workspace is a multi-method route
// (GET reads the active workspace, POST sets it) and /api/terminal-style
// per-method gates are enforced by registering one Huma operation per method.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerWorkspaceHumaOperations registers the workspace/instances-family
// Huma operations.
func registerWorkspaceHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "workspaceGet",
		Method:      http.MethodGet,
		Path:        "/api/workspace",
		Summary:     "Report the client's active workspace.",
		Description: "Reports the requesting client's active workspace root, whether it is a recognized project directory, and recent-workspace metadata.",
		Tags:        []string{"workspace/instances"},
	}, ws.workspaceGetHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "workspaceSet",
		Method:      http.MethodPost,
		Path:        "/api/workspace",
		Summary:     "Set the client's active workspace.",
		Description: "Sets the requesting client's active workspace root to the `path` in the body. The path must resolve within the daemon root.",
		Tags:        []string{"workspace/instances"},
	}, ws.workspaceSetHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "workspaceBrowse",
		Method:      http.MethodGet,
		Path:        "/api/workspace/browse",
		Summary:     "Browse directories under the daemon root.",
		Description: "Lists the directories under the daemon root (the parent of the active workspace) so the client can pick a new workspace. Defaults to the daemon root when `path` is omitted.",
		Tags:        []string{"workspace/instances"},
	}, ws.workspaceBrowseHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "workspaceSymbols",
		Method:      http.MethodGet,
		Path:        "/api/workspace/symbols",
		Summary:     "List workspace symbols.",
		Description: "Returns the workspace's indexed symbols, optionally filtered by `query`. Falls back to building the index on a cache miss.",
		Tags:        []string{"workspace/instances"},
	}, ws.workspaceSymbolsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "workspaceProjects",
		Method:      http.MethodGet,
		Path:        "/api/workspace/projects",
		Summary:     "List known projects.",
		Description: "Lists the recent projects known to the client (the workspace picker) with their last-used ordering.",
		Tags:        []string{"workspace/instances"},
	}, ws.workspaceProjectsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "workspaceSync",
		Method:      http.MethodPost,
		Path:        "/api/workspace/sync",
		Summary:     "Synchronize the workspace tree.",
		Description: "Applies the pending file-sync operations for the workspace (the SP-046 workspace-sync surface).",
		Tags:        []string{"workspace/instances"},
	}, ws.workspaceSyncHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "workspaceTakeover",
		Method:      http.MethodPost,
		Path:        "/api/workspace/takeover",
		Summary:     "Take over a workspace from another device.",
		Description: "Marks the requesting client as the active owner of the workspace, superseding the previous owner.",
		Tags:        []string{"workspace/instances"},
	}, ws.workspaceTakeoverHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "instancesList",
		Method:      http.MethodGet,
		Path:        "/api/instances",
		Summary:     "List live Sprout instances.",
		Description: "Lists the live Sprout instances with their PID, port, working directory, and host/current flags.",
		Tags:        []string{"workspace/instances"},
	}, ws.instancesListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "instanceSelect",
		Method:      http.MethodPost,
		Path:        "/api/instances/select",
		Summary:     "Select the active instance.",
		Description: "Sets the desired Sprout instance to the `pid` in the body so the client attaches to that instance on the next load.",
		Tags:        []string{"workspace/instances"},
	}, ws.instanceSelectHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sshHosts",
		Method:      http.MethodGet,
		Path:        "/api/instances/ssh-hosts",
		Summary:     "List known SSH hosts.",
		Description: "Lists the known SSH hosts available for remote instance attachment.",
		Tags:        []string{"workspace/instances"},
	}, ws.sshHostsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sshOpen",
		Method:      http.MethodPost,
		Path:        "/api/instances/ssh-open",
		Summary:     "Open an SSH connection to a remote instance.",
		Description: "Opens an SSH connection to a remote host and launches a Sprout instance there.",
		Tags:        []string{"workspace/instances"},
	}, ws.sshOpenHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sshLaunchStatus",
		Method:      http.MethodGet,
		Path:        "/api/instances/ssh-launch-status",
		Summary:     "Report the status of an SSH instance launch.",
		Description: "Reports the status and logs of a remote instance launch started through ssh-open.",
		Tags:        []string{"workspace/instances"},
	}, ws.sshLaunchStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sshBrowse",
		Method:      http.MethodPost,
		Path:        "/api/instances/ssh-browse",
		Summary:     "Browse the remote host's directories over SSH.",
		Description: "Lists the directories on the remote host over the SSH session so the client can pick a remote workspace.",
		Tags:        []string{"workspace/instances"},
	}, ws.sshBrowseHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sshSessions",
		Method:      http.MethodGet,
		Path:        "/api/instances/ssh-sessions",
		Summary:     "List active SSH sessions.",
		Description: "Lists the active SSH sessions with their host and state.",
		Tags:        []string{"workspace/instances"},
	}, ws.sshSessionsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sshClose",
		Method:      http.MethodPost,
		Path:        "/api/instances/ssh-close",
		Summary:     "Close an SSH session.",
		Description: "Closes the SSH session for the `session_id` in the body.",
		Tags:        []string{"workspace/instances"},
	}, ws.sshCloseHumaHandler)
}

// workspaceGetHumaHandler is the Huma handler for GET /api/workspace.
func (ws *ReactWebServer) workspaceGetHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWorkspace(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// workspaceSetHumaHandler is the Huma handler for POST /api/workspace.
func (ws *ReactWebServer) workspaceSetHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWorkspace(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// workspaceBrowseHumaHandler is the Huma handler for GET /api/workspace/browse.
func (ws *ReactWebServer) workspaceBrowseHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWorkspaceBrowse(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// workspaceSymbolsHumaHandler is the Huma handler for GET /api/workspace/symbols.
func (ws *ReactWebServer) workspaceSymbolsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWorkspaceSymbols(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// workspaceProjectsHumaHandler is the Huma handler for GET /api/workspace/projects.
func (ws *ReactWebServer) workspaceProjectsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWorkspaceProjects(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// workspaceSyncHumaHandler is the Huma handler for POST /api/workspace/sync.
func (ws *ReactWebServer) workspaceSyncHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWorkspaceSync(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// workspaceTakeoverHumaHandler is the Huma handler for POST /api/workspace/takeover.
func (ws *ReactWebServer) workspaceTakeoverHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWorkspaceTakeover(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// instancesListHumaHandler is the Huma handler for GET /api/instances.
func (ws *ReactWebServer) instancesListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIInstances(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// instanceSelectHumaHandler is the Huma handler for POST /api/instances/select.
func (ws *ReactWebServer) instanceSelectHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIInstanceSelect(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sshHostsHumaHandler is the Huma handler for GET /api/instances/ssh-hosts.
func (ws *ReactWebServer) sshHostsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISSHHosts(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sshOpenHumaHandler is the Huma handler for POST /api/instances/ssh-open.
func (ws *ReactWebServer) sshOpenHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISSHOpen(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sshLaunchStatusHumaHandler is the Huma handler for GET /api/instances/ssh-launch-status.
func (ws *ReactWebServer) sshLaunchStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISSHLaunchStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sshBrowseHumaHandler is the Huma handler for POST /api/instances/ssh-browse.
func (ws *ReactWebServer) sshBrowseHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISSHBrowse(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sshSessionsHumaHandler is the Huma handler for GET /api/instances/ssh-sessions.
func (ws *ReactWebServer) sshSessionsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISSHSessions(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sshCloseHumaHandler is the Huma handler for POST /api/instances/ssh-close.
func (ws *ReactWebServer) sshCloseHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISSHSessionDelete(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
