//go:build darwin && arm64 && cgo

// Command llm_download fetches a local chat model from HuggingFace into
// sprout's models root (~/.sprout-local/models), using the same catalog
// and EnsureModel path as the in-process provider and the WebUI — one
// download engine, three entry points.
//
// Usage:
//
//	llm_download                        # download the RAM-recommended model
//	llm_download -model qwen3.5-9b      # download a specific catalog model
//
// Model IDs are catalog Names (e.g. "qwen3.5-9b", "minicpm5-2b") — the
// same IDs /model, the WebUI, and onboarding use. Run it once; afterwards
// `llm_server` with no -model flag picks the best installed model by RAM.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sprout-foundry/sinter/mlx"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

func main() {
	modelFlag := flag.String("model", "", "catalog model ID to download (default: the RAM-recommended model)")
	flag.Parse()

	// Resolve the target: explicit ID first, else the RAM recommendation.
	var id string
	if *modelFlag != "" {
		id = *modelFlag
	} else {
		rec := localmodel.RecommendedModel(mlx.TotalSystemRAM())
		if rec == nil {
			log.Fatalf("no model recommended for this machine")
		}
		id = rec.Name
		fmt.Printf("Machine has %.0f GB RAM — recommending %s\n", float64(mlx.TotalSystemRAM())/1073741824, id)
	}

	status, err := localmodel.ResolveModelID(id)
	if err != nil {
		log.Fatalf("unknown model %q — valid IDs come from the sprout catalog (see /model in the CLI)", id)
	}
	if status.Installed {
		fmt.Printf("%s already installed at %s\n", status.Name, status.Dir)
		return
	}

	// Cancelable via Ctrl-C: the download subprocess dies with the context.
	// No fixed timeout: multi-GB downloads over slow links take longer than
	// any safe constant — cancellation and process exit are the bounds.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Downloading %s from %s into %s\n", status.Name, status.HFRepo, localmodel.DownloadModelsDir())
	start := time.Now()
	var last int64 = -1
	dest, err := localmodel.EnsureModel(ctx, *status, func(downloaded, total int64) {
		if total > 0 {
			fmt.Printf("\r  %d%% (%s)", downloaded*100/maxInt(total, 1), humanBytes(downloaded))
		} else if downloaded != last {
			last = downloaded
			fmt.Printf("\r  %s", humanBytes(downloaded))
		}
	})
	if err != nil {
		fmt.Println()
		log.Fatalf("download failed: %v", err)
	}
	fmt.Printf("\nInstalled %s at %s (%.1fs). Start the server with:\n  llm_server -model %s\n",
		status.Name, dest, time.Since(start).Seconds(), dest)
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func humanBytes(n int64) string {
	const gb = 1024 * 1024 * 1024
	switch {
	case n < 1024*1024:
		return fmt.Sprintf("%d KB", n/1024)
	case n < gb:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/gb)
	}
}
