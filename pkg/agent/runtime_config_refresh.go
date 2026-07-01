package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// refreshMu serializes RefreshRuntimeConfig calls. The MCP reconcile logic
// takes a snapshot of the manager's server set, then modifies it — two
// concurrent calls would interleave those modifications and produce
// non-deterministic results (duplicate-add failures, unnecessary restarts).
// The lock is per-Agent so independent agents don't block each other.
var refreshMu sync.Mutex

// RefreshRuntimeConfig reloads configuration from disk and reconciles the
// in-memory MCP server state so that servers added, removed, or modified
// through the webui settings API take effect without restarting the sprout
// process. This is the single entry point the webui calls after changing
// MCP servers or installing skills.
//
// The context propagates cancellation from the caller (e.g. an HTTP request
// that the user closed). MCP server startup goroutines honor ctx.Done().
//
// The method is safe to call concurrently with an active query — the MCP
// manager's own mutex protects AddServer/RemoveServer/ListServers and
// RefreshMCPTools uses the init mutex for cache invalidation. Concurrent
// RefreshRuntimeConfig calls are serialized via refreshMu.
func (a *Agent) RefreshRuntimeConfig(ctx context.Context) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()

	// --- Step A: Reload config from disk ---
	if a.configManager == nil {
		return agenterrors.NewConfig("config manager is not available", nil)
	}
	if err := a.configManager.Reload(); err != nil {
		return agenterrors.NewConfig("reload config", err)
	}

	// --- Step B: Reconcile MCP servers ---
	// Even if mcpSub is nil, the config reload above still picks up new
	// skills. Only attempt MCP reconciliation if the sub-manager exists.
	if a.mcpSub == nil || a.mcpSub.GetManager() == nil {
		return nil
	}

	if err := a.reconcileMCPServers(ctx); err != nil {
		return err
	}

	// --- Step C: Mark initialized and clear tool cache ---
	// reconcileMCPServers already started the configured servers. We mark
	// MCP as initialized so getMCPTools() doesn't re-run the add-all loop
	// in initializeMCP() (which would duplicate-add servers and conflict
	// with our reconcile state). Then we clear the cache so the next
	// getMCPTools() call refreshes the tool list from the now-running
	// servers via GetAllTools().
	a.mcpSub.LockInit()
	a.mcpSub.SetInitialized(true)
	a.mcpSub.SetInitError(nil)
	a.mcpSub.SetToolsCache(nil)
	a.mcpSub.UnlockInit()

	return nil
}

// RefreshSkills reloads configuration from disk so that newly discovered
// skills (e.g., SKILL.md files dropped on disk) appear in list_skills
// without requiring a restart. This is called by the webui after skill
// installation.
func (a *Agent) RefreshSkills() error {
	if a.configManager == nil {
		return agenterrors.NewConfig("config manager is not available", nil)
	}
	return a.configManager.Reload()
}

// reconcileMCPServers synchronizes the live MCP manager's server set with
// the freshly-reloaded config. Servers that were removed from config are
// stopped and removed; servers that were added are registered and started;
// servers whose key config fields changed are restarted.
func (a *Agent) reconcileMCPServers(ctx context.Context) error {
	mgr := a.mcpSub.GetManager()
	newConfig := a.configManager.GetConfig()
	newServers := newConfig.MCP.Servers

	// Build a map of currently registered servers (name -> config)
	currentServers := make(map[string]mcp.MCPServerConfig)
	for _, srv := range mgr.ListServers() {
		cfg := srv.GetConfig()
		currentServers[srv.GetName()] = cfg
	}

	var errs []error

	// 1. Remove servers that are no longer in config
	for name, _ := range currentServers {
		if _, exists := newServers[name]; !exists {
			if a.debug {
				a.Logger().Info("Removing MCP server no longer in config: %s", name)
			}
			if err := mgr.RemoveServer(name); err != nil {
				errs = append(errs, agenterrors.NewConfig(fmt.Sprintf("remove server %s", name), err))
				a.Logger().Warn("Failed to remove MCP server %s: %v", name, err)
			}
		}
	}

	// 2. Detect added / changed / unchanged servers
	for name, newCfg := range newServers {
		curCfg, exists := currentServers[name]
		if !exists {
			// New server — add it
			if a.debug {
				a.Logger().Info("Adding new MCP server from config: %s", name)
			}
			if err := mgr.AddServer(newCfg); err != nil {
				if !isAlreadyExistsError(err) {
					errs = append(errs, agenterrors.NewConfig(fmt.Sprintf("add server %s", name), err))
					a.Logger().Warn("Failed to add MCP server %s: %v", name, err)
				}
			}
		} else if serverConfigChanged(curCfg, newCfg) {
			// Existing server with changed config — restart it
			if a.debug {
				a.Logger().Info("Restarting MCP server with changed config: %s", name)
			}
			if err := mgr.RemoveServer(name); err != nil {
				errs = append(errs, agenterrors.NewConfig(fmt.Sprintf("remove server %s for restart", name), err))
				a.Logger().Warn("Failed to remove MCP server %s for restart: %v", name, err)
				continue
			}
			if addErr := mgr.AddServer(newCfg); addErr != nil {
				errs = append(errs, agenterrors.NewConfig(fmt.Sprintf("re-add server %s", name), addErr))
				a.Logger().Warn("Failed to re-add MCP server %s: %v", name, addErr)
			}
		}
	}

	// 3. Start all servers that should be running
	if startErr := mgr.StartAll(ctx); startErr != nil {
		errs = append(errs, agenterrors.NewConfig("start MCP servers", startErr))
		a.Logger().Warn("Failed to start some MCP servers: %v", startErr)
	}

	// Return aggregate error if any operation failed
	if len(errs) > 0 {
		return agenterrors.NewConfig("mcp reconcile", errors.Join(errs...))
	}

	return nil
}

// serverConfigChanged compares two MCPServerConfig values on the fields that
// require a server restart when they change (structural fields). It ignores
// Name (identity), Env/Credentials (runtime-only), and MaxRestarts (behavior
// that doesn't affect the process).
func serverConfigChanged(old, new mcp.MCPServerConfig) bool {
	if old.Type != new.Type {
		return true
	}
	if old.Command != new.Command {
		return true
	}
	if old.URL != new.URL {
		return true
	}
	if old.WorkingDir != new.WorkingDir {
		return true
	}
	if !slices.Equal(old.Args, new.Args) {
		return true
	}
	if old.Timeout != new.Timeout {
		return true
	}
	if old.AutoStart != new.AutoStart {
		return true
	}
	// Env changes require a restart — env vars are injected at subprocess
	// start time and a running process won't see new values.
	if !mapsEqual(old.Env, new.Env) {
		return true
	}
	return false
}

// mapsEqual compares two string maps for equality.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// isAlreadyExistsError checks if the error indicates the server already exists.
func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "already exists")
}
