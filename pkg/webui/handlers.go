package webui

import (
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// handleIndex serves the React application
func (ws *ReactWebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		http.Error(w, "API endpoint not found", http.StatusNotFound)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/ws") || strings.HasPrefix(r.URL.Path, "/terminal") {
		http.NotFound(w, r)
		return
	}

	data, err := readStaticFile("index.html")
	if err != nil {
		// The binary was built without embedding the React UI (e.g. installed
		// via "go install" from a source tree where pkg/webui/static/ is
		// gitignored).  Serve a helpful page instead of a bare 404.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, uiBuildRequiredHTML)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}

// uiBuildRequiredHTML is shown when the binary was built without the embedded
// React UI — typically after "go install" from a fresh clone where
// pkg/webui/static/ is gitignored.
const uiBuildRequiredHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>ledit — UI not built</title>
  <style>
    body { font-family: system-ui, sans-serif; background: #0f172a; color: #e2e8f0;
           display: flex; align-items: center; justify-content: center;
           min-height: 100vh; margin: 0; }
    .card { background: #1e293b; border: 1px solid #334155; border-radius: 12px;
            padding: 2rem 2.5rem; max-width: 520px; width: 90%; }
    h1 { color: #14b8c8; margin: 0 0 1rem; font-size: 1.4rem; }
    code { background: #0f172a; color: #94a3b8; border-radius: 4px;
           padding: 0.15rem 0.4rem; font-size: 0.9em; }
    pre  { background: #0f172a; color: #94a3b8; border-radius: 8px;
           padding: 1rem; overflow-x: auto; font-size: 0.875rem; line-height: 1.6; }
    a { color: #14b8c8; }
    p { line-height: 1.6; margin: 0.75rem 0; }
  </style>
</head>
<body>
  <div class="card">
    <h1>ledit — UI not built</h1>
    <p>The React front-end is not embedded in this binary. This happens when
       <code>ledit</code> is installed with <code>go install</code> from a source
       tree where <code>pkg/webui/static/</code> is gitignored.</p>
    <p>Build and embed the UI, then rebuild the binary:</p>
    <pre>git clone https://github.com/alantheprice/ledit
cd ledit
make build-all    # builds React UI + Go binary
go install .</pre>
    <p>Or download a pre-built release from
       <a href="https://github.com/alantheprice/ledit/releases">GitHub Releases</a>.</p>
    <p>The <code>/health</code> and <code>/api/*</code> endpoints are available
       and working normally.</p>
  </div>
</body>
</html>
`

// handleStaticFiles serves static files with proper MIME types
func (ws *ReactWebServer) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/static/") {
		http.NotFound(w, r)
		return
	}

	filePath := strings.TrimPrefix(r.URL.Path, "/static/")
	if filePath == "" || strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") || strings.HasPrefix(filePath, "\\") {
		http.NotFound(w, r)
		return
	}

	data, err := readStaticFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if contentType := mime.TypeByExtension(path.Ext(filePath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}

// handleServiceWorker serves the Service Worker with proper MIME type
func (ws *ReactWebServer) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := readStaticFile("sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}

func (ws *ReactWebServer) handleManifest(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAsset(w, r, "manifest.json", "application/manifest+json; charset=utf-8")
}

func (ws *ReactWebServer) handleBrowserConfig(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAsset(w, r, "browserconfig.xml", "application/xml; charset=utf-8")
}

func (ws *ReactWebServer) handleAssetManifest(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAsset(w, r, "asset-manifest.json", "application/json; charset=utf-8")
}

func (ws *ReactWebServer) handleIcon192(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAsset(w, r, "icon-192.png", "image/png")
}

func (ws *ReactWebServer) handleIcon512(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAsset(w, r, "icon-512.png", "image/png")
}

func (ws *ReactWebServer) handleLogoMark(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAsset(w, r, "logo-mark.svg", "image/svg+xml; charset=utf-8")
}

func (ws *ReactWebServer) handleFavicon(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAssetOptional(w, r, "favicon.ico", "image/x-icon")
}

func (ws *ReactWebServer) serveRootAsset(w http.ResponseWriter, r *http.Request, name string, contentType string) {
	data, err := ws.readRootAsset(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ws.writeEmbeddedBytes(w, data, contentType, false)
}

func (ws *ReactWebServer) serveRootAssetOptional(w http.ResponseWriter, r *http.Request, name string, contentType string) {
	data, err := ws.readRootAsset(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ws.writeEmbeddedBytes(w, data, contentType, false)
}

func (ws *ReactWebServer) serveEmbeddedFile(w http.ResponseWriter, r *http.Request, embeddedPath string, contentType string, optional bool, cacheable bool) {
	data, err := readStaticFile(strings.TrimPrefix(embeddedPath, "static/"))
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	ws.writeEmbeddedBytes(w, data, contentType, cacheable)
}

func (ws *ReactWebServer) readRootAsset(name string) ([]byte, error) {
	data, err := readStaticFile(name)
	if err != nil {
		return nil, fmt.Errorf("read root asset %q: %w", name, err)
	}
	return data, nil
}

func (ws *ReactWebServer) writeEmbeddedBytes(w http.ResponseWriter, data []byte, contentType string, cacheable bool) {
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if cacheable {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	} else {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	w.Write(data)
}
