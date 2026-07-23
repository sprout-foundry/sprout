//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

var embeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage embedding index",
	Long: `Manage the embedding index used for semantic search and duplicate detection.

Subcommands:
  clear  Clear embedding index files`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var embeddingsClearType string
var embeddingsClearYes bool
var embeddingsClearDryRun bool

var embeddingsClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear embedding index files",
	Long: `Clear embedding index files from the index directory.

By default, clears all embedding types (code and conversation_turn/memory).
Use --type to specify which type to clear.
Use --yes to skip the confirmation prompt.
	Use --dry-run to see what would be cleared without deleting anything.

Types:
  code              Code file embeddings (index.jsonl and related)
  conversation_turn Conversation turn embeddings (conversation_turns.jsonl and related)
  memory            Memory embeddings (same files as conversation_turn)
  all               All embedding types (default)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEmbeddingsClear()
	},
}

func init() {
	embeddingsClearCmd.Flags().StringVar(&embeddingsClearType, "type", "all",
		"Type of embeddings to clear: code, conversation_turn, memory, or all (default: all)")
	embeddingsClearCmd.Flags().BoolVarP(&embeddingsClearYes, "yes", "y", false, "Skip confirmation prompt")
	embeddingsClearCmd.Flags().BoolVar(&embeddingsClearDryRun, "dry-run", false,
		"Show what would be cleared without deleting anything")

	embeddingsCmd.AddCommand(embeddingsClearCmd)
	rootCmd.AddCommand(embeddingsCmd)
}

func runEmbeddingsClear() error {
	// Validate --type flag
	switch embeddingsClearType {
	case "code", "conversation_turn", "memory", "all":
	default:
		return fmt.Errorf("invalid --type %q: valid options are code, conversation_turn, memory, all", embeddingsClearType)
	}

	// Resolve the embedding index directory
	indexDir, err := resolveEmbeddingIndexDir()
	if err != nil {
		return fmt.Errorf("resolve embedding index directory: %w", err)
	}

	// Check if the directory exists
	if _, err := os.Stat(indexDir); os.IsNotExist(err) {
		fmt.Println("No embedding index found, nothing to clear.")
		return nil
	} else if err != nil {
		return fmt.Errorf("stat embedding index directory: %w", err)
	}

	// Ask for confirmation unless --yes or --dry-run
	if !embeddingsClearYes && !embeddingsClearDryRun {
		if !StdinIsTerminal() {
			return fmt.Errorf("this command requires confirmation. Pass --yes to skip confirmation or run interactively")
		}
		if !ConfirmPrompt("This will clear embedding files of type " + embeddingsClearType + " from " + indexDir + ". Continue") {
			return fmt.Errorf("aborted by user")
		}
	}

	// Handle --dry-run
	if embeddingsClearDryRun {
		return runEmbeddingsDryRun(indexDir)
	}

	// Clear the files
	count, err := embedding.ClearEmbeddingFiles(indexDir, embeddingsClearType)
	if err != nil {
		return err
	}

	if count == 0 {
		fmt.Println("No embedding files found, nothing to clear.")
	} else {
		fmt.Printf("Cleared %d embedding file(s) in %s\n", count, indexDir)
	}

	return nil
}

// runEmbeddingsDryRun counts files that would be deleted without actually deleting them.
func runEmbeddingsDryRun(indexDir string) error {
	files := listEmbeddingFiles(indexDir, embeddingsClearType)
	count := 0
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			count++
		}
	}

	if count == 0 {
		fmt.Println("No embedding files found, nothing to clear.")
	} else {
		fmt.Printf("Would clear %d embedding file(s) from %s\n", count, indexDir)
	}

	return nil
}

// listEmbeddingFiles returns the list of file paths for the given embedding type.
func listEmbeddingFiles(indexDir string, fileType string) []string {
	switch fileType {
	case "code":
		return []string{
			filepath.Join(indexDir, "index.hnsw"),
			filepath.Join(indexDir, "index.hnsw.meta"),
			filepath.Join(indexDir, "index.hnsw.records.json"),
		}
	case "conversation_turn", "memory":
		return []string{
			filepath.Join(indexDir, "conversation_turns.hnsw"),
			filepath.Join(indexDir, "conversation_turns.hnsw.meta"),
			filepath.Join(indexDir, "conversation_turns.hnsw.records.json"),
		}
	case "all":
		return append(
			listEmbeddingFiles(indexDir, "code"),
			listEmbeddingFiles(indexDir, "conversation_turn")...,
		)
	default:
		return nil
	}
}

// resolveEmbeddingIndexDir determines the embedding index directory from config or defaults.
// Matches the resolution logic in manager.go:initLocked() for consistency.
func resolveEmbeddingIndexDir() (string, error) {
	// Try loading config for EmbeddingIndex.IndexDir
	cfg, err := configuration.Load()
	if err == nil && cfg.EmbeddingIndex != nil && cfg.EmbeddingIndex.IndexDir != "" {
		return cfg.EmbeddingIndex.IndexDir, nil
	}

	// Fall back to default, matching manager.go resolution order
	configDir := os.Getenv("SPROUT_CONFIG")
	if configDir == "" {
		configDir = os.Getenv("SPROUT_CONFIG")
	}
	if configDir == "" {
		configDir, err = configuration.GetConfigDir()
		if err != nil {
			return "", fmt.Errorf("get config directory: %w", err)
		}
	}
	return filepath.Join(configDir, "embeddings"), nil
}
