//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// gatherStats collects server statistics
func (ws *ReactWebServer) gatherStats(r *http.Request) map[string]interface{} {
	return ws.gatherStatsForClientID(ws.resolveClientID(r))
}

// wslToWindowsPath is a package-level conversion function set once during init.
// On WSL systems it wraps "wslpath -w"; on non-WSL systems it is nil.
// Using a func var avoids calling exec.LookPath("wslpath") on every request.
var wslToWindowsPath func(linuxPath string) (string, error)

// wslToWindowsPathFunc creates the actual conversion function used by wslToWindowsPath.
func wslToWindowsPathFunc(linuxPath string) (string, error) {
	// Use wslpath to convert Linux path to Windows UNC path.
	// Handles /mnt/c/... → C:\... and /home/... → \\wsl.localhost\<distro>\...
	out, err := exec.Command("wslpath", "-w", linuxPath).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -w %q: %w", linuxPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func init() {
	// Detect WSL and set up the path converter.
	// WSL_DISTRO_NAME is set by the WSL runtime environment.
	if os.Getenv("WSL_DISTRO_NAME") != "" && shellExists("wslpath") {
		wslToWindowsPath = wslToWindowsPathFunc
	}
}

// populateAgentStats populates a stats map with fields from the given agent instance.
func populateAgentStats(stats map[string]interface{}, agentInst *agent.Agent) {
	stats["provider"] = agentInst.GetProvider()
	stats["model"] = agentInst.GetModel()
	stats["persona"] = agentInst.GetActivePersona()
	stats["session_id"] = agentInst.GetSessionID()
	stats["total_tokens"] = agentInst.GetTotalTokens()
	stats["prompt_tokens"] = agentInst.GetPromptTokens()
	stats["completion_tokens"] = agentInst.GetCompletionTokens()
	stats["cached_tokens"] = agentInst.GetCachedTokens()
	stats["cache_efficiency"] = float64(0)
	if totalTokens := agentInst.GetTotalTokens(); totalTokens > 0 {
		stats["cache_efficiency"] = float64(agentInst.GetCachedTokens()) / float64(totalTokens) * 100
	}
	stats["cached_cost_savings"] = agentInst.GetCachedCostSavings()
	stats["current_context_tokens"] = agentInst.GetCurrentContextTokens()
	stats["max_context_tokens"] = agentInst.GetMaxContextTokens()
	stats["context_usage_percent"] = float64(0)
	if maxTokens := agentInst.GetMaxContextTokens(); maxTokens > 0 {
		stats["context_usage_percent"] = float64(agentInst.GetCurrentContextTokens()) / float64(maxTokens) * 100
	}
	stats["context_warning_issued"] = agentInst.GetContextWarningIssued()
	stats["total_cost"] = agentInst.GetTotalCost()
	stats["last_tps"] = agentInst.GetLastTPS()
	stats["current_iteration"] = agentInst.GetCurrentIteration()
	if agentInst.GetMaxIterations() == 0 {
		stats["max_iterations"] = "unlimited"
	} else {
		stats["max_iterations"] = agentInst.GetMaxIterations()
	}
	stats["streaming_enabled"] = agentInst.IsStreamingEnabled()
	stats["debug_mode"] = agentInst.IsDebugMode()
	stats["embedding_index_enabled"] = agentInst.IsEmbeddingIndexEnabled()
	if em := agentInst.GetEmbeddingManager(); em != nil {
		stats["embedding_index_building"] = em.IsBuilding()
		stats["embedding_index_initialized"] = em.IsInitialized()
		stats["embedding_index_size"] = em.IndexSize()
	}
}

func (ws *ReactWebServer) gatherStatsForClientID(clientID string) map[string]interface{} {
	ws.mutex.RLock()
	stats := ws.gatherStatsForClientIDLocked(clientID)
	ws.mutex.RUnlock()

	// If the locked function didn't find an agent (clientCtx.Agent was nil),
	// try to get or create one outside the lock to avoid deadlock —
	// getClientAgent acquires ws.mutex itself.
	if stats["provider"] == "" && clientID != "" {
		if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
			populateAgentStats(stats, agentInst)
		}
	}

	return stats
}

