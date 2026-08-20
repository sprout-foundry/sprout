//go:build !js && mlx

package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/sprout-foundry/sinter/mlx"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// onboardingLocal handles the sprout-local provider onboarding flow.
// Unlike cloud providers, sprout-local needs:
//   - Model selection (with hardware recommendation)
//   - Model download (with progress bar)
//   - Server lifecycle (start the llm_server binary)
//
// Returns the model name on success, or empty string on failure/skip.
func onboardingLocal() (string, bool) {
	fmt.Println()
	fmt.Println("═ Local AI Setup ═")
	fmt.Println()
	fmt.Println("Run a language model entirely on your machine — no API key,")
	fmt.Println("no network needed after download. Requires Apple Silicon.")
	fmt.Println()

	// Check hardware.
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		console.GlyphWarning.Fprintln(os.Stdout, "Local AI is currently only available on Apple Silicon (M1/M2/M3/M4).")
		fmt.Println("  Cloud providers work everywhere — try one of those instead.")
		return "", false
	}

	ram := mlx.TotalSystemRAM()
	ramGB := float64(ram) / 1073741824
	fmt.Printf("Detected: Apple Silicon, %.0f GB RAM\n", ramGB)
	fmt.Println()

	// List available models and highlight the recommended one.
	models := localmodel.ListModels()
	rec := localmodel.RecommendedModel(ram)

	fmt.Println("Available models:")
	var items []console.SelectItem
	for i, m := range models {
		var label string
		if m.Installed {
			label = fmt.Sprintf("%s  —  %s %.1f GB", m.Name, console.GlyphSuccess.Rune(), float64(m.Size)/1073741824)
			if m.IsTuned {
				label += "  [sprout-tuned]"
			}
			if m.QuantBits != "" {
				label += fmt.Sprintf("  (%s)", m.QuantBits)
			}
		} else {
			label = fmt.Sprintf("%s  —  download %.0f+ GB", m.Name, float64(m.MinRAM))
		}
		if rec != nil && m.Name == rec.Name {
			label = "★ " + label + "  [recommended]"
		}
		items = append(items, console.SelectItem{
			Label: label,
			Value: fmt.Sprintf("%d", i),
		})
	}
	items = append(items, console.SelectItem{
		Label: "Back",
		Value: "back",
	})

	sl := console.NewSelectList(console.SelectListOptions{
		Title:    "Choose a local model",
		Items:    items,
		PageSize: 8,
	})

	ctx := context.Background()
	value, ok, err := sl.Run(ctx)
	if err != nil || !ok || value == "back" {
		return "", false
	}

	idx := 0
	fmt.Sscanf(value, "%d", &idx)
	if idx < 0 || idx >= len(models) {
		return "", false
	}

	selected := models[idx]

	// Download if needed.
	if !selected.Installed {
		fmt.Println()
		fmt.Printf("Downloading %s from %s...\n", selected.Name, selected.HFRepo)
		fmt.Println("This is a one-time download (~2-5 GB depending on model size).")
		fmt.Println()

		dlCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()

		var lastPct int64 = -1
		modelPath, err := localmodel.EnsureModel(dlCtx, selected, func(downloaded, total int64) {
			if total > 0 && downloaded != lastPct {
				pct := downloaded * 100 / total
				if pct != lastPct {
					lastPct = pct
					bar := localProgressBar(int(pct), 40)
					fmt.Printf("\r  %s %d%%", bar, pct)
					if pct >= 100 {
						fmt.Println()
					}
				}
			}
		})
		if err != nil {
			fmt.Println()
			console.GlyphWarning.Printf("Download failed: %v", err)
			return "", false
		}
		selected.Dir = modelPath
		fmt.Println()
		console.GlyphSuccess.Printf("Download complete!")
	}

	// Start the server.
	fmt.Println()
	fmt.Println("Starting local AI server...")
	srvCtx, srvCancel := context.WithTimeout(ctx, 60*time.Second)
	defer srvCancel()

	_, err = localmodel.EnsureServerWithBackend(srvCtx, localmodel.DefaultPort, selected.Dir, selected.ServerBackend)
	if err != nil {
		console.GlyphWarning.Printf("Could not start local server: %v", err)
		console.GlyphInfo.Printf("You can start it manually: llm_server -model %s", selected.Dir)
		// Still persist config — the server can be started later
	} else {
		console.GlyphSuccess.Printf("Local server running (model: %s)", selected.Name)
	}

	fmt.Println()
	console.GlyphWarning.Printf("Local AI is experimental: output quality varies more than cloud providers, and memory use can spike well beyond configured limits during generation.")

	// Persist the config.
	modelName := selected.Name
	if err := persistProviderAndModel("sprout-local", modelName); err != nil {
		fmt.Println()
		console.GlyphWarning.Printf("Could not save config: %v", err)
		return "", false
	}

	return modelName, true
}

func ensureLocalServerRunning() error {
	return nil // in-process provider loads on first request; nothing to pre-start
}

func localProgressBar(pct, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	return "[" + repeatStr("█", filled) + repeatStr("░", width-filled) + "]"
}

func repeatStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		result = append(result, s...)
	}
	return string(result)
}
