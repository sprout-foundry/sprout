//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/codecompletion"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// handleAPICompletion generates a code completion for the given prefix/suffix.
func (ws *ReactWebServer) handleAPICompletion(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Prefix    string `json:"prefix"`     // code before cursor (required)
		Suffix    string `json:"suffix"`     // code after cursor
		Language  string `json:"language"`   // language ID
		FilePath  string `json:"file_path"`  // file being edited
		MaxTokens int    `json:"max_tokens"` // optional, default 128
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.Prefix) == "" {
		writeJSONErr(w, http.StatusBadRequest, "prefix_required", "Prefix is required")
		return
	}

	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_not_available", "Agent is not available")
		return
	}

	configManager := agentInst.GetConfigManager()
	if configManager == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_configuration_unavailable", "Agent configuration is unavailable")
		return
	}

	var clientType api.ClientType
	var model string
	var client api.ClientInterface
	cfg := configManager.GetConfig()

	// Try the completion-specific provider/model first (e.g. a cheap local
	// Ollama model). If it is not configured or cannot be reached, fall back
	// to the main provider.
	if completionProvider := cfg.GetCompletionProvider(); completionProvider != "" {
		if ct, mapErr := configManager.MapStringToClientType(completionProvider); mapErr == nil {
			clientType = ct
			model = cfg.GetCompletionModel()
			if cl, createErr := factory.CreateProviderClient(clientType, model); createErr == nil {
				client = cl
			}
		}
	}

	// No completion provider configured, or it failed to create — use the main provider.
	if client == nil {
		var resolveErr error
		clientType, resolveErr = configManager.GetProvider()
		if resolveErr != nil {
			writeJSONErr(w, http.StatusInternalServerError, "failed_to_resolve_provider", fmt.Sprintf("Failed to resolve provider: %v", resolveErr))
			return
		}
		model = configManager.GetModelForProvider(clientType)
		var createErr error
		client, createErr = factory.CreateProviderClient(clientType, model)
		if createErr != nil {
			writeJSONErr(w, http.StatusInternalServerError, "failed_to_create_provider_client", fmt.Sprintf("Failed to create provider client: %v", createErr))
			return
		}
	}

	result, err := codecompletion.GenerateCompletion(client, codecompletion.CompletionRequest{
		Prefix:    req.Prefix,
		Suffix:    req.Suffix,
		Language:  req.Language,
		FilePath:  req.FilePath,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_generate_completion", fmt.Sprintf("Failed to generate completion: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"completion":  result.Text,
		"provider":    client.GetProvider(),
		"model":       client.GetModel(),
		"tokens_used": result.TokensUsed,
	})
}
