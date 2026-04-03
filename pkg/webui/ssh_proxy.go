package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// sshProxyHTTPClient is shared across proxy calls. Long timeout because some
// API calls (e.g. streaming queries) can run for several minutes.
var sshProxyHTTPClient = &http.Client{
	Timeout: 10 * time.Minute,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// sshProxyUpgrader is the WebSocket upgrader used for inbound proxy connections.
var sshProxyUpgrader = websocket.Upgrader{
	CheckOrigin:     func(*http.Request) bool { return true },
	ReadBufferSize:  65536,
	WriteBufferSize: 65536,
}

// resolveInitialWorkspace expands shell-like home references in a workspace path
// so the frontend receives a concrete path it can pass to the backend API.
//
//	"$HOME"   → "$HOME"   (kept as-is; parsed by BROWSER=none frontend context)
//	"${HOME}"  → "${HOME}"  (kept as-is)
//	"~/..."   → "$HOME/..."
//	everything else → original
//
// The actual expansion of $HOME happens on the remote ledit daemon side;
// this function only handles ~/ shortcuts that the frontend needs to resolve
// to send a valid absolute path to the setWorkspace API.
func resolveInitialWorkspace(path string) string {
	if strings.HasPrefix(path, "~/") {
		rest := strings.TrimPrefix(path, "~/")
		if rest != "" {
			return "$HOME/" + rest
		}
		return "$HOME"
	}
	return path
}

// handleSSHProxy is the catch-all handler for /ssh/{encodedKey}/{rest…}.
// It routes:
//   - root / index paths → local index.html with LEDIT_PROXY_BASE injected
//   - /static/…, /sw.js, /manifest.json, etc. → local embedded assets
//   - /ws, /terminal (WebSocket upgrade) → proxied to the SSH tunnel port
//   - everything else → HTTP-proxied to the SSH tunnel port
func (srv *ReactWebServer) handleSSHProxy(w http.ResponseWriter, r *http.Request) {
	// Strip the /ssh/ prefix so we're left with "{encodedKey}/{rest}"
	trimmed := strings.TrimPrefix(r.URL.Path, "/ssh/")

	var encodedKey, rest string
	if idx := strings.Index(trimmed, "/"); idx < 0 {
		encodedKey = trimmed
		rest = "/"
	} else {
		encodedKey = trimmed[:idx]
		rest = trimmed[idx:]
	}

	sessionKey, err := url.PathUnescape(encodedKey)
	if err != nil {
		http.Error(w, "invalid session key", http.StatusBadRequest)
		return
	}

	srv.sshSessionsMu.Lock()
	session := srv.sshSessions[sessionKey]
	var tunnelPort int
	if session != nil {
		tunnelPort = session.LocalPort
	}
	srv.sshSessionsMu.Unlock()

	if session == nil {
		http.Error(w, "SSH session not found or expired", http.StatusNotFound)
		return
	}

	// Re-encode the session key so LEDIT_PROXY_BASE is consistently
	// percent-encoded, matching what launchSSHWorkspace returns as ProxyBase.
	proxyBase := "/ssh/" + url.PathEscape(sessionKey)

	// WebSocket upgrade — proxy to the tunnel.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		sshProxyWebSocket(w, r, tunnelPort, rest)
		return
	}

	// Static assets served locally (no round-trip to the remote backend).
	switch {
	case rest == "/" || rest == "" || rest == "/index.html":
		sshServeIndexWithBase(w, proxyBase, resolveInitialWorkspace(session.RemoteWorkspacePath))
		return
	case strings.HasPrefix(rest, "/static/"):
		// Reuse the existing handler by rewriting URL.Path temporarily.
		original := r.URL.Path
		r.URL.Path = rest
		srv.handleStaticFiles(w, r)
		r.URL.Path = original
		return
	case rest == "/sw.js":
		original := r.URL.Path
		r.URL.Path = rest
		srv.handleServiceWorker(w, r)
		r.URL.Path = original
		return
	case rest == "/manifest.json":
		original := r.URL.Path
		r.URL.Path = rest
		srv.handleManifest(w, r)
		r.URL.Path = original
		return
	case rest == "/favicon.ico":
		original := r.URL.Path
		r.URL.Path = rest
		srv.handleFavicon(w, r)
		r.URL.Path = original
		return
	case rest == "/browserconfig.xml":
		original := r.URL.Path
		r.URL.Path = rest
		srv.handleBrowserConfig(w, r)
		r.URL.Path = original
		return
	case rest == "/asset-manifest.json":
		original := r.URL.Path
		r.URL.Path = rest
		srv.handleAssetManifest(w, r)
		r.URL.Path = original
		return
	case rest == "/logo-mark.svg":
		original := r.URL.Path
		r.URL.Path = rest
		srv.handleLogoMark(w, r)
		r.URL.Path = original
		return
	case strings.HasPrefix(rest, "/icon-") && strings.HasSuffix(rest, ".png"):
		original := r.URL.Path
		r.URL.Path = rest
		if rest == "/icon-192.png" {
			srv.handleIcon192(w, r)
		} else {
			srv.handleIcon512(w, r)
		}
		r.URL.Path = original
		return
	}

	// Everything else: proxy to the SSH tunnel backend.
	sshProxyHTTP(w, r, tunnelPort, rest)
}

