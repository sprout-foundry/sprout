//go:build !js

// preview_origin.go serves workspace files to the HTML preview iframe from a
// dedicated loopback listener with its own origin.
//
// Why a second origin: the preview document runs untrusted HTML (a downloaded
// artifact bundle, a mockup from elsewhere). Rendered same-origin it could read
// sessionStorage ("sprout_auth_token"), the client-ID cookie, and call any API
// with the user's credentials. A separate listener makes the previewed page
// cross-origin to the app, so the browser's same-origin policy does the
// isolating rather than a sandbox attribute the page could be served without.
//
// The listener serves exactly two things:
//
//	/__preview/<token>/<workspace-relative-path>  the file, with its MIME type
//	/__preview/<token>/__sprout_preview_status    readiness probe
//
// Every path is resolved through canonicalizePath against the workspace root,
// so traversal and symlink escapes are rejected the same way /api/file rejects
// them. The token is minted per preview open and expires, so a stale URL in a
// browser history cannot re-read the workspace.
package webui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// previewTokenTTL bounds how long a minted preview URL stays valid. Long
	// enough to read a document, short enough that a leaked URL goes stale.
	previewTokenTTL = 30 * time.Minute
	previewPathPref = "/__preview/"
	// previewCSP allows the CDN scripts a bundled artifact loads (React,
	// Babel). It is deliberately permissive on script-src and strict
	// everywhere else: no connect-src to the app origin, and no access to the
	// app's cookies because the origin differs.
	//
	// frame-ancestors is NOT 'self': the preview origin is a different origin
	// from the app that embeds it, so 'self' would block the app's iframe and
	// render the preview blank. The app origin is the only permitted embedder;
	// the token in the path is what stops other pages from loading a preview.
	previewCSP = "default-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob: https://unpkg.com https://cdn.jsdelivr.net; " +
		"script-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob: https://unpkg.com https://cdn.jsdelivr.net; " +
		"style-src 'self' 'unsafe-inline' data: https://fonts.googleapis.com; " +
		"img-src 'self' data: blob:; " +
		"font-src 'self' data: blob: https://fonts.gstatic.com; " +
		"connect-src 'self' data: blob: https://unpkg.com https://cdn.jsdelivr.net; " +
		"worker-src 'self' blob:; " +
		"child-src 'self' blob:; " +
		"object-src 'none';"
)

// previewToken is one minted preview grant.
type previewToken struct {
	expires time.Time
}

// previewOrigin is the preview listener's state: the bound address and the
// live token set.
type previewOrigin struct {
	mu      sync.RWMutex
	tokens  map[string]previewToken
	addr    string
	server  *http.Server
	root    string
	started bool
}

func newPreviewOrigin() *previewOrigin {
	return &previewOrigin{tokens: make(map[string]previewToken)}
}

// mintToken issues a fresh preview token for a workspace root and returns it.
func (p *previewOrigin) mintToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("minting preview token: %w", err)
	}
	token := hex.EncodeToString(buf)
	p.mu.Lock()
	defer p.mu.Unlock()
	// Drop expired grants on every mint so the map cannot grow without bound.
	now := time.Now()
	for t, grant := range p.tokens {
		if now.After(grant.expires) {
			delete(p.tokens, t)
		}
	}
	p.tokens[token] = previewToken{expires: now.Add(previewTokenTTL)}
	return token, nil
}

// validToken reports whether token is a live grant.
func (p *previewOrigin) validToken(token string) bool {
	p.mu.RLock()
	grant, ok := p.tokens[token]
	p.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Now().Before(grant.expires)
}

// start binds the preview listener on loopback and begins serving. It is a
// no-op when already started. A bind failure is returned to the caller, which
// degrades the preview to srcDoc rather than failing the whole server.
func (p *previewOrigin) start(workspaceRoot string) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("binding preview listener: %w", err)
	}

	p.mu.Lock()
	// Resolve the root the same way canonicalizePath resolves request paths
	// (EvalSymlinks), so isWithinWorkspace compares like with like. On macOS a
	// temp dir is /var/... while its resolved form is /private/var/..., and an
	// unresolved root would reject every file under it.
	resolvedRoot := workspaceRoot
	if resolved, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		resolvedRoot = resolved
	}
	p.root = resolvedRoot
	p.addr = listener.Addr().String()
	p.server = &http.Server{Handler: p.handler(), ReadHeaderTimeout: 10 * time.Second}
	p.started = true
	server := p.server
	p.mu.Unlock()

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			webuiLogger.Warn("preview listener stopped", "err", serveErr)
		}
	}()
	return nil
}

// address returns the listener's host:port, or "" when not started.
func (p *previewOrigin) address() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.addr
}

// stop shuts the listener down and clears the address, so a stopped origin
// reports itself unavailable rather than minting URLs nothing serves.
func (p *previewOrigin) stop() {
	p.mu.Lock()
	server := p.server
	p.started = false
	p.server = nil
	p.addr = ""
	p.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
}

func (p *previewOrigin) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The preview origin is a separate origin, so it needs its own CORS
		// posture: no credentials, and the app origin is the only allowed
		// caller (it only ever embeds, but a fetch from the shell is harmless).
		w.Header().Set("Content-Security-Policy", previewCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rest := strings.TrimPrefix(r.URL.Path, previewPathPref)
		if rest == r.URL.Path {
			http.NotFound(w, r)
			return
		}
		token, relPath, found := strings.Cut(rest, "/")
		if !found {
			http.NotFound(w, r)
			return
		}
		if !p.validToken(token) {
			http.Error(w, "preview link expired", http.StatusForbidden)
			return
		}
		if relPath == "" {
			http.NotFound(w, r)
			return
		}

		p.mu.RLock()
		root := p.root
		p.mu.RUnlock()

		// Resolve through the same canonicalizer /api/file uses so traversal
		// and symlink escapes are rejected identically.
		canonical, err := canonicalizePath(relPath, root, false)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !isWithinWorkspace(canonical, root) {
			http.Error(w, "path outside workspace", http.StatusForbidden)
			return
		}

		info, err := os.Stat(canonical)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		content, err := os.ReadFile(canonical) //nolint:gosec // G304: canonical, workspace-verified path
		if err != nil {
			http.Error(w, "failed to read file", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", previewContentType(canonical, content))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		// The bytes are a workspace file served with an explicit Content-Type
		// and nosniff, from an origin whose CSP confines it — this is the
		// documented purpose of the endpoint, not an injection sink.
		_, _ = w.Write(content) //nolint:gosec // G705: serving workspace files is the endpoint's purpose
	})
}

// previewContentType picks a MIME type for a previewed asset. HTML is forced
// to text/html so the document renders rather than displaying as source, and
// scripts/styles get their real types so relative imports execute.
func previewContentType(path string, content []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	}
	if byExt := mime.TypeByExtension(filepath.Ext(path)); byExt != "" {
		return byExt
	}
	if detected := http.DetectContentType(content); detected != "" {
		return detected
	}
	return "application/octet-stream"
}

// previewURLFor builds the URL the iframe loads for a workspace-relative path.
// The token is compared in constant time by validToken's map lookup, so no
// extra comparison is needed here.
func (p *previewOrigin) previewURLFor(token, relPath string) string {
	addr := p.address()
	if addr == "" {
		return ""
	}
	clean := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	return "http://" + addr + previewPathPref + token + "/" + clean
}
