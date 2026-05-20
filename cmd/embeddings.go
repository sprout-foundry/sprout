//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/spf13/cobra"
)

var embeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage embedding index",
	Long: `Manage the embedding index used for semantic search and duplicate detection.

Subcommands:
  clear  Clear embedding index files`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var embeddingsClearType string

var embeddingsClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear embedding index files",
	Long: `Clear embedding index files from the index directory.

By default, clears all embedding types (code and conversation_turn/memory).
Use --type to specify which type to clear.

Types:
  code              Code file embeddings (index.jsonl and related)
  conversation_turn Conversation turn embeddings (conversation_turns.jsonl and related)
  memory            Memory embeddings (same files as conversation_turn)
  all               All embedding types (default)`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runEmbeddingsClear(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	embeddingsClearCmd.Flags().StringVar(&embeddingsClearType, "type", "all",
		"Type of embeddings to clear: code, conversation_turn, memory, or all (default: all)")

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
		configDir = os.Getenv("LEDIT_CONFIG")
	}
	if configDir == "" {
		configDir, err = configuration.GetConfigDir()
		if err != nil {
			return "", fmt.Errorf("get config directory: %w", err)
		}
	}
	return filepath.Join(configDir, "embeddings"), nil
}
