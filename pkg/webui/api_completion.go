//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/codecompletion"
	"github.com/sprout-foundry/sprout/pkg/configuration"
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

	client, _, _, selErr := resolveCompletionClient(configManager)
	if selErr != nil {
		code := "failed_to_create_provider_client"
		if errors.Is(selErr, errCompletionProviderUnresolvable) {
			code = "failed_to_resolve_provider"
		}
		writeJSONErr(w, http.StatusInternalServerError, code, selErr.Error())
		return
	}

	result, err := codecompletion.GenerateCompletion(r.Context(), client, codecompletion.CompletionRequest{
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

// Sentinel errors for resolveCompletionClient's main-provider fallback so
// the handler keeps its pre-role error codes.
var (
	// errCompletionProviderUnresolvable marks a main-provider resolution
	// failure (handler code failed_to_resolve_provider).
	errCompletionProviderUnresolvable = errors.New("failed to resolve provider")
	// errCompletionClientCreation marks a main-provider client creation
	// failure (handler code failed_to_create_provider_client).
	errCompletionClientCreation = errors.New("failed to create provider client")
)

// resolveCompletionClient picks the LLM client for code completion,
// completion-first: it tries the completion-specific getters first —
// GetCompletionProvider/GetCompletionModel implement the legacy getters'
// field-wise precedence (an explicit completion setting wins, then the coder
// role, no last-used-provider fallback) — then, when the completion provider
// resolves empty, the main conversation provider. The subagent settings (the
// coder role's general alias) are never consulted here, so a configured
// subagent model cannot leak into inline completions; the completion
// settings are the completion path's own alias, read through the coder role
// by these getters only. The error, when non-nil, wraps one of the two
// sentinel errors above.
func resolveCompletionClient(configManager *configuration.Manager) (api.ClientInterface, api.ClientType, string, error) {
	cfg := configManager.GetConfig()
	if cfg != nil {
		if completionProvider := cfg.GetCompletionProvider(); completionProvider != "" {
			completionModel := cfg.GetCompletionModel()
			if clientType, mapErr := configManager.MapStringToClientType(completionProvider); mapErr == nil {
				if client, createErr := factory.CreateProviderClient(clientType, completionModel); createErr == nil {
					return client, clientType, completionModel, nil
				}
			}
		}
	}

	clientType, resolveErr := configManager.GetProvider()
	if resolveErr != nil {
		return nil, clientType, "", fmt.Errorf("%w: %w", errCompletionProviderUnresolvable, resolveErr)
	}
	model := configManager.GetModelForProvider(clientType)
	client, createErr := factory.CreateProviderClient(clientType, model)
	if createErr != nil {
		return nil, clientType, model, fmt.Errorf("%w: %w", errCompletionClientCreation, createErr)
	}
	return client, clientType, model, nil
}
