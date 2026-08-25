//go:build !js

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

func promptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimSpace(value), nil
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func removeString(values []string, target string) []string {
	filtered := values[:0]
	for _, value := range values {
		if value == target {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func parseToolCallList(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	toolCalls := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		toolName := strings.TrimSpace(part)
		if toolName == "" {
			continue
		}
		if _, exists := seen[toolName]; exists {
			continue
		}
		seen[toolName] = struct{}{}
		toolCalls = append(toolCalls, toolName)
	}
	return toolCalls
}

func resolvePreferredCustomProviderModel(input string, models []configuration.ProviderDiscoveryModel) (string, error) {
	trimmed := strings.TrimSpace(input)
	if len(models) == 0 {
		return trimmed, nil
	}
	if trimmed == "" {
		return models[0].ID, nil
	}

	if selectedIndex, err := strconv.Atoi(trimmed); err == nil {
		if selectedIndex < 1 || selectedIndex > len(models) {
			return "", fmt.Errorf("model selection %d is out of range", selectedIndex)
		}
		return models[selectedIndex-1].ID, nil
	}

	for _, model := range models {
		if strings.EqualFold(model.ID, trimmed) {
			return model.ID, nil
		}
	}

	return "", fmt.Errorf("model %q was not found in the discovered model list", trimmed)
}

func promptForCredentialIfNeeded(reader *bufio.Reader, providerName, envVar string) {
	if envVar == "" {
		return
	}
	if strings.TrimSpace(os.Getenv(envVar)) != "" {
		return
	}
	if hasStoredCredential(providerName) {
		return
	}
	answer, err := promptLine(reader, fmt.Sprintf("\nSet the API key for %s now via the credential backend? %s: ",
		providerName, console.FormatYesNoPromptStdout(false)))
	if err != nil || !isYes(answer) {
		return
	}
	key, keyErr := promptLine(reader, fmt.Sprintf("API key (will be stored; or set %s): ", envVar))
	if keyErr != nil || strings.TrimSpace(key) == "" {
		return
	}
	if storeErr := credentials.SetToActiveBackend(providerName, strings.TrimSpace(key)); storeErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to store credential: %v\n", storeErr)
		return
	}
	console.GlyphSuccess.Printf("Stored credential for %s", providerName)
}

func hasStoredCredential(provider string) bool {
	resolved, err := credentials.ResolveProvider(provider)
	if err != nil {
		return false
	}
	return strings.TrimSpace(resolved.Value) != ""
}

// errCustomSetupCancelled is returned by helpers when the user explicitly
// cancels an interactive prompt. Callers in runCustomModelAdd translate
// this into a nil error so the wizard exits cleanly without saving a
// half-configured provider.
var errCustomSetupCancelled = errors.New("custom provider setup cancelled by user")

func discoverAndPickModel(reader *bufio.Reader, provider *configuration.CustomProviderConfig) error {
	models, discoverErr := configuration.DiscoverCustomProviderModels(*provider)
	if discoverErr != nil {
		fmt.Println()
		console.GlyphWarning.Printf("Model discovery failed: %v", discoverErr)
		fmt.Println("The provider can still be saved, but model selection will rely on runtime discovery.")
		return nil
	}

	fmt.Println()
	console.GlyphSuccess.Printf("Discovered %d model(s)", len(models))
	maxShow := len(models)
	if maxShow > 10 {
		maxShow = 10
	}
	for i := 0; i < maxShow; i++ {
		ctxInfo := ""
		if models[i].ContextLength > 0 {
			ctxInfo = fmt.Sprintf("  (%dK context)", models[i].ContextLength/1000)
		}
		fmt.Printf("  %d. %s%s\n", i+1, models[i].ID, ctxInfo)
	}
	if len(models) > maxShow {
		fmt.Printf("  ... and %d more\n", len(models)-maxShow)
	}

	preferredItems := make([]console.SelectItem, 0, len(models))
	for _, m := range models {
		detail := ""
		if m.ContextLength > 0 {
			detail = fmt.Sprintf("%dK context", m.ContextLength/1000)
		}
		preferredItems = append(preferredItems, console.SelectItem{
			Label:  m.ID,
			Detail: detail,
			Value:  m.ID,
		})
	}
	preferredSL := console.NewSelectList(console.SelectListOptions{
		Title:      "Pick a preferred default model",
		Items:      preferredItems,
		Searchable: true,
		PageSize:   10,
	})
	preferredValue, ok, err := preferredSL.Run(context.Background())
	if err != nil {
		return fmt.Errorf("failed to prompt for preferred model: %w", err)
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Setup cancelled.")
		return errCustomSetupCancelled
	}
	// The picker only returns discovered model IDs, so the resolve
	// call is effectively a typed assertion that the ID matches.
	// (We keep the call rather than a direct assignment so any
	// future case-folding/normalization in resolvePreferred is
	// applied here too.)
	selectedModel, err := resolvePreferredCustomProviderModel(preferredValue, models)
	if err != nil {
		return fmt.Errorf("failed to resolve preferred model: %w", err)
	}
	provider.ModelName = selectedModel

	// Auto-populate per-model context sizes from discovery data
	if provider.ModelContextSizes == nil {
		provider.ModelContextSizes = make(map[string]int)
	}
	for _, m := range models {
		if m.ContextLength > 0 {
			provider.ModelContextSizes[m.ID] = m.ContextLength
		}
	}
	return nil
}

// getModelListForVisionPicker reruns discovery on the saved provider so the
// vision-model picker sees the same model list the user picked from earlier.
// Discovery is cheap and avoids storing an extra copy of the list in the
// wizard's state. Returns nil if discovery fails — the caller falls back to
// "reuse default model" without prompting.
func getModelListForVisionPicker(provider configuration.CustomProviderConfig) []configuration.ProviderDiscoveryModel {
	models, err := configuration.DiscoverCustomProviderModels(provider)
	if err != nil {
		return nil
	}
	return models
}

func pickVisionModel(reader *bufio.Reader, provider *configuration.CustomProviderConfig, models []configuration.ProviderDiscoveryModel) error {
	provider.SupportsVision = true
	// Only show the vision-model picker if discovery actually returned
	// models. If discovery failed, fall back to "use default model".
	if len(models) == 0 {
		fmt.Println("(no models discovered; vision will reuse the default model)")
		provider.VisionModel = provider.ModelName
		return nil
	}

	visionItems := []console.SelectItem{
		{
			Label:  "Use default model",
			Detail: fmt.Sprintf("reuse %s", provider.ModelName),
			Value:  "",
		},
	}
	for _, m := range models {
		detail := ""
		if m.ContextLength > 0 {
			detail = fmt.Sprintf("%dK context", m.ContextLength/1000)
		}
		visionItems = append(visionItems, console.SelectItem{
			Label:  m.ID,
			Detail: detail,
			Value:  m.ID,
		})
	}
	visionSL := console.NewSelectList(console.SelectListOptions{
		Title:      "Pick a vision model",
		Items:      visionItems,
		Searchable: true,
		PageSize:   10,
	})
	visionValue, ok, err := visionSL.Run(context.Background())
	if err != nil {
		return fmt.Errorf("failed to prompt for vision model: %w", err)
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Setup cancelled.")
		return errCustomSetupCancelled
	}
	// Empty value = "use default model" option picked.
	if visionValue == "" {
		provider.VisionModel = provider.ModelName
		return nil
	}
	selectedVisionModel, err := resolvePreferredCustomProviderModel(visionValue, models)
	if err != nil {
		return fmt.Errorf("failed to resolve vision model: %w", err)
	}
	provider.VisionModel = selectedVisionModel
	return nil
}