// gatherStatsForClientIDLocked gathers stats assuming ws.mutex is already held.
func (ws *ReactWebServer) gatherStatsForClientIDLocked(clientID string) map[string]interface{} {
	uptime := time.Since(ws.startTime)
	terminalSessions := 0
	for _, clientCtx := range ws.clientContexts {
		if clientCtx != nil && clientCtx.Terminal != nil {
			terminalSessions += clientCtx.Terminal.GetVisibleSessionCount()
		}
	}
	if terminalSessions == 0 && ws.terminalManager != nil {
		terminalSessions = ws.terminalManager.GetVisibleSessionCount()
	}

	// Get agent stats if available
	stats := map[string]interface{}{
		"uptime_seconds":                       int64(uptime.Seconds()),
		"connections":                          ws.countConnections(),
		"queries":                              ws.queryCount,
		"query_count":                          ws.queryCount,
		"terminal_sessions":                    terminalSessions,
		"client_context_count":                 len(ws.clientContexts),
		"client_context_cleanup_removed_last":  ws.lastClientContextCleanupRemoved,
		"client_context_cleanup_removed_total": ws.totalClientContextsRemoved,
		"server_time":                          time.Now().Unix(),
		"start_time":                           ws.startTime.Unix(),
		"uptime_formatted":                     uptime.String(),
		"uptime":                               uptime.String(),
	}
	if !ws.lastClientContextCleanupAt.IsZero() {
		stats["client_context_cleanup_last_unix"] = ws.lastClientContextCleanupAt.Unix()
	}

	clientCtx := ws.clientContexts[clientID]
	if clientCtx == nil {
		clientCtx = ws.clientContexts[defaultWebClientID]
	}

	// Report whether this client currently has an active query.
	// The frontend uses this during reconnect to immediately restore (or
	// clear) the processing indicator instead of relying on a 3-second
	// safety timer.
	stats["is_processing"] = clientCtx != nil && clientCtx.ActiveQuery
	if clientCtx != nil && clientCtx.ActiveQuery && clientCtx.CurrentQuery != "" {
		stats["current_query"] = clientCtx.CurrentQuery
	}

	var agentInst *agent.Agent
	if clientCtx != nil {
		agentInst = clientCtx.Agent
	}

	// Always include provider/model/persona in stats so the frontend can reliably
	// detect whether a provider is configured (empty = none).
	stats["provider"] = ""
	stats["model"] = ""
	stats["persona"] = ""
	stats["embedding_index_enabled"] = false

	// Add agent-specific stats if available
	if agentInst != nil {
		populateAgentStats(stats, agentInst)
	} else {
		// Agent hasn't been lazily created yet. Fall back to the configured
		// provider/model from user settings so the frontend doesn't flash
		// "no provider" even though one is configured.
		cfg, cfgErr := configuration.Load()
		if cfgErr == nil && cfg != nil {
			if p := strings.TrimSpace(cfg.LastUsedProvider); p != "" && p != "editor" {
				stats["provider"] = p
				stats["model"] = cfg.GetModelForProvider(p)
			}
		}
	}
	if clientCtx != nil && len(clientCtx.AgentState) > 0 {
		var clientState agent.AgentState
		if err := json.Unmarshal(clientCtx.AgentState, &clientState); err == nil {
			stats["session_id"] = clientState.SessionID
			stats["total_tokens"] = clientState.TotalTokens
			stats["prompt_tokens"] = clientState.PromptTokens
			stats["completion_tokens"] = clientState.CompletionTokens
			stats["cached_tokens"] = clientState.CachedTokens
			stats["cached_cost_savings"] = clientState.CachedCostSavings
			stats["total_cost"] = clientState.TotalCost
		}
	}

	return stats
}
