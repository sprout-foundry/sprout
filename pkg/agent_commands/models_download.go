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

// ensureLocalModelDownloaded resolves modelID against the local RAM-tier
// catalog and, if it's a real catalog/installed entry that isn't on disk
// yet, downloads it with visible progress before the caller persists the
// selection. Refuses outright (no download attempted) for a RAM-tier-blocked
// pick, matching LocalProvider.SetModel's own gate — no point spending
// minutes downloading something that will then be rejected, unless the user
// has explicitly opted into SPROUT_ALLOW_OVERWEIGHT.
func ensureLocalModelDownloaded(modelID string) error {
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
			console.GlyphWarning.Printf("%s risks running out of memory on this machine — downloading anyway since you selected it explicitly.", status.Name)
		}
	}

	fmt.Println()
	fmt.Printf("Downloading %s from %s...\n", status.Name, status.HFRepo)
	fmt.Println("This is a one-time download.")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var lastPct int64 = -1
	var lastBytes int64 = -1
	if _, err := localmodel.EnsureModel(ctx, *status, func(downloaded, total int64) {
		// total<=0 means the caller doesn't know the full download size up
		// front (see pollDownloadProgress's doc comment: hf download gives
		// no way to learn it without an extra API round-trip) — show bytes
		// downloaded so far instead of a percentage, still far better than
		// showing nothing while a multi-GB download runs for minutes.
		if total <= 0 {
			if downloaded != lastBytes {
				lastBytes = downloaded
				fmt.Printf("\r  %s downloaded...", formatDownloadBytes(downloaded))
			}
			return
		}
		pct := downloaded * 100 / total
		if pct != lastPct {
			lastPct = pct
			fmt.Printf("\r  %d%%", pct)
			if pct >= 100 {
				fmt.Println()
			}
		}
	}); err != nil {
		fmt.Println()
		return fmt.Errorf("download failed: %w", err)
	}
	fmt.Println()
	console.GlyphSuccess.Printf("Download complete!")
	return nil
}

// formatDownloadBytes renders a byte count for the download progress line —
// GB-scale by the time any real model finishes, but scales down cleanly
// for small files early in a download.
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
