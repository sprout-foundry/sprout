// model_registry_server serves per-provider model JSON files as a static HTTP server.
// Designed to sit behind a CDN for sub-10ms latency.
//
// Usage:
//
//	go run ./cmd/model_registry_server --dir ./model_data --addr :8080
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func main() {
	dir := flag.String("dir", "./model_data", "directory containing model JSON files")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	modelsDir := filepath.Join(*dir, "models")
	if info, err := os.Stat(modelsDir); err != nil || !info.IsDir() {
		log.Fatalf("models directory not found: %s", modelsDir)
	}

	handler := newHandler(*dir)
	server := &http.Server{Addr: *addr, Handler: handler}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "received %s, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	fmt.Fprintf(os.Stderr, "model registry server listening on %s (dir: %s)\n", *addr, modelsDir)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	fmt.Fprintln(os.Stderr, "server stopped")
}

// newHandler creates the HTTP handler for the model registry.
// Exported for testing.
func newHandler(dir string) http.Handler {
	modelsDir := filepath.Join(dir, "models")

	mux := http.NewServeMux()

	mux.HandleFunc("/models/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		providerFile := strings.TrimPrefix(r.URL.Path, "/models/")
		if providerFile == "" || strings.Contains(providerFile, "/") {
			http.Error(w, "not found", http.StatusNotFound)
			logRequest(r, http.StatusNotFound, start)
			return
		}

		if !strings.HasSuffix(providerFile, ".json") {
			http.Error(w, "not found", http.StatusNotFound)
			logRequest(r, http.StatusNotFound, start)
			return
		}

		filePath := filepath.Join(modelsDir, filepath.Clean(providerFile))
		data, err := os.ReadFile(filePath)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			logRequest(r, http.StatusNotFound, start)
			return
		}

		if !json.Valid(data) {
			http.Error(w, "invalid JSON", http.StatusInternalServerError)
			logRequest(r, http.StatusInternalServerError, start)
			return
		}

		setCORSHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300, stale-while-revalidate=60")
		w.Header().Set("ETag", computeETag(data))
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		logRequest(r, http.StatusOK, start)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		setCORSHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"service": "ledit-model-registry",
			"models":  "/models/{provider}.json",
			"health":  "/health",
		})
	})

	return mux
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func logRequest(r *http.Request, status int, start time.Time) {
	ms := time.Since(start).Milliseconds()
	fmt.Fprintf(os.Stderr, "%s %s %d %dms\n", r.Method, r.URL.Path, status, ms)
}

func computeETag(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(h[:]))
}
