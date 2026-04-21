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
	"sync"
	"syscall"
	"time"
)

const (
	defaultPort         = 8080
	defaultRegistryDir  = "./registry"
	defaultReadTimeout  = 5 * time.Second
	defaultWriteTimeout = 10 * time.Second
	defaultIdleTimeout  = 60 * time.Second
)

var (
	port        = flag.Int("port", defaultPort, "HTTP port to listen on")
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
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/models/", handleModels)

	// Configure server with appropriate timeouts
	server := &http.Server{
		Addr:          fmt.Sprintf(":%d", *port),
		Handler:       mux,
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
		log.Printf("[server] Starting model registry server on port %d", *port)
		log.Printf("[server] Serving model files from: %s", *registryDir)
		log.Printf("[server] Endpoints:")
		log.Printf("  GET /              - API info")
		log.Printf("  GET /healthz       - Health check")
		log.Printf("  GET /models/<id>.json - Per-provider model list")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[server] failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-stop
	log.Printf("[server] Shutting down gracefully...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("[server] shutdown error: %v", err)
		}
	}()

	// Wait for shutdown to complete or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[server] Shutdown complete")
	case <-ctx.Done():
		log.Printf("[server] Shutdown timeout, forcing exit")
	}
}

// validateRegistryDir checks that the registry directory exists and contains models subdirectory
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

	// Check for models subdirectory
	modelsDir := filepath.Join(dir, "models")
	if info, err := os.Stat(modelsDir); err != nil {
		if os.IsNotExist(err) {
			// Models subdirectory does not exist yet - create it
			if err := os.MkdirAll(modelsDir, 0755); err != nil {
				return fmt.Errorf("failed to create models subdirectory: %w", err)
			}
			log.Printf("[server] Created models subdirectory: %s", modelsDir)
		} else {
			return fmt.Errorf("failed to stat models subdirectory: %w", err)
		}
	} else if !info.IsDir() {
		return fmt.Errorf("models path is not a directory: %s", modelsDir)
	}

	return nil
}

// handleRoot serves API information
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	response := map[string]interface{}{
		"name":        "Model Registry Server",
		"version":     buildInfo.Version,
		"commit":      buildInfo.Commit,
		"build_time":  buildInfo.BuildTime,
		"description": "Static file server for per-provider model lists",
		"endpoints": map[string]string{
			"/healthz":              "Health check",
			"/models/<provider>.json": "Per-provider model list",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// handleHealth returns simple health check
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// handleModels serves per-provider model JSON files
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

	// Validate provider ID (alphanumeric, hyphen, underscore)
	providerID := strings.TrimSuffix(filename, ".json")
	if !isValidProviderID(providerID) {
		http.Error(w, "invalid provider ID: must contain only lowercase letters, numbers, hyphens, and underscores", http.StatusBadRequest)
		return
	}

	// Build the file path and sanitize it to prevent path traversal
	filePath := filepath.Join(*registryDir, "models", filename)
	cleanPath := filepath.Clean(filePath)
	registryDirClean := filepath.Clean(*registryDir)
	modelsDirClean := filepath.Join(registryDirClean, "models")

	// Defense-in-depth: Verify the resolved path is within the models directory
	if !strings.HasPrefix(cleanPath, modelsDirClean+string(filepath.Separator)) &&
		cleanPath != modelsDirClean {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Check if file exists before serving to prevent information disclosure
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Add security headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	http.ServeFile(w, r, filePath)
}

// isValidProviderID checks that provider ID contains only safe characters
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

// init sets build info from environment variables.
// These can be set at build time via:
//   go build -ldflags "-X main.VERSION=$VERSION -X main.COMMIT=$GIT_SHA -X main.BUILD_TIME=$BUILD_TIME"
// or at runtime via env vars.
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
