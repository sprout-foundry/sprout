//go:build !js

package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// semantic_search over an unbuilt index used to fall through to the normal
// formatter, which says "No results found matching %q ... try broadening your
// search query". That is a claim about the codebase produced by searching
// nothing, and an agent reasonably reads it as evidence the code is absent.
func TestSemanticSearchRefusesToAnswerFromAnEmptyIndex(t *testing.T) {
	mgr := embedding.NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: t.TempDir()}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	store, err := embedding.NewHNSWStore(filepath.Join(t.TempDir(), "i.hnsw"), "hash")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	provider := &gateTestProvider{dims: 8}
	mgr.SetForTesting(provider, store, embedding.NewIndexManager(provider, store, embedding.IndexOptions{}))

	h := &semanticSearchHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{EmbeddingMgr: mgr, WorkspaceRoot: t.TempDir()},
		map[string]any{"query": "how are payments reconciled"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := res.Output
	if strings.Contains(out, "No results found") {
		t.Errorf("empty index produced a no-results verdict about the codebase:\n%s", out)
	}
	if !strings.Contains(out, "not a statement about the codebase") {
		t.Errorf("output does not tell the caller that nothing was searched:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "index") {
		t.Errorf("output does not name the index as the reason:\n%s", out)
	}
}

// gateTestProvider is a minimal EmbeddingProvider so the handler can be driven
// against a real-but-empty index without loading ONNX.
type gateTestProvider struct{ dims int }

func (p *gateTestProvider) Embed(context.Context, string) ([]float32, error) {
	return make([]float32, p.dims), nil
}
func (p *gateTestProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, p.dims)
	}
	return out, nil
}
func (p *gateTestProvider) EmbedWithPrefix(ctx context.Context, text, _ string) ([]float32, error) {
	return p.Embed(ctx, text)
}
func (p *gateTestProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, _ string) ([][]float32, error) {
	return p.EmbedBatch(ctx, texts)
}
func (p *gateTestProvider) Dimensions() int   { return p.dims }
func (p *gateTestProvider) Name() string      { return "gate-test" }
func (p *gateTestProvider) ModelHash() string { return "hash" }
func (p *gateTestProvider) Close() error      { return nil }
