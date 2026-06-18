//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// embedMgr is the singleton EmbeddingManager for the WASM process.
// We init it lazily on first use so the WASM module can start cheaply and
// only pay the index-load cost when the page actually calls into embedding.
//
// NOTE: After SP-045-3, the JS-facing embedding functions live in the
// separate cmd/embedding-wasm module. This manager remains in the shell
// module because conversation_funcs.go uses it to embed conversation
// turns for future-context retrieval.
var (
	embedMgrOnce sync.Once
	embedMgr     *embedding.EmbeddingManager
	embedMgrErr  error
)

func getEmbeddingManager() (*embedding.EmbeddingManager, error) {
	embedMgrOnce.Do(func() {
		cwd, err := os.Getwd()
		if err != nil {
			embedMgrErr = err
			return
		}
		cfg := &configuration.EmbeddingIndexConfig{
			Enabled:             true,
			AutoIndex:           false,
			SimilarityThreshold: 0.5,
			MaxResults:          10,
		}
		embedMgr = embedding.NewEmbeddingManager(cfg, cwd)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := embedMgr.Init(ctx); err != nil {
			embedMgrErr = err
		}
	})
	return embedMgr, embedMgrErr
}

// ─── Memory disk helpers ────────────────────────────────────────

func memoryDir() string {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(configDir, "memories")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func saveMemoryToDisk(name, content string) error {
	dir := memoryDir()
	if dir == "" {
		return fmt.Errorf("memory directory unavailable")
	}
	if strings.ContainsAny(name, "/\\") || name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid memory name %q", name)
	}
	path := filepath.Join(dir, name+".md")
	return os.WriteFile(path, []byte(content), 0o644)
}

func deleteMemoryFromDisk(name string) error {
	dir := memoryDir()
	if dir == "" {
		return fmt.Errorf("memory directory unavailable")
	}
	if strings.ContainsAny(name, "/\\") || name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid memory name %q", name)
	}
	path := filepath.Join(dir, name+".md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
