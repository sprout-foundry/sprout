package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sinter/llm/catalog"

	"github.com/sprout-foundry/sprout/pkg/gpulink"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// prepareSinterModel resolves the model `sprout gpu link --sinter` serves (the
// named one, else the one recommended for this machine's RAM), downloads it
// and the MLX runtime when missing, and returns its directory and name.
func prepareSinterModel(ctx context.Context, out io.Writer, name string) (dir, model string, err error) {
	if !gpulink.SinterAvailable {
		_, _, _, err := gpulink.SinterHandler("")
		return "", "", err
	}
	var status *localmodel.ModelStatus
	if name != "" && isModelDir(name) {
		// A model directory the person downloaded themselves.
		abs, err := filepath.Abs(name)
		if err != nil {
			return "", "", err
		}
		status = &localmodel.ModelStatus{Name: filepath.Base(abs), Dir: abs, Installed: true}
	} else if name != "" {
		if status, err = localmodel.ResolveModelID(name); err != nil {
			return "", "", fmt.Errorf("--model %q: %w (pass a catalog name such as qwen3.5-9b, an installed model, or a model directory)", name, err)
		}
	} else {
		ram := localmodel.TotalSystemRAM()
		status = localmodel.RecommendedModel(ram)
		if status == nil {
			rec := catalog.RecommendModelForRAM(ram)
			if rec == nil {
				return "", "", errors.New("no sinter model fits this machine; pass --model")
			}
			if status, err = localmodel.ResolveModelID(rec.Name); err != nil {
				return "", "", fmt.Errorf("resolving the recommended model %q: %w", rec.Name, err)
			}
		}
	}
	dir = status.Dir
	if !status.Installed {
		_, _ = fmt.Fprintf(out, "Downloading %s from %s (one time)...\n", status.Name, status.HFRepo)
		var lastPct int64 = -1
		dir, err = localmodel.EnsureModel(ctx, *status, func(done, total int64) {
			if total <= 0 {
				return
			}
			if pct := done * 100 / total; pct != lastPct {
				lastPct = pct
				_, _ = fmt.Fprintf(out, "\r  %d%%", pct)
			}
		})
		_, _ = fmt.Fprintln(out)
		if err != nil {
			return "", "", fmt.Errorf("downloading %s: %w", status.Name, err)
		}
	}
	if localmodel.RuntimeSupported() && !localmodel.MLXAvailable() && !localmodel.RuntimeInstalled() {
		_, _ = fmt.Fprintln(out, "Downloading the local AI runtime (one time, about 40 MB)...")
		if err := localmodel.InstallRuntime(ctx, nil); err != nil {
			return "", "", fmt.Errorf("installing the local AI runtime: %w", err)
		}
	}
	return dir, status.Name, nil
}

// isModelDir reports whether path is a model directory (it holds a
// config.json), as opposed to a catalog or installed model name.
func isModelDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, "config.json"))
	return err == nil && !info.IsDir()
}
