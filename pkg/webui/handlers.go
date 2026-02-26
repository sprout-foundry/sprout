package webui

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// handleIndex serves the React application
func (ws *ReactWebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}

// handleStaticFiles serves static files with proper MIME types
func (ws *ReactWebServer) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/static/") {
		http.NotFound(w, r)
		return
	}

	filePath := strings.TrimPrefix(r.URL.Path, "/static/")
	if filePath == "" || strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") {
		http.NotFound(w, r)
		return
	}

	data, err := staticFiles.ReadFile("static/" + filePath)
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
	data, err := staticFiles.ReadFile("static/sw.js")
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

func (ws *ReactWebServer) handleFavicon(w http.ResponseWriter, r *http.Request) {
	ws.serveRootAssetOptional(w, r, "favicon.ico", "image/x-icon")
}

func (ws *ReactWebServer) serveRootAsset(w http.ResponseWriter, r *http.Request, name string, contentType string) {
	ws.serveEmbeddedFile(w, r, "static/"+name, contentType, false)
}

func (ws *ReactWebServer) serveRootAssetOptional(w http.ResponseWriter, r *http.Request, name string, contentType string) {
	ws.serveEmbeddedFile(w, r, "static/"+name, contentType, true)
}

func (ws *ReactWebServer) serveEmbeddedFile(w http.ResponseWriter, r *http.Request, embeddedPath string, contentType string, optional bool) {
	data, err := staticFiles.ReadFile(embeddedPath)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}
