//go:build !js

package webui

import (
	"context"
	"net/http"
	"runtime/pprof"
	"time"

	lspproxy "github.com/sprout-foundry/sprout/pkg/lsp/proxy"
)

func (ws *ReactWebServer) setupRoutes(ctx context.Context) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", ws.handleIndex)
	// /ssh/ is a reverse proxy registered before /ws and /terminal so prefix match works.
	mux.HandleFunc("/ssh/", ws.handleSSHProxy)

	ws.registerCoreRoutes(mux)
	ws.registerTerminalRoutes(mux, ctx)
	ws.registerPreviewRoutes(mux)
	ws.registerChangesRoutes(mux)
	ws.registerAutomateRoutes(mux)
	ws.registerHumaRoutes(mux)

	return mux
}

// The Huma operations (every huma.Register call) live in huma_routes.go,
// which registerHumaRoutes (huma_api.go) mounts on the same ServeMux. Each
// operation is registered once by the humago adapter as a method+path pattern,
// so the plain mux registrations for those routes were removed. The contract
// test's AST walk reads the Huma paths from huma_routes.go.
func (ws *ReactWebServer) registerCoreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws", ws.handleWebSocket)
	mux.HandleFunc("/terminal", ws.handleTerminalWebSocket)
	mux.HandleFunc("/static/", ws.handleStaticFiles)
	mux.HandleFunc("/assets/", ws.handleAssets)
	mux.HandleFunc("/sw.js", ws.handleServiceWorker)
	mux.HandleFunc("/manifest.json", ws.handleManifest)
	mux.HandleFunc("/browserconfig.xml", ws.handleBrowserConfig)
	mux.HandleFunc("/asset-manifest.json", ws.handleAssetManifest)
	mux.HandleFunc("/icon-192.png", ws.handleIcon192)
	mux.HandleFunc("/icon-512.png", ws.handleIcon512)
	mux.HandleFunc("/logo-mark.svg", ws.handleLogoMark)
	mux.HandleFunc("/favicon.ico", ws.handleFavicon)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "ok",
			"port":   ws.port,
			"uptime": time.Since(ws.startTime).String(),
		}
		// Report whether an agent backend is available. In daemon mode
		// (ws.agent == nil) agents are created per-client, so "available"
		// means the config manager can produce one — approximated here by
		// checking whether any client context has an agent.
		if ws.agent != nil {
			resp["agent_available"] = true
		} else {
			resp["agent_available"] = ws.serviceMode
		}
		ws.mutex.RLock()
		resp["active_queries"] = ws.activeQueries
		ws.mutex.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("/api/bootstrap", ws.handleAPIBootstrap)

	// Always-on goroutine dump endpoint. Unlike --debug-pprof (which requires
	// a separate port and is opt-in), this is available on the main webui port
	// so a stuck session can be diagnosed by curling
	//   curl http://localhost:<port>/debug/goroutines
	// without restarting the process. Returns the same stack dump as SIGQUIT
	// but does NOT kill the process.
	mux.HandleFunc("/debug/goroutines", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_ = pprof.Lookup("goroutine").WriteTo(w, 1)
	})
}

// registerTerminalRoutes keeps the LSP manager initialization and the /api/lsp/ws
// WebSocket bridge (a non-JSON route that stays a plain handler). The
// /api/terminal/* routes and /api/lsp/status are Huma operations (see
// registerTerminalHumaOperations in huma_terminal.go and
// registerFilesHumaOperations in huma_files.go); their plain registrations were
// removed so each method+path pattern is registered once.
func (ws *ReactWebServer) registerTerminalRoutes(mux *http.ServeMux, ctx context.Context) {
	ws.lspManager = lspproxy.NewManager(ctx)
	// /api/lsp/ws is a WebSocket bridge, not a JSON operation, so it stays a
	// plain handler.
	mux.HandleFunc("/api/lsp/ws", lspproxy.BridgeHandler(ws.lspManager, ws.upgrader, ws.workspaceRoot))
}
