package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// KeysCommand implements the /keys slash command for managing API credentials
type KeysCommand struct{}

// Name returns the command name
func (k *KeysCommand) Name() string {
	return "keys"
}

// Description returns the command description
func (k *KeysCommand) Description() string {
	return "Manage API credentials for providers"
}

// Usage returns the detailed help text shown by `/help keys`.
func (k *KeysCommand) Usage() string {
	return strings.Join([]string{
		"/keys                 Show credential status for all providers.",
		"/keys list            List providers and their credential status.",
		"/keys set <provider> [api_key]",
		"                      Set credential for a provider via argument.",
		"/keys remove <provider>",
		"                      Remove stored credential for a provider.",
		"",
		"Providers that need credentials but don't have them configured will",
		"show as 'not configured'. Use '/keys set <provider> <key>' to add them.",
	}, "\n")
}

// Execute runs the keys command
func (k *KeysCommand) Execute(args []string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()

	if len(args) == 0 {
		return k.listKeys(configManager)
	}

	switch args[0] {
	case "list":
		return k.listKeys(configManager)
	case "set":
		if len(args) < 2 {
			return fmt.Errorf("usage: /keys set <provider> [api_key]")
		}
		provider := strings.ToLower(strings.TrimSpace(args[1]))
		apiKey := ""
		if len(args) > 2 {
			apiKey = strings.TrimSpace(args[2])
		}
		return k.setKey(configManager, provider, apiKey)
	case "remove", "delete", "unset":
		if len(args) < 2 {
			return fmt.Errorf("usage: /keys remove <provider>")
		}
		provider := strings.ToLower(strings.TrimSpace(args[1]))
		return k.removeKey(configManager, provider)
	default:
		return fmt.Errorf("unknown subcommand '%s'. Use: list, set, remove", args[0])
	}
}

// Complete provides argument completions for /keys
func (k *KeysCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	if len(args) == 0 {
		return []string{"list", "set", "remove"}
	}

	if len(args) == 1 {
		return []string{"list", "set", "remove"}
	}

	if len(args) == 2 {
		// Suggest provider names
		providers := chatAgent.GetConfigManager().GetAvailableProviders()
		out := make([]string, 0, len(providers))
		for _, p := range providers {
			out = append(out, string(p))
		}
		sort.Strings(out)
		return out
	}

	return nil
}

// listKeys shows credential status for all providers
func (k *KeysCommand) listKeys(configManager *configuration.Manager) error {
	providers := configManager.GetAvailableProviders()
	if len(providers) == 0 {
		console.GlyphInfo.Print("No providers configured.")
		return nil
	}

	// Sort providers alphabetically
	sorted := make([]string, 0, len(providers))
	seen := map[string]bool{}
	for _, p := range providers {
		name := string(p)
		if !seen[name] {
			seen[name] = true
			sorted = append(sorted, name)
		}
	}
	sort.Strings(sorted)

	fmt.Println("Provider Credentials")
	fmt.Println("===================")

	hasIssues := false
	for _, name := range sorted {
		status := k.getCredentialStatus(configManager, name)
		statusStr := status.icon + " " + status.text
		fmt.Printf("%-20s %s\n", name+":", statusStr)
		if status.missing {
			hasIssues = true
		}
	}

	if hasIssues {
		fmt.Println()
		fmt.Println("Use '/keys set <provider> <api_key>' to configure missing credentials.")
	}

	return nil
}

// credentialStatus holds the status information for a provider's credential
type credentialStatus struct {
	icon    string
	text    string
	missing bool
	envVar  string
}

