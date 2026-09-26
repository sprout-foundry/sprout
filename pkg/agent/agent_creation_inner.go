package agent

// agent_creation_inner.go — newAgentWithConfigManagerInner: the shared
// body of the NewAgentWith* constructors (configuration →
// workspace/layer setup → factory build → event wiring → provider
// registration → session init). Split out of agent_creation.go.
import (
	"context"
	"fmt"
	"os"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
)

// newAgentWithConfigManagerInner is the core implementation that accepts an explicit workspace root.
func newAgentWithConfigManagerInner(configManager *configuration.Manager, workspaceRoot, model string) (*Agent, error) {
	var err error

	var clientType api.ClientType
	var finalModel string

	// --mock-llm flag override: use a deterministic mock provider. Excluded from WASM build.
	if handled, agent, err := tryMockLLMAgent(model, configManager, workspaceRoot); handled {
		return agent, err
	}

	// If running under `go test`, prefer the test/mock client to avoid network/API key
	// dependencies unless explicitly overridden by SPROUT_ALLOW_REAL_PROVIDER (or legacy SPROUT_ALLOW_REAL_PROVIDER).
	if isRunningUnderTest() && configuration.GetEnvSimple("ALLOW_REAL_PROVIDER") == "" {
		clientType = api.TestClientType
		finalModel = model
		// Create the test client immediately to avoid API key checks
		client, err := factory.CreateProviderClient(clientType, finalModel)
		if err != nil {
			return nil, agenterrors.NewProviderError("failed to create API client for tests", err, "", "")
		}

		// Load system prompt for test agent
		providerName := api.GetProviderName(clientType)
		systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
		if err != nil {
			return nil, agenterrors.NewPermanentError("failed to load system prompt", err)
		}
		systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

		// Initialize agent using the helper
		return initAgentFromResolvedProvider(agentInitParams{
			client:          client,
			clientType:      clientType,
			systemPrompt:    systemPrompt,
			configManager:   configManager,
			workspaceRoot:   workspaceRoot,
			debug:           isDebugEnvEnabled(),
			interruptCtx:    context.Background(),
			interruptCancel: func() { /* no-op */ },
			isProduction:    false,
		})
	}

	// Non-interactive fast-fail: check provider availability before entering the retry loop.
	// SSH daemons allow startup even without a provider so the web UI can handle provider setup.
	if isNonInteractive() && !isRunningUnderTest() && !isSSHDaemon() {
		resolvedType, _, resolveErr := configManager.ResolveProviderModel("", model)
		if resolveErr != nil {
			return nil, agenterrors.NewProviderError("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint, resolveErr, "", "")
		}
		// Check if editor mode is active
		if resolvedType == api.EditorClientType {
			return nil, agenterrors.NewProviderError("editor mode is active — no AI provider configured. "+
				"Set up a provider with: sprout agent --provider <provider> "+
				"or configure via Settings in the webui (sprout agent -d)", nil, "", "")
		}
		// Provider resolved — ensure API key exists without prompting.
		if keyErr := configManager.EnsureAPIKey(resolvedType); keyErr != nil {
			return nil, agenterrors.NewProviderError("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint, keyErr, "", "")
		}

		// Warn that non-interactive runs use a permissive security posture.
		console.GlyphWarning.Fprintf(os.Stderr,
			"Non-interactive mode: security is permissive (Medium/High operations auto-approved; only Critical ops block). "+
				"Run inside a container or sandbox for isolation.\n")
	}

	// The early check ensures the provider resolves before the retry loop. The retry loop's recoverProviderStartup calls serve as defense-in-depth.
	clientType, finalModel, err = configManager.ResolveProviderModel("", model)
	if err != nil {
		console.GlyphWarning.Fprintf(os.Stderr, "Failed to resolve configured provider/model: %v", err)
		// SSH daemon exception: allow startup even without provider
		if isSSHDaemon() {
			// Continue with whatever clientType was resolved (may be EditorClientType)
		} else if isNonInteractive() {
			return nil, agenterrors.NewProviderError("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint, err, "", "")
		} else {
			// Interactive mode: offer to select a provider
			console.GlyphAction.Fprintf(os.Stderr, "Selecting an available provider...")
			clientType, err = configManager.SelectNewProvider()
			if err != nil {
				return nil, agenterrors.NewProviderError("failed to select provider", err, "", "")
			}
			finalModel = configManager.GetModelForProvider(clientType)
			if model != "" && !looksLikeProviderModelSpecifier(configManager, model) {
				finalModel = model
			}
		}
	}

	// Check if editor mode is active — no AI provider configured
	if clientType == api.EditorClientType {
		// SSH daemon exception: try to find a provider with API key automatically
		if isSSHDaemon() {
			if autoProvider, autoModel := findProviderWithAPIKey(configManager); autoProvider != "" {
				console.GlyphInfo.Fprintf(os.Stderr, "SSH: Auto-selected provider %s (has API key)", autoProvider)
				clientType = autoProvider
				finalModel = autoModel
			} else {
				return nil, agenterrors.NewProviderError("editor mode is active — no AI provider configured. "+
					"Set up a provider with: sprout agent --provider <provider> "+
					"or configure via Settings in the webui (sprout agent -d)", nil, "", "")
			}
		} else {
			return nil, agenterrors.NewProviderError("editor mode is active — no AI provider configured. "+
				"Set up a provider with: sprout agent --provider <provider> "+
				"or configure via Settings in the webui (sprout agent -d)", nil, "", "")
		}
	}

	// Ensure provider can be initialized; allow recovery in interactive mode.
	var client api.ClientInterface
	for {
		if err := configManager.EnsureAPIKey(clientType); err != nil {
			console.GlyphWarning.Fprintf(os.Stderr, "Provider %s is not configured: %v", api.GetProviderName(clientType), err)
			nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
			if recoverErr != nil {
				return nil, agenterrors.NewProviderError("provider recovery failed after ensuring API key", recoverErr, "", "")
			}
			clientType = nextClientType
			finalModel = nextModel
			continue
		}

		// Create the client
		client, err = factory.CreateProviderClient(clientType, finalModel)
		if err != nil {
			nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
			if recoverErr != nil {
				return nil, agenterrors.NewProviderError("provider recovery failed after creating client", recoverErr, "", "")
			}
			clientType = nextClientType
			finalModel = nextModel
			continue
		}

		// Set debug mode on the client
		debug := isDebugEnvEnabled()
		client.SetDebug(debug)

		// Check connection. Skip for providers where a fast/reliable connectivity probe is not available (Z.AI, GLM Coding).
		skipConnectionCheck := configuration.GetEnvSimple("SKIP_CONNECTION_CHECK") != "" ||
			clientType == api.ZAIClientType ||
			clientType == api.ZAICodingClientType
		if !skipConnectionCheck {
			if err := client.CheckConnection(); err != nil {
				nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
				if recoverErr != nil {
					return nil, agenterrors.NewProviderError("provider recovery failed after connection check", recoverErr, "", "")
				}
				clientType = nextClientType
				finalModel = nextModel
				continue
			}
		} else if debug {
			fmt.Println()
			console.GlyphWarning.Printf("Skipping provider connection check for %s", api.GetProviderName(clientType))
		}

		break
	}

	// Save the selection
	if err := configManager.SetProvider(clientType); err != nil {
		_, _ = os.Stdout.Write([]byte(fmt.Sprintf("Warning: Failed to save provider selection: %v\n", err)))
	}
	if finalModel != "" && finalModel != configManager.GetModelForProvider(clientType) && clientType != api.TestClientType {
		if err := configManager.SetModelForProvider(clientType, finalModel); err != nil {
			fmt.Println()
			console.GlyphWarning.Printf("Failed to save model selection: %v", err)
		}
	}

	// Check if debug mode is enabled
	debug := isDebugEnvEnabled()

	// Resolve the context profile and load the matching system prompt via the shared helper.
	_, systemPrompt, err := resolveProfileAndSystemPrompt(configManager, client, clientType, workspaceRoot)
	if err != nil {
		return nil, err
	}

	// Create interrupt context for the agent
	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	// Initialize agent using the helper
	return initAgentFromResolvedProvider(agentInitParams{
		client:          client,
		clientType:      clientType,
		systemPrompt:    systemPrompt,
		configManager:   configManager,
		workspaceRoot:   workspaceRoot,
		debug:           debug,
		interruptCtx:    interruptCtx,
		interruptCancel: interruptCancel,
		isProduction:    true,
	})
}
