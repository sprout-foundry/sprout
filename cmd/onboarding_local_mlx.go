//go:build !js && darwin && arm64 && cgo

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sinter/mlx"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// localAIAvailable reports whether this build can run the local provider.
// Apple Silicon builds can: the MLX runtime downloads on demand when
// Homebrew's mlx-c is not installed.
func localAIAvailable() bool { return true }

// onboardingLocal handles the sprout-local provider onboarding flow.
// Unlike cloud providers, sprout-local needs the MLX runtime and a model
// download; the model then runs inside sprout itself.
//
// Returns the model name on success, or empty string on failure/skip.
func onboardingLocal() (string, bool) {
	fmt.Println()
	fmt.Println("═ Local AI Setup ═")
	fmt.Println()
	fmt.Println("Run a language model entirely on your machine — no API key,")
	fmt.Println("no network needed after download. Requires Apple Silicon.")
	fmt.Println()

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

	needsRuntime := !mlx.Available()
	if needsRuntime {
		if !installLocalRuntime(ctx) {
			return "", false
		}
	}

	if !selected.Installed {
		fmt.Println()
		fmt.Printf("Downloading %s from %s...\n", selected.Name, selected.HFRepo)
		fmt.Println("This is a one-time download (~2-5 GB depending on model size).")
		fmt.Println()

		dlCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
		defer cancel()

		modelPath, err := localmodel.EnsureModel(dlCtx, selected, localProgressPrinter())
		if err != nil {
			fmt.Println()
			console.GlyphWarning.Printf("Download failed: %v", err)
			return "", false
		}
		selected.Dir = modelPath
		fmt.Println()
		console.GlyphSuccess.Printf("Download complete!")
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

	if needsRuntime {
		fmt.Println()
		fmt.Println("Restarting Sprout to load the local AI runtime...")
		if err := localmodel.RestartWithRuntime(); err != nil {
			console.GlyphWarning.Printf("Could not restart: %v", err)
			fmt.Println("Run sprout again to start using the local model.")
			os.Exit(0)
		}
	}

	return modelName, true
}

// installLocalRuntime downloads the MLX runtime sprout runs local models
// with, reporting whether it is ready.
func installLocalRuntime(ctx context.Context) bool {
	fmt.Println()
	fmt.Println("Downloading the local AI runtime (one time, about 40 MB)...")
	rtCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := localmodel.InstallRuntime(rtCtx, localProgressPrinter())
	fmt.Println()
	if err != nil {
		console.GlyphWarning.Printf("Could not install the local AI runtime: %v", err)
		return false
	}
	console.GlyphSuccess.Printf("Runtime installed.")
	return true
}

func localProgressPrinter() localmodel.ProgressCallback {
	var lastPct int64 = -1
	return func(downloaded, total int64) {
		if total <= 0 {
			return
		}
		pct := downloaded * 100 / total
		if pct == lastPct {
			return
		}
		lastPct = pct
		fmt.Printf("\r  %s %d%%", localProgressBar(int(pct), 40), pct)
	}
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