// getCredentialStatus determines the credential status for a provider
func (k *KeysCommand) getCredentialStatus(configManager *configuration.Manager, provider string) credentialStatus {
	// Check if it's a custom provider with a defined env var
	customProviders := configManager.GetConfig().CustomProviders
	if customProviders != nil {
		if cp, exists := customProviders[provider]; exists {
			if cp.EnvVar == "" && !cp.RequiresAPIKey {
				return credentialStatus{
					icon:    console.GlyphSuccess.Prefix(),
					text:    "no credential needed",
					missing: false,
				}
			}
			if cp.EnvVar != "" {
				// Check if env var is set
				if val := strings.TrimSpace(os.Getenv(cp.EnvVar)); val != "" {
					return credentialStatus{
				icon:    console.GlyphSuccess.Prefix(),
				text:    fmt.Sprintf("env var %s set", cp.EnvVar),
				missing: false,
				envVar:  cp.EnvVar,
			}}
				// Check stored credential
				resolved, err := credentials.ResolveProvider(provider)
				if err == nil && strings.TrimSpace(resolved.Value) != "" {
					return credentialStatus{
						icon:    console.GlyphSuccess.Prefix(),
						text:    "stored credential available",
						missing: false,
					}
				}
				return credentialStatus{
					icon:    console.GlyphWarning.Prefix(),
					text:    fmt.Sprintf("env var %s not set", cp.EnvVar),
					missing: true,
					envVar:  cp.EnvVar,
				}
			}
		}
	}

	// Built-in provider
	envVar := credentials.ProviderEnvVar(provider)
	if envVar == "" {
		// Provider doesn't need a key
		return credentialStatus{
			icon:    console.GlyphSuccess.Prefix(),
			text:    "no credential needed",
			missing: false,
		}
	}

	if val := strings.TrimSpace(os.Getenv(envVar)); val != "" {
		return credentialStatus{
			icon:    console.GlyphSuccess.Prefix(),
			text:    fmt.Sprintf("env var %s set", envVar),
			missing: false,
			envVar:  envVar,
		}
	}

	// Check stored credential
	resolved, err := credentials.ResolveProvider(provider)
	if err == nil && strings.TrimSpace(resolved.Value) != "" {
		return credentialStatus{
			icon:    console.GlyphSuccess.Prefix(),
			text:    "stored credential available",
			missing: false,
		}
	}

	// No credential available
	return credentialStatus{
		icon:    console.GlyphWarning.Prefix(),
		text:    fmt.Sprintf("not configured (set %s)", envVar),
		missing: true,
		envVar:  envVar,
	}
}

// setKey stores a credential for a provider
func (k *KeysCommand) setKey(configManager *configuration.Manager, provider string, apiKey string) error {
	// Validate provider exists
	providers := configManager.GetAvailableProviders()
	valid := false
	for _, p := range providers {
		if string(p) == provider {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown provider '%s'. Use '/keys list' to see available providers.", provider)
	}

	// If apiKey is empty, prompt for usage
	if apiKey == "" {
		envVar := credentials.ProviderEnvVar(provider)
		return fmt.Errorf("API key required. Usage: /keys set %s <api_key>\nAlternatively, set the environment variable: export %s=<key>", provider, envVar)
	}

	// Store the credential
	if err := credentials.SetToActiveBackend(provider, apiKey); err != nil {
		return fmt.Errorf("failed to store credential: %w", err)
	}

	envVar := credentials.ProviderEnvVar(provider)
	if envVar != "" {
		fmt.Printf("Credential set for %s (or set %s env var).\n", provider, envVar)
	} else {
		fmt.Printf("Credential set for %s.\n", provider)
	}

	return nil
}

// removeKey removes a stored credential for a provider
func (k *KeysCommand) removeKey(configManager *configuration.Manager, provider string) error {
	// Validate provider exists
	providers := configManager.GetAvailableProviders()
	valid := false
	for _, p := range providers {
		if string(p) == provider {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown provider '%s'. Use '/keys list' to see available providers.", provider)
	}

	// Try to delete from credential store
	if err := credentials.DeleteFromActiveBackend(provider); err != nil {
		return fmt.Errorf("failed to remove credential: %w", err)
	}

	fmt.Printf("Credential removed for %s.\n", provider)
	fmt.Println("Note: If the credential was set via environment variable, you need to unset it manually.")
	return nil
}