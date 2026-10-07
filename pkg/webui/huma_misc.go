//go:build !js

// huma_misc.go holds the Huma operations and their thin handlers for the
// cross-family miscellaneous routes that were left as plain handlers after the
// family migrations: /api/support-bundle, /api/ws-metrics,
// /api/open-in-file-browser, /api/computer-use/test, and /api/browse (registered
// via registerMiscHumaOperations from registerHumaOperations in
// huma_routes.go). Each route keeps the tag its family assigned it.
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are unchanged.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerMiscHumaOperations registers the cross-family miscellaneous Huma
// operations.
func registerMiscHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "supportBundle",
		Method:      http.MethodGet,
		Path:        "/api/support-bundle",
		Summary:     "Generate a support-bundle snapshot.",
		Description: "Collects the logs, configuration, and state snapshot needed to diagnose an issue on this client.",
		Tags:        []string{"diagnostics"},
	}, ws.supportBundleHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "wsMetrics",
		Method:      http.MethodGet,
		Path:        "/api/ws-metrics",
		Summary:     "Report WebSocket metrics.",
		Description: "Reports the live WebSocket connection and message metrics for the server.",
		Tags:        []string{"misc/static"},
	}, ws.wsMetricsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "openInFileBrowser",
		Method:      http.MethodPost,
		Path:        "/api/open-in-file-browser",
		Summary:     "Open a path in the system file browser.",
		Description: "Reveals the file (or its containing directory) at `path` in the system file browser using the platform-appropriate command. Returns 501 when no file-browser command is available on the host.",
		Tags:        []string{"files"},
	}, ws.openInFileBrowserHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "computerUseTest",
		Method:      http.MethodPost,
		Path:        "/api/computer-use/test",
		Summary:     "Test the computer-use capability.",
		Description: "Runs a computer-use self-test to confirm the capability is available on this host.",
		Tags:        []string{"misc/static"},
	}, ws.computerUseTestHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "browseDir",
		Method:      http.MethodGet,
		Path:        "/api/browse",
		Summary:     "List a directory's contents.",
		Description: "Lists the files and directories inside the `path` (defaulting to the workspace root), always skipping the .git directory. `ignore=true` additionally skips entries matched by the workspace's gitignore rules.",
		Tags:        []string{"files"},
	}, ws.browseDirHumaHandler)
}

// supportBundleHumaHandler is the Huma handler for GET /api/support-bundle.
func (ws *ReactWebServer) supportBundleHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISupportBundle(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// wsMetricsHumaHandler is the Huma handler for GET /api/ws-metrics.
func (ws *ReactWebServer) wsMetricsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIWSMetrics(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// openInFileBrowserHumaHandler is the Huma handler for POST /api/open-in-file-browser.
func (ws *ReactWebServer) openInFileBrowserHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIOpenInFileBrowser(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// computerUseTestHumaHandler is the Huma handler for POST /api/computer-use/test.
func (ws *ReactWebServer) computerUseTestHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIComputerUseTest(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// browseDirHumaHandler is the Huma handler for GET /api/browse.
func (ws *ReactWebServer) browseDirHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIBrowse(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
