package commands

// models_download.go — the /models local-model download layer: ensuring
// a local model is downloaded (ensureLocalModelDownloaded) and the
// download-size formatter (formatDownloadBytes). Split out of models.go.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sinter/llm/catalog"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

func ensureLocalModelDownloaded(out *outputSink, modelID string) error {
	status, err := localmodel.ResolveModelID(modelID)
	if err != nil {
		// Not a catalog/installed name we recognize — let SetModelPersisted's
		// own validation produce the error; nothing to download here.
		return nil
	}
	if status.Installed {
		return nil
	}

	ram := localmodel.TotalSystemRAM()
	if tier, known := catalog.SelectableForRAM(status.Name, ram); known {
		switch {
		case tier == catalog.TierBlocked && os.Getenv("SPROUT_ALLOW_OVERWEIGHT") != "1":
			return fmt.Errorf("%s needs more RAM than this machine has (%.0f GB) — set SPROUT_ALLOW_OVERWEIGHT=1 to force it anyway",
				status.Name, float64(ram)/(1024*1024*1024))
		case tier == catalog.TierStretch:
			console.GlyphWarning.Fprintf(out.out(), "%s risks running out of memory on this machine — downloading anyway since you selected it explicitly.", status.Name)
		}
	}

	out.println()
	out.printf("Downloading %s from %s...\n", status.Name, status.HFRepo)
	out.println("This is a one-time download.")
	out.println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var lastPct int64 = -1
	var lastBytes int64 = -1
	if _, err := localmodel.EnsureModel(ctx, *status, func(downloaded, total int64) {
		// total<=0 means the download size is not known up front — show
		// bytes downloaded so far instead of a percentage.
		if total <= 0 {
			if downloaded != lastBytes {
				lastBytes = downloaded
				out.printf("\r  %s downloaded...", formatDownloadBytes(downloaded))
			}
			return
		}
		pct := downloaded * 100 / total
		if pct != lastPct {
			lastPct = pct
			out.printf("\r  %d%%", pct)
			if pct >= 100 {
				out.println()
			}
		}
	}); err != nil {
		out.println()
		return fmt.Errorf("download failed: %w", err)
	}
	out.println()
	console.GlyphSuccess.Fprintf(out.out(), "Download complete!")
	return ensureLocalRuntime(ctx, out)
}

// ensureLocalRuntime downloads the MLX runtime when this Mac has none. MLX
// loads once at startup, so the model is usable after sprout restarts.
func ensureLocalRuntime(ctx context.Context, out *outputSink) error {
	if !localmodel.RuntimeSupported() || localmodel.MLXAvailable() {
		return nil
	}
	if !localmodel.RuntimeInstalled() {
		out.println("Downloading the local AI runtime (one time, about 40 MB)...")
		if err := localmodel.InstallRuntime(ctx, nil); err != nil {
			return fmt.Errorf("install local AI runtime: %w", err)
		}
	}
	console.GlyphInfo.Fprintf(out.out(), "Restart sprout to start using local models.")
	return nil
}

func formatDownloadBytes(n int64) string {
	const unit = 1024
	switch {
	case n < unit:
		return fmt.Sprintf("%d B", n)
	case n < unit*unit:
		return fmt.Sprintf("%.1f KB", float64(n)/unit)
	case n < unit*unit*unit:
		return fmt.Sprintf("%.1f MB", float64(n)/(unit*unit))
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/(unit*unit*unit))
	}
}
