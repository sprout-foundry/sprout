//go:build !js

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
	"golang.org/x/term"
)

var customModelCmd = &cobra.Command{
	Use:   "custom",
	Short: "Manage custom OpenAI-compatible providers",
	Long: `Manage custom OpenAI-compatible providers backed by ~/.config/sprout/providers/*.json.
Each custom provider stores an endpoint URL and optional API-key environment variable,
and sprout discovers available models from the provider's /v1/models endpoint.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var customModelAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a custom provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCustomModelAdd()
	},
}

var customModelRemoveCmd = &cobra.Command{
	Use:   "remove [provider-name]",
	Short: "Remove a custom provider",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return runCustomModelRemove(name)
	},
}

var customModelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List custom providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCustomModelList()
	},
}

func runCustomModelAdd() error {
	// The wizard prompts interactively for endpoint URL, API key env var,
	// and preferred model. If stdin isn't a terminal, promptLine will block
	// on EOF forever — fail fast with guidance instead.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("'sprout custom add' requires an interactive terminal. " + noninteractive.HelpHint)
	}

	reader := bufio.NewReader(os.Stdin)
	cfg, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("Custom Provider Setup")
	fmt.Println("=====================")
	fmt.Println("Sprout assumes the endpoint is OpenAI-compatible and discovers models from /v1/models.")
	fmt.Println()

	name, err := promptLine(reader, "Provider name (e.g. my-gateway): ")
	if err != nil {
		return fmt.Errorf("failed to prompt for provider name: %w", err)
	}

	// Short-flow branch: if this name matches an already-registered provider
	// (user's custom config or embedded factory), we skip URL/discovery
	// and only offer to set credentials. This is the common case after
	// a `/provider <name>` failure shows the user "Run 'sprout custom add'".
	known, isKnown := configuration.LookupKnownProvider(name)
	if isKnown {
		return runCustomModelAddKnown(reader, known)
	}

	endpoint, err := promptLine(reader, "Base URL (e.g., https://example.com/v1): ")
	if err != nil {
		return fmt.Errorf("failed to prompt for endpoint URL: %w", err)
	}
	if err := configuration.ValidateCustomProviderEndpoint(endpoint); err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}

	envVar, err := promptLine(reader, "API key env var (leave empty for no auth): ")
	if err != nil {
		return fmt.Errorf("failed to prompt for API key env var: %w", err)
	}
	envVar = strings.TrimSpace(envVar)

	// If the user named an env var that's already set in the environment,
	// tell them — no need to ask them to re-set the credential via
	// `sprout keys set`.
	if envVar != "" && strings.TrimSpace(os.Getenv(envVar)) != "" {
		fmt.Printf("(env var %s is already set; discovery will use it)\n", envVar)
	}

	provider := configuration.CustomProviderConfig{
		Name:           name,
		Endpoint:       endpoint,
		EnvVar:         envVar,
		RequiresAPIKey: envVar != "",
	}

	models, discoverErr := configuration.DiscoverCustomProviderModels(provider)
	if discoverErr != nil {
		fmt.Println()
		console.GlyphWarning.Printf("Model discovery failed: %v", discoverErr)
		fmt.Println("The provider can still be saved, but model selection will rely on runtime discovery.")
	} else {
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
			return nil
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
	}

	// Prompt for default context size.
	// If discovery succeeded and the default model has a known context size,
	// offer it as the default answer.
	defaultCtxHint := ""
	if provider.ModelName != "" && provider.ModelContextSizes != nil {
		if ctxSz, ok := provider.ModelContextSizes[provider.ModelName]; ok {
			defaultCtxHint = fmt.Sprintf(" [%d]", ctxSz)
		}
	}
	fmt.Printf("\nDefault context size (tokens) for the provider%s: ", defaultCtxHint)
	ctxInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read context size input: %w", err)
	}
	ctxInput = strings.TrimSpace(ctxInput)
	if ctxInput != "" {
		if n, err := strconv.Atoi(ctxInput); err == nil && n > 0 {
			provider.ContextSize = n
		}
	} else if defaultCtxHint != "" {
		// User pressed enter—use the discovered size for the default model
		provider.ContextSize = provider.ModelContextSizes[provider.ModelName]
	}

	visionAnswer, err := promptLine(reader, "Does this provider/model support vision (multimodal images)? "+console.FormatYesNoPromptStdout(false)+": ")
	if err != nil {
		return fmt.Errorf("failed to prompt for vision support: %w", err)
	}
	if isYes(visionAnswer) {
		provider.SupportsVision = true
		// Only show the vision-model picker if discovery actually returned
		// models. If discovery failed, fall back to "use default model".
		if len(models) > 0 {
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
				return nil
			}
			// Empty value = "use default model" option picked.
			if visionValue == "" {
				provider.VisionModel = provider.ModelName
			} else {
				selectedVisionModel, err := resolvePreferredCustomProviderModel(visionValue, models)
				if err != nil {
					return fmt.Errorf("failed to resolve vision model: %w", err)
				}
				provider.VisionModel = selectedVisionModel
			}
		} else {
			fmt.Println("(no models discovered; vision will reuse the default model)")
			provider.VisionModel = provider.ModelName
		}
	}

	if err := configuration.SaveCustomProvider(provider); err != nil {
		return fmt.Errorf("failed to save provider: %w", err)
	}

	normalized, err := configuration.NormalizeCustomProviderConfig(provider)
	if err != nil {
		return fmt.Errorf("failed to normalize custom provider config: %w", err)
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
	fmt.Printf("  Chat endpoint: %s\n", secretdetect.RedactOpaque(normalized.Endpoint))
	fmt.Printf("  Models endpoint: %s\n", secretdetect.RedactOpaque(normalized.ModelsEndpoint()))
	if normalized.EnvVar != "" {
		fmt.Printf("  API key env: %s\n", normalized.EnvVar)
	}
	if normalized.ModelName != "" {
		fmt.Printf("  Default model: %s\n", normalized.ModelName)
		fmt.Printf("  Default context size: %d tokens\n", normalized.ContextSize)
	}
	if len(normalized.ModelContextSizes) > 0 {
		fmt.Printf("  Per-model context sizes: %d model(s) with known context\n", len(normalized.ModelContextSizes))
	}
	fmt.Printf("  Supports vision: %t\n", normalized.SupportsVision)
	if normalized.SupportsVision && normalized.VisionModel != "" {
		fmt.Printf("  Vision model: %s\n", normalized.VisionModel)
	}
	fmt.Printf("  File: %s\n", path)

	// If the user declared an API key env var, offer to set it now via
	// the active credential backend. Skip the prompt when the env var is
	// already set in the environment (no need to copy it into the store)
	// or when the provider doesn't need auth.
	promptForCredentialIfNeeded(reader, normalized.Name, normalized.EnvVar)

	return nil
}

// runCustomModelAddKnown handles the "register credentials for an
// already-known provider" short-flow. We skip URL/discovery entirely
// because the provider's endpoint, default model, and auth env var are
// already configured. This is the common follow-up after a `/provider
// <name>` failure surfaces "Run 'sprout custom add <name>'".
func runCustomModelAddKnown(reader *bufio.Reader, known configuration.KnownProviderInfo) error {
	fmt.Println()
	fmt.Printf("Provider %q is already configured (source: %s).\n", known.DisplayName, known.Source)
	if known.Endpoint != "" {
		fmt.Printf("  Endpoint: %s\n", secretdetect.RedactOpaque(known.Endpoint))
	}
	if known.DefaultModel != "" {
		fmt.Printf("  Default model: %s\n", known.DefaultModel)
	}
	if known.ContextSize > 0 {
		fmt.Printf("  Default context size: %d tokens\n", known.ContextSize)
	}
	if known.EnvVar != "" {
		fmt.Printf("  API key env var: %s\n", known.EnvVar)
	}
	fmt.Println()

	if known.RequiresAPIKey {
		// Check current state so we can offer the right action.
		envSet := known.EnvVar != "" && strings.TrimSpace(os.Getenv(known.EnvVar)) != ""
		storedCred := hasStoredCredential(known.Name)
		switch {
		case envSet && storedCred:
			console.GlyphSuccess.Print("API key already configured (env var + credential store).")
			return nil
		case envSet:
			fmt.Println("(env var is set; credential store has no value for this provider)")
		case storedCred:
			console.GlyphSuccess.Print("A credential is already stored for this provider.")
			fmt.Println("Re-run /keys set if you want to rotate it.")
			return nil
		default:
			fmt.Println("No credentials are configured yet.")
		}

		answer, err := promptLine(reader, fmt.Sprintf("Set the API key for %s now? %s: ",
			known.Name, console.FormatYesNoPromptStdout(true)))
		if err != nil || !isYes(answer) {
			fmt.Println()
			fmt.Printf("Skipped. Run `/keys set %s <key>` (or `sprout keys set %s <key>`) later.\n",
				known.Name, known.Name)
			return nil
		}

		key, keyErr := promptLine(reader, fmt.Sprintf("API key (or set %s): ", known.EnvVar))
		if keyErr != nil {
			return fmt.Errorf("failed to read API key: %w", keyErr)
		}
		if strings.TrimSpace(key) == "" {
			fmt.Println("Empty key — nothing stored.")
			return nil
		}
		if storeErr := credentials.SetToActiveBackend(known.Name, strings.TrimSpace(key)); storeErr != nil {
			return fmt.Errorf("failed to store credential: %w", storeErr)
		}
		console.GlyphSuccess.Printf("Stored credential for %s", known.Name)
	} else {
		fmt.Println("This provider does not require an API key.")
	}

	fmt.Println()
	fmt.Printf("Try `/provider %s` to switch.\n", known.Name)
	return nil
}

// promptForCredentialIfNeeded is the post-save credential prompt shared
// between the full wizard and any future flows that save a custom
// provider config. It is a no-op when the env var is already set or the
// provider doesn't need auth.
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

// hasStoredCredential reports whether the active credential backend
// already has a non-empty value for the named provider. We use it to
// avoid re-prompting when the user already set a key in this session
// (e.g. via `/keys set` before re-running the wizard).
func hasStoredCredential(provider string) bool {
	resolved, err := credentials.ResolveProvider(provider)
	if err != nil {
		return false
	}
	return strings.TrimSpace(resolved.Value) != ""
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
			return fmt.Errorf("failed to prompt for provider to remove: %w", err)
		}
		providerName = value
	}

	normalizedName, err := configuration.CanonicalizeCustomProviderName(providerName)
	if err != nil {
		return fmt.Errorf("failed to canonicalize provider name: %w", err)
	}
	providerName = normalizedName

	if _, exists := customProviders[providerName]; !exists {
		return fmt.Errorf("custom provider '%s' not found", providerName)
	}

	answer, err := promptLine(reader, fmt.Sprintf("Remove provider '%s'? %s: ", providerName, console.FormatYesNoPromptStdout(false)))
	if err != nil {
		return fmt.Errorf("failed to prompt for removal confirmation: %w", err)
	}
	if !isYes(answer) {
		return nil
	}

	if err := configuration.DeleteCustomProvider(providerName); err != nil {
		return fmt.Errorf("failed to delete custom provider: %w", err)
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
		fmt.Println("Use 'sprout custom add' to add one.")
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
		fmt.Printf("  Chat endpoint: %s\n", secretdetect.RedactOpaque(provider.Endpoint))
		fmt.Printf("  Models endpoint: %s\n", secretdetect.RedactOpaque(provider.ModelsEndpoint()))
		if provider.EnvVar != "" {
			fmt.Printf("  API key env: %s\n", provider.EnvVar)
		} else {
			fmt.Printf("  API key env: none\n")
		}
		if model := cfg.ProviderModels[name]; model != "" {
			fmt.Printf("  Selected model: %s\n", model)
			if ctxSz := provider.ContextSize; ctxSz > 0 {
				fmt.Printf("  Default context size: %d tokens\n", ctxSz)
			}
			if perModel, ok := provider.ModelContextSizes[model]; ok && perModel > 0 {
				fmt.Printf("  Model context limit: %d tokens\n", perModel)
			}
		}
		if len(provider.ModelContextSizes) > 0 {
			fmt.Printf("  Known model contexts: %d model(s)\n", len(provider.ModelContextSizes))
		}
		fmt.Printf("  Supports vision: %t\n", provider.SupportsVision)
		if provider.SupportsVision && provider.VisionModel != "" {
			fmt.Printf("  Vision model: %s\n", provider.VisionModel)
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
