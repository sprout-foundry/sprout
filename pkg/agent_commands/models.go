package commands

// models.go — the /models command: the ModelsCommand interface (Name,
// SafeDuringSteer, Description, Usage, Execute), the JSON payload, the
// autocomplete cache (cachedModelsForProvider, refreshModelCache, Complete),
// and the modelsJSONPayload type. The model-listing / selection layer lives
// in models_selection.go; the local-model download layer in
// models_download.go.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ModelsCommand implements the /model slash command
type ModelsCommand struct {
	outputSink
}

// Name returns the command name
func (m *ModelsCommand) Name() string {
	return "model"
}

// SafeDuringSteer returns true - /model is config for next turn only
func (m *ModelsCommand) SafeDuringSteer() bool {
	return true
}

// Description returns the command description
func (m *ModelsCommand) Description() string {
	return "List available models and select which model to use"
}

// Usage returns the detailed help text shown by `/help model`.
func (m *ModelsCommand) Usage() string {
	return strings.Join([]string{
		"/model              List all available models for the current provider.",
		"/model select       Interactive model picker (searchable).",
		"/model <model_id>   Set model directly by ID.",
		"",
		"Use /provider select to switch providers first.",
		"Alias: /m",
		"",
		"Flags:",
		"  --json   Output the model list as a JSON array",
	}, "\n")
}

// Execute runs the models command
func (m *ModelsCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// If no arguments, list available models
	if len(args) == 0 {
		return m.listModels(chatAgent)
	}

	// If arguments provided, handle model selection
	if len(args) == 1 {
		if args[0] == "select" {
			return m.selectModel(chatAgent)
		} else {
			// Direct model selection by ID
			return m.setModel(args[0], chatAgent)
		}
	}

	return errors.New("usage: /model [select|<model_id>]")
}

// modelsJSONPayload wraps the model list with provider context.
type modelsJSONPayload struct {
	Provider string          `json:"provider"`
	Current  string          `json:"current_model"`
	Models   []api.ModelInfo `json:"models"`
}

// ExecuteWithJSONOutput emits the available models for the current provider
// as a JSON array. This mirrors the no-args /model list path. The
// `select` and `<model_id>` subcommands are interactive/stateful and are
// not meaningfully representable as JSON, so they fall through to the
// text Execute.
func (m *ModelsCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	if chatAgent == nil {
		return WriteJSONToOutput(modelsJSONPayload{})
	}

	clientType := chatAgent.GetProviderType()
	models, err := api.GetModelsForProvider(clientType)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}
	if models == nil {
		models = []api.ModelInfo{}
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return WriteJSONToOutput(modelsJSONPayload{
		Provider: api.GetProviderName(clientType),
		Current:  chatAgent.GetModel(),
		Models:   models,
	})
}

// modelCompleteCache caches model lists per provider for autocomplete.
// The list is small (typically < 50 entries) and changes rarely, but
// fetching it can take up to 500ms on a cold model registry or longer
// on a live provider API call. Serving stale results while refreshing
// in the background keeps the input loop unblocked.
var (
	modelCompleteMu       sync.RWMutex
	modelCompleteCache    = map[string]modelCompleteEntry{}
	modelCompleteRefresh  = make(map[string]bool) // prevents duplicate background refreshes
	modelCompleteCacheTTL = 30 * time.Second
)

type modelCompleteEntry struct {
	models    []api.ModelInfo
	fetchedAt time.Time
}

// cachedModelsForProvider returns models for the provider, using a cached
// result when available. If the cache is stale it kicks off a background
// refresh but returns the stale data immediately (stale-while-revalidate),
// so the autocomplete dropdown is never blocked on network I/O. The first
// call (cold cache) must fetch synchronously.
func cachedModelsForProvider(clientType api.ClientType) []api.ModelInfo {
	key := string(clientType)

	modelCompleteMu.RLock()
	entry, ok := modelCompleteCache[key]
	modelCompleteMu.RUnlock()

	age := time.Since(entry.fetchedAt)
	if ok && age < modelCompleteCacheTTL {
		return entry.models
	}

	// Stale or missing. If we have stale data, return it immediately and
	// refresh in the background.
	if ok {
		modelCompleteMu.Lock()
		alreadyRefreshing := modelCompleteRefresh[key]
		if !alreadyRefreshing {
			modelCompleteRefresh[key] = true
		}
		modelCompleteMu.Unlock()

		if !alreadyRefreshing {
			go refreshModelCache(key, clientType)
		}
		return entry.models
	}

	// Cold cache — must fetch synchronously. This only happens once per
	// provider per session.
	refreshModelCache(key, clientType)

	modelCompleteMu.RLock()
	defer modelCompleteMu.RUnlock()
	return modelCompleteCache[key].models
}

func refreshModelCache(key string, clientType api.ClientType) {
	defer func() {
		modelCompleteMu.Lock()
		delete(modelCompleteRefresh, key)
		modelCompleteMu.Unlock()
	}()

	models, err := api.GetModelsForProvider(clientType)
	if err != nil || len(models) == 0 {
		return
	}

	modelCompleteMu.Lock()
	modelCompleteCache[key] = modelCompleteEntry{
		models:    models,
		fetchedAt: time.Now(),
	}
	modelCompleteMu.Unlock()
}

// Complete provides argument completions for /model. Suggests the
// "select" subcommand, and when a partial model name is typed, lists
// matching models from the current provider. Uses a stale-while-revalidate
// cache so the dropdown is never blocked on a network call after the first.
func (m *ModelsCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	if len(args) == 0 {
		return []string{"select"}
	}

	if chatAgent == nil {
		return nil
	}
	clientType := chatAgent.GetProviderType()
	models := cachedModelsForProvider(clientType)
	if len(models) == 0 {
		return nil
	}

	prefix := args[len(args)-1]
	var matches []string
	for _, model := range models {
		if prefix == "" || strings.HasPrefix(strings.ToLower(model.ID), strings.ToLower(prefix)) {
			matches = append(matches, model.ID)
		}
	}
	sort.Strings(matches)
	// Cap at a reasonable limit to avoid overwhelming the completion cycle.
	if len(matches) > 20 {
		matches = matches[:20]
	}
	return matches
}