// sshServeIndexWithBase reads the embedded index.html and injects a small
// inline script that sets window.LEDIT_PROXY_BASE and window.LEDIT_INITIAL_WORKSPACE
// before the </head> tag.
func sshServeIndexWithBase(w http.ResponseWriter, proxyBase, initialWorkspace string) {
	data, err := readStaticFile("index.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// JSON-encode the values so any characters that require escaping in a JS
	// string literal are handled correctly.
	proxyBaseJSON, _ := json.Marshal(proxyBase)
	initialWorkspaceJSON, _ := json.Marshal(initialWorkspace)
	script := []byte(
		"<script>window.LEDIT_PROXY_BASE=" + string(proxyBaseJSON) +
			";window.LEDIT_INITIAL_WORKSPACE=" + string(initialWorkspaceJSON) +
			";</script>",
	)
	data = bytes.Replace(data, []byte("</head>"), append(script, []byte("</head>")...), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}

// sshProxyHTTP forwards an HTTP request to the SSH tunnel backend and copies
// the response back to the client.
func sshProxyHTTP(w http.ResponseWriter, r *http.Request, tunnelPort int, targetPath string) {
	targetURL := &url.URL{
		Scheme:   "http",
		Host:     fmt.Sprintf("127.0.0.1:%d", tunnelPort),
		Path:     targetPath,
		RawQuery: r.URL.RawQuery,
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
	if err != nil {
		http.Error(w, "proxy error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy all request headers.
	for key, vals := range r.Header {
		proxyReq.Header[key] = vals
	}
	// Correct the Host header so the remote backend doesn't reject it.
	proxyReq.Host = targetURL.Host

	resp, err := sshProxyHTTPClient.Do(proxyReq)
	if err != nil {
		http.Error(w, "remote backend unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers and status.
	for key, vals := range resp.Header {
		w.Header()[key] = vals
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// sshProxyWebSocket upgrades the inbound request and bidirectionally pipes
// messages between the client and the SSH tunnel backend WebSocket.
func sshProxyWebSocket(w http.ResponseWriter, r *http.Request, tunnelPort int, targetPath string) {
	scheme := "ws"
	targetURL := fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, tunnelPort, targetPath)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Forward client-identifying headers to the upstream.
	forwardHeaders := http.Header{}
	for _, h := range []string{"X-Ledit-Client-ID", "X-Forwarded-For"} {
		if v := r.Header.Get(h); v != "" {
			forwardHeaders.Set(h, v)
		}
	}

	upstream, _, err := websocket.DefaultDialer.Dial(targetURL, forwardHeaders)
	if err != nil {
		http.Error(w, "failed to connect to remote backend WebSocket: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	downstream, err := sshProxyUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes its own error response.
		return
	}
	defer downstream.Close()

	errCh := make(chan error, 2)

	// upstream → downstream
	go func() {
		for {
			mt, msg, err := upstream.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if err := downstream.WriteMessage(mt, msg); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// downstream → upstream
	go func() {
		for {
			mt, msg, err := downstream.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if err := upstream.WriteMessage(mt, msg); err != nil {
				errCh <- err
				return
			}
		}
	}()

	<-errCh
}
