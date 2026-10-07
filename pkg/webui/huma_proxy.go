//go:build !js

// huma_proxy.go holds the Huma operations and their thin handlers for the
// proxy family: the /api/proxy/* Foundry chat-proxy routes (registered via
// registerProxyHumaOperations from registerHumaOperations in huma_routes.go).
// These are tagged conversation/query because they are the chat-proxy surface
// the tag description covers.
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are unchanged.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerProxyHumaOperations registers the proxy-family Huma operations.
func registerProxyHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "proxyChat",
		Method:      http.MethodPost,
		Path:        "/api/proxy/chat",
		Summary:     "Start or steer a proxied chat query.",
		Description: "Accepts the translated chat format (provider/model/messages) from the CloudAdapter and starts (or, with `steer`, injects into) a query for the resolved chat. `chat_id` in the body takes precedence over the client's active chat.",
		Tags:        []string{"conversation/query"},
	}, ws.proxyChatHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "proxyChatStop",
		Method:      http.MethodPost,
		Path:        "/api/proxy/chat/stop",
		Summary:     "Stop the active proxied query.",
		Description: "Stops the active query for the resolved chat. `chat_id` in the body (optional) targets a specific chat.",
		Tags:        []string{"conversation/query"},
	}, ws.proxyChatStopHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "proxyChatStatus",
		Method:      http.MethodGet,
		Path:        "/api/proxy/chat/status",
		Summary:     "Report the active-query status for the resolved chat.",
		Description: "Reports whether a query is currently active for the resolved chat (the proxied counterpart of the query status).",
		Tags:        []string{"conversation/query"},
	}, ws.proxyChatStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "proxyStats",
		Method:      http.MethodGet,
		Path:        "/api/proxy/stats",
		Summary:     "Report proxy statistics.",
		Description: "Reports the server and per-client statistics for the resolved chat, exposed under the proxy path for cloud-mode consumers.",
		Tags:        []string{"conversation/query"},
	}, ws.proxyStatsHumaHandler)
}

// proxyChatHumaHandler is the Huma handler for POST /api/proxy/chat.
func (ws *ReactWebServer) proxyChatHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIProxyChat(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// proxyChatStopHumaHandler is the Huma handler for POST /api/proxy/chat/stop.
func (ws *ReactWebServer) proxyChatStopHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIProxyChatStop(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// proxyChatStatusHumaHandler is the Huma handler for GET /api/proxy/chat/status.
func (ws *ReactWebServer) proxyChatStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIProxyChatStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// proxyStatsHumaHandler is the Huma handler for GET /api/proxy/stats.
func (ws *ReactWebServer) proxyStatsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIProxyStats(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
