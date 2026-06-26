package agent

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"golang.org/x/term"
)

// ErrModelNotAvailable is returned when the configured model for the current provider
// is not available. In daemon mode, this allows the web UI to detect the issue and
// present a model selection UI rather than hard-failing.
var ErrModelNotAvailable = errors.New("configured model is not available for this provider")

// ErrProviderNotConfigured is returned when the provider cannot be initialized
// (unrecognized provider, missing API key, etc.) in daemon mode. This allows the
// web UI to start without an agent and present a provider configuration UI instead
// of crashing the daemon.
var ErrProviderNotConfigured = errors.New("provider is not configured — configure via webui settings")

// ErrQueryInProgress is returned when ProcessQuery is called while another
// query is already running on the same Agent instance. This happens when two
// frontends (CLI REPL and WebUI) share the same Agent — only one query can
// execute at a time to prevent message-list and state corruption.
var ErrQueryInProgress = errors.New("a query is already in progress on this agent")

// isNonInteractive returns true if the process is running in non-interactive
// mode (stdin is not a terminal). Used to prevent blocking prompts when
// running as a daemon, in tests, or piped input.
func isNonInteractive() bool {
	return !term.IsTerminal(int(os.Stdin.Fd()))
}

// isSSHDaemon returns true if running as an SSH daemon or regular daemon.
// SSH daemons set BROWSER=none to indicate they're running in headless mode.
// Regular daemons set SPROUT_DAEMON=1 when the -d flag is passed.
// Both cases should allow agent startup even without a provider
// configured, so that the web UI can handle provider setup.
func isSSHDaemon() bool {
	return strings.TrimSpace(os.Getenv("BROWSER")) == "none" ||
		strings.TrimSpace(os.Getenv("SPROUT_DAEMON")) == "1"
}

// findProviderWithAPIKey searches through available providers and returns the first one
// that has an API key configured (either via environment variable or stored credentials).
// This is used by SSH daemons to automatically select a working provider without requiring
// interactive configuration.
func findProviderWithAPIKey(configManager *configuration.Manager) (api.ClientType, string) {
	if configManager == nil {
		return "", ""
	}

	// Get available providers
	availableProviders := configManager.GetAvailableProviders()

	// Try each provider in order of priority
	for _, provider := range availableProviders {
		// Skip local providers that don't need API keys (handled elsewhere)
		if provider == api.OllamaLocalClientType || provider == api.LMStudioClientType || provider == api.TestClientType {
			continue
		}

		// Check if this provider has an API key
		if configManager.HasAPIKey(provider) {
			model := configManager.GetModelForProvider(provider)
			return provider, model
		}
	}

	return "", ""
}

// SelectProvider allows interactive provider selection
func (a *Agent) SelectProvider() error {
	newProvider, err := a.configManager.SelectNewProvider()
	if err != nil {
		return agenterrors.NewProviderError("failed to select provider", err, "", "")
	}

	// Update agent's client type and client atomically
	// Recreate client with new provider
	model := a.configManager.GetModelForProvider(newProvider)
	client, err := factory.CreateProviderClient(newProvider, model)
	if err != nil {
		return agenterrors.NewProviderError(fmt.Sprintf("failed to create client for %s", newProvider), err, "", "")
	}

	client.SetDebug(a.debug)
	a.setClient(client, newProvider)

	return nil
}

func looksLikeProviderModelSpecifier(configManager *configuration.Manager, model string) bool {
	parts := strings.SplitN(strings.TrimSpace(model), ":", 2)
	if len(parts) != 2 {
		return false
	}
	if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	if _, err := configManager.MapStringToClientType(parts[0]); err != nil {
		return false
	}
	return true
}

