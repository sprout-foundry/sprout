package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
	// --- actual logic (always runs) ---

	operation, err := extractString(args, "operation")
	if err != nil {
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
		return h.handleStatus(embeddingCfg, workspaceRoot, env.EmbeddingMgr)
	case "build":
		mgr, ownsMgr := pickEmbeddingMgr(env, embeddingCfg, workspaceRoot)
		if ownsMgr {
			defer mgr.Close()
		}
		return h.handleBuild(ctx, mgr, !ownsMgr)
	case "update":
		mgr, ownsMgr := pickEmbeddingMgr(env, embeddingCfg, workspaceRoot)
		if ownsMgr {
			defer mgr.Close()
		}
		return h.handleUpdate(ctx, mgr, !ownsMgr)
	default:
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

func (h *embeddingIndexHandler) handleStatus(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string, mgr *embedding.EmbeddingManager) (ToolResult, error) {
	indexDir := cfg.IndexDir
	if indexDir == "" {
		// SP-133: the index is regenerable data, so it lives under the data
		// root — the same place embedding.DefaultIndexDir writes it. Resolving
		// it off the config root here made this tool report on a different
		// (stale) index than the daemon actually builds.
		indexDir = embedding.DefaultIndexDir()
	}

	enabled := cfg.Enabled

	var sb strings.Builder
	sb.WriteString("Embedding Index Status:\n\n")
	sb.WriteString(fmt.Sprintf("  Enabled: %v\n", enabled))
	sb.WriteString("  Provider: bundled\n")
	sb.WriteString(fmt.Sprintf("  Index Directory: %s\n", indexDir))

	// Report build status from the agent-owned manager when available.
	if mgr != nil {
		if mgr.IsBuilding() {
			sb.WriteString("  Build State: building in progress\n")
		} else {
			sb.WriteString(fmt.Sprintf("  Build State: idle (%d records indexed)\n", mgr.IndexSize()))
		}
	}

	info, err := os.Stat(indexDir)
	if err != nil {
		if os.IsNotExist(err) {
			sb.WriteString("  State: No index exists (run 'build' to create)\n")
		} else {
			sb.WriteString(fmt.Sprintf("  State: Error checking index: %v\n", err))
		}
	} else if info.IsDir() {
		files, readErr := readDirCompat(indexDir)
		if readErr != nil {
			sb.WriteString(fmt.Sprintf("  State: Error reading index directory: %v\n", readErr))
		} else {
			sb.WriteString(fmt.Sprintf("  State: Index exists (%d file(s))\n", len(files)))
		}
	}

	return ToolResult{
		Output:  sb.String(),
		IsError: false,
	}, nil
}

// handleBuild builds the embedding index. When agentOwned is true (the manager
// is the persistent, agent-owned one), the build runs in a background goroutine
// and the tool returns immediately so the conversation is not blocked. When
// agentOwned is false (a transient manager with no agent context), the build
// runs synchronously.
func (h *embeddingIndexHandler) handleBuild(ctx context.Context, mgr *embedding.EmbeddingManager, agentOwned bool) (ToolResult, error) {
	if mgr.IsBuilding() {
		return ToolResult{
			Output:  "Embedding index build is already in progress. Use 'status' to check progress.",
			IsError: false,
		}, nil
	}

	if agentOwned {
		// Detach from the tool's context so the build survives the tool
		// timeout. BuildIndexBackground applies its own BuildTimeout (10min).
		mgr.BuildIndexBackground(context.Background())
		return ToolResult{
			Output:  "Embedding index build started in the background. Use 'status' to check progress.",
			IsError: false,
		}, nil
	}

	// Transient manager — run synchronously.
	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
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

	cgStats, cgErr := buildCodegraphIndex(buildCtx)
	sb.WriteString("\nCode Intelligence Graph:\n")
	if cgErr != nil {
		sb.WriteString(fmt.Sprintf("  %v\n", cgErr))
	} else {
		sb.WriteString(fmt.Sprintf("  %s\n", cgStats))
	}

	return ToolResult{
		Output:  sb.String(),
		IsError: false,
	}, nil
}

// backgroundBuildKey is a context key used to signal that the agent owns the
// embedding manager and the build should run in the background.
type backgroundBuildKey struct{}

func (h *embeddingIndexHandler) handleUpdate(ctx context.Context, mgr *embedding.EmbeddingManager, agentOwned bool) (ToolResult, error) {
	if mgr.IsBuilding() {
		return ToolResult{
			Output:  "Embedding index build/update is already in progress. Use 'status' to check progress.",
			IsError: false,
		}, nil
	}

	if agentOwned {
		mgr.UpdateFromGitDiffBackground(context.Background())
		return ToolResult{
			Output:  "Embedding index update started in the background. Use 'status' to check progress.",
			IsError: false,
		}, nil
	}

	// Transient manager — run synchronously.
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
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

	cgStats, cgErr := updateCodegraphIndex(updateCtx)
	sb.WriteString("\nCode Intelligence Graph:\n")
	if cgErr != nil {
		sb.WriteString(fmt.Sprintf("  %v\n", cgErr))
	} else {
		sb.WriteString(fmt.Sprintf("  %s\n", cgStats))
	}

	return ToolResult{
		Output:  sb.String(),
		IsError: false,
	}, nil
}

func (h *embeddingIndexHandler) Aliases() []string { return nil }
func (h *embeddingIndexHandler) Timeout() time.Duration {
	// Background builds return immediately; transient builds need up to 5min.
	return 5 * time.Minute
}
func (h *embeddingIndexHandler) MaxResultSize() int    { return 0 }
func (h *embeddingIndexHandler) SafeForParallel() bool { return false }
func (h *embeddingIndexHandler) Interactive() bool     { return false }

// --- codegraph helpers ---

// codegraphFileParser adapts ExtractCallsAndSymbols to the codegraph.FileParser
// signature, converting raw extraction results to qualified symbols and edges.
func codegraphFileParser(path string, content []byte) ([]codegraph.Symbol, []codegraph.Edge, error) {
	sw, err := ExtractCallsAndSymbols(path, content)
	if err != nil {
		return nil, nil, err
	}
	return sw.ToCodegraphSymbols(path)
}

// buildCodegraphIndex performs a full code intelligence graph build.
// Returns a human-readable stats string on success, or an error.
func buildCodegraphIndex(ctx context.Context) (string, error) {
	// The codegraph build can take much longer than the embedding build
	// because it parses each file, indexes symbols, then bulk-inserts edges.
	// Use a generous timeout independent of the embedding build's context.
	buildCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	store, err := codegraph.NewStore("")
	if err != nil {
		return "", agenterrors.NewAgent("embedding_index", "failed to open codegraph store", err)
	}
	defer store.Close()

	if err := store.IndexAll(buildCtx, codegraphFileParser); err != nil {
		return "", agenterrors.NewAgent("embedding_index", "indexing failed", err)
	}

	stats := store.Stats()
	return fmt.Sprintf("%d nodes, %d edges, %d files", stats.NodeCount, stats.EdgeCount, stats.FileCount), nil
}

// updateCodegraphIndex incrementally updates the code intelligence graph
// by re-indexing only files that have changed since the last build.
func updateCodegraphIndex(ctx context.Context) (string, error) {
	store, err := codegraph.NewStore("")
	if err != nil {
		return "", agenterrors.NewAgent("embedding_index", "failed to open codegraph store", err)
	}
	defer store.Close()

	if err := store.IndexChangedFiles(ctx, codegraphFileParser); err != nil {
		return "", agenterrors.NewAgent("embedding_index", "update failed", err)
	}

	stats := store.Stats()
	return fmt.Sprintf("%d nodes, %d edges, %d files", stats.NodeCount, stats.EdgeCount, stats.FileCount), nil
}
