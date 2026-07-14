//go:build !js

package cmd

import (
	"bufio"
	"errors"
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

	if err := discoverAndPickModel(reader, &provider); err != nil {
		if errors.Is(err, errCustomSetupCancelled) {
			return nil
		}
		return err
	}
	models := getModelListForVisionPicker(provider)

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
		if err := pickVisionModel(reader, &provider, models); err != nil {
			if errors.Is(err, errCustomSetupCancelled) {
				return nil
			}
			return err
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

func init() {
	customModelCmd.AddCommand(customModelAddCmd)
	customModelCmd.AddCommand(customModelRemoveCmd)
	customModelCmd.AddCommand(customModelListCmd)
}
