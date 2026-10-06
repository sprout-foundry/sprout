package runner

import (
	"crypto/subtle"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// maxProxyBody matches the txn contract's manifest cap; the platform enforces
// its own cap and this is the runner-side backstop.
const maxProxyBody = 100 << 20

// HostServer is the only surface the platform reaches on a runner:
// /daemon/{workspaceID}/... proxied to that workspace's daemon on loopback,
// after checking the per-workspace bearer secret. The daemon checks the same
// secret again (its SPROUT_AUTH_TOKEN), so the runner is not the only gate.
type HostServer struct {
	mu       sync.RWMutex
	bindings map[string]binding
}

type binding struct {
	port   int
	secret string
}

// NewHostServer returns an empty host server.
func NewHostServer() *HostServer {
	return &HostServer{bindings: make(map[string]binding)}
}

// Bind routes a workspace to its daemon port, authenticated by secret.
func (h *HostServer) Bind(workspaceID string, port int, secret string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bindings[workspaceID] = binding{port: port, secret: secret}
}

// Unbind removes a workspace; later calls for it get 404.
func (h *HostServer) Unbind(workspaceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.bindings, workspaceID)
}

func (h *HostServer) lookup(workspaceID string) (binding, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	b, ok := h.bindings[workspaceID]
	return b, ok
}

// Handler is the host server's HTTP surface.
func (h *HostServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/daemon/", h.handleDaemon)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func (h *HostServer) handleDaemon(w http.ResponseWriter, r *http.Request) {
	workspaceID, daemonPath, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/daemon/"), "/")
	if !ok || workspaceID == "" || daemonPath == "" {
		http.NotFound(w, r)
		return
	}
	b, found := h.lookup(workspaceID)
	if !found {
		http.NotFound(w, r)
		return
	}
	token, hasBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	// An empty secret (a binding restored without one) never matches.
	if !hasBearer || token == "" || b.secret == "" || subtle.ConstantTimeCompare([]byte(token), []byte(b.secret)) != 1 {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	if r.ContentLength > maxProxyBody {
		http.Error(w, `{"error":"body too large"}`, http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxProxyBody)

	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(b.port)}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = "/" + daemonPath
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
		},
		// Stream as bytes arrive: run output and long transfers must not be
		// buffered behind the whole response.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, `{"error":"daemon unreachable"}`, http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}
