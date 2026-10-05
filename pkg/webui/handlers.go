//go:build !js

package webui

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// handleIndex serves the React application
func (ws *ReactWebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		writeJSONErr(w, http.StatusNotFound, "api_endpoint_not_found", "API endpoint not found")
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
  <title>sprout — UI not built</title>
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
    <h1>sprout — UI not built</h1>
    <p>The React front-end is not embedded in this binary. This happens when
       <code>sprout</code> is installed with <code>go install</code> from a source
       tree where <code>pkg/webui/static/</code> is gitignored.</p>
    <p>Build and embed the UI, then rebuild the binary:</p>
    <pre>git clone https://github.com/sprout-foundry/sprout
cd sprout
make build-all    # builds React UI + Go binary
go install .</pre>
    <p>Or download a pre-built release from
       <a href="https://github.com/sprout-foundry/sprout/releases">GitHub Releases</a>.</p>
    <p>The <code>/health</code> and <code>/api/*</code> endpoints are available
       and working normally.</p>
  </div>
</body>
</html>
`

// handleAssets serves Vite-bundled assets from static/assets/ with proper MIME types
func (ws *ReactWebServer) handleAssets(w http.ResponseWriter, r *http.Request) {
	filePath := strings.TrimPrefix(r.URL.Path, "/assets/")
	if filePath == "" || strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") || strings.HasPrefix(filePath, "\\") {
		http.NotFound(w, r)
		return
	}

	data, err := readStaticFile("assets/" + filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if contentType := assetContentType(path.Ext(filePath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	// Vite hashes filenames, so these are immutable — cache aggressively
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}

// handleWasmAssets serves the WASM runtime files (sprout.wasm,
// wasm_exec.js) from static/wasm/. Before this route existed the requests
// fell into the SPA catch-all and were answered with index.html as
// text/html — the shell fetch passed its ok check and WebAssembly
// instantiate died with an "invalid magic number" compile error.
//
// sprout.wasm is served precompressed when the client advertises support:
// the raw binary is ~55MB, ~15MB gzipped, ~12MB brotli — transfer time
// dominates embed startup. The .gz/.br siblings are produced by
// scripts/build-wasm.sh next to the raw binary.
func (ws *ReactWebServer) handleWasmAssets(w http.ResponseWriter, r *http.Request) {
	filePath := strings.TrimPrefix(r.URL.Path, "/wasm/")
	if filePath == "" || strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") || strings.HasPrefix(filePath, "\\") {
		http.NotFound(w, r)
		return
	}

	if encoded, encoding, ok := negotiatePrecompressedWasm(w, r, filePath); ok {
		data, err := readStaticFile("wasm/" + filePath + "." + encoded)
		if err == nil {
			if contentType := assetContentType(path.Ext(filePath)); contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			w.Header().Set("Content-Encoding", encoding)
			// The binary is content-addressed by the release tag; cache
			// hard. (wasm_exec.js rides the same release discipline — see
			// the checked-in-browser-runtime note in scripts/build-wasm.sh.)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			// Vary on Accept-Encoding so shared caches don't serve a
			// brotli body to a client that asked for identity.
			w.Header().Add("Vary", "Accept-Encoding")
			w.Write(data)
			return
		}
		// Precompressed variant missing (e.g. a dist built before this
		// change): fall through to the raw file rather than 404.
	}

	data, err := readStaticFile("wasm/" + filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if contentType := assetContentType(path.Ext(filePath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	// The binary is content-addressed by the release tag; cache hard.
	// (wasm_exec.js rides the same release discipline — see the
	// checked-in-browser-runtime note in scripts/build-wasm.sh.)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}

// negotiatePrecompressedWasm picks the best precompressed variant the
// client supports. Token matching is substring-based but q=0 rejections
// disable the token (a client sending "gzip;q=0" cannot decompress a
// gzip body — serving one would hard-break the load). No full q-value
// parsing: the raw file is always the fallback.
func negotiatePrecompressedWasm(_ http.ResponseWriter, r *http.Request, filePath string) (encoded, encoding string, ok bool) {
	if filePath != "sprout.wasm" {
		return "", "", false // wasm_exec.js compresses poorly and is small
	}
	ae := r.Header.Get("Accept-Encoding")
	switch {
	case strings.Contains(ae, "br") && !rejectsEncoding(ae, "br"):
		return "br", "br", true
	case strings.Contains(ae, "gzip") && !rejectsEncoding(ae, "gzip"):
		return "gz", "gzip", true
	default:
		return "", "", false
	}
}

// rejectsEncoding reports whether the Accept-Encoding header explicitly
// rejects `token` (a ";q=0" qualifier, optionally ";q=0.000"-style).
func rejectsEncoding(ae, token string) bool {
	for _, part := range strings.Split(ae, ",") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.HasPrefix(part, token) {
			continue
		}
		q := strings.ToLower(strings.TrimPrefix(part, token))
		if strings.HasPrefix(q, ";q=0") && !strings.HasPrefix(q, ";q=0.") {
			return true
		}
		// ";q=0.000" variants: parse the value.
		if strings.HasPrefix(q, ";q=0.") {
			if v, err := strconv.ParseFloat(strings.TrimPrefix(q, ";q="), 64); err == nil && v == 0 {
				return true
			}
		}
	}
	return false
}

// handleStandalonePage serves the editor.html / terminal.html component
// entries. Like the WASM assets these previously fell into the SPA
// catch-all and returned the React app instead of the component shells.
func (ws *ReactWebServer) handleStandalonePage(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name != "editor.html" && name != "terminal.html" {
		http.NotFound(w, r)
		return
	}

	data, err := readStaticFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The pages are entry documents: they must revalidate so a rebuilt
	// bundle (new hashed asset URLs) is picked up immediately.
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Write(data)
}

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

	if contentType := assetContentType(path.Ext(filePath)); contentType != "" {
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
