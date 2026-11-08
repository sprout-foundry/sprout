//go:build !js

package appcheck

// static_server.go serves a build without a worker the way the platform
// hosts it: only the site directory, the same file lookup rules, and the
// same not-found handling.
import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// staticServer serves the site the way hosting does: the same lookup rules
// (/about → about.html or about/index.html), then its not-found handling.
// It listens on loopback only.
func staticServer(site, notFound string, port int) *http.Server {
	paths := siteServer{site: site, notFound: notFound}
	srv := &http.Server{
		Handler:           paths,
		ReadHeaderTimeout: 5 * time.Second,
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
	}
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		// The port was free a moment ago; a handler-less server reports the
		// failure through every request, and waitFor turns it into "the
		// hosted app did not start".
		paths.listenErr = err
	} else {
		go func() { _ = srv.Serve(listener) }()
	}
	return srv
}

// siteServer resolves request paths to files in the served directory.
type siteServer struct {
	site      string
	notFound  string
	listenErr error
}

// contentTypes maps a file extension to the Content-Type served for it.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript",
	".css":   "text/css",
	".svg":   "image/svg+xml",
	".json":  "application/json",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".txt":   "text/plain",
}

func (s siteServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.listenErr != nil {
		http.Error(w, s.listenErr.Error(), http.StatusServiceUnavailable)
		return
	}
	path := r.URL.Path
	hit := s.file(path)
	if hit == "" {
		hit = s.file(path + ".html")
	}
	if hit == "" {
		hit = s.file(filepath.Join(path, "index.html"))
	}
	status := http.StatusOK
	if hit == "" {
		status = http.StatusNotFound
		hit, status = s.notFoundPage(path)
	}
	contentType := "text/plain"
	if hit != "" {
		if t, ok := contentTypes[filepath.Ext(hit)]; ok {
			contentType = t
		} else {
			contentType = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if hit != "" {
		data, err := os.ReadFile(hit) //nolint:gosec // G304: resolved inside the served directory
		if err == nil {
			_, _ = w.Write(data)
			return
		}
	}
	_, _ = io.WriteString(w, "Not found")
}

// notFoundPage applies the site's not-found handling to a missing path.
func (s siteServer) notFoundPage(path string) (string, int) {
	switch s.notFound {
	case "single-page-application":
		return s.file("/index.html"), http.StatusOK
	case "404-page":
		for dir := strings.TrimSuffix(path, "/"); ; dir = filepath.Dir(dir) {
			if hit := s.file(filepath.Join(dir, "404.html")); hit != "" {
				return hit, http.StatusNotFound
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return "", http.StatusNotFound
}

// file returns the absolute path of an existing regular file for a URL path,
// or "" .
func (s siteServer) file(urlPath string) string {
	abs := filepath.Join(s.site, filepath.FromSlash(urlPath))
	rel, err := filepath.Rel(s.site, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	return abs
}