func recoverProviderStartup(configManager *configuration.Manager, failedProvider api.ClientType, modelArg string, startupErr error) (api.ClientType, string, error) {
	// Check if editor mode was the "failed" provider — this isn't a real failure
	// since editor mode has no provider to initialize
	if failedProvider == api.EditorClientType {
		return "", "", agenterrors.NewProviderError("editor mode is active — no AI provider configured. "+
			"Set up a provider with: sprout agent --provider <provider> or via webui settings (sprout agent -d)", nil, "", "")
	}

	failedProviderName := api.GetProviderName(failedProvider)
	// Diagnostic detail about which provider failed and why. The final error
	// (rendered by the CLI) already states the cause and how to configure a
	// provider, so keep this gated behind debug to avoid duplicating it.
	debugLogf("[provider] failed to initialize %q: %v", failedProviderName, startupErr)

	// Detect whether the failure is a model-not-found issue so we can offer
	// the user the option to pick a different model on the same provider.
	isModelError := isModelNotFoundError(startupErr)

	// Non-interactive mode. In daemon mode (web UI), return ErrProviderNotConfigured
	// for ANY provider error (not just model-not-found) so the web UI can present a
	// provider configuration UI. For regular non-interactive mode, fail with a hint.
	if isNonInteractive() {
		if isSSHDaemon() {
			return "", "", ErrProviderNotConfigured
		}
		return "", "", agenterrors.NewProviderError(fmt.Sprintf("failed to initialize provider %s: Running in non-interactive mode. %s", failedProviderName, noninteractive.HelpHint), startupErr, "", "")
	}

	choice, err := promptProviderRecoveryChoice(isModelError)
	if err != nil {
		return "", "", agenterrors.NewInvalidInputError("read user choice", err)
	}

	if choice == 0 {
		return "", "", fmt.Errorf("%w: %s", errProviderStartupClosed, failedProviderName)
	}

	// Choice 1 (or choice 2 when no model error): try a different model on the
	// same provider. List available models and let the user pick one.
	if choice == 1 && isModelError {
		models, listErr := api.GetModelsForProvider(failedProvider)
		if listErr != nil || len(models) == 0 {
			console.GlyphWarning.Fprintf(os.Stderr, "Failed to list models for %s: %v", failedProviderName, listErr)
			_, _ = os.Stderr.Write([]byte("Falling back to selecting a different provider.\n"))
			return recoverProviderBySwitching(configManager, failedProvider, failedProviderName, modelArg)
		}
		selectedModel, ok := promptModelSelection(models)
		if !ok {
			return "", "", fmt.Errorf("%w: %s", errProviderStartupClosed, failedProviderName)
		}
		return failedProvider, selectedModel, nil
	}

	// Choice 1 (no model error) or choice 2 (with model error): switch provider.
	return recoverProviderBySwitching(configManager, failedProvider, failedProviderName, modelArg)
}

// recoverProviderBySwitching prompts the user to select a new provider and
// returns the new provider type and model.
func recoverProviderBySwitching(configManager *configuration.Manager, failedProvider api.ClientType, failedProviderName string, modelArg string) (api.ClientType, string, error) {
	nextProvider, err := configManager.SelectNewProvider()
	if err != nil {
		return "", "", agenterrors.NewProviderError("failed to select provider", err, "", "")
	}

	nextModel := configManager.GetModelForProvider(nextProvider)
	if modelArg != "" && !looksLikeProviderModelSpecifier(configManager, modelArg) {
		nextModel = modelArg
	}

	return nextProvider, nextModel, nil
}

// isModelNotFoundError checks if the error indicates a model-not-found issue.
func isModelNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "model not found") ||
		strings.Contains(msg, "model not exist") ||
		strings.Contains(msg, "invalid model") ||
		strings.Contains(msg, "model.*not.*supported") ||
		strings.Contains(msg, "unsupported model")
}

func promptProviderRecoveryChoice(isModelError bool) (int, error) {
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	defer clihooks.ResumeSteer()
	reader := bufio.NewReader(os.Stdin)
	for {
		_, _ = os.Stderr.Write([]byte("Options:\n"))
		if isModelError {
			_, _ = os.Stderr.Write([]byte("  1. Choose a different model on this provider\n"))
			_, _ = os.Stderr.Write([]byte("  2. Select a different provider\n"))
			_, _ = os.Stderr.Write([]byte("  0. Close\n"))
			_, _ = os.Stderr.Write([]byte("Choice (0-2): "))
		} else {
			_, _ = os.Stderr.Write([]byte("  1. Select a different provider\n"))
			_, _ = os.Stderr.Write([]byte("  0. Close\n"))
			_, _ = os.Stderr.Write([]byte("Choice (0-1): "))
		}

		input, err := reader.ReadString('\n')
		if err != nil {
			return -1, err
		}

		parsed, err := strconv.Atoi(strings.TrimSpace(input))
		if err != nil {
			_, _ = os.Stderr.Write([]byte("Please enter a valid number.\n"))
			continue
		}

		if isModelError {
			if parsed >= 0 && parsed <= 2 {
				return parsed, nil
			}
			_, _ = os.Stderr.Write([]byte("Please enter 0, 1, or 2.\n"))
		} else {
			if parsed >= 0 && parsed <= 1 {
				return parsed, nil
			}
			_, _ = os.Stderr.Write([]byte("Please enter 0 or 1.\n"))
		}
	}
}

// promptModelSelection lists available models for a provider and lets the user
// pick one. Returns the selected model ID and true, or ("", false) if the user
// cancels.
func promptModelSelection(models []api.ModelInfo) (string, bool) {
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	defer clihooks.ResumeSteer()
	reader := bufio.NewReader(os.Stdin)
	_, _ = os.Stderr.Write([]byte("\nAvailable models:\n"))
	for i, m := range models {
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("  %2d. %s\n", i+1, m.ID)))
	}
	_, _ = os.Stderr.Write([]byte("Select a model (number, or 0 to cancel): "))

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", false
	}
	idx, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || idx < 0 {
		return "", false
	}
	if idx == 0 {
		return "", false
	}
	if idx < 1 || idx > len(models) {
		_, _ = os.Stderr.Write([]byte("Invalid selection. Cancelling.\n"))
		return "", false
	}
	return models[idx-1].ID, true
}
