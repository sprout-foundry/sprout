package agent

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/noninteractive"
	"golang.org/x/term"
)

// isNonInteractive returns true if the process is running in non-interactive
// mode (stdin is not a terminal). Used to prevent blocking prompts when
// running as a daemon, in tests, or piped input.
func isNonInteractive() bool {
	return !term.IsTerminal(int(os.Stdin.Fd()))
}

// isSSHDaemon returns true if running as an SSH daemon.
// SSH daemons set BROWSER=none to indicate they're running
// in headless mode and should allow startup even without a provider
// configured, so that the web UI can handle provider setup.
func isSSHDaemon() bool {
	return strings.TrimSpace(os.Getenv("BROWSER")) == "none"
}

// SelectProvider allows interactive provider selection
func (a *Agent) SelectProvider() error {
	newProvider, err := a.configManager.SelectNewProvider()
	if err != nil {
		return fmt.Errorf("failed to select provider: %w", err)
	}

	// Update agent's client type
	a.clientType = newProvider

	// Recreate client with new provider
	model := a.configManager.GetModelForProvider(newProvider)
	client, err := factory.CreateProviderClient(newProvider, model)
	if err != nil {
		return fmt.Errorf("failed to create client for %s: %w", newProvider, err)
	}

	a.client = client
	a.client.SetDebug(a.debug)

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
		return "", "", fmt.Errorf("editor mode is active — no AI provider configured. "+
			"Set up a provider with: ledit agent --provider <provider> or via webui settings (ledit agent -d)")
	}

	failedProviderName := api.GetProviderName(failedProvider)
	fmt.Fprintf(os.Stderr, "[WARN] Failed to initialize provider '%s': %v\n", failedProviderName, startupErr)

	// Non-interactive mode cannot recover via prompt.
	if isNonInteractive() {
		return "", "", fmt.Errorf("failed to initialize provider %s: Running in non-interactive mode. %s: %w", failedProviderName, noninteractive.HelpHint, startupErr)
	}

	choice, err := promptProviderRecoveryChoice()
	if err != nil {
		return "", "", fmt.Errorf("failed to read provider recovery choice: %w", err)
	}

	if choice == 2 {
		return "", "", fmt.Errorf("%w: %s", errProviderStartupClosed, failedProviderName)
	}

	nextProvider, err := configManager.SelectNewProvider()
	if err != nil {
		return "", "", fmt.Errorf("failed to select provider: %w", err)
	}

	nextModel := configManager.GetModelForProvider(nextProvider)
	if modelArg != "" && !looksLikeProviderModelSpecifier(configManager, modelArg) {
		nextModel = modelArg
	}

	return nextProvider, nextModel, nil
}

func promptProviderRecoveryChoice() (int, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  1. Select a different provider")
		fmt.Fprintln(os.Stderr, "  2. Close")
		fmt.Fprint(os.Stderr, "Choice (1-2): ")

		input, err := reader.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("read user choice: %w", err)
		}

		choice, err := strconv.Atoi(strings.TrimSpace(input))
		if err != nil || choice < 1 || choice > 2 {
			fmt.Fprintln(os.Stderr, "Please enter 1 or 2.")
			continue
		}

		return choice, nil
	}
}
