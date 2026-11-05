package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// KeysCommand implements the /keys slash command for managing API credentials
type KeysCommand struct {
	outputSink
}

// Name returns the command name
func (k *KeysCommand) Name() string {
	return "keys"
}

// SafeDuringSteer returns true - /keys is config, independent of active turn
func (k *KeysCommand) SafeDuringSteer() bool {
	return true
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
		"/keys set <provider> [api_key] [--global|--local]",
		"                      Set credential for a provider via argument.",
		"/keys remove <provider>",
		"                      Remove stored credential for a provider.",
		"",
		"By default a key is stored wherever this process's config root points:",
		"inside a git repo that is <repo>/.sprout, so the key is scoped to that",
		"workspace. Pass --global to store it at the user level instead, or",
		"--local to force the workspace store. The two flags are mutually",
		"exclusive.",
		"",
		"Note: reads resolve through a single config root with no fallback, so",
		"a --global key is used only where the config root resolves to the",
		"user-level store (i.e. outside a git repo). Inside a repo the",
		"workspace store is what gets read. An explicit --global/--local write",
		"also writes the file store directly; if the OS keyring is active it",
		"still takes precedence on read, and the command will say so.",
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
		scope, rest := parseKeyScope(args[1:])
		if scope == keyScopeConflict {
			return fmt.Errorf("--global and --local are mutually exclusive; pass at most one")
		}
		if len(rest) < 1 {
			return fmt.Errorf("usage: /keys set <provider> [api_key] [--global|--local]")
		}
		provider := strings.ToLower(strings.TrimSpace(rest[0]))
		apiKey := ""
		if len(rest) > 1 {
			apiKey = strings.TrimSpace(rest[1])
		}
		return k.setKeyScoped(configManager, provider, apiKey, scope, chatAgent.GetWorkspaceRoot())
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
		// Suggest provider names for any subcommand that takes one.
		switch args[0] {
		case "set", "remove", "delete", "unset":
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

	if len(args) >= 3 && args[0] == "set" {
		return []string{"--global", "--local"}
	}

	return nil
}

// listKeys shows credential status for all providers
func (k *KeysCommand) listKeys(configManager *configuration.Manager) error {
	providers := configManager.GetAvailableProviders()
	if len(providers) == 0 {
		console.GlyphInfo.Fprintln(k.out(), "No providers configured.")
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

	k.println("Provider Credentials")
	k.println("===================")

	hasIssues := false
	for _, name := range sorted {
		status := k.getCredentialStatus(configManager, name)
		statusStr := status.icon + " " + status.text
		k.printf("%-20s %s\n", name+":", statusStr)
		if status.missing {
			hasIssues = true
		}
	}

	if hasIssues {
		k.println()
		k.println("Use '/keys set <provider> <api_key>' to configure missing credentials.")
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

// keyScope selects which credential store a `/keys set` writes to. The
// store root is normally repointed at <repo>/.sprout inside a git repo, so
// a key set in one repo is invisible from another; --global writes to the
// user-level config instead.
type keyScope int

const (
	// keyScopeDefault uses whatever root the process resolved (repo-local
	// inside a git repo, user-level otherwise). This is the historical
	// behaviour and stays the default.
	keyScopeDefault keyScope = iota
	// keyScopeGlobal forces the user-level config root.
	keyScopeGlobal
	// keyScopeLocal forces the workspace .sprout root.
	keyScopeLocal
	// keyScopeConflict is reported when --global and --local are both given.
	keyScopeConflict
)

// parseKeyScope pulls --global/--local out of the argument list and returns
// the requested scope plus the remaining positional arguments.
func parseKeyScope(args []string) (keyScope, []string) {
	scope := keyScopeDefault
	seen := false
	conflict := false
	rest := make([]string, 0, len(args))
	for _, arg := range args {
		var next keyScope
		switch arg {
		case "--global", "-g":
			next = keyScopeGlobal
		case "--local", "-l":
			next = keyScopeLocal
		default:
			rest = append(rest, arg)
			continue
		}
		if seen && next != scope {
			conflict = true
		}
		scope = next
		seen = true
	}
	if conflict {
		return keyScopeConflict, rest
	}
	return scope, rest
}

// setKey stores a credential for a provider in the process's resolved store.
func (k *KeysCommand) setKey(configManager *configuration.Manager, provider string, apiKey string) error {
	return k.setKeyScoped(configManager, provider, apiKey, keyScopeDefault, "")
}

// setKeyScoped stores a credential for a provider, honouring an explicit
// scope choice. --global and --local bypass the resolved backend and write
// straight to the file store under the chosen config root, because the
// keyring backend is process-wide and has no per-workspace notion.
//
// workspaceRoot is the agent's authoritative workspace root (not the raw
// cwd — from a repo subdirectory those differ, and using cwd would create
// a store the rest of the process never reads).
func (k *KeysCommand) setKeyScoped(configManager *configuration.Manager, provider string, apiKey string, scope keyScope, workspaceRoot string) error {
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
		return fmt.Errorf("API key required. Usage: /keys set %s <api_key> [--global|--local]\nAlternatively, set the environment variable: export %s=<key>", provider, envVar)
	}

	// Explicit scope: write to the named config root's file store.
	if scope != keyScopeDefault {
		configDir, err := scopeConfigDir(scope, workspaceRoot)
		if err != nil {
			return err
		}
		if err := credentials.SetForDir(configDir, provider, apiKey); err != nil {
			return fmt.Errorf("failed to store credential: %w", err)
		}
		envVar := credentials.ProviderEnvVar(provider)
		if envVar != "" {
			k.printf("Credential set for %s (or set %s env var).\n", provider, envVar)
		} else {
			k.printf("Credential set for %s.\n", provider)
		}
		k.printf("Scope: %s\n", scopeLabel(scope))
		if path, err := credentials.GetAPIKeysPathFromDir(configDir); err == nil {
			k.printf("Stored in %s\n", path)
		}
		k.printf("Backend: file (explicit scope)\n")
		// The OS keyring is process-wide and takes precedence over the file
		// store on read, so an explicit-scope write can be shadowed by an
		// existing keyring entry for the same provider. Say so rather than
		// let the user wonder why their new key has no effect.
		if mode, err := credentials.GetStorageMode(); err == nil && mode == "keyring" {
			k.printf("Note: the OS keyring is active and takes precedence on read; remove it with '/keys remove %s' if this key seems ignored.\n", provider)
		}
		return nil
	}

	// Store the credential
	if err := credentials.SetToActiveBackend(provider, apiKey); err != nil {
		return fmt.Errorf("failed to store credential: %w", err)
	}

	envVar := credentials.ProviderEnvVar(provider)
	if envVar != "" {
		k.printf("Credential set for %s (or set %s env var).\n", provider, envVar)
	} else {
		k.printf("Credential set for %s.\n", provider)
	}
	// SP-161f: the credential store is config-root scoped, and inside a git
	// repo the root command repoints that root at <repo>/.sprout. A key set
	// here is then invisible from another repo — so say where it landed
	// rather than let the user discover it by surprise.
	k.printf("Scope: %s\n", resolvedScopeLabel())
	if path, err := credentials.GetAPIKeysPath(); err == nil {
		k.printf("Stored in %s\n", path)
	}
	if mode, err := credentials.GetStorageMode(); err == nil && mode != "" {
		k.printf("Backend: %s\n", mode)
	}

	return nil
}

// scopeConfigDir resolves the config root for an explicit scope.
func scopeConfigDir(scope keyScope, workspaceRoot string) (string, error) {
	switch scope {
	case keyScopeGlobal:
		return configuration.DefaultConfigDir()
	case keyScopeLocal:
		return workspaceConfigDir(workspaceRoot)
	default:
		return "", fmt.Errorf("internal error: scopeConfigDir called with default scope")
	}
}

// workspaceConfigDir returns the workspace-local .sprout config root. It
// prefers the agent's resolved workspace root; only when that is unknown
// does it fall back to walking up from cwd to the enclosing git repo,
// mirroring the root command's auto-detection. Joining the raw cwd would
// be wrong: from a repo subdirectory it would create <subdir>/.sprout, a
// store the rest of the process never reads.
func workspaceConfigDir(workspaceRoot string) (string, error) {
	if root := strings.TrimSpace(workspaceRoot); root != "" {
		return requireWorkspaceLayer(root)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory: %w", err)
	}
	dir := cwd
	for {
		// .git may be a directory (normal repo) or a file (worktree /
		// submodule gitdir pointer). Accept either, matching git's own
		// notion of a work tree.
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return requireWorkspaceLayer(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Not in a git repo: the workspace root is the cwd itself.
	return requireWorkspaceLayer(cwd)
}

// requireWorkspaceLayer resolves root's .sprout layer, refusing when the
// root has none. configuration.WorkspaceConfigDir deliberately returns ""
// for $HOME (a ~/.sprout layer is a documented footgun), so --local there
// must error rather than create a store nothing reads.
func requireWorkspaceLayer(root string) (string, error) {
	if dir := configuration.WorkspaceConfigDir(root); dir != "" {
		return dir, nil
	}
	return "", fmt.Errorf("cannot use --local: %s has no workspace config layer (the home directory is not a workspace)", root)
}

func scopeLabel(scope keyScope) string {
	switch scope {
	case keyScopeGlobal:
		return "user-level (used outside a git repo; a repo reads its own .sprout)"
	case keyScopeLocal:
		return "workspace (.sprout in this repo)"
	default:
		return resolvedScopeLabel()
	}
}

// resolvedScopeLabel describes where the process's active config root points,
// so the user can tell a repo-local store from the user-level one. It
// compares the resolved root against the user-level default rather than
// guessing from the path shape, so a custom SPROUT_CONFIG is labelled
// correctly. The "Stored in" line that follows is authoritative regardless.
func resolvedScopeLabel() string {
	active, err := envutil.ConfigDir()
	if err != nil {
		return "unknown"
	}
	global, err := configuration.DefaultConfigDir()
	if err != nil {
		return "unknown"
	}
	if filepath.Clean(active) == filepath.Clean(global) {
		return "user-level"
	}
	if strings.HasSuffix(filepath.Clean(active), string(filepath.Separator)+".sprout") {
		return "workspace (this repo's .sprout)"
	}
	return fmt.Sprintf("custom (%s)", active)
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

	k.printf("Credential removed for %s.\n", provider)
	k.println("Note: If the credential was set via environment variable, you need to unset it manually.")
	return nil
}
