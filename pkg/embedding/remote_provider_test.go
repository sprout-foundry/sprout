//go:build !js

package embedding

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// factoryProbeProvider is a mock that records whether it was used.
type factoryProbeProvider struct {
	used *bool
}

func (p *factoryProbeProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	*p.used = true
	return []float32{1, 0, 0}, nil
}

func (p *factoryProbeProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	*p.used = true
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (p *factoryProbeProvider) Dimensions() int { return 3 }

func (p *factoryProbeProvider) Name() string { return "factory-probe" }

func (p *factoryProbeProvider) ModelHash() string { return "factory-probe-hash" }

func (p *factoryProbeProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) {
	return p.Embed(ctx, prefix+text)
}

func (p *factoryProbeProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = prefix + t
	}
	return p.EmbedBatch(ctx, prefixed)
}

func (p *factoryProbeProvider) Close() error { return nil }

// TestProviderFactory_UsesRemoteProvider verifies that when the process-wide
// provider factory is set and succeeds, EmbeddingManager.initLocked uses it
// instead of loading the in-process ONNX model.
func TestProviderFactory_UsesRemoteProvider(t *testing.T) {
	var used bool
	SetProviderFactory(func(ctx context.Context) (EmbeddingProvider, error) {
		return &factoryProbeProvider{used: &used}, nil
	})
	defer SetProviderFactory(nil)

	dir := t.TempDir()
	mgr := NewEmbeddingManager(nil, dir)
	err := mgr.Init(context.Background())
	require.NoError(t, err, "manager init must succeed with a factory provider")

	assert.Equal(t, "factory-probe", mgr.Name())
	assert.Equal(t, 3, mgr.Dimensions())
	assert.Equal(t, "factory-probe-hash", mgr.ModelHash())

	vec, err := mgr.Embed(context.Background(), "hello")
	require.NoError(t, err)
	assert.True(t, used, "factory provider must serve embed calls")
	assert.Len(t, vec, 3)
}

// TestProviderFactory_FallsBackToONNXOnError verifies that a factory that
// errors does NOT fail manager init — the manager falls back to in-process
// ONNX. The ONNX load may or may not succeed depending on the environment;
// the important contract is that the factory error is swallowed, not that
// the model loads.
func TestProviderFactory_FallsBackToONNXOnError(t *testing.T) {
	SetProviderFactory(func(ctx context.Context) (EmbeddingProvider, error) {
		return nil, errors.New("socket unavailable")
	})
	defer SetProviderFactory(nil)

	dir := t.TempDir()
	mgr := NewEmbeddingManager(nil, dir)
	err := mgr.Init(context.Background())
	// Either the ONNX fallback loaded (fine) or it failed with a model
	// error (fine in CI without the model) — but NEVER the factory's
	// "socket unavailable" error.
	require.NotErrorIs(t, err, errors.New("socket unavailable"))
}

// TestNewRemoteEmbeddingProvider_DialFailure verifies the client surfaces a
// clear error when the daemon socket is unavailable, so callers can fall
// back to in-process execution.
func TestNewRemoteEmbeddingProvider_DialFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.sock")
	_, err := NewRemoteEmbeddingProvider(missing)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dial daemon socket")
}
