package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// sessionCleanupOnce ensures session cleanup runs only once per process.
var sessionCleanupOnce sync.Once

// backgroundOrphanCleanupOnce kills background processes left behind by a previous unclean exit.
var backgroundOrphanCleanupOnce sync.Once

func isDebugEnvEnabled() bool {
	value := strings.TrimSpace(configuration.GetEnvSimple("DEBUG"))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// agentInitParams encapsulates the parameters needed to initialize an Agent after the provider and client have been resolved.
type agentInitParams struct {
	client          api.ClientInterface
	clientType      api.ClientType
	systemPrompt    string
	configManager   *configuration.Manager
	workspaceRoot   string
	debug           bool
	interruptCtx    context.Context
	interruptCancel context.CancelFunc
	// subagentDepth tracks the nesting depth of this agent. 0 = primary, 1 = orchestrator, 2 = coder/tester, etc.
	subagentDepth int
	// rootPersonaID tracks the persona of the top-level (depth 0) agent. Propagated to subagents so depth limits can vary by root persona.
	rootPersonaID string
	// isProduction indicates this is a production agent, not a test agent. Production agents have additional initialization steps.
	isProduction bool
}

// NewAgent creates a new agent with auto-detected provider
func NewAgent() (*Agent, error) {
	return NewAgentWithModel("")
}

// resolveProfileAndSystemPrompt resolves the context profile and matching system prompt.
// Shared by both the SDK/WASM path (NewAgentWithClient) and the CLI path (newAgentWithConfigManagerInner).
// Floor errors (window < 8K) propagate to the caller.
func resolveProfileAndSystemPrompt(
	configManager *configuration.Manager,
	client api.ClientInterface,
	clientType api.ClientType,
	workspaceRoot string,
) (configuration.ContextProfile, string, error) {
	providerName := api.GetProviderName(clientType)

	// Read the model context window. Errors are non-fatal — falls back to default profile.
	contextWindow := 0
	if client != nil {
		if limit, err := client.GetModelContextLimit(); err == nil {
			contextWindow = limit
		}
	}

	// Resolve the profile. Floor errors propagate to the caller.
	var cfg *configuration.Config
	if configManager != nil {
		cfg = configManager.GetConfig()
	}
	profile, err := configuration.ResolveContextProfile(cfg, contextWindow)
	if err != nil {
		return profile, "", agenterrors.NewPermanentError("context profile resolution failed", err)
	}

	// (3) Load the prompt matched to the resolved profile. The path is
	// derived from profile.SystemPromptPath — the helper never hardcodes
	// "prompts/system_prompt.md" or "prompts/system_prompt.lite.md".
	systemPrompt, err := GetEmbeddedSystemPromptForProfile(profile, providerName, contextWindow, workspaceRoot)
	if err != nil {
		return profile, "", agenterrors.NewPermanentError("failed to load system prompt", err)
	}

	// (4) Apply the configured SystemPromptText override if any.
	systemPrompt = resolveConfiguredSystemPrompt(cfg, systemPrompt)

	return profile, systemPrompt, nil
}

// NewAgentWithClient builds an agent around a pre-constructed provider client.
// Skips the interactive provider-resolution path — useful for WASM/SDK callers where the caller already knows which provider and model to use.
// The configManager must already be initialized. The returned agent is a production agent.
func NewAgentWithClient(client api.ClientInterface, clientType api.ClientType, configManager *configuration.Manager) (*Agent, error) {
	if client == nil {
		return nil, agenterrors.NewPermanentError("client is required", nil)
	}
	if configManager == nil {
		return nil, agenterrors.NewPermanentError("configManager is required", nil)
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	if absWorkspaceRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
		workspaceRoot = absWorkspaceRoot
	}

	// Resolve the context profile and load the matching system prompt via the shared helper.
	// The profile is also re-resolved inside initAgentFromResolvedProvider; the two resolutions agree because they use the same inputs.
	_, systemPrompt, err := resolveProfileAndSystemPrompt(configManager, client, clientType, workspaceRoot)
	if err != nil {
		return nil, err
	}

	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	return initAgentFromResolvedProvider(agentInitParams{
		client:          client,
		clientType:      clientType,
		systemPrompt:    systemPrompt,
		configManager:   configManager,
		workspaceRoot:   workspaceRoot,
		debug:           isDebugEnvEnabled(),
		interruptCtx:    interruptCtx,
		interruptCancel: interruptCancel,
		isProduction:    true,
	})
}

// NewAgentWithModel creates a new agent with optional model override
func NewAgentWithModel(model string) (*Agent, error) {
	// Initialize configuration manager (silent mode for faster startup)
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to initialize configuration", err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithConfigDir creates a new agent using a per-client config directory for WebUI isolation.
func NewAgentWithConfigDir(configDir, model string) (*Agent, error) {
	// Initialize configuration manager with a client-specific directory
	configManager, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError(fmt.Sprintf("failed to initialize configuration from %s", configDir), err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithLayers creates a new agent using layered configuration (global + workspace).
func NewAgentWithLayers(globalDir, workspaceDir, model string) (*Agent, error) {
	configManager, err := configuration.NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to initialize layered configuration", err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithLayersInWorkspace creates a new agent using layered configuration with an explicit workspace root.
func NewAgentWithLayersInWorkspace(globalDir, workspaceDir, workspaceRoot, model string) (*Agent, error) {
	configManager, err := configuration.NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to initialize layered configuration", err)
	}

	return newAgentWithConfigManagerAndWorkspace(configManager, workspaceRoot, model)
}

// newAgentWithConfigManagerAndWorkspace is like newAgentWithConfigManager but accepts an explicit workspace root.
func newAgentWithConfigManagerAndWorkspace(configManager *configuration.Manager, workspaceRoot, model string) (*Agent, error) {
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			workspaceRoot = "."
		}
	}
	if absWorkspaceRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
		workspaceRoot = absWorkspaceRoot
	}

	return newAgentWithConfigManagerInner(configManager, workspaceRoot, model)
}

// newAgentWithConfigManager is the internal implementation that creates an agent
// with a pre-configured configuration manager.
func newAgentWithConfigManager(configManager *configuration.Manager, model string) (*Agent, error) {
	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	if absWorkspaceRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
		workspaceRoot = absWorkspaceRoot
	}

	return newAgentWithConfigManagerInner(configManager, workspaceRoot, model)
}

// isHomeDirPath reports whether dir resolves to the user's home directory.
func isHomeDirPath(dir string) bool {
	if dir == "" {
		return false
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	resolvedDir, dirErr := filepath.EvalSymlinks(dir)
	resolvedHome, homeErr := filepath.EvalSymlinks(homeDir)
	if dirErr != nil || homeErr != nil {
		resolvedDir = dir
		resolvedHome = homeDir
	}
	return resolvedDir == resolvedHome
}
