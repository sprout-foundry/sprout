// Command model_registry_server runs a lightweight static file server
// for serving per-provider model JSON files. This server is designed
// for deployment behind a CDN (e.g., Cloudflare Workers, CloudFront) to
// achieve sub-10ms latency. It serves files directly with no processing.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	defaultPort         = 8080
	defaultAddr         = "0.0.0.0"
	defaultRegistryDir  = "./registry/models"
	defaultReadTimeout  = 5 * time.Second
	defaultWriteTimeout = 10 * time.Second
	defaultIdleTimeout  = 60 * time.Second
	defaultCacheMaxAge  = 300 // 5 minutes in seconds
)

var (
	port        = flag.Int("port", defaultPort, "HTTP port to listen on")
	addr        = flag.String("addr", defaultAddr, "HTTP address to listen on")
	registryDir = flag.String("dir", defaultRegistryDir, "Directory containing model JSON files")
	version     = flag.Bool("version", false, "Print version and exit")
)

var buildInfo = struct {
	Version   string
	Commit    string
	BuildTime string
}{
	Version:   "dev",
	Commit:    "unknown",
	BuildTime: "unknown",
}

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("model_registry_server %s (commit: %s, built: %s)\n",
			buildInfo.Version, buildInfo.Commit, buildInfo.BuildTime)
		os.Exit(0)
	}

	// Validate registry directory exists
	if err := validateRegistryDir(*registryDir); err != nil {
		log.Fatalf("registry directory validation failed: %v", err)
	}

	// Create HTTP file server with custom handler
	mux := http.NewServeMux()
	mux.HandleFunc("/", loggingMiddleware(handleRoot))
	mux.HandleFunc("/health", loggingMiddleware(handleHealth))
	mux.HandleFunc("/models/", loggingMiddleware(handleModels))

	// Configure server with appropriate timeouts
	server := &http.Server{
		Addr:           fmt.Sprintf("%s:%d", *addr, *port),
		Handler:        mux,
		ReadTimeout:    defaultReadTimeout,
		WriteTimeout:   defaultWriteTimeout,
		IdleTimeout:    defaultIdleTimeout,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	// Set up graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		log.Printf("[server] Starting model registry server on %s:%d", *addr, *port)
		log.Printf("[server] Serving model files from: %s", *registryDir)
		log.Printf("[server] Endpoints:")
		log.Printf("  GET /                  - API info and available providers")
		log.Printf("  GET /health            - Health check")
		log.Printf("  GET /models/<id>.json  - Per-provider model list")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[server] failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-stop
	log.Printf("[server] Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[server] shutdown error: %v", err)
	} else {
		log.Printf("[server] Shutdown complete")
	}
}

// validateRegistryDir checks that the registry directory exists and is a directory.
func validateRegistryDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("registry directory does not exist: %s", dir)
		}
		return fmt.Errorf("failed to stat registry directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("registry path is not a directory: %s", dir)
	}
	return nil
}

// handleRoot serves API information and lists available providers.
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	providers, err := listAvailableProviders(*registryDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list providers: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"name":        "Model Registry Server",
		"version":     buildInfo.Version,
		"commit":      buildInfo.Commit,
		"build_time":  buildInfo.BuildTime,
		"description": "Static file server for per-provider model lists",
		"endpoints": map[string]string{
			"/health":                 "Health check",
			"/models/<provider>.json": "Per-provider model list",
		},
		"providers": providers,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// handleHealth returns a simple health check response.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// handleModels serves per-provider model JSON files with security protections.
func handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract provider ID from path: /models/<provider>.json
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/models/"), "/")
	if len(parts) != 1 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	filename := parts[0]
	if filename == "" || !strings.HasSuffix(filename, ".json") {
		http.Error(w, "filename must end with .json", http.StatusBadRequest)
		return
	}

	// Validate provider ID (lowercase letters, numbers, hyphens, underscores only).
	// Use 400 Bad Request — the input is malformed, not unauthorized.
	providerID := strings.TrimSuffix(filename, ".json")
	if !isValidProviderID(providerID) {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}

	// Build and sanitize the file path to prevent path traversal.
	filePath := filepath.Join(*registryDir, filename)
	cleanPath := filepath.Clean(filePath)
	registryDirClean := filepath.Clean(*registryDir)

	// Defense-in-depth: use filepath.Rel to detect any escape attempt,
	// including via symlinks on Unix or case-insensitive drive letters on Windows.
	relPath, err := filepath.Rel(registryDirClean, cleanPath)
	if err != nil || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || relPath == ".." {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Verify symlinks don't escape the registry directory. A file named
	// "openrouter.json" may be a symlink pointing outside the registry.
	resolvedPath, symlinkErr := filepath.EvalSymlinks(filePath)
	if symlinkErr != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	// Resolve the registry dir the same way so the Rel comparison works
	// on macOS where /var/folders is a symlink to /private/var/folders.
	resolvedRegistryDir := registryDirClean
	if evaled, err := filepath.EvalSymlinks(registryDirClean); err == nil {
		resolvedRegistryDir = evaled
	}
	resolvedRel, err := filepath.Rel(resolvedRegistryDir, resolvedPath)
	if err != nil || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || resolvedRel == ".." {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Set security and cache headers before serving.
	// Explicit Content-Type ensures correct MIME even if ServeFile sniffs differently.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", defaultCacheMaxAge))

	http.ServeFile(w, r, filePath)
}

// isValidProviderID checks that a provider ID contains only safe characters:
// lowercase letters, digits, hyphens, and underscores. Max length 128 chars.
func isValidProviderID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// listAvailableProviders scans the registry directory for valid provider JSON files.
func listAvailableProviders(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var providers []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".json") {
			providerID := strings.TrimSuffix(name, ".json")
			if isValidProviderID(providerID) {
				providers = append(providers, providerID)
			}
		}
	}
	return providers, nil
}

// loggingMiddleware wraps a handler to log request method, path, status, and duration.
// Sanitizes the URL path to prevent log injection.
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingResponseWriter{ResponseWriter: w}
		next(lw, r)
		duration := time.Since(start)
		log.Printf("[request] %s %s status=%d duration=%v",
			r.Method, sanitizeForLog(r.URL.Path), lw.status, duration)
	}
}

// sanitizeForLog strips control characters (except tab) from a string to prevent
// log injection attacks where crafted URLs insert fake log entries.
func sanitizeForLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || (r >= 32 && r <= 126) {
			return r
		}
		return -1
	}, s)
}

// loggingResponseWriter wraps http.ResponseWriter to capture the status code
// for logging. Defaults to 200 if WriteHeader is never called.
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (lw *loggingResponseWriter) WriteHeader(code int) {
	lw.wrote = true
	lw.status = code
	lw.ResponseWriter.WriteHeader(code)
}

func (lw *loggingResponseWriter) Write(b []byte) (int, error) {
	if !lw.wrote {
		lw.wrote = true
		lw.status = http.StatusOK
	}
	return lw.ResponseWriter.Write(b)
}

// init sets build info from environment variables.
// Can be overridden at build time via:
//
//	go build -ldflags "-X main.VERSION=$VERSION -X main.COMMIT=$GIT_SHA -X main.BUILD_TIME=$BUILD_TIME"
func init() {
	if v := os.Getenv("VERSION"); v != "" {
		buildInfo.Version = v
	}
	if v := os.Getenv("COMMIT"); v != "" {
		buildInfo.Commit = v
	}
	if v := os.Getenv("BUILD_TIME"); v != "" {
		buildInfo.BuildTime = v
	}
}
