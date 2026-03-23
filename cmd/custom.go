package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/spf13/cobra"
)

var customModelCmd = &cobra.Command{
	Use:   "custom",
	Short: "Manage custom OpenAI-compatible providers",
	Long: `Manage custom OpenAI-compatible providers backed by ~/.ledit/providers/*.json.
Each custom provider stores an endpoint URL and optional API-key environment variable,
and ledit discovers available models from the provider's /v1/models endpoint.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var customModelAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a custom provider",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runCustomModelAdd(); err != nil {
			fmt.Fprintf(os.Stderr, "Error adding custom provider: %v\n", err)
			os.Exit(1)
		}
	},
}

var customModelRemoveCmd = &cobra.Command{
	Use:   "remove [provider-name]",
	Short: "Remove a custom provider",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		if err := runCustomModelRemove(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing custom provider: %v\n", err)
			os.Exit(1)
		}
	},
}

var customModelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List custom providers",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runCustomModelList(); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing custom providers: %v\n", err)
			os.Exit(1)
		}
	},
}

func runCustomModelAdd() error {
	reader := bufio.NewReader(os.Stdin)
	cfg, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("Custom Provider Setup")
	fmt.Println("=====================")
	fmt.Println("Ledit assumes the endpoint is OpenAI-compatible and discovers models from /v1/models.")
	fmt.Println()

	name, err := promptLine(reader, "Provider name (e.g. my-gateway): ")
	if err != nil {
		return err
	}

	existingProviders := cfg.CustomProviders
	if _, exists := existingProviders[strings.ToLower(strings.TrimSpace(name))]; exists {
		answer, err := promptLine(reader, "Provider exists. Replace it? [y/N]: ")
		if err != nil {
			return err
		}
		if !isYes(answer) {
			return nil
		}
	}

	endpoint, err := promptLine(reader, "Base URL (e.g., https://example.com/v1): ")
	if err != nil {
		return err
	}

	envVar, err := promptLine(reader, "API key env var (leave empty for no auth): ")
	if err != nil {
		return err
	}

	provider := configuration.CustomProviderConfig{
		Name:           name,
		Endpoint:       endpoint,
		EnvVar:         strings.TrimSpace(envVar),
		RequiresAPIKey: strings.TrimSpace(envVar) != "",
	}

	models, discoverErr := configuration.DiscoverCustomProviderModels(provider)
	if discoverErr != nil {
		fmt.Printf("\n[WARN] Model discovery failed: %v\n", discoverErr)
		fmt.Println("The provider can still be saved, but model selection will rely on runtime discovery.")
	} else {
		fmt.Printf("\n[OK] Discovered %d model(s)\n", len(models))
		maxShow := len(models)
		if maxShow > 10 {
			maxShow = 10
		}
		for i := 0; i < maxShow; i++ {
			fmt.Printf("  %d. %s\n", i+1, models[i].ID)
		}
		if len(models) > maxShow {
			fmt.Printf("  ... and %d more\n", len(models)-maxShow)
		}

		for {
			preferred, err := promptLine(reader, "Preferred default model (name or number, leave empty for first discovered): ")
			if err != nil {
				return err
			}
			selectedModel, err := resolvePreferredCustomProviderModel(preferred, models)
			if err != nil {
				fmt.Printf("\n[WARN] %v\n", err)
				continue
			}
			provider.ModelName = selectedModel
			break
		}
	}

	if err := configuration.SaveCustomProvider(provider); err != nil {
		return fmt.Errorf("failed to save provider: %w", err)
	}

	normalized, err := configuration.NormalizeCustomProviderConfig(provider)
	if err != nil {
		return err
	}

	if cfg.ProviderModels == nil {
		cfg.ProviderModels = make(map[string]string)
	}
	if normalized.ModelName != "" {
		cfg.ProviderModels[normalized.Name] = normalized.ModelName
	}
	if !containsString(cfg.ProviderPriority, normalized.Name) {
		cfg.ProviderPriority = append(cfg.ProviderPriority, normalized.Name)
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	path, _ := configuration.GetCustomProviderPath(normalized.Name)
	fmt.Println()
	fmt.Printf("Saved provider '%s'\n", normalized.Name)
	fmt.Printf("  Chat endpoint: %s\n", normalized.Endpoint)
	fmt.Printf("  Models endpoint: %s\n", normalized.ModelsEndpoint())
	if normalized.EnvVar != "" {
		fmt.Printf("  API key env: %s\n", normalized.EnvVar)
	}
	if normalized.ModelName != "" {
		fmt.Printf("  Default model: %s\n", normalized.ModelName)
	}
	fmt.Printf("  File: %s\n", path)
	return nil
}

func runCustomModelRemove(providerName string) error {
	cfg, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	customProviders := cfg.CustomProviders
	if len(customProviders) == 0 {
		fmt.Println("No custom providers configured.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	if strings.TrimSpace(providerName) == "" {
		names := make([]string, 0, len(customProviders))
		for name := range customProviders {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Println("Custom providers:")
		for _, name := range names {
			fmt.Printf("  - %s\n", name)
		}
		value, err := promptLine(reader, "Provider to remove: ")
		if err != nil {
			return err
		}
		providerName = value
	}

	normalizedName, err := configuration.CanonicalizeCustomProviderName(providerName)
	if err != nil {
		return err
	}
	providerName = normalizedName

	if _, exists := customProviders[providerName]; !exists {
		return fmt.Errorf("custom provider '%s' not found", providerName)
	}

	answer, err := promptLine(reader, fmt.Sprintf("Remove provider '%s'? [y/N]: ", providerName))
	if err != nil {
		return err
	}
	if !isYes(answer) {
		return nil
	}

	if err := configuration.DeleteCustomProvider(providerName); err != nil {
		return err
	}
	delete(cfg.ProviderModels, providerName)
	cfg.ProviderPriority = removeString(cfg.ProviderPriority, providerName)
	if cfg.LastUsedProvider == providerName {
		cfg.LastUsedProvider = ""
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Removed provider '%s'\n", providerName)
	return nil
}

func runCustomModelList() error {
	cfg, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(cfg.CustomProviders) == 0 {
		fmt.Println("No custom providers configured.")
		fmt.Println("Use 'ledit custom add' to add one.")
		return nil
	}

	names := make([]string, 0, len(cfg.CustomProviders))
	for name := range cfg.CustomProviders {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Custom Providers")
	fmt.Println("================")
	for _, name := range names {
		provider := cfg.CustomProviders[name]
		path, _ := configuration.GetCustomProviderPath(name)
		fmt.Printf("%s\n", name)
		fmt.Printf("  Chat endpoint: %s\n", provider.Endpoint)
		fmt.Printf("  Models endpoint: %s\n", provider.ModelsEndpoint())
		if provider.EnvVar != "" {
			fmt.Printf("  API key env: %s\n", provider.EnvVar)
		} else {
			fmt.Printf("  API key env: none\n")
		}
		if model := cfg.ProviderModels[name]; model != "" {
			fmt.Printf("  Selected model: %s\n", model)
		}
		fmt.Printf("  File: %s\n", path)
		fmt.Println()
	}

	return nil
}

func promptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
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

func init() {
	customModelCmd.AddCommand(customModelAddCmd)
	customModelCmd.AddCommand(customModelRemoveCmd)
	customModelCmd.AddCommand(customModelListCmd)
}
