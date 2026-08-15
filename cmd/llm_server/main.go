//go:build cgo && ((darwin && arm64 && (mlx || ggml)) || (linux && ggml && (arm64 || amd64)))

// Command llm_server exposes the local gomlx LLM engine over an
// OpenAI-compatible HTTP API (POST /v1/chat/completions, GET /v1/models,
// GET /health), so sprout can use it as a provider via the generic provider
// machinery (like LM Studio / Ollama local endpoints).
//
// The HTTP layer lives in pkg/gomlx/llm/openaisserver (testable with a fake
// model); this command loads a real model and mounts it.
//
// Usage:
//
//	GO_QUANTIZE=4 go run -tags mlx ./cmd/llm_server -port 18081  # auto-select model by RAM
//	GO_QUANTIZE=4 go run -tags mlx ./cmd/llm_server -model ~/dev/llm-models/qwen3.5-4b-4bit -port 18081
//
// Then configure sprout with a provider whose endpoint is
// http://127.0.0.1:18081/v1/chat/completions.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/llm/openaisserver"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func main() {
	modelDir := flag.String("model", "", "path to the model directory (default: auto-select from ~/dev/llm-models by RAM)")
	port := flag.Int("port", 18081, "port to listen on (default matches the sprout-local provider config)")
	maxTokens := flag.Int("max-tokens", 512, "cap on max_tokens per request (0 = honor client value)")
	pprofAddr := flag.String("pprof", "", "serve net/http/pprof on this address (e.g. 127.0.0.1:6060); off when empty")
	flag.Parse()

	if *pprofAddr != "" {
		go func() {
			log.Printf("pprof listening on http://%s/debug/pprof/", *pprofAddr)
			if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
				log.Printf("pprof: %v", err)
			}
		}()
	}

	dir := *modelDir
	if dir == "" {
		modelsRoot := os.Getenv("HOME") + "/dev/llm-models"
		backend := tensor.DetectBackend()
		ram := uint64(8 * 1024 * 1024 * 1024)
		if backend != nil {
			ram = backend.TotalSystemRAM()
		}
		picked, err := llm.SelectModelForRAM(modelsRoot, ram)
		if err != nil {
			log.Fatalf("auto-select model: %v", err)
		}
		dir = picked.Dir
		log.Printf("auto-selected %s (%.0f GB RAM, min %.1f GB)", picked.Name,
			float64(ram)/1073741824, float64(picked.MinRAM)/1073741824)
	}

	log.Printf("loading model from %s ...", dir)
	model, err := llm.NewModel(dir)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer model.Close()

	// SP-134 memory protections: size MLX's allocator to this machine so a
	// long generation fails cleanly instead of blocking the process in a
	// Metal cond wait, and return pooled buffers to the OS between requests.
	if err := llm.ApplyMemoryLimits(); err != nil {
		log.Fatalf("apply MLX memory limits: %v", err)
	}

	name := "local"
	if cfg := model.Config(); cfg.Arch != "" {
		name = fmt.Sprintf("%s-local-%d-%d", cfg.Arch, cfg.HiddenSize, cfg.NumLayers)
	}

	srv := openaisserver.New(model, name, *maxTokens)
	handler := cacheTrimHandler(srv.Handler())

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	httpSrv := &http.Server{Addr: addr, Handler: handler}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Println("shutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Printf("llm server listening on http://%s (model %s)", addr, name)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

// cacheTrimHandler returns pooled MLX buffers to the OS after each request so
// long-running server sessions don't accumulate the previous request's cache.
func cacheTrimHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = llm.TrimCachedMemory() }()
		next.ServeHTTP(w, r)
	})
}
