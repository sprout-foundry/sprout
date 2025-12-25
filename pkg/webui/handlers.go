package webui

import (
	"net/http"
	"os"
	"strings"
)

// handleIndex serves the React application
func (ws *ReactWebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Try to serve from embedded filesystem first
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		// Fallback to filesystem
		http.ServeFile(w, r, "./pkg/webui/static/index.html")
		return
	}

	// Set proper HTML content type
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleStaticFiles serves static files with proper MIME types
func (ws *ReactWebServer) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	// Remove /static/ prefix to get the relative path
	filePath := r.URL.Path[len("/static/"):]

	// Prevent directory traversal
	if filePath == "" || filePath[0] == '.' || filePath[0] == '/' {
		http.NotFound(w, r)
		return
	}

	// Try to serve from embedded filesystem first
	embeddedPath := "static/" + filePath
	data, err := staticFiles.ReadFile(embeddedPath)
	if err != nil {
		// Fallback to filesystem
		fullPath := "./pkg/webui/static/" + filePath
		http.ServeFile(w, r, fullPath)
		return
	}

	// Set appropriate Content-Type header based on file extension
	ext := ""
	if lastDot := strings.LastIndex(filePath, "."); lastDot != -1 {
		ext = filePath[lastDot:]
	}

	switch ext {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		// Handle Service Worker files specifically
		if filePath == "sw.js" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Service-Worker-Allowed", "/")
		} else {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		}
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		// Let Go's DetectContentType handle unknown types
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	// Enable caching for static assets
	w.Header().Set("Cache-Control", "public, max-age=3600") // 1 hour cache

	// Serve the embedded data
	w.Write(data)
}

// handleServiceWorker serves the Service Worker with proper MIME type
func (ws *ReactWebServer) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	// Try to serve from embedded filesystem first
	data, err := staticFiles.ReadFile("static/sw.js")
	if err != nil {
		// Fallback to filesystem
		fallbackPath := "./pkg/webui/static/sw.js"
		data, err = os.ReadFile(fallbackPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	// Set proper headers for Service Worker
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	w.Write(data)
}
