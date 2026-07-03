package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type embeddingIndexHandler struct{}

func (h *embeddingIndexHandler) Name() string {
	return "embedding_index"
}

func (h *embeddingIndexHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "embedding_index",
		Description: "Manage the embedding index for duplicate detection and semantic search. Use 'build' to create a full index, 'update' to incrementally update changed files, or 'status' to check index state.",
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Description: "Operation to perform: 'build' (full re-index), 'update' (incremental via git diff), or 'status' (check index state)", Required: true},
		},
		Required: []string{"operation"},
	}
}

func (h *embeddingIndexHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "operation")
	return err
}

func (h *embeddingIndexHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()

	var hadError bool

	// EventBus events are optional — best-effort side effect only, not a gate
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": hadError,
			})
		}()
	}

	// --- actual logic (always runs) ---

	operation, err := extractString(args, "operation")
	if err != nil {
		hadError = true
		return ToolResult{
			Output:  err.Error(),
			IsError: true,
		}, nil
	}

	// Get config
	var config *configuration.Config
	if env.ConfigManager != nil {
		config = env.ConfigManager.GetConfig()
	} else {
		manager, err := configuration.NewManager()
		if err != nil {
			hadError = true
			return ToolResult{
				Output:  fmt.Sprintf("Error getting configuration: %v", err),
				IsError: true,
			}, nil
		}
		config = manager.GetConfig()
	}

	workspaceRoot := env.WorkspaceRoot
	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	embeddingCfg := config.EmbeddingIndex
	if embeddingCfg == nil {
		embeddingCfg = &configuration.EmbeddingIndexConfig{}
	}

	switch operation {
	case "status":
		// Status is a directory walk; doesn't need an embedding manager.
		return h.handleStatus(embeddingCfg, workspaceRoot)
	case "build":
		mgr, ownsMgr := pickEmbeddingMgr(env, embeddingCfg, workspaceRoot)
		if ownsMgr {
			defer mgr.Close()
		}
		return h.handleBuild(ctx, mgr)
	case "update":
		mgr, ownsMgr := pickEmbeddingMgr(env, embeddingCfg, workspaceRoot)
		if ownsMgr {
			defer mgr.Close()
		}
		return h.handleUpdate(ctx, mgr)
	default:
		hadError = true
		return ToolResult{
			Output:  fmt.Sprintf("Unknown operation '%s'. Valid operations: build, update, status", operation),
			IsError: true,
		}, nil
	}
}

// pickEmbeddingMgr returns the agent-owned manager when available, otherwise
// constructs a transient one (the caller is responsible for closing it; the
// second return value is true when the caller owns the lifecycle).
func pickEmbeddingMgr(env ToolEnv, cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) (*embedding.EmbeddingManager, bool) {
	if env.EmbeddingMgr != nil {
		return env.EmbeddingMgr, false
	}
	return embedding.NewEmbeddingManager(cfg, workspaceRoot), true
}

func (h *embeddingIndexHandler) handleStatus(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) (ToolResult, error) {
	indexDir := cfg.IndexDir
	if indexDir == "" {
		configDir := os.Getenv("SPROUT_CONFIG")
		if configDir == "" {
			configDir = os.Getenv("LEDIT_CONFIG")
		}
		if configDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				home = ""
			}
			configDir = filepath.Join(home, ".config", "sprout")
		}
		indexDir = filepath.Join(configDir, "embeddings")
	}

	enabled := cfg.Enabled

	var sb strings.Builder
	sb.WriteString("Embedding Index Status:\n\n")
	sb.WriteString(fmt.Sprintf("  Enabled: %v\n", enabled))
	// Provider is always the bundled ONNX EmbeddingGemma-300M today —
	// the previously-configurable `provider` field was removed because no
	// code branched on it. If remote providers are ever added, restore the
	// config field and the per-provider routing in pkg/embedding/manager.go.
	sb.WriteString("  Provider: bundled\n")
	sb.WriteString(fmt.Sprintf("  Index Directory: %s\n", indexDir))

	info, err := os.Stat(indexDir)
	if err != nil {
		if os.IsNotExist(err) {
			sb.WriteString("  State: No index exists (run 'build' to create)\n")
		} else {
			sb.WriteString(fmt.Sprintf("  State: Error checking index: %v\n", err))
		}
	} else if info.IsDir() {
		files, readErr := os.ReadDir(indexDir)
		if readErr != nil {
			sb.WriteString(fmt.Sprintf("  State: Error reading index directory: %v\n", readErr))
		} else {
			sb.WriteString(fmt.Sprintf("  State: Index exists (%d file(s))\n", len(files)))
			sb.WriteString("  Files:\n")
			for _, f := range files {
				sb.WriteString(fmt.Sprintf("    - %s\n", f.Name()))
			}
		}
	}

	return ToolResult{
		Output:  sb.String(),
		IsError: false,
	}, nil
}

func (h *embeddingIndexHandler) handleBuild(ctx context.Context, mgr *embedding.EmbeddingManager) (ToolResult, error) {
	// Use a timeout for the build
	buildCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	stats, err := mgr.BuildIndex(buildCtx)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Error building embedding index: %v", err),
			IsError: true,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("Embedding index built successfully.\n\n")
	sb.WriteString(fmt.Sprintf("  Files processed: %d\n", stats.FilesProcessed))
	sb.WriteString(fmt.Sprintf("  Units extracted: %d\n", stats.UnitsExtracted))
	sb.WriteString(fmt.Sprintf("  Units embedded: %d\n", stats.UnitsEmbedded))
	sb.WriteString(fmt.Sprintf("  Duration: %s\n", stats.Duration))

	return ToolResult{
		Output:  sb.String(),
		IsError: false,
	}, nil
}

func (h *embeddingIndexHandler) handleUpdate(ctx context.Context, mgr *embedding.EmbeddingManager) (ToolResult, error) {
	// Use a timeout for the update
	updateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	stats, err := mgr.UpdateFromGitDiff(updateCtx)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Error updating embedding index: %v", err),
			IsError: true,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("Embedding index updated successfully.\n\n")
	sb.WriteString(fmt.Sprintf("  Files processed: %d\n", stats.FilesProcessed))
	sb.WriteString(fmt.Sprintf("  Units extracted: %d\n", stats.UnitsExtracted))
	sb.WriteString(fmt.Sprintf("  Units embedded: %d\n", stats.UnitsEmbedded))
	sb.WriteString(fmt.Sprintf("  Duration: %s\n", stats.Duration))

	return ToolResult{
		Output:  sb.String(),
		IsError: false,
	}, nil
}

func (h *embeddingIndexHandler) Aliases() []string         { return nil }
func (h *embeddingIndexHandler) Timeout() time.Duration    { return 0 }
func (h *embeddingIndexHandler) MaxResultSize() int        { return 0 }
func (h *embeddingIndexHandler) SafeForParallel() bool     { return false }
func (h *embeddingIndexHandler) Interactive() bool         { return false }
